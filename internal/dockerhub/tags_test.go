package dockerhub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestParseTagArches_LinuxOnlyByDefault(t *testing.T) {
	tr := tagResult{
		Name: "latest",
		Images: []imageInfo{
			{Architecture: "amd64", OS: "linux"},
			{Architecture: "arm64", Variant: "v8", OS: "linux"},
			{Architecture: "amd64", OS: "windows"},
		},
	}
	arches, archSet := parseTagArches(tr, false)
	// Default: linux only, variant appended.
	want := []string{"amd64", "arm64/v8"}
	if !reflect.DeepEqual(arches, want) {
		t.Errorf("arches = %v, want %v", arches, want)
	}
	// archSet holds linux architecture fields (no variant) for filtering.
	if !archSet["amd64"] || !archSet["arm64"] {
		t.Errorf("archSet = %v, want amd64+arm64", archSet)
	}
}

func TestParseTagArches_WithOSShowsNonLinux(t *testing.T) {
	tr := tagResult{
		Name: "latest",
		Images: []imageInfo{
			{Architecture: "amd64", OS: "linux"},
			{Architecture: "amd64", OS: "windows"},
		},
	}
	arches, _ := parseTagArches(tr, true)
	// With OS: linux stays plain, windows gets os/ prefix.
	want := []string{"amd64", "windows/amd64"}
	if !reflect.DeepEqual(arches, want) {
		t.Errorf("arches = %v, want %v", arches, want)
	}
}

func TestParseTagArches_SkipsUnknown(t *testing.T) {
	tr := tagResult{
		Name: "latest",
		Images: []imageInfo{
			{Architecture: "unknown", OS: ""},
			{Architecture: "amd64", OS: "linux"},
		},
	}
	arches, _ := parseTagArches(tr, false)
	if len(arches) != 1 || arches[0] != "amd64" {
		t.Errorf("arches = %v, want [amd64]", arches)
	}
}

func TestParseTagArches_EmptyOSDefaultsLinux(t *testing.T) {
	tr := tagResult{
		Name: "latest",
		Images: []imageInfo{
			{Architecture: "amd64", OS: ""},
		},
	}
	arches, archSet := parseTagArches(tr, false)
	if len(arches) != 1 || arches[0] != "amd64" {
		t.Errorf("arches = %v, want [amd64]", arches)
	}
	if !archSet["amd64"] {
		t.Errorf("empty os should count as linux for archSet")
	}
}

func TestFetchTags_FollowsNextCursor(t *testing.T) {
	page := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		var pr paginatedTags
		if page == 1 {
			pr = paginatedTags{
				Next:    srv.URL + "/v2/repositories/library/nginx/tags/?page=2",
				Results: []tagResult{{Name: "v1", TagStatus: "active", LastUpdated: "2026-01-01T00:00:00Z"}},
			}
		} else {
			pr = paginatedTags{
				Next:    "",
				Results: []tagResult{{Name: "v2", TagStatus: "active", LastUpdated: "2026-02-01T00:00:00Z"}},
			}
		}
		json.NewEncoder(w).Encode(pr)
	}))
	defer srv.Close()

	// limit=0 => fetch all pages. Reduce sleep to keep test fast.
	prevSleep := tagsPageSleep
	tagsPageSleep = 0
	defer func() { tagsPageSleep = prevSleep }()

	tags, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 0, false)
	if err != nil {
		t.Fatalf("FetchTags error: %v", err)
	}
	if len(tags) != 2 || tags[0].Name != "v1" || tags[1].Name != "v2" {
		t.Errorf("tags = %+v, want [v1 v2]", tags)
	}
}

func TestFetchTags_LimitStopsEarly(t *testing.T) {
	calls := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		pr := paginatedTags{
			Next: srv.URL + "/v2/repositories/library/nginx/tags/?page=2",
			Results: []tagResult{
				{Name: "a"}, {Name: "b"}, {Name: "c"},
			},
		}
		json.NewEncoder(w).Encode(pr)
	}))
	defer srv.Close()

	prevSleep := tagsPageSleep
	tagsPageSleep = 0
	defer func() { tagsPageSleep = prevSleep }()

	tags, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 3, false)
	if err != nil {
		t.Fatalf("FetchTags error: %v", err)
	}
	if len(tags) != 3 {
		t.Errorf("got %d tags, want 3", len(tags))
	}
	if calls != 1 {
		t.Errorf("got %d calls, want 1 (limit reached after first page)", calls)
	}
}

func TestFetchTags_404ReturnsNotFoundError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	_, err := FetchTags(srv.Client(), srv.URL, "nope", "nope", 10, false)
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

// --- 追加：fixture 多页、错误分支、归一化与 HasArch ---

func loadFixtureTags(t *testing.T, name string) paginatedTags {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var pr paginatedTags
	if err := json.Unmarshal(data, &pr); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return pr
}

// serveFixtureTags serves a named testdata tags fixture with Next cleared
// (single page, so the 500ms inter-page sleep never triggers).
func serveFixtureTags(t *testing.T, name string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pr := loadFixtureTags(t, name)
		pr.Next = ""
		json.NewEncoder(w).Encode(pr)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchTags_FixtureMultiPageInjectsNext(t *testing.T) {
	page := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		pr := loadFixtureTags(t, "tags_nginx_page2.json")
		if page == 1 {
			pr = loadFixtureTags(t, "tags_nginx_page1.json")
			pr.Next = srv.URL + "/v2/repositories/library/nginx/tags/?page=2"
		}
		json.NewEncoder(w).Encode(pr)
	}))
	defer srv.Close()

	prevSleep := tagsPageSleep
	tagsPageSleep = 0
	defer func() { tagsPageSleep = prevSleep }()

	tags, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 0, false)
	if err != nil {
		t.Fatalf("FetchTags error: %v", err)
	}
	if len(tags) != 12 {
		t.Errorf("got %d tags across pages, want 12", len(tags))
	}
	if tags[0].Name != "latest" || tags[10].Name != "1.25-alpine" || tags[11].Name != "1.25" {
		t.Errorf("page order broken: [0]=%s [10]=%s [11]=%s", tags[0].Name, tags[10].Name, tags[11].Name)
	}
}

func TestFetchTags_LimitBeyondOnePageFetchesSecond(t *testing.T) {
	page := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		pr := loadFixtureTags(t, "tags_nginx_page1.json") // 10 results
		if page == 1 {
			pr.Next = srv.URL + "/v2/repositories/library/nginx/tags/?page=2"
		}
		json.NewEncoder(w).Encode(pr)
	}))
	defer srv.Close()

	prevSleep := tagsPageSleep
	tagsPageSleep = 0
	defer func() { tagsPageSleep = prevSleep }()

	// limit 11: one page (10) is not enough, the second page is fetched.
	tags, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 11, false)
	if err != nil {
		t.Fatalf("FetchTags error: %v", err)
	}
	if len(tags) != 20 || page != 2 {
		t.Errorf("got %d tags in %d pages, want 20 in 2", len(tags), page)
	}
}

func TestFetchTags_FullSizeConvertedToMB(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(paginatedTags{
			Results: []tagResult{{Name: "mib", FullSize: 50 * 1024 * 1024}},
		})
	}))
	defer srv.Close()
	tags, err := FetchTags(srv.Client(), srv.URL, "library", "x", 0, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if tags[0].SizeMB != 50.0 {
		t.Errorf("SizeMB = %v, want 50.0", tags[0].SizeMB)
	}
}

func TestFetchTags_StatusAndLastUpdatedPassThrough(t *testing.T) {
	srv := serveFixtureTags(t, "tags_nginx_page1.json")
	tags, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 0, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if tags[0].Status != "active" {
		t.Errorf("status = %q", tags[0].Status)
	}
	if !strings.HasPrefix(tags[0].LastUpdated, "2026-08-01T08:00:00") {
		t.Errorf("last_updated = %q", tags[0].LastUpdated)
	}
	// alpine-slim carries the inactive status from the fixture.
	for _, tg := range tags {
		if tg.Name == "alpine-slim" && tg.Status != "inactive" {
			t.Errorf("alpine-slim status = %q, want inactive", tg.Status)
		}
	}
}

func TestFetchTags_HasArchFromFixture(t *testing.T) {
	srv := serveFixtureTags(t, "tags_nginx_page1.json")
	tags, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 0, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	byName := map[string]Tag{}
	for _, tg := range tags {
		byName[tg.Name] = tg
	}
	// latest: amd64 + arm64 + arm/v7 + 386 on linux; windows/amd64 excluded from set.
	for _, a := range []string{"amd64", "arm64", "arm", "386"} {
		if !byName["latest"].HasArch(a) {
			t.Errorf("latest should report arch %s", a)
		}
	}
	// alpine: 386 only.
	if byName["alpine"].HasArch("amd64") || !byName["alpine"].HasArch("386") {
		t.Error("alpine arch set should be exactly {386}")
	}
	// stable-alpine: arm64 only.
	if byName["stable-alpine"].HasArch("amd64") || !byName["stable-alpine"].HasArch("arm64") {
		t.Error("stable-alpine arch set should be exactly {arm64}")
	}
	// perl: windows-only => empty linux arch set by default.
	if byName["perl"].HasArch("amd64") {
		t.Error("windows-only tag must not report amd64 in linux arch set")
	}
	// Display list excludes windows arch by default.
	if strings.Contains(strings.Join(byName["latest"].Arches, ","), "windows") {
		t.Errorf("latest Arches = %v, must not include windows by default", byName["latest"].Arches)
	}
}

func TestFetchTags_ShowOSSurfacesWindowsArches(t *testing.T) {
	srv := serveFixtureTags(t, "tags_nginx_page1.json")
	tags, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 0, true)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	for _, tg := range tags {
		if tg.Name == "latest" {
			joined := strings.Join(tg.Arches, ",")
			if !strings.Contains(joined, "windows/amd64") {
				t.Errorf("latest with showOS = %v, want windows/amd64", tg.Arches)
			}
			if !tg.HasArch("amd64") {
				t.Error("showOS must still populate linux archSet for filtering")
			}
		}
		if tg.Name == "perl" {
			// windows-only tag surfaces with prefix and no bare linux arch.
			if len(tg.Arches) != 1 || tg.Arches[0] != "windows/amd64" {
				t.Errorf("perl with showOS = %v, want [windows/amd64]", tg.Arches)
			}
		}
	}
}

func TestFetchTags_ServerError500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := FetchTags(srv.Client(), srv.URL, "u", "i", 10, false)
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("err = %v, want HTTP 500 error", err)
	}
}

func TestFetchTags_InvalidJSONReturnsParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"count":1,"results":[bad`))
	}))
	defer srv.Close()
	_, err := FetchTags(srv.Client(), srv.URL, "u", "i", 10, false)
	if err == nil || !strings.Contains(err.Error(), "parsing") {
		t.Errorf("err = %v, want parsing error", err)
	}
}

func TestFetchTags_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	_, err := FetchTags(srv.Client(), url, "u", "i", 10, false)
	if err == nil || !strings.Contains(err.Error(), "tags request failed") {
		t.Errorf("err = %v, want transport error", err)
	}
}

func TestFetchTags_EmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(paginatedTags{Count: 0, Results: []tagResult{}})
	}))
	defer srv.Close()
	tags, err := FetchTags(srv.Client(), srv.URL, "u", "i", 0, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("got %d tags, want 0", len(tags))
	}
}

func TestHasArch_EmptySetIsFalse(t *testing.T) {
	tg := Tag{Name: "x"}
	if tg.HasArch("amd64") {
		t.Error("empty arch set must report false")
	}
}

func TestParseTagArches_VariantComposedAndSorted(t *testing.T) {
	tr := tagResult{Images: []imageInfo{
		{Architecture: "arm64", Variant: "v8", OS: "linux"},
		{Architecture: "amd64", OS: "linux"},
		{Architecture: "arm", Variant: "v7", OS: "linux"},
	}}
	arches, archSet := parseTagArches(tr, false)
	want := []string{"amd64", "arm/v7", "arm64/v8"}
	if !reflect.DeepEqual(arches, want) {
		t.Errorf("arches = %v, want %v (sorted, variant kept)", arches, want)
	}
	for _, a := range []string{"amd64", "arm", "arm64"} {
		if !archSet[a] {
			t.Errorf("archSet missing %s (variant must be stripped)", a)
		}
	}
}

func TestParseTagArches_EmptyImages(t *testing.T) {
	arches, archSet := parseTagArches(tagResult{}, false)
	if len(arches) != 0 || len(archSet) != 0 {
		t.Errorf("no images => empty lists, got %v %v", arches, archSet)
	}
}

// --- 追加：审查结论 1/2/3/6/7 的钉测试 ---

// noSleep 把页间 sleep 置 0 并在测试结束后恢复。
func noSleep(t *testing.T) {
	t.Helper()
	prev := tagsPageSleep
	tagsPageSleep = 0
	t.Cleanup(func() { tagsPageSleep = prev })
}

// TestFetchTags_RelativeNextCursorShouldResolveAgainstBase 【绿·钉修复】(审查结论 1a)
// 修复前：client 盲跟 pr.Next，next 为相对路径（"/v2/...?page=2"）时直接
// client.Get(相对URL)，报 `unsupported protocol scheme ""`。修复后 resolveNext
// 将相对 next 基于 base 解析为绝对 URL 再请求，第二页正常取回。
func TestFetchTags_RelativeNextCursorShouldResolveAgainstBase(t *testing.T) {
	noSleep(t)
	page2Hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			page2Hit = true
			json.NewEncoder(w).Encode(paginatedTags{Results: []tagResult{{Name: "v2"}}})
			return
		}
		json.NewEncoder(w).Encode(paginatedTags{
			Next:    "/v2/repositories/library/nginx/tags/?page=2", // 相对路径
			Results: []tagResult{{Name: "v1"}},
		})
	}))
	defer srv.Close()

	tags, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 0, false)
	if err != nil {
		t.Fatalf("relative next cursor should be resolved against base, got error: %v", err)
	}
	if len(tags) != 2 || tags[0].Name != "v1" || tags[1].Name != "v2" {
		t.Errorf("tags = %+v, want [v1 v2]", tags)
	}
	if !page2Hit {
		t.Error("second page was never requested")
	}
}

// TestFetchTags_NextCursorOutsideBaseMustBeRejected 【绿·钉修复】(审查结论 1b)
// 修复前：next 为指向外部 host 的绝对 URL 时，client 原样请求，第二页流量打到
// base 之外。修复后 resolveNext 拒绝越出配置 base 的游标（报错且不发起请求）。
func TestFetchTags_NextCursorOutsideBaseMustBeRejected(t *testing.T) {
	noSleep(t)
	externalHits := 0
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalHits++
		json.NewEncoder(w).Encode(paginatedTags{Results: []tagResult{{Name: "evil"}}})
	}))
	defer external.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(paginatedTags{
			Next:    external.URL + "/v2/repositories/library/nginx/tags/?page=2",
			Results: []tagResult{{Name: "v1"}},
		})
	}))
	defer srv.Close()

	_, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 0, false)
	if err == nil {
		t.Error("next cursor pointing outside the configured base must be rejected, got nil error")
	}
	if externalHits != 0 {
		t.Errorf("request leaked to host outside base: external hit %d time(s)", externalHits)
	}
}

// TestFetchTags_ImageNameWithHashMustBePathEscaped 【绿·钉修复】(审查结论 3)
// 修复前：user/image 用 fmt.Sprintf 直接拼进路径，未做 url.PathEscape。
// image="a#b" 时 "#" 后面全部变成 URL fragment，mock 只收到路径
// /v2/repositories/library/a，page_size 参数也随之丢失。修复后 '#' 以 %23 传输。
func TestFetchTags_ImageNameWithHashMustBePathEscaped(t *testing.T) {
	var gotPath, gotPageSize string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		gotPageSize = r.URL.Query().Get("page_size")
		json.NewEncoder(w).Encode(paginatedTags{Results: []tagResult{}})
	}))
	defer srv.Close()

	if _, err := FetchTags(srv.Client(), srv.URL, "library", "a#b", 10, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "/v2/repositories/library/a%23b/tags/"; gotPath != want {
		t.Errorf("path = %q, want %q (image name must be PathEscape-d)", gotPath, want)
	}
	if gotPageSize != "100" {
		t.Errorf("page_size = %q, want 100 (unescaped '#' turns the rest of the URL into a fragment)", gotPageSize)
	}
}

// TestFetchTags_HTTPErrorShouldIncludeURL 【绿·钉修复】(审查结论 6)
// 修复前：非 2xx 错误消息 "docker hub tags returned HTTP %d" 不含出错 URL，
// 定位不了是哪个 base/镜像源出错。修复后错误里带 URL（经 redactURL 脱敏，
// 密码不外泄，见 TestFetchTags_HTTPErrorRedactsUserinfoPassword）。
func TestFetchTags_HTTPErrorShouldIncludeURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	_, err := FetchTags(srv.Client(), srv.URL, "u", "i", 10, false)
	if err == nil {
		t.Fatal("expected error for 502, got nil")
	}
	if !strings.Contains(err.Error(), srv.URL) {
		t.Errorf("error %q should contain the failing URL %q", err, srv.URL)
	}
}

// TestFetchTags_LoopingNextMustBeDetected 【绿·钉现状】(审查结论 2)
// 分页环保护：next 恒指回同一页时，盲跟游标会无限循环（--all/limit=0 直接
// 挂死；limit=3 也只是碰巧提前退出）。现在维护已访问 URL 集合，next 指向
// 已请求过的 URL 即报错。本测试 mock 的 next 恒为 ...?page=1：首页
// （?page_size=100）请求 1 次，随后 next 指向的 ?page=1 请求 1 次（第 2 次），
// 其 next 再次指回 ?page=1 => 环检测报错，共 2 次请求（首页+重访被拒）。
func TestFetchTags_LoopingNextMustBeDetected(t *testing.T) {
	noSleep(t)
	calls := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(paginatedTags{
			Next:    srv.URL + "/v2/repositories/library/nginx/tags/?page=1", // 恒指回第 1 页
			Results: []tagResult{{Name: "latest"}},
		})
	}))
	defer srv.Close()

	_, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 3, false)
	if err == nil {
		t.Fatal("looping next cursor must be detected and rejected, got nil error")
	}
	if !strings.Contains(err.Error(), "loop") {
		t.Errorf("err = %v, want loop detection error", err)
	}
	if calls != 2 {
		t.Errorf("got %d requests, want 2 (first page + revisit rejected by loop detection)", calls)
	}
}

// TestFetchTags_NextNonEmptyButEmptyResultsContinues 【绿】(审查结论 7 边界)
// 第 1 页 results 为空但 next 非空：应继续跟游标取第 2 页，最终拿到第 2 页的
// tag，共 2 次请求，不报错、不死循环（第 2 页 next 为空终止）。
func TestFetchTags_NextNonEmptyButEmptyResultsContinues(t *testing.T) {
	noSleep(t)
	calls := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			json.NewEncoder(w).Encode(paginatedTags{
				Count:   1,
				Next:    srv.URL + "/v2/repositories/library/nginx/tags/?page=2",
				Results: []tagResult{},
			})
			return
		}
		json.NewEncoder(w).Encode(paginatedTags{Results: []tagResult{{Name: "v2"}}})
	}))
	defer srv.Close()

	tags, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 0, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(tags) != 1 || tags[0].Name != "v2" {
		t.Errorf("tags = %+v, want [v2]", tags)
	}
	if calls != 2 {
		t.Errorf("got %d requests, want 2", calls)
	}
}

// TestFetchTags_LimitExactPageSizeMultipleStopsAtBoundary 【绿】(审查结论 7 边界)
// limit 恰好等于页大小（100，page_size 的整数倍）：第 1 页收满 100 个后
// len(out)>=limit 即停，不再请求第 2 页（恰好卡满边界，不多取一页）。
func TestFetchTags_LimitExactPageSizeMultipleStopsAtBoundary(t *testing.T) {
	noSleep(t)
	calls := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		results := make([]tagResult, 100)
		for i := range results {
			results[i] = tagResult{Name: fmt.Sprintf("t%03d", i)}
		}
		json.NewEncoder(w).Encode(paginatedTags{
			Next:    srv.URL + "/v2/repositories/library/nginx/tags/?page=2",
			Results: results,
		})
	}))
	defer srv.Close()

	tags, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 100, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(tags) != 100 {
		t.Errorf("got %d tags, want exactly 100", len(tags))
	}
	if tags[99].Name != "t099" {
		t.Errorf("tags[99] = %q, want t099", tags[99].Name)
	}
	if calls != 1 {
		t.Errorf("got %d requests, want 1 (limit==page size must not fetch page 2)", calls)
	}
}

// TestFetchTags_CrossPageDuplicateTagNamesAppended 【绿·钉现状】(审查结论 7 边界)
// 跨页出现同名 tag（API 异常/镜像源脏数据）时直接 append，无去重：两页各一个
// "latest" => 结果 2 条同名。钉住现状；若后续加去重需更新本测试。
func TestFetchTags_CrossPageDuplicateTagNamesAppended(t *testing.T) {
	noSleep(t)
	calls := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		pr := paginatedTags{Results: []tagResult{{Name: "latest"}}}
		if calls == 1 {
			pr.Next = srv.URL + "/v2/repositories/library/nginx/tags/?page=2"
		}
		json.NewEncoder(w).Encode(pr)
	}))
	defer srv.Close()

	tags, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 0, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("got %d tags, want 2 (no dedup across pages)", len(tags))
	}
	if tags[0].Name != "latest" || tags[1].Name != "latest" {
		t.Errorf("tags = [%s %s], want [latest latest] (blind append)", tags[0].Name, tags[1].Name)
	}
}

// TestFetchTags_TruncatedBodyReturnsReadError 【绿】(审查结论 7 边界：unexpected EOF 路径)
// 响应头 Content-Length 大于实际 body：服务端提前断开，io.ReadAll 读到
// unexpected EOF，走 "reading tags response" 错误分支（而非解析错误）。
func TestFetchTags_TruncatedBodyReturnsReadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := []byte(`{"count":1,"next":"","results":[{"name":"v1"}]}`)
		// 声明的长度比实际写入多 64 字节，响应被截断。
		w.Header().Set("Content-Length", strconv.Itoa(len(body)+64))
		w.Write(body)
	}))
	defer srv.Close()

	_, err := FetchTags(srv.Client(), srv.URL, "u", "i", 10, false)
	if err == nil || !strings.Contains(err.Error(), "reading tags response") {
		t.Errorf("err = %v, want 'reading tags response' error (unexpected EOF from truncated body)", err)
	}
}

// --- 追加：第二轮安全审查（resolveNext 加固）的钉测试 ---

// hostOf 提取 httptest 服务器 URL 的 host（含端口），用于构造异 scheme /
// 协议相对 / 带 userinfo 的 next 游标。
func hostOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u.Host
}

// TestResolveNext_CursorMustMatchBaseSchemeHostUserinfo 【绿·钉修复】
// resolveNext 行为矩阵（单元级）：游标先对 base 解析为绝对 URL，再统一校验
// scheme/host/userinfo 与 base 完全一致。修复前协议相对 URL（//evil/...）因
// Scheme=="" 走 ResolveReference 分支被放行、http 游标对 https base 因只比
// Host 也被放行、userinfo 从不检查——三条均已堵死。
func TestResolveNext_CursorMustMatchBaseSchemeHostUserinfo(t *testing.T) {
	const base = "https://hub.example.com"
	cases := []struct {
		name string
		next string
		want string // 期望解析结果；"" 表示期望拒绝
	}{
		{"relative path resolves against base", "/v2/repositories/library/nginx/tags/?page=2", base + "/v2/repositories/library/nginx/tags/?page=2"},
		{"same-host same-scheme absolute passes", base + "/v2/x/?page=3", base + "/v2/x/?page=3"},
		{"protocol-relative foreign host rejected", "//evil.example.com/v2/x/?page=2", ""},
		{"scheme downgrade http rejected", "http://hub.example.com/v2/x/?page=2", ""},
		{"userinfo injected rejected", "https://evil@hub.example.com/v2/x/?page=2", ""},
		{"userinfo with password rejected", "https://u:p@hub.example.com/v2/x/?page=2", ""},
		{"foreign host rejected", "https://evil.example.com/v2/x/?page=2", ""},
		{"malformed control character rejected", "/v2/x\u0007y?next=1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveNext(base, tc.next)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("resolveNext(base, %q) = %q, want rejection error", tc.next, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveNext(base, %q) error: %v", tc.next, err)
			}
			if got != tc.want {
				t.Errorf("resolveNext(base, %q) = %q, want %q", tc.next, got, tc.want)
			}
		})
	}
}

// TestFetchTags_ProtocolRelativeNextCursorMustBeRejected 【绿·钉修复】
// 修复前：next="//evil.example.com/v2/x" 时 url.Parse 得 Scheme==""、Host!=""
// 走 ResolveReference 分支，返回 https://evil.example.com/... 且 err=nil——
// 恰好是 resolveNext 注释声称防御的恶意镜像场景。修复后 ResolveReference 的
// 结果同样过 scheme/host 校验，协议相对游标指向 base 之外即拒绝且零外泄。
func TestFetchTags_ProtocolRelativeNextCursorMustBeRejected(t *testing.T) {
	noSleep(t)
	externalHits := 0
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalHits++
		json.NewEncoder(w).Encode(paginatedTags{Results: []tagResult{{Name: "evil"}}})
	}))
	defer external.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(paginatedTags{
			Next:    "//" + hostOf(t, external.URL) + "/v2/repositories/library/nginx/tags/?page=2", // 协议相对 URL
			Results: []tagResult{{Name: "v1"}},
		})
	}))
	defer srv.Close()

	_, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 0, false)
	if err == nil {
		t.Fatal("protocol-relative next cursor pointing outside base must be rejected, got nil error")
	}
	if externalHits != 0 {
		t.Errorf("request leaked to host outside base: external hit %d time(s)", externalHits)
	}
}

// TestFetchTags_NextCursorSchemeMismatchMustBeRejected 【绿·钉修复】
// 修复前只比 Host 不比 Scheme：http://hub.docker.com/... 游标对 https base
// 放行（降级为明文请求）。修复后 scheme 必须与 base 一致，异 scheme 游标
// 在发起请求前即被拒绝。
func TestFetchTags_NextCursorSchemeMismatchMustBeRejected(t *testing.T) {
	noSleep(t)
	calls := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(paginatedTags{
			Next:    "https://" + hostOf(t, srv.URL) + "/v2/repositories/library/nginx/tags/?page=2", // base 是 http，游标是 https
			Results: []tagResult{{Name: "v1"}},
		})
	}))
	defer srv.Close()

	_, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 0, false)
	if err == nil || !strings.Contains(err.Error(), "refusing to follow") {
		t.Fatalf("err = %v, want scheme-mismatch rejection error", err)
	}
	if calls != 1 {
		t.Errorf("got %d requests, want 1 (downgraded cursor must be rejected before any request)", calls)
	}
}

// TestFetchTags_NextCursorWithUserinfoMustBeRejected 【绿·钉修复】
// 修复前 userinfo 从不校验：https://evil@hub.docker.com/... 游标放行。
// 修复后游标解析结果携带与 base 不同的 userinfo 即拒绝（本例 scheme 与 host
// 均与 base 一致，只有 userinfo 异，钉住 userinfo 规则本身）。
func TestFetchTags_NextCursorWithUserinfoMustBeRejected(t *testing.T) {
	noSleep(t)
	calls := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(paginatedTags{
			Next:    "http://evil@" + hostOf(t, srv.URL) + "/v2/repositories/library/nginx/tags/?page=2", // 同 scheme 同 host，仅多 userinfo
			Results: []tagResult{{Name: "v1"}},
		})
	}))
	defer srv.Close()

	_, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 0, false)
	if err == nil || !strings.Contains(err.Error(), "refusing to follow") {
		t.Fatalf("err = %v, want userinfo rejection error", err)
	}
	if calls != 1 {
		t.Errorf("got %d requests, want 1 (userinfo cursor must be rejected before any request)", calls)
	}
}

// TestFetchTags_PaginationExceedsMaxPages 【绿·钉修复】
// 修复前环保护只防"精确重复 URL"：镜像源每页返回唯一新游标时永不重复、
// 无限翻页且 visited 无界增长。修复后 maxPages（默认 1000，测试可改小）为
// 页数硬上限，超限报错并停止请求。本测试把 maxPages 压到 3。
func TestFetchTags_PaginationExceedsMaxPages(t *testing.T) {
	noSleep(t)
	prev := maxPages
	maxPages = 3
	t.Cleanup(func() { maxPages = prev })

	page := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		// 每页返回唯一的 next 游标，visited 环检测永远不会命中。
		json.NewEncoder(w).Encode(paginatedTags{
			Next:    fmt.Sprintf("%s/v2/repositories/library/nginx/tags/?page=%d", srv.URL, page+1),
			Results: []tagResult{{Name: fmt.Sprintf("v%d", page)}},
		})
	}))
	defer srv.Close()

	_, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 0, false)
	if err == nil || !strings.Contains(err.Error(), "tags pagination exceeded 3 pages") {
		t.Fatalf("err = %v, want 'tags pagination exceeded 3 pages' error", err)
	}
	if page != 3 {
		t.Errorf("got %d requests, want exactly 3 (cap reached, 4th page must not be fetched)", page)
	}
}

// TestFetchTags_MalformedNextCursorRejected 【绿·钉修复】
// next 游标含控制字符（JSON \u0007 转义携带，镜像源可控）时 url.Parse 报
// "invalid control character in URL"，resolveNext 返回解析错误且不请求。
func TestFetchTags_MalformedNextCursorRejected(t *testing.T) {
	noSleep(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(paginatedTags{
			Next:    "/v2/repositories/library/nginx/tags/?page=2\u0007", // \u0007 => BEL 控制字符
			Results: []tagResult{{Name: "v1"}},
		})
	}))
	defer srv.Close()

	_, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 0, false)
	if err == nil || !strings.Contains(err.Error(), "parsing tags next cursor") {
		t.Fatalf("err = %v, want 'parsing tags next cursor' error", err)
	}
	if calls != 1 {
		t.Errorf("got %d requests, want 1 (malformed cursor must fail before the second request)", calls)
	}
}

// TestFetchTags_HTTPErrorRedactsUserinfoPassword 【绿·钉修复】
// 修复前非 2xx 错误消息用 %s 原样打印 URL：base 含 userinfo（如
// DHS_API_BASE=https://user:pass@mirror）时密码直接泄漏到终端。修复后所有
// 打印 URL 的错误统一走 redactURL（u.Redacted()），密码被 xxxx 掩码。
func TestFetchTags_HTTPErrorRedactsUserinfoPassword(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	base := "http://mirror:supersecret@" + hostOf(t, srv.URL)
	_, err := FetchTags(srv.Client(), base, "u", "i", 10, false)
	if err == nil {
		t.Fatal("expected error for 502, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 502") {
		t.Errorf("err = %v, want HTTP 502 error", err)
	}
	if strings.Contains(err.Error(), "supersecret") {
		t.Errorf("error %q must not leak the URL password in plaintext", err)
	}
	if !strings.Contains(err.Error(), "mirror:xxxxx@") {
		t.Errorf("error %q should contain the redacted URL (mirror:xxxxx@...)", err)
	}
}
