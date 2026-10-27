package ui

import (
	"strings"
	"testing"
)

func TestRenderRepoTable_EmptyRowsHeaderOnly(t *testing.T) {
	out := RenderRepoTable(nil)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected header+separator (2 lines), got %d: %q", len(lines), out)
	}
	if lines[0] != "NAME   STARS   PULLS   OFFICIAL   DESCRIPTION" {
		t.Errorf("header = %q", lines[0])
	}
	if strings.TrimSpace(lines[1]) == "" {
		t.Error("separator line should contain dashes")
	}
}

func TestRenderTagTable_EmptyRowsHeaderOnly(t *testing.T) {
	out := RenderTagTable([]TagRow{})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], "Size(MB)") || !strings.Contains(lines[0], "Architecture") {
		t.Errorf("tag header = %q", lines[0])
	}
}

func TestRenderRepoTable_SeparatorMatchesWidestCell(t *testing.T) {
	out := RenderRepoTable([]RepoRow{
		{Name: "nginx", Stars: "1", Pulls: "2", Official: "y", Description: "a-very-long-description-cell"},
	})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// The separator under DESCRIPTION must be at least as wide as the cell.
	if len(lines[1]) < len("a-very-long-description-cell") {
		t.Errorf("separator too short: %q", lines[1])
	}
	if !strings.Contains(out, "a-very-long-description-cell") {
		t.Error("wide cell missing from output")
	}
}

// TestRenderRepoTable_AlignsColumns（自 ui_test.go 合并）
func TestRenderRepoTable_AlignsColumns(t *testing.T) {
	repos := []RepoRow{
		{Name: "nginx", Stars: "20000", Pulls: "10000000", Official: "yes", Description: "Official build"},
		{Name: "bitnami/nginx", Stars: "300", Pulls: "50000000", Official: "no", Description: "Bitnami"},
	}
	out := RenderRepoTable(repos)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("expected >=4 lines (header+sep+2 rows), got %d", len(lines))
	}
	if !strings.Contains(lines[0], "NAME") || !strings.Contains(lines[0], "STARS") {
		t.Errorf("header missing columns: %q", lines[0])
	}
	// Columns aligned: each data row's NAME field starts at the same column
	// as the header's NAME (which is 0). lines[2] is the first data row ("nginx").
	nameIdx := strings.Index(lines[0], "NAME")
	for i, row := range []string{"nginx", "bitnami/nginx"} {
		line := lines[2+i]
		if strings.Index(line, row) != nameIdx {
			t.Errorf("NAME column not aligned on data row %d; header idx=%d, row=%q", i, nameIdx, line)
		}
	}
}

func TestRenderTagTable_CellAlignment(t *testing.T) {
	rows := []TagRow{
		{Tag: "a", Status: "active", Size: "1.0", Arch: "amd64", Updated: "2026-01-01"},
		{Tag: "very-long-tag-name", Status: "inactive", Size: "100.5", Arch: "amd64, arm64/v8, 386", Updated: "2026-02-02"},
	}
	out := RenderTagTable(rows)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// Every data row must start its Tag cell at column 0 and have Status at
	// the same offset as the header's Status column.
	statusIdx := strings.Index(lines[0], "Status")
	for _, row := range rows {
		found := false
		for _, l := range lines {
			if strings.HasPrefix(l, row.Tag) {
				found = true
				if strings.Index(l, row.Status) != statusIdx {
					t.Errorf("Status column misaligned for %q: %q (want Status at %d)", row.Tag, l, statusIdx)
				}
			}
		}
		if !found {
			t.Errorf("row for tag %q missing", row.Tag)
		}
	}
}

// TestRenderTagTable_AlignsColumns（自 ui_test.go 合并）
func TestRenderTagTable_AlignsColumns(t *testing.T) {
	tags := []TagRow{
		{Tag: "latest", Status: "active", Size: "192.5", Arch: "amd64, arm64/v8", Updated: "2026-07-15"},
		{Tag: "1.27-alpine", Status: "active", Size: "45.2", Arch: "amd64, arm64/v8", Updated: "2026-07-10"},
	}
	out := RenderTagTable(tags)
	if !strings.Contains(out, "Tag") || !strings.Contains(out, "Architecture") {
		t.Errorf("missing headers: %q", out)
	}
}

func TestRenderRepoTable_CJKCellDoesNotPanic(t *testing.T) {
	// CJK runes are 3 bytes; padding uses byte length (misaligned but stable).
	// The contract here is only: no panic, content present.
	out := RenderRepoTable([]RepoRow{
		{Name: "镜像", Stars: "1", Pulls: "2", Official: "y", Description: "官方构建"},
	})
	if !strings.Contains(out, "镜像") || !strings.Contains(out, "官方构建") {
		t.Errorf("CJK cells missing: %q", out)
	}
}

// TestRenderTable_AnsiCellWidthMatchesBare 【绿·钉修复：审查 P2#2】
// 修复前 renderTable 用 len() 计算列宽，把 ANSI 转义字节也计入：
// "\x1b[31m✗\x1b[0m" 是 12 字节、裸 "✗" 是 3 字节，但可见宽度都是 1 列，
// 同一逻辑内容在彩色/裸两种模式下 "V" 列起始偏移、分隔线宽度都不同。
// 修复后（宽度计量走 stripANSI）两种模式可见偏移一致。
// 【度量方式注记】输出契约是保留原始带色字符串（writeRow 原样写出单元格），
// 转义字节物理存在于输出中，因此裸 strings.Index 的字节偏移在彩色/裸两种
// 输出间天然相差转义字节数（本例 9），与列宽无关。故本测试在比较偏移前先
// stripANSI 剥离转义序列，度量 "V 列的可见起始偏移"——这正是本测试要钉的
// 契约（ANSI 字节不计入列宽）。修复后剥离 ANSI 的两份输出逐字节一致。
func TestRenderTable_AnsiCellWidthMatchesBare(t *testing.T) {
	colored := "\x1b[31m✗\x1b[0m" // 等价于启用彩色时的 ui.Red("✗")
	bare := "✗"

	coloredOut := renderTable([]string{"S", "V"}, [][]string{{colored, "x"}})
	bareOut := renderTable([]string{"S", "V"}, [][]string{{bare, "x"}})

	// 带色单元格原样出现在输出中（输出契约：不丢色）。
	if !strings.Contains(coloredOut, colored) {
		t.Errorf("输出应保留原始 ANSI 颜色码, got %q", coloredOut)
	}
	// V 列可见起始偏移：剥离 ANSI 后度量，彩色/裸必须一致。
	if ci, bi := strings.Index(stripANSI(coloredOut), "x"), strings.Index(stripANSI(bareOut), "x"); ci != bi {
		t.Errorf("同一单元格彩色/裸文本下 V 列可见起始偏移不同: colored=%d, bare=%d；ANSI 字节不应计入列宽", ci, bi)
	}
	cSep := strings.Split(coloredOut, "\n")[1]
	bSep := strings.Split(bareOut, "\n")[1]
	if cSep != bSep {
		t.Errorf("分隔线宽度不同: colored=%q, bare=%q", cSep, bSep)
	}
}

// TestRenderTable_EmptyCellsArePadded 【绿·钉住现状】
// 空字符串单元格按 0 宽参与对齐：仅补空格，不 panic，内容不丢失。
func TestRenderTable_EmptyCellsArePadded(t *testing.T) {
	out := renderTable([]string{"A", "B"}, [][]string{{"", "x"}, {"y", ""}})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d: %q", len(lines), out)
	}
	// 行1: 首列为空 → 1 个补齐空格 + 3 列间距后是 "x"。
	if lines[2] != "    x" {
		t.Errorf("空首列行 = %q, want %q", lines[2], "    x")
	}
	// 行2: 末列为空 → "y" + 3 列间距 + 1 个补齐空格。
	if lines[3] != "y    " {
		t.Errorf("空末列行 = %q, want %q", lines[3], "y    ")
	}
}

// TestRenderTable_LastColumnPadsWithTrailingSpaces 【绿·钉住现状】
// 当前 writeRow 对包括最后一列在内的所有列做补齐：末列单元格短于列宽时，
// 行尾会留下悬挂空格（本例 "LAST" 列宽 4，"b" 后挂 3 个空格）。
// 这是当前实际行为契约；若未来改为末列不补齐（消除行尾空格），需同步更新本测试。
func TestRenderTable_LastColumnPadsWithTrailingSpaces(t *testing.T) {
	out := renderTable([]string{"H", "LAST"}, [][]string{{"a", "b"}})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if lines[2] != "a   b   " {
		t.Errorf("末列行尾应保留补齐空格, got %q, want %q", lines[2], "a   b   ")
	}
}

// TestRenderTable_RowLongerThanHeadersPanics_CurrentBehavior 【绿·钉住现状：审查 P3】
// writeRow 遍历整行单元格但 widths 只有 len(headers) 项：
// 数据行列数超过表头列数时会 index out of range panic。
// 钉住当前行为；修复 renderTable（截断或扩宽）后应把本测试改为断言不 panic。
func TestRenderTable_RowLongerThanHeadersPanics_CurrentBehavior(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("当前实现对列数超过表头的行会 index out of range panic, 应捕获到 panic")
		}
	}()
	renderTable([]string{"A"}, [][]string{{"1", "2"}})
}

func TestFormatPullCommands_EmptySpecsNil(t *testing.T) {
	if cmds := FormatPullCommands("library", "nginx", nil); len(cmds) != 0 {
		t.Errorf("nil specs should give no commands, got %v", cmds)
	}
}

func TestFormatPullCommands_SortedByTag(t *testing.T) {
	cmds := FormatPullCommands("library", "nginx", []PullSpec{
		{Tag: "1.27", Arches: map[string]bool{"amd64": true}},
		{Tag: "1.25", Arches: map[string]bool{"amd64": true}},
		{Tag: "latest", Arches: map[string]bool{"amd64": true}},
	})
	want := []string{"nginx:1.25", "nginx:1.27", "nginx:latest"}
	if len(cmds) != len(want) {
		t.Fatalf("cmds = %v, want %v", cmds, want)
	}
	for i := range want {
		if cmds[i] != want[i] {
			t.Errorf("cmds[%d] = %q, want %q (must be sorted by tag)", i, cmds[i], want[i])
		}
	}
}

func TestFormatPullCommands_Amd64BeforeArm64WithinTag(t *testing.T) {
	cmds := FormatPullCommands("library", "nginx", []PullSpec{
		{Tag: "t", Arches: map[string]bool{"arm64": true, "amd64": true}},
	})
	want := []string{"nginx:t", "--platform=linux/arm64 nginx:t"}
	if len(cmds) != 2 || cmds[0] != want[0] || cmds[1] != want[1] {
		t.Errorf("cmds = %v, want %v (amd64 first)", cmds, want)
	}
}

func TestFormatPullCommands_OtherArchesProduceNothing(t *testing.T) {
	cmds := FormatPullCommands("library", "nginx", []PullSpec{
		{Tag: "a", Arches: map[string]bool{"386": true}},
		{Tag: "b", Arches: map[string]bool{"riscv64": true, "ppc64le": true}},
		{Tag: "c", Arches: map[string]bool{}},
	})
	if len(cmds) != 0 {
		t.Errorf("only amd64/arm64 map to commands, got %v", cmds)
	}
}

// TestFormatPullCommands_NilArchesMapIsSafe 【绿·钉住现状】
// Arches 为 nil map：索引 nil map 安全地返回 false，对应 spec 产出 0 条命令，不 panic。
func TestFormatPullCommands_NilArchesMapIsSafe(t *testing.T) {
	cmds := FormatPullCommands("library", "nginx", []PullSpec{
		{Tag: "v1", Arches: nil},
		{Tag: "v2", Arches: map[string]bool{"amd64": true}},
	})
	want := []string{"nginx:v2"}
	if len(cmds) != len(want) || cmds[0] != want[0] {
		t.Errorf("cmds = %v, want %v (nil Arches 产出 0 条)", cmds, want)
	}
}

// TestFormatPullCommands_DuplicateTagsKeepInputOrder 【绿·钉修复：审查 P3（sort.Slice 非稳定）】
// 输入 13 个 spec，tag 在 "a"/"b" 间交替，组内第偶数个出现的是 amd64、
// 第奇数个是 arm64，因此每个 spec 产出可区分的命令（"nginx:a" vs
// "--platform=linux/arm64 nginx:a"）。稳定排序应保持同名 tag 内的输入顺序；
// 修复前用非稳定 sort.Slice，在本输入上（Go 1.26 pdqsort）a 组顺序被重排为
// 9,1,3,5,7,11，命令序列随之错位。换用 sort.SliceStable 后转绿。
func TestFormatPullCommands_DuplicateTagsKeepInputOrder(t *testing.T) {
	specs := make([]PullSpec, 0, 13)
	for i := 0; i < 13; i++ {
		tag := "b"
		if i%2 == 1 {
			tag = "a"
		}
		arch := "amd64"
		if (i/2)%2 == 1 {
			arch = "arm64"
		}
		specs = append(specs, PullSpec{Tag: tag, Arches: map[string]bool{arch: true}})
	}
	// 期望（稳定排序）：tag 升序分组，组内保持输入顺序。
	want := []string{
		// tag "a" 依次: amd64, arm64, amd64, arm64, amd64, arm64
		"nginx:a", "--platform=linux/arm64 nginx:a",
		"nginx:a", "--platform=linux/arm64 nginx:a",
		"nginx:a", "--platform=linux/arm64 nginx:a",
		// tag "b" 依次: amd64, arm64, amd64, arm64, amd64, arm64, amd64
		"nginx:b", "--platform=linux/arm64 nginx:b",
		"nginx:b", "--platform=linux/arm64 nginx:b",
		"nginx:b", "--platform=linux/arm64 nginx:b",
		"nginx:b",
	}
	cmds := FormatPullCommands("library", "nginx", specs)
	if len(cmds) != len(want) {
		t.Fatalf("cmds = %d 条 %v, want %d 条", len(cmds), cmds, len(want))
	}
	for i := range want {
		if cmds[i] != want[i] {
			t.Errorf("重复 tag 下命令顺序不稳定: cmds[%d] = %q, want %q (同名 tag 应保持输入顺序)", i, cmds[i], want[i])
		}
	}
}

// TestFormatPullCommands_AlreadySortedInputStaysSorted 【绿·钉住现状】
// 已按 tag 正序输入：输出保持同样顺序，条数不变（排序为 no-op）。
func TestFormatPullCommands_AlreadySortedInputStaysSorted(t *testing.T) {
	cmds := FormatPullCommands("library", "nginx", []PullSpec{
		{Tag: "1.25", Arches: map[string]bool{"amd64": true}},
		{Tag: "1.27", Arches: map[string]bool{"amd64": true}},
		{Tag: "latest", Arches: map[string]bool{"arm64": true}},
	})
	want := []string{"nginx:1.25", "nginx:1.27", "--platform=linux/arm64 nginx:latest"}
	if len(cmds) != len(want) {
		t.Fatalf("cmds = %v, want %v", cmds, want)
	}
	for i := range want {
		if cmds[i] != want[i] {
			t.Errorf("cmds[%d] = %q, want %q (正序输入不应被重排)", i, cmds[i], want[i])
		}
	}
}

// TestFormatPullCommands_EmptyUserYieldsSlashPrefixedName 【绿·钉住现状】
// user 为空串：现状无任何校验，直接拼接得到 "/nginx"（悬挂斜杠）。
// 钉住当前行为；若未来对空 user 做处理（如按 library 处理），更新本测试。
func TestFormatPullCommands_EmptyUserYieldsSlashPrefixedName(t *testing.T) {
	cmds := FormatPullCommands("", "nginx", []PullSpec{
		{Tag: "t", Arches: map[string]bool{"amd64": true}},
	})
	want := []string{"/nginx:t"}
	if len(cmds) != 1 || cmds[0] != want[0] {
		t.Errorf("cmds = %v, want %v (现状: 空 user 原样拼接 \"/nginx\")", cmds, want)
	}
}

// TestFormatPullCommands_UserWithSlashPassedThrough 【绿·钉住现状】
// user 含 "/"（如嵌套命名空间 "foo/bar"）：现状原样透传，拼出 "foo/bar/nginx"。
func TestFormatPullCommands_UserWithSlashPassedThrough(t *testing.T) {
	cmds := FormatPullCommands("foo/bar", "nginx", []PullSpec{
		{Tag: "t", Arches: map[string]bool{"arm64": true}},
	})
	want := []string{"--platform=linux/arm64 foo/bar/nginx:t"}
	if len(cmds) != 1 || cmds[0] != want[0] {
		t.Errorf("cmds = %v, want %v (现状: user 原样透传)", cmds, want)
	}
}

// TestFormatPullCommands_SortsCallerSliceInPlace_CurrentBehavior 【绿·钉住现状：审查 P3】
// FormatPullCommands 用 sort.Slice 原地排序，会重排调用方传入的 specs 切片（副作用）。
// 调用方若还需原始顺序，必须自行拷贝。钉住当前行为。
func TestFormatPullCommands_SortsCallerSliceInPlace_CurrentBehavior(t *testing.T) {
	specs := []PullSpec{
		{Tag: "v9", Arches: map[string]bool{"amd64": true}},
		{Tag: "v1", Arches: map[string]bool{"amd64": true}},
	}
	FormatPullCommands("library", "nginx", specs)
	if specs[0].Tag != "v1" {
		t.Errorf("现状应原地重排入参: specs[0].Tag = %q, want %q", specs[0].Tag, "v1")
	}
}

// TestFormatPullCommands_LibraryPrefixStripped（自 ui_test.go 合并）
func TestFormatPullCommands_LibraryPrefixStripped(t *testing.T) {
	cmds := FormatPullCommands("library", "nginx", []PullSpec{
		{Tag: "latest", Arches: map[string]bool{"amd64": true, "arm64": true}},
	})
	want := []string{"nginx:latest", "--platform=linux/arm64 nginx:latest"}
	if len(cmds) != len(want) {
		t.Fatalf("got %d cmds %v, want %d", len(cmds), cmds, len(want))
	}
	for i := range want {
		if cmds[i] != want[i] {
			t.Errorf("cmds[%d] = %q, want %q", i, cmds[i], want[i])
		}
	}
}

// TestFormatPullCommands_NonLibraryKeepsUser（自 ui_test.go 合并）
func TestFormatPullCommands_NonLibraryKeepsUser(t *testing.T) {
	cmds := FormatPullCommands("bitnami", "nginx", []PullSpec{
		{Tag: "latest", Arches: map[string]bool{"amd64": true}},
	})
	want := []string{"bitnami/nginx:latest"}
	if len(cmds) != 1 || cmds[0] != want[0] {
		t.Errorf("cmds = %v, want %v", cmds, want)
	}
}

// TestFormatPullCommands_Arm64Only（自 ui_test.go 合并）
func TestFormatPullCommands_Arm64Only(t *testing.T) {
	cmds := FormatPullCommands("library", "redis", []PullSpec{
		{Tag: "7", Arches: map[string]bool{"arm64": true}},
	})
	want := []string{"--platform=linux/arm64 redis:7"}
	if len(cmds) != 1 || cmds[0] != want[0] {
		t.Errorf("cmds = %v, want %v", cmds, want)
	}
}
