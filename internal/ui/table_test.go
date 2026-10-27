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
