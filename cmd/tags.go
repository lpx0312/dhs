package cmd

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/dockerhub"
	"dhs/internal/ui"
	"github.com/spf13/cobra"
)

var (
	tagsLimit   int
	tagsSort    string
	tagsOrder   string
	tagsAll     bool
	tagsOS      bool
	tagsFilter  string
	tagsArch    string
	tagsGenPull bool
	tagsOutput  string
)

var tagsCmd = &cobra.Command{
	Use:   "tags <image>",
	Short: "Query tags of a Docker Hub image",
	Long: `Query tags of a Docker Hub image.

Examples:
  dhs tags nginx
  dhs tags user/repo --sort size --order asc
  dhs tags nginx --arch amd64,arm64 --gen-pull
  dhs tags nginx --all -o tags.md`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		image := args[0]
		user, name := parseImageRef(image)

		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		applyColorSetting(cfg)

		proxy := cfg.EffectiveProxy(flagProxy, "")
		client, err := dockerhub.NewClient(proxy)
		if err != nil {
			return err
		}

		fmt.Println(ui.Cyan(fmt.Sprintf("获取 %s/%s 镜像标签...", user, name)))

		limit := 0 // 0 => all
		if !tagsAll {
			if tagsLimit <= 0 {
				tagsLimit = 10
			}
			limit = tagsLimit
		}
		tags, err := dockerhub.FetchTags(client, dockerhub.BaseURL(), user, name, limit, tagsOS)
		if err != nil {
			return errWithProxy(err, proxy)
		}

		archTargets := parseArchFlag(tagsArch)

		// Pipeline: filter by name -> filter by arch -> sort -> limit.
		if tagsFilter != "" {
			tags = filterByName(tags, tagsFilter)
		}
		if len(archTargets) > 0 {
			tags = filterByArch(tags, archTargets)
		}
		sortTags(tags, tagsSort, tagsOrder == "asc")
		displayTags := tags
		if !tagsAll && len(tags) > tagsLimit {
			displayTags = tags[:tagsLimit]
		}

		// Header.
		fmt.Printf("\n%s\n\n", ui.Green(fmt.Sprintf("镜像: %s/%s", user, name)))
		archNote := ""
		if len(archTargets) > 0 {
			archNote = fmt.Sprintf("（架构: %s）", strings.Join(archTargets, ","))
		}
		if tagsAll {
			fmt.Printf("%s%s\n", ui.Green(fmt.Sprintf("找到 %d 个标签", len(tags))), archNote)
		} else {
			// Cap the "first N" figure at the actual count so filtering
			// never advertises more rows than it will print.
			displayLimit := tagsLimit
			if len(tags) < displayLimit {
				displayLimit = len(tags)
			}
			fmt.Printf("%s%s\n", ui.Green(fmt.Sprintf("找到 %d 个标签，显示前 %d 个", len(tags), displayLimit)), archNote)
		}
		fmt.Println()

		// Table.
		fmt.Print(renderTagRows(displayTags))
		if !tagsAll && len(tags) > tagsLimit {
			fmt.Printf("\n%s\n", ui.Yellow(fmt.Sprintf("提示: 使用 --all 参数查看全部 %d 个标签", len(tags))))
		}

		// Pull commands.
		if tagsGenPull {
			if len(archTargets) == 0 {
				fmt.Printf("\n%s\n", ui.Yellow("--gen-pull 仅在指定 --arch 时生效，已忽略"))
			} else {
				specs := buildPullSpecs(displayTags, archTargets)
				cmds := ui.FormatPullCommands(user, name, specs)
				fmt.Printf("\n%s\n", ui.Yellow("=== Docker 镜像拉取命令 ==="))
				for _, c := range cmds {
					fmt.Println(c)
				}
				fmt.Printf("\n%s\n", ui.Green(fmt.Sprintf("共生成 %d 条命令", len(cmds))))
			}
		}

		// File export.
		if tagsOutput != "" {
			if err := exportTags(tagsOutput, user, name, displayTags, archTargets, tagsGenPull); err != nil {
				return fmt.Errorf("writing output file: %w", err)
			}
			fmt.Printf("\n%s\n", ui.Green(fmt.Sprintf("标签信息已保存到文件: %s", tagsOutput)))
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(tagsCmd)
	tagsCmd.Flags().IntVarP(&tagsLimit, "limit", "n", 10, "max number of tags to display")
	tagsCmd.Flags().StringVar(&tagsSort, "sort", "date", "sort by: name|size|date")
	tagsCmd.Flags().StringVar(&tagsOrder, "order", "desc", "sort order: asc|desc")
	tagsCmd.Flags().BoolVarP(&tagsAll, "all", "a", false, "show all tags (overrides --limit)")
	tagsCmd.Flags().BoolVar(&tagsOS, "os", false, "show non-linux architectures with OS prefix")
	tagsCmd.Flags().StringVar(&tagsFilter, "filter", "", "substring filter for tag name")
	tagsCmd.Flags().StringVar(&tagsArch, "arch", "", "comma-separated architectures to filter (e.g. amd64,arm64)")
	tagsCmd.Flags().BoolVar(&tagsGenPull, "gen-pull", false, "generate docker pull commands (use with --arch)")
	tagsCmd.Flags().StringVarP(&tagsOutput, "output", "o", "", "write results to a markdown file")
}

// parseImageRef splits "user/image" into (user, image). Without a slash,
// user defaults to "library" (official images).
func parseImageRef(ref string) (string, string) {
	if i := strings.Index(ref, "/"); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return "library", ref
}

// parseArchFlag turns "amd64,arm64" into ["amd64","arm64"]; empty => nil.
func parseArchFlag(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func filterByName(tags []dockerhub.Tag, sub string) []dockerhub.Tag {
	out := make([]dockerhub.Tag, 0, len(tags))
	for _, t := range tags {
		if strings.Contains(t.Name, sub) {
			out = append(out, t)
		}
	}
	return out
}

func filterByArch(tags []dockerhub.Tag, targets []string) []dockerhub.Tag {
	set := map[string]bool{}
	for _, a := range targets {
		set[a] = true
	}
	out := make([]dockerhub.Tag, 0, len(tags))
	for _, t := range tags {
		for a := range set {
			if t.HasArch(a) {
				out = append(out, t)
				break
			}
		}
	}
	return out
}

func sortTags(tags []dockerhub.Tag, by string, asc bool) {
	less := func(i, j int) bool {
		a, b := tags[i], tags[j]
		var lt, gt bool
		switch by {
		case "name":
			lt, gt = a.Name < b.Name, a.Name > b.Name
		case "size":
			lt, gt = a.SizeMB < b.SizeMB, a.SizeMB > b.SizeMB
		default: // date
			lt, gt = a.LastUpdated < b.LastUpdated, a.LastUpdated > b.LastUpdated
		}
		if asc {
			return lt
		}
		return gt
	}
	sort.SliceStable(tags, less)
}

func renderTagRows(tags []dockerhub.Tag) string {
	rows := make([]ui.TagRow, 0, len(tags))
	for _, t := range tags {
		rows = append(rows, ui.TagRow{
			Tag:     t.Name,
			Status:  t.Status,
			Size:    strconv.FormatFloat(t.SizeMB, 'f', 1, 64),
			Arch:    strings.Join(t.Arches, ", "),
			Updated: shortDate(t.LastUpdated),
		})
	}
	return ui.RenderTagTable(rows)
}

func buildPullSpecs(tags []dockerhub.Tag, targets []string) []ui.PullSpec {
	specs := make([]ui.PullSpec, 0, len(tags))
	for _, t := range tags {
		arches := map[string]bool{}
		for _, a := range targets {
			if t.HasArch(a) {
				arches[a] = true
			}
		}
		if len(arches) > 0 {
			specs = append(specs, ui.PullSpec{Tag: t.Name, Arches: arches})
		}
	}
	return specs
}

// exportTags writes a markdown summary of the displayed tags (and optional
// pull commands) to path.
func exportTags(path, user, name string, tags []dockerhub.Tag, archTargets []string, genPull bool) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Docker Hub 镜像标签信息\n\n")
	fmt.Fprintf(&b, "- 镜像: `%s/%s`\n", user, name)
	if len(archTargets) > 0 {
		fmt.Fprintf(&b, "- 架构过滤: %s\n", strings.Join(archTargets, ", "))
	}
	fmt.Fprintf(&b, "- 标签数: %d\n\n", len(tags))
	fmt.Fprintf(&b, "| Tag | Status | Size(MB) | Architecture | LastUpdated |\n")
	fmt.Fprintf(&b, "| --- | --- | --- | --- | --- |\n")
	for _, t := range tags {
		fmt.Fprintf(&b, "| %s | %s | %.1f | %s | %s |\n",
			t.Name, t.Status, t.SizeMB, strings.Join(t.Arches, ", "), shortDate(t.LastUpdated))
	}
	if genPull && len(archTargets) > 0 {
		specs := buildPullSpecs(tags, archTargets)
		cmds := ui.FormatPullCommands(user, name, specs)
		fmt.Fprintf(&b, "\n## Docker 镜像拉取命令\n\n")
		fmt.Fprintf(&b, "```\n")
		for _, c := range cmds {
			fmt.Fprintln(&b, c)
		}
		fmt.Fprintf(&b, "```\n")
	}
	return osWriteFile(path, []byte(b.String()), 0644)
}
