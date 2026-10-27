package e2e

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// searchBody / tagsBody mirror the shared fixtures in
// internal/dockerhub/testdata (kept inline here so the binary test is
// self-contained).
const searchBody = `{
  "count": 3, "next": "", "results": [
    {"repo_name":"nginx","star_count":20000,"pull_count":10000000000,"is_official":true,"short_description":"Official build of Nginx."},
    {"repo_name":"bitnami/nginx","star_count":300,"pull_count":500000000,"is_official":false,"short_description":"Bitnami container image for nginx"}
  ]}`

const tagsBody = `{
  "count": 3, "next": "", "results": [
    {"name":"latest","tag_status":"active","full_size":187600000,"last_updated":"2026-08-01T08:00:00Z",
     "images":[{"architecture":"amd64","os":"linux"},{"architecture":"arm64","variant":"v8","os":"linux"}]},
    {"name":"1.27-alpine","tag_status":"active","full_size":47185920,"last_updated":"2026-07-28T10:30:00Z",
     "images":[{"architecture":"amd64","os":"linux"},{"architecture":"arm64","variant":"v8","os":"linux"}]},
    {"name":"perl","tag_status":"active","full_size":90000000,"last_updated":"2026-07-01T00:00:00Z",
     "images":[{"architecture":"amd64","os":"windows"}]}
  ]}`

var binPath string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "dhs-e2e-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "mkdir:", err)
		os.Exit(1)
	}

	binPath = filepath.Join(tmp, "dhs-e2e")
	if ext := execExt(); ext != "" {
		binPath += ext
	}
	if out, err := exec.Command("go", "build", "-o", binPath, "..").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	// os.Exit skips defers, so the temp dir must be removed explicitly
	// before exiting or every `go test` run leaks a dhs-e2e-* directory.
	os.RemoveAll(tmp)
	os.Exit(code)
}

func execExt() string {
	if filepath.Separator == '\\' {
		return ".exe"
	}
	return ""
}

// run executes the built binary with a 30s watchdog. The child inherits
// DHS_API_BASE (t.Setenv) and gets proxies blanked so EffectiveProxy stays
// deterministic. NO_COLOR=1 keeps output assertions free of ANSI escapes.
func run(t *testing.T, args ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Env = append(os.Environ(), "HTTPS_PROXY=", "HTTP_PROXY=", "NO_COLOR=1")
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if ctx.Err() != nil {
		// Timed out and was killed: surface whatever was captured first.
		t.Fatalf("run %v timed out after 30s, captured output:\n%s", args, out.String())
	}
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run %v: %v\n%s", args, err, out.String())
		}
	}
	return out.String(), code
}

func newMockHub(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v2/search/repositories/"):
			w.Write([]byte(searchBody))
		case strings.HasPrefix(r.URL.Path, "/v2/repositories/library/nginx/tags/"):
			w.Write([]byte(tagsBody))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"not found"}`))
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("DHS_API_BASE", srv.URL)
}

func TestBinary_HelpExitsZero(t *testing.T) {
	out, code := run(t, "--help")
	if code != 0 {
		t.Errorf("--help exit=%d, want 0\n%s", code, out)
	}
	for _, want := range []string{"dhs is a CLI tool", "search", "tags", "config"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
}

func TestBinary_UnknownCommandExitsNonZero(t *testing.T) {
	out, code := run(t, "frobnicate")
	if code == 0 {
		t.Errorf("unknown command exit=%d, want non-zero\n%s", code, out)
	}
	if !strings.Contains(out, "unknown command") {
		t.Errorf("output should name the unknown command:\n%s", out)
	}
}

// TestBinary_UnknownCommandExitsExactlyOne pins the precise exit code: the
// root Execute() helper calls os.Exit(1) on any command error, so an unknown
// command must exit with exactly 1 (not just any non-zero value).
func TestBinary_UnknownCommandExitsExactlyOne(t *testing.T) {
	out, code := run(t, "definitely-not-a-command")
	if code != 1 {
		t.Errorf("unknown command exit=%d, want exactly 1\n%s", code, out)
	}
	if !strings.Contains(out, "unknown command") {
		t.Errorf("output should name the unknown command:\n%s", out)
	}
}

func TestBinary_ConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dhs", "config.yaml")

	out, code := run(t, "config", "set", "proxy", "http://192.168.0.225:7897", "--config", path)
	if code != 0 || !strings.Contains(out, "已写入") {
		t.Fatalf("set exit=%d\n%s", code, out)
	}

	out, code = run(t, "config", "get", "proxy", "--config", path)
	if code != 0 || strings.TrimSpace(out) != "http://192.168.0.225:7897" {
		t.Errorf("get exit=%d out=%q, want bare proxy value", code, out)
	}

	out, code = run(t, "config", "unset", "proxy", "--config", path)
	if code != 0 || !strings.Contains(out, "已清除") {
		t.Errorf("unset exit=%d\n%s", code, out)
	}

	out, code = run(t, "config", "get", "proxy", "--config", path)
	if code != 0 || !strings.Contains(out, "(未设置)") {
		t.Errorf("get after unset exit=%d out=%q", code, out)
	}
}

func TestBinary_InvalidConfigValueExitsNonZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	out, code := run(t, "config", "set", "proxy", "no-scheme", "--config", path)
	if code == 0 || !strings.Contains(out, "代理地址格式不正确") {
		t.Errorf("exit=%d, want validation failure\n%s", code, out)
	}
}

func TestBinary_SearchAgainstMock(t *testing.T) {
	newMockHub(t)
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	out, code := run(t, "search", "nginx", "--config", cfg)
	if code != 0 {
		t.Fatalf("search exit=%d\n%s", code, out)
	}
	for _, want := range []string{"搜索镜像: nginx", "bitnami/nginx", "找到 2 个结果（共 3）"} {
		if !strings.Contains(out, want) {
			t.Errorf("search output missing %q:\n%s", want, out)
		}
	}
}

func TestBinary_TagsArchGenPullAgainstMock(t *testing.T) {
	newMockHub(t)
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	out, code := run(t, "tags", "nginx", "--arch", "amd64,arm64", "--gen-pull", "--config", cfg)
	if code != 0 {
		t.Fatalf("tags exit=%d\n%s", code, out)
	}
	// perl is windows-only: filtered out of both the table and the commands.
	for _, want := range []string{
		"找到 2 个标签",
		"nginx:latest",
		"--platform=linux/arm64 nginx:1.27-alpine",
		"共生成 4 条命令",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tags output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "perl") {
		t.Errorf("windows-only tag must be filtered out:\n%s", out)
	}
}

func TestBinary_TagsExportFile(t *testing.T) {
	newMockHub(t)
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	md := filepath.Join(t.TempDir(), "out.md")
	out, code := run(t, "tags", "nginx", "-o", md, "--config", cfg)
	if code != 0 || !strings.Contains(out, "标签信息已保存到文件") {
		t.Fatalf("export exit=%d\n%s", code, out)
	}
	data, err := os.ReadFile(md)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	if !strings.Contains(string(data), "# Docker Hub 镜像标签信息") {
		t.Errorf("export content unexpected:\n%s", data)
	}
}

func TestBinary_TagsNotFoundExitsNonZero(t *testing.T) {
	newMockHub(t)
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	out, code := run(t, "tags", "no-such/image", "--config", cfg)
	if code == 0 || !strings.Contains(out, "not found") {
		t.Errorf("404 exit=%d, want non-zero with not-found message:\n%s", code, out)
	}
}
