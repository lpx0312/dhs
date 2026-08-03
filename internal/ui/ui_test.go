package ui

import (
	"strings"
	"testing"
)

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
	// Header present.
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

func TestFormatPullCommands_NonLibraryKeepsUser(t *testing.T) {
	cmds := FormatPullCommands("bitnami", "nginx", []PullSpec{
		{Tag: "latest", Arches: map[string]bool{"amd64": true}},
	})
	want := []string{"bitnami/nginx:latest"}
	if len(cmds) != 1 || cmds[0] != want[0] {
		t.Errorf("cmds = %v, want %v", cmds, want)
	}
}

func TestFormatPullCommands_Arm64Only(t *testing.T) {
	cmds := FormatPullCommands("library", "redis", []PullSpec{
		{Tag: "7", Arches: map[string]bool{"arm64": true}},
	})
	want := []string{"--platform=linux/arm64 redis:7"}
	if len(cmds) != 1 || cmds[0] != want[0] {
		t.Errorf("cmds = %v, want %v", cmds, want)
	}
}
