package dockerhub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchRepositories_SinglePageReturnsResults(t *testing.T) {
	var capturedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		resp := paginatedRepos{
			Count: 2,
			Results: []repoResult{
				{Name: "nginx", StarCount: 20000, PullCount: 10000000, IsOfficial: true, ShortDescription: "Official build of Nginx."},
				{Name: "bitnami/nginx", StarCount: 300, PullCount: 50000000, IsOfficial: false, ShortDescription: "Bitnami nginx image"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := srv.Client()
	repos, total, err := FetchRepositories(client, srv.URL, "nginx", 25)
	if err != nil {
		t.Fatalf("FetchRepositories error: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(repos) != 2 {
		t.Fatalf("got %d repos, want 2", len(repos))
	}
	if repos[0].Name != "nginx" || !repos[0].Official {
		t.Errorf("repos[0] = %+v", repos[0])
	}
	// Verify page_size query param set to limit.
	parsed, _ := url.Parse(capturedURL)
	if parsed.Query().Get("page_size") != "25" {
		t.Errorf("page_size = %q, want 25", parsed.Query().Get("page_size"))
	}
	if parsed.Query().Get("query") != "nginx" {
		t.Errorf("query = %q, want nginx", parsed.Query().Get("query"))
	}
}

func TestFetchRepositories_HTTPErrorReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	_, _, err := FetchRepositories(srv.Client(), srv.URL, "nginx", 25)
	if err == nil {
		t.Fatal("expected error for 429, got nil")
	}
}

// --- 追加：mock fixture、默认值与错误分支 ---

// serveFixture starts a server replying with the named testdata file (or
// raw when the body is inline JSON), recording the last request URL.
func serveFixture(t *testing.T, name, raw string, status int) (*httptest.Server, *string) {
	t.Helper()
	captured := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.String()
		if status != 0 {
			w.WriteHeader(status)
		}
		var body []byte
		if name != "" {
			body, _ = os.ReadFile(filepath.Join("testdata", name))
		} else {
			body = []byte(raw)
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &captured
}

func TestFetchRepositories_FromFixture(t *testing.T) {
	srv, _ := serveFixture(t, "search_nginx.json", "", 0)
	repos, total, err := FetchRepositories(srv.Client(), srv.URL, "nginx", 25)
	if err != nil {
		t.Fatalf("FetchRepositories error: %v", err)
	}
	if total != 3 || len(repos) != 3 {
		t.Fatalf("total=%d len=%d, want 3/3", total, len(repos))
	}
	if repos[0].Name != "nginx" || !repos[0].Official || repos[0].Stars != 20000 {
		t.Errorf("repos[0] = %+v", repos[0])
	}
	if repos[1].Name != "bitnami/nginx" || repos[1].Official {
		t.Errorf("repos[1] = %+v", repos[1])
	}
	if repos[2].Description != "Unprivileged NGINX image" {
		t.Errorf("repos[2] description = %q", repos[2].Description)
	}
}

func TestFetchRepositories_NonPositiveLimitDefaultsTo25(t *testing.T) {
	for _, limit := range []int{0, -5} {
		srv, captured := serveFixture(t, "search_nginx.json", "", 0)
		if _, _, err := FetchRepositories(srv.Client(), srv.URL, "nginx", limit); err != nil {
			t.Fatalf("limit=%d: %v", limit, err)
		}
		parsed, _ := url.Parse(*captured)
		if got := parsed.Query().Get("page_size"); got != "25" {
			t.Errorf("limit=%d: page_size = %q, want 25", limit, got)
		}
	}
}

func TestFetchRepositories_KeywordIsEscaped(t *testing.T) {
	srv, captured := serveFixture(t, "search_nginx.json", "", 0)
	if _, _, err := FetchRepositories(srv.Client(), srv.URL, "nginx alpine&co", 10); err != nil {
		t.Fatalf("error: %v", err)
	}
	parsed, _ := url.Parse(*captured)
	if got := parsed.Query().Get("query"); got != "nginx alpine&co" {
		t.Errorf("decoded query = %q, want original keyword", got)
	}
}

func TestFetchRepositories_EmptyResults(t *testing.T) {
	srv, _ := serveFixture(t, "", `{"count":0,"next":null,"results":[]}`, 0)
	repos, total, err := FetchRepositories(srv.Client(), srv.URL, "zzzznothing", 10)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if total != 0 || len(repos) != 0 {
		t.Errorf("total=%d repos=%v, want 0/empty", total, repos)
	}
}

func TestFetchRepositories_InvalidJSONReturnsParseError(t *testing.T) {
	srv, _ := serveFixture(t, "", `{"count": 1, "results": [not-json`, 0)
	_, _, err := FetchRepositories(srv.Client(), srv.URL, "nginx", 10)
	if err == nil || !strings.Contains(err.Error(), "parsing") {
		t.Errorf("err = %v, want parsing error", err)
	}
}

func TestFetchRepositories_ServerError500(t *testing.T) {
	srv, _ := serveFixture(t, "", "", http.StatusInternalServerError)
	_, _, err := FetchRepositories(srv.Client(), srv.URL, "nginx", 10)
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("err = %v, want HTTP 500 error", err)
	}
}

func TestFetchRepositories_TransportError(t *testing.T) {
	srv, _ := serveFixture(t, "", "", 0)
	url := srv.URL
	srv.Close() // force a connection failure on the next request
	_, _, err := FetchRepositories(srv.Client(), url, "nginx", 10)
	if err == nil || !strings.Contains(err.Error(), "search request failed") {
		t.Errorf("err = %v, want transport error", err)
	}
}

// --- 追加：审查结论 4/5/6/8 的钉测试 ---

// TestFetchRepositories_HTTPErrorShouldIncludeURL 【绿·钉修复】(审查结论 6)
// 修复前：非 2xx 时错误消息只有 "docker hub search returned HTTP %d"，不含出错
// 的 URL，排障时无法定位是哪个 base（尤其配合 DHS_API_BASE 镜像源）。修复后
// 错误里带上出错的 URL（经 redactURL 脱敏，见
// TestFetchRepositories_HTTPErrorRedactsUserinfoPassword）。
func TestFetchRepositories_HTTPErrorShouldIncludeURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	_, _, err := FetchRepositories(srv.Client(), srv.URL, "nginx", 10)
	if err == nil {
		t.Fatal("expected error for 502, got nil")
	}
	if !strings.Contains(err.Error(), srv.URL) {
		t.Errorf("error %q should contain the failing URL %q", err, srv.URL)
	}
}

// TestFetchRepositories_PageSizeLimit100PassedThrough 【绿·钉现状】(审查结论 4)
// FetchRepositories 对 page_size 只做透传，不做上限钳制（API 侧实测把 >100 的
// page_size 静默当成 100 处理；cmd 层的钳制不在本包测）。limit=100 是 API 实际
// 上限，这里钉住透传行为。
func TestFetchRepositories_PageSizeLimit100PassedThrough(t *testing.T) {
	srv, captured := serveFixture(t, "", `{"count":0,"results":[]}`, 0)
	if _, _, err := FetchRepositories(srv.Client(), srv.URL, "nginx", 100); err != nil {
		t.Fatalf("error: %v", err)
	}
	parsed, _ := url.Parse(*captured)
	if got := parsed.Query().Get("page_size"); got != "100" {
		t.Errorf("page_size = %q, want 100 (passed through verbatim)", got)
	}
}

// TestFetchRepositories_Large1MBBodyParses 【绿】(审查结论 5)
// io.ReadAll 无上限的另一半：约 1MB 的合法响应体应能正常读完并解析。
// （超过上限应报错的测试留待修复限流后再补。）
func TestFetchRepositories_Large1MBBodyParses(t *testing.T) {
	const size = 1 << 20
	raw, err := json.Marshal(paginatedRepos{
		Count: 1,
		Results: []repoResult{
			{Name: "big", ShortDescription: strings.Repeat("x", size)},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(raw) < size {
		t.Fatalf("body only %d bytes, want >= %d", len(raw), size)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(raw)
	}))
	defer srv.Close()

	repos, total, err := FetchRepositories(srv.Client(), srv.URL, "big", 10)
	if err != nil {
		t.Fatalf("FetchRepositories error: %v", err)
	}
	if total != 1 || len(repos) != 1 {
		t.Fatalf("total=%d len=%d, want 1/1", total, len(repos))
	}
	if len(repos[0].Description) != size {
		t.Errorf("description len = %d, want %d", len(repos[0].Description), size)
	}
}

// TestFetchRepositories_TrailingSlashBaseSendsDoubleSlashPath 【绿·钉现状】(审查结论 8 的下游后果)
// base 带尾斜杠时（模拟绕过 BaseURL() 直接传入的 base，如修复前的 DHS_API_BASE 路径），
// base + "/v2/..." 直接拼接，mock 实际收到 "//v2/..." 双斜杠路径。钉住当前
// 畸形行为；若后续在 BaseURL()/拼接处做规范化，此测试需同步更新。
func TestFetchRepositories_TrailingSlashBaseSendsDoubleSlashPath(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.EscapedPath()
		w.Write([]byte(`{"count":0,"results":[]}`))
	}))
	defer srv.Close()

	base := srv.URL + "/" // 尾斜杠，模拟未 Trim 的 BaseURL() 输出
	if _, _, err := FetchRepositories(srv.Client(), base, "nginx", 5); err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.HasPrefix(capturedPath, "//v2/search/repositories/") {
		t.Errorf("path = %q, want it to start with //v2/search/repositories/ (double slash from untrimmed base)", capturedPath)
	}
}

// --- 追加：第二轮安全审查（错误消息脱敏）的钉测试 ---

// TestFetchRepositories_HTTPErrorRedactsUserinfoPassword 【绿·钉修复】
// 修复前非 2xx 错误消息用 %s 原样打印 URL：base 含 userinfo（如
// DHS_API_BASE=https://user:pass@mirror）时密码直接泄漏到终端。修复后错误
// 里的 URL 统一走 redactURL（u.Redacted()），密码被 xxxx 掩码。
func TestFetchRepositories_HTTPErrorRedactsUserinfoPassword(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	srvURL, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	base := "http://mirror:topsecret@" + srvURL.Host
	_, _, err = FetchRepositories(srv.Client(), base, "nginx", 10)
	if err == nil {
		t.Fatal("expected error for 502, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 502") {
		t.Errorf("err = %v, want HTTP 502 error", err)
	}
	if strings.Contains(err.Error(), "topsecret") {
		t.Errorf("error %q must not leak the URL password in plaintext", err)
	}
	if !strings.Contains(err.Error(), "mirror:xxxxx@") {
		t.Errorf("error %q should contain the redacted URL (mirror:xxxxx@...)", err)
	}
}
