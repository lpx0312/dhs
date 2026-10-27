package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/ui"
)

// --- 基础设施：flag 复位、stdout 捕获、mock Docker Hub ---

// resetFlags restores every package-level flag variable to its definition
// default. Cobra flag values persist across Execute calls, so one test's
// --limit/--arch would otherwise leak into the next.
func resetFlags() {
	flagProxy, flagConfig, flagNoColor = "", "", false
	searchLimit = 25
	tagsLimit, tagsSort, tagsOrder = 10, "date", "desc"
	tagsAll, tagsOS, tagsFilter = false, false, ""
	tagsArch, tagsGenPull, tagsOutput = "", false, ""
}

// captureStdout swaps os.Stdout for a pipe, runs fn, and returns what it
// printed. The CLI prints via fmt.Print* directly, so SetOut is not enough.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()

	fn()

	w.Close()
	return <-done
}

// runCLI executes the cobra root command in-process with the given args and
// returns everything printed to stdout plus the command error.
func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	resetFlags()
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		resetFlags()
		ui.SetEnabled(false) // restore the non-TTY default
	})

	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	return out, execErr
}

// newTempConfig returns a fresh --config path inside a temp dir.
func newTempConfig(t *testing.T) string {
	return filepath.Join(t.TempDir(), "dhs", "config.yaml")
}

// fixture loads a shared mock payload from internal/dockerhub/testdata.
func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "internal", "dockerhub", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

// newMockHub serves a fake Docker Hub v2 API backed by the shared fixtures.
// Unknown repos 404 so error paths can be exercised too.
func newMockHub(t *testing.T) *httptest.Server {
	t.Helper()
	searchBody := fixture(t, "search_nginx.json")
	tagsBody := fixture(t, "tags_nginx_page1.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v2/search/repositories/"):
			w.Write([]byte(searchBody))
		case strings.HasPrefix(r.URL.Path, "/v2/repositories/library/nginx/tags/"):
			w.Write([]byte(tagsBody))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message": "not found"}`))
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("DHS_API_BASE", srv.URL)
	// Keep proxy resolution deterministic even if the dev shell exports proxies.
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("HTTP_PROXY", "")
	return srv
}

// --- config 子命令 e2e ---

func TestE2E_ConfigListFreshFile(t *testing.T) {
	path := newTempConfig(t)
	out, err := runCLI(t, "config", "list", "--config", path)
	if err != nil {
		t.Fatalf("err: %v\n%s", err, out)
	}
	for _, want := range []string{"配置文件:", "proxy      文件值: -", "no-color   文件值: false", "生效代理: -"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}
}

func TestE2E_ConfigSetGetUnsetRoundTrip(t *testing.T) {
	path := newTempConfig(t)

	out, err := runCLI(t, "config", "set", "proxy", "http://192.168.0.225:7897", "--config", path)
	if err != nil {
		t.Fatalf("set proxy: %v\n%s", err, out)
	}
	if !strings.Contains(out, "已写入") || !strings.Contains(out, "proxy = http://192.168.0.225:7897") {
		t.Errorf("set output:\n%s", out)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "http://192.168.0.225:7897") {
		t.Errorf("config file not written:\n%s", data)
	}

	out, err = runCLI(t, "config", "get", "proxy", "--config", path)
	if err != nil || !strings.Contains(out, "http://192.168.0.225:7897") {
		t.Errorf("get proxy = (%q, %v)", out, err)
	}

	if _, err := runCLI(t, "config", "set", "no-color", "true", "--config", path); err != nil {
		t.Fatalf("set no-color: %v", err)
	}
	out, err = runCLI(t, "config", "get", "no-color", "--config", path)
	if err != nil || !strings.Contains(out, "true") {
		t.Errorf("get no-color = (%q, %v)", out, err)
	}
	// Alias form also reads the same key.
	out, err = runCLI(t, "config", "get", "no_color", "--config", path)
	if err != nil || !strings.Contains(out, "true") {
		t.Errorf("get no_color alias = (%q, %v)", out, err)
	}

	// list now shows the configured proxy as effective.
	out, err = runCLI(t, "config", "list", "--config", path)
	if err != nil || !strings.Contains(out, "生效代理: http://192.168.0.225:7897") {
		t.Errorf("list after set = (%q, %v)", out, err)
	}

	out, err = runCLI(t, "config", "unset", "proxy", "--config", path)
	if err != nil || !strings.Contains(out, "已清除 proxy") {
		t.Errorf("unset = (%q, %v)", out, err)
	}
	out, err = runCLI(t, "config", "get", "proxy", "--config", path)
	if err != nil || !strings.Contains(out, "(未设置)") {
		t.Errorf("get after unset = (%q, %v)", out, err)
	}
}

func TestE2E_ConfigGetUnsetKeyShowsPlaceholder(t *testing.T) {
	out, err := runCLI(t, "config", "get", "proxy", "--config", newTempConfig(t))
	if err != nil || !strings.Contains(out, "(未设置)") {
		t.Errorf("fresh get proxy = (%q, %v), want (未设置)", out, err)
	}
}

func TestE2E_ConfigSetCreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deep", "nest", "config.yaml")
	out, err := runCLI(t, "config", "set", "proxy", "http://p:1", "--config", path)
	if err != nil {
		t.Fatalf("err: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("config file missing after set: %v", statErr)
	}
}

func TestE2E_ConfigSetInvalidValues(t *testing.T) {
	path := newTempConfig(t)
	cases := [][]string{
		{"config", "set", "proxy", "192.168.0.1:7897", "--config", path}, // no scheme
		{"config", "set", "proxy", "http://", "--config", path},          // no host
		{"config", "set", "proxy", "", "--config", path},                 // empty => suggest unset
		{"config", "set", "no-color", "maybe", "--config", path},
		{"config", "set", "bogus", "x", "--config", path},
		{"config", "get", "bogus", "--config", path},
		{"config", "unset", "bogus", "--config", path},
	}
	wantMsg := []string{
		"代理地址格式不正确", "代理地址格式不正确", "不能为空", "只接受 true/false",
		"未知的配置项", "未知的配置项", "未知的配置项",
	}
	for i, args := range cases {
		_, err := runCLI(t, args...)
		if err == nil {
			t.Errorf("case %d (%v): expected error", i, args)
			continue
		}
		if !strings.Contains(err.Error(), wantMsg[i]) {
			t.Errorf("case %d: err = %v, want it to contain %q", i, err, wantMsg[i])
		}
	}
}

func TestE2E_ConfigBadYAMLFileFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("proxy: [unclosed"), 0644)
	for _, args := range [][]string{
		{"config", "get", "proxy", "--config", path},
		{"config", "set", "proxy", "http://p:1", "--config", path},
		{"config", "list", "--config", path},
		{"config", "unset", "proxy", "--config", path},
	} {
		if _, err := runCLI(t, args...); err == nil {
			t.Errorf("%v: expected yaml error", args)
		}
	}
}

func TestE2E_ArgumentCountErrors(t *testing.T) {
	cases := [][]string{
		{"search"},
		{"tags"},
		{"config", "set", "proxy"},
		{"config", "get"},
		{"config", "list", "extra"},
		{"config", "unset"},
		{"frobnicate"},
	}
	for _, args := range cases {
		if _, err := runCLI(t, args...); err == nil {
			t.Errorf("%v: expected an argument/unknown-command error", args)
		}
	}
}

func TestE2E_RootHelpSucceeds(t *testing.T) {
	out, err := runCLI(t, "--help")
	if err != nil {
		t.Fatalf("help errored: %v", err)
	}
	for _, want := range []string{"dhs is a CLI tool", "search", "tags", "config"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
}

// --- search 命令 e2e ---

func TestE2E_SearchRendersTable(t *testing.T) {
	newMockHub(t)
	out, err := runCLI(t, "search", "nginx", "--config", newTempConfig(t))
	if err != nil {
		t.Fatalf("err: %v\n%s", err, out)
	}
	for _, want := range []string{
		"搜索镜像: nginx",
		"NAME", "STARS", "PULLS", "OFFICIAL",
		"\nnginx ", "bitnami/nginx", "nginxinc/nginx-unprivileged",
		"找到 3 个结果（共 3）",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("search output missing %q:\n%s", want, out)
		}
	}
}

func TestE2E_SearchLimitSendsPageSize(t *testing.T) {
	captured := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.RawQuery
		w.Write([]byte(fixture(t, "search_nginx.json")))
	}))
	defer srv.Close()
	t.Setenv("DHS_API_BASE", srv.URL)
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("HTTP_PROXY", "")

	out, err := runCLI(t, "search", "nginx", "--limit", "5", "--config", newTempConfig(t))
	if err != nil {
		t.Fatalf("err: %v\n%s", err, out)
	}
	if !strings.Contains(captured, "page_size=5") || !strings.Contains(captured, "query=nginx") {
		t.Errorf("request query = %q, want page_size=5 & query=nginx", captured)
	}
}

func TestE2E_SearchNonPositiveLimitDefaultsTo25(t *testing.T) {
	captured := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.RawQuery
		w.Write([]byte(fixture(t, "search_nginx.json")))
	}))
	defer srv.Close()
	t.Setenv("DHS_API_BASE", srv.URL)
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("HTTP_PROXY", "")

	out, err := runCLI(t, "search", "nginx", "--limit", "0", "--config", newTempConfig(t))
	if err != nil {
		t.Fatalf("err: %v\n%s", err, out)
	}
	if !strings.Contains(captured, "page_size=25") {
		t.Errorf("query = %q, want default page_size=25", captured)
	}
}

func TestE2E_SearchHTTPErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	t.Setenv("DHS_API_BASE", srv.URL)

	_, err := runCLI(t, "search", "nginx", "--config", newTempConfig(t))
	if err == nil || !strings.Contains(err.Error(), "HTTP 429") {
		t.Errorf("err = %v, want HTTP 429", err)
	}
}

func TestE2E_SearchUnreachableProxyIsAnnotated(t *testing.T) {
	// No DHS_API_BASE: with a dead proxy the request fails before leaving
	// the machine, and errWithProxy must name the proxy.
	out, err := runCLI(t, "search", "nginx", "--proxy", "http://127.0.0.1:1", "--config", newTempConfig(t))
	if err == nil {
		t.Fatalf("expected proxy dial failure, out:\n%s", out)
	}
	if !strings.Contains(err.Error(), "当前代理: http://127.0.0.1:1") {
		t.Errorf("err = %v, want proxy annotation", err)
	}
}

func TestE2E_SearchInvalidProxyFlagFailsFast(t *testing.T) {
	_, err := runCLI(t, "search", "nginx", "--proxy", "://bad", "--config", newTempConfig(t))
	if err == nil || !strings.Contains(err.Error(), "invalid proxy URL") {
		t.Errorf("err = %v, want invalid proxy URL", err)
	}
}

// --- tags 命令 e2e ---

func TestE2E_TagsRendersTable(t *testing.T) {
	newMockHub(t)
	out, err := runCLI(t, "tags", "nginx", "--config", newTempConfig(t))
	if err != nil {
		t.Fatalf("err: %v\n%s", err, out)
	}
	for _, want := range []string{
		"获取 library/nginx 镜像标签",
		"镜像: library/nginx",
		"找到 10 个标签，显示前 10 个",
		"\nlatest ", "\n1.27-alpine ", "\nalpine-slim ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tags output missing %q:\n%s", want, out)
		}
	}
	// Windows-only tag shows no arch column content by default.
	if strings.Contains(out, "windows/") {
		t.Errorf("default view must not show windows arches:\n%s", out)
	}
}

func TestE2E_TagsUserRepoPath(t *testing.T) {
	newMockHub(t)
	// user/repo must query /v2/repositories/bitnami/nginx/tags/ (404 in mock).
	_, err := runCLI(t, "tags", "bitnami/nginx", "--config", newTempConfig(t))
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v, want image not found (mock 404)", err)
	}
}

func TestE2E_TagsAllAndLimitHint(t *testing.T) {
	newMockHub(t)

	out, err := runCLI(t, "tags", "nginx", "--all", "--config", newTempConfig(t))
	if err != nil {
		t.Fatalf("err: %v\n%s", err, out)
	}
	// --all: plain count, no "first N" and no hint. ANSI codes wrap the
	// count text, so anchor on the text itself rather than line ends.
	if !strings.Contains(out, "找到 10 个标签") || strings.Contains(out, "显示前") {
		t.Errorf("--all output must say 找到 10 个标签 without 显示前:\n%s", out)
	}

	out, err = runCLI(t, "tags", "nginx", "--limit", "3", "--config", newTempConfig(t))
	if err != nil {
		t.Fatalf("err: %v\n%s", err, out)
	}
	for _, want := range []string{"找到 10 个标签，显示前 3 个", "使用 --all 参数查看全部 10 个标签"} {
		if !strings.Contains(out, want) {
			t.Errorf("limited view missing %q:\n%s", want, out)
		}
	}
}

func TestE2E_TagsZeroLimitDefaultsToTen(t *testing.T) {
	newMockHub(t)
	out, err := runCLI(t, "tags", "nginx", "--limit", "0", "--config", newTempConfig(t))
	if err != nil {
		t.Fatalf("err: %v\n%s", err, out)
	}
	if !strings.Contains(out, "显示前 10 个") {
		t.Errorf("--limit 0 should fall back to 10:\n%s", out)
	}
}

func TestE2E_TagsFilter(t *testing.T) {
	newMockHub(t)
	out, err := runCLI(t, "tags", "nginx", "--filter", "1.27", "--config", newTempConfig(t))
	if err != nil {
		t.Fatalf("err: %v\n%s", err, out)
	}
	for _, want := range []string{"找到 3 个标签，显示前 3 个", "\n1.27-alpine ", "\n1.27 ", "\n1.27-alpine-perl "} {
		if !strings.Contains(out, want) {
			t.Errorf("filtered view missing %q:\n%s", want, out)
		}
	}
	for _, absent := range []string{"\nlatest ", "\nstable-alpine "} {
		if strings.Contains(out, absent) {
			t.Errorf("filtered view must drop %q:\n%s", absent, out)
		}
	}
}

func TestE2E_TagsFilterNoMatch(t *testing.T) {
	newMockHub(t)
	out, err := runCLI(t, "tags", "nginx", "--filter", "zzz", "--config", newTempConfig(t))
	if err != nil {
		t.Fatalf("err: %v\n%s", err, out)
	}
	if !strings.Contains(out, "找到 0 个标签") {
		t.Errorf("no-match filter should still render an empty table:\n%s", out)
	}
}

func TestE2E_TagsArchFilterAndGenPull(t *testing.T) {
	newMockHub(t)
	out, err := runCLI(t, "tags", "nginx", "--arch", "amd64,arm64", "--gen-pull", "--config", newTempConfig(t))
	if err != nil {
		t.Fatalf("err: %v\n%s", err, out)
	}
	for _, want := range []string{
		"（架构: amd64,arm64）",
		"找到 8 个标签", // 10 - alpine(386) - perl(windows)
		"=== Docker 镜像拉取命令 ===",
		"nginx:latest",
		"--platform=linux/arm64 nginx:1.27-alpine",
		"--platform=linux/arm64 nginx:stable-alpine",
		"共生成 14 条命令", // 7 amd64 + 7 arm64
	} {
		if !strings.Contains(out, want) {
			t.Errorf("arch+gen-pull view missing %q:\n%s", want, out)
		}
	}
	for _, absent := range []string{"\nperl ", "\nalpine "} {
		if strings.Contains(out, absent) {
			t.Errorf("arch filter must drop %q:\n%s", absent, out)
		}
	}
}

func TestE2E_TagsGenPullWithoutArchWarns(t *testing.T) {
	newMockHub(t)
	out, err := runCLI(t, "tags", "nginx", "--gen-pull", "--config", newTempConfig(t))
	if err != nil {
		t.Fatalf("err: %v\n%s", err, out)
	}
	if !strings.Contains(out, "仅在指定 --arch 时生效，已忽略") {
		t.Errorf("--gen-pull without --arch must warn:\n%s", out)
	}
}

func TestE2E_TagsArchWithoutPullCommands(t *testing.T) {
	newMockHub(t)
	out, err := runCLI(t, "tags", "nginx", "--arch", "386", "--gen-pull", "--config", newTempConfig(t))
	if err != nil {
		t.Fatalf("err: %v\n%s", err, out)
	}
	// latest (multi-arch) and alpine both carry 386 in the fixture.
	if !strings.Contains(out, "找到 2 个标签") || !strings.Contains(out, "\nalpine ") || !strings.Contains(out, "\nlatest ") {
		t.Errorf("386 filter should keep latest+alpine:\n%s", out)
	}
	if !strings.Contains(out, "共生成 0 条命令") {
		t.Errorf("386-only tags map to no pull commands:\n%s", out)
	}
}

func TestE2E_TagsSortNameAsc(t *testing.T) {
	newMockHub(t)
	out, err := runCLI(t, "tags", "nginx", "--sort", "name", "--order", "asc", "--config", newTempConfig(t))
	if err != nil {
		t.Fatalf("err: %v\n%s", err, out)
	}
	// Ascending name order: 1.26* < 1.27* < alpine < latest < perl < stable.
	order := []string{"\n1.26 ", "\n1.27 ", "\nalpine-slim ", "\nlatest ", "\nperl ", "\nstable-alpine "}
	last := -1
	for _, anchor := range order {
		i := strings.Index(out, anchor)
		if i < 0 {
			t.Fatalf("missing row %q in:\n%s", anchor, out)
		}
		if i < last {
			t.Errorf("row %q out of order (idx %d < %d):\n%s", anchor, i, last, out)
		}
		last = i
	}
}

func TestE2E_TagsSortSizeAsc(t *testing.T) {
	newMockHub(t)
	out, err := runCLI(t, "tags", "nginx", "--sort", "size", "--order", "asc", "--limit", "1", "--config", newTempConfig(t))
	if err != nil {
		t.Fatalf("err: %v\n%s", err, out)
	}
	if !strings.Contains(out, "\nalpine-slim ") {
		t.Errorf("smallest tag (alpine-slim) should be first:\n%s", out)
	}
}

func TestE2E_TagsDefaultSortIsDateDesc(t *testing.T) {
	newMockHub(t)
	out, err := runCLI(t, "tags", "nginx", "--config", newTempConfig(t))
	if err != nil {
		t.Fatalf("err: %v\n%s", err, out)
	}
	if strings.Index(out, "\nlatest ") > strings.Index(out, "\n1.27-alpine ") {
		t.Errorf("default date-desc should list latest (2026-08-01) first:\n%s", out)
	}
}

func TestE2E_TagsShowOS(t *testing.T) {
	newMockHub(t)
	out, err := runCLI(t, "tags", "nginx", "--os", "--config", newTempConfig(t))
	if err != nil {
		t.Fatalf("err: %v\n%s", err, out)
	}
	if !strings.Contains(out, "windows/amd64") {
		t.Errorf("--os must surface windows arches:\n%s", out)
	}
}

func TestE2E_TagsOutputFile(t *testing.T) {
	newMockHub(t)
	path := filepath.Join(t.TempDir(), "nginx-tags.md")
	out, err := runCLI(t, "tags", "nginx", "--arch", "amd64,arm64", "--gen-pull",
		"-o", path, "--config", newTempConfig(t))
	if err != nil {
		t.Fatalf("err: %v\n%s", err, out)
	}
	if !strings.Contains(out, "标签信息已保存到文件") {
		t.Errorf("export confirmation missing:\n%s", out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	md := string(data)
	for _, want := range []string{
		"# Docker Hub 镜像标签信息",
		"- 镜像: `library/nginx`",
		"- 架构过滤: amd64, arm64",
		"- 标签数: 8",
		"## Docker 镜像拉取命令",
		"nginx:latest",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("export file missing %q:\n%s", want, md)
		}
	}
}

func TestE2E_TagsOutputWriteErrorWrapped(t *testing.T) {
	newMockHub(t)
	old := osWriteFile
	osWriteFile = func(string, []byte, os.FileMode) error { return os.ErrPermission }
	t.Cleanup(func() { osWriteFile = old })

	_, err := runCLI(t, "tags", "nginx", "-o", "whatever.md", "--config", newTempConfig(t))
	if err == nil || !strings.Contains(err.Error(), "writing output file") {
		t.Errorf("err = %v, want writing output file wrapper", err)
	}
}

func TestE2E_TagsUnreachableProxyIsAnnotated(t *testing.T) {
	_, err := runCLI(t, "tags", "nginx", "--proxy", "http://127.0.0.1:1", "--config", newTempConfig(t))
	if err == nil || !strings.Contains(err.Error(), "当前代理: http://127.0.0.1:1") {
		t.Errorf("err = %v, want proxy annotation", err)
	}
}

func TestE2E_TagsInvalidProxyFlagFailsFast(t *testing.T) {
	_, err := runCLI(t, "tags", "nginx", "--proxy", "no-scheme", "--config", newTempConfig(t))
	if err == nil || !strings.Contains(err.Error(), "invalid proxy URL") {
		t.Errorf("err = %v, want invalid proxy URL", err)
	}
}

func TestE2E_TagsWithProxyConfigInFile(t *testing.T) {
	// A proxy stored in the config file is used (and its failure annotated),
	// proving the config -> client wiring end to end.
	path := newTempConfig(t)
	if out, err := runCLI(t, "config", "set", "proxy", "http://127.0.0.1:1", "--config", path); err != nil {
		t.Fatalf("set proxy: %v\n%s", err, out)
	}
	_, err := runCLI(t, "tags", "nginx", "--config", path)
	if err == nil || !strings.Contains(err.Error(), "当前代理: http://127.0.0.1:1") {
		t.Errorf("err = %v, want file-configured proxy to be used", err)
	}
}
