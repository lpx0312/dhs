package ui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ansiPattern matches SGR-style ANSI escape sequences (e.g. "\x1b[31m", "\x1b[0m").
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripANSI removes ANSI color escape sequences. The byte length of the result
// reflects only visible content, so it is used for column-width math while the
// original (possibly colored) string is still what gets written to the output.
func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

// --- Row structs (pre-formatted strings; the caller formats numbers) ---

// RepoRow is a pre-formatted row for the repo search table.
type RepoRow struct {
	Name        string
	Stars       string
	Pulls       string
	Official    string
	Description string
}

// TagRow is a pre-formatted row for the tag table.
type TagRow struct {
	Tag     string
	Status  string
	Size    string
	Arch    string
	Updated string
}

// PullSpec is one tag's matched arches, used to build pull commands.
type PullSpec struct {
	Tag    string
	Arches map[string]bool
}

// renderTable builds a column-aligned text table from headers + rows.
// All cells are already formatted strings.
func renderTable(headers []string, rows [][]string) string {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(stripANSI(h))
	}
	for _, r := range rows {
		for i := 0; i < len(headers) && i < len(r); i++ {
			if w := len(stripANSI(r[i])); w > widths[i] {
				widths[i] = w
			}
		}
	}
	var b strings.Builder
	writeRow := func(cells []string) {
		for i, c := range cells {
			b.WriteString(c)
			pad := widths[i] - len(stripANSI(c))
			if pad > 0 {
				b.WriteString(strings.Repeat(" ", pad))
			}
			if i < len(cells)-1 {
				b.WriteString("   ")
			}
		}
		b.WriteString("\n")
	}
	writeRow(headers)
	// Separator line.
	for i := range headers {
		b.WriteString(strings.Repeat("-", widths[i]))
		if i < len(headers)-1 {
			b.WriteString("   ")
		}
	}
	b.WriteString("\n")
	for _, r := range rows {
		writeRow(r)
	}
	return b.String()
}

// RenderRepoTable returns the aligned repo table text.
func RenderRepoTable(rows []RepoRow) string {
	headers := []string{"NAME", "STARS", "PULLS", "OFFICIAL", "DESCRIPTION"}
	cells := make([][]string, 0, len(rows))
	for _, r := range rows {
		cells = append(cells, []string{r.Name, r.Stars, r.Pulls, r.Official, r.Description})
	}
	return renderTable(headers, cells)
}

// RenderTagTable returns the aligned tag table text.
func RenderTagTable(rows []TagRow) string {
	headers := []string{"Tag", "Status", "Size(MB)", "Architecture", "LastUpdated"}
	cells := make([][]string, 0, len(rows))
	for _, r := range rows {
		cells = append(cells, []string{r.Tag, r.Status, r.Size, r.Arch, r.Updated})
	}
	return renderTable(headers, cells)
}

// FormatPullCommands builds docker pull command strings for given specs.
// user=="library" => image name omits the library/ prefix (matches ps1).
// amd64 => plain "<image>:<tag>"; arm64 => "--platform=linux/arm64 <image>:<tag>".
// Output is ordered by tag name then amd64-before-arm64; specs with equal tag
// names keep their input order (stable sort). The input slice is sorted in place.
func FormatPullCommands(user, image string, specs []PullSpec) []string {
	imageName := image
	if user != "library" {
		imageName = user + "/" + image
	}
	sort.SliceStable(specs, func(i, j int) bool { return specs[i].Tag < specs[j].Tag })
	var out []string
	for _, s := range specs {
		if s.Arches["amd64"] {
			out = append(out, fmt.Sprintf("%s:%s", imageName, s.Tag))
		}
		if s.Arches["arm64"] {
			out = append(out, fmt.Sprintf("--platform=linux/arm64 %s:%s", imageName, s.Tag))
		}
	}
	return out
}
