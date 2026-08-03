package cmd

import (
	"fmt"
	"strconv"

	"dhs/internal/dockerhub"
	"dhs/internal/ui"
	"github.com/spf13/cobra"
)

var searchLimit int

var searchCmd = &cobra.Command{
	Use:   "search <keyword>",
	Short: "Search Docker Hub repositories",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		keyword := args[0]
		if searchLimit <= 0 {
			searchLimit = 25
		}

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

		fmt.Println(ui.Cyan(fmt.Sprintf("搜索镜像: %s ...", keyword)))
		repos, total, err := dockerhub.FetchRepositories(client, dockerhub.BaseURL(), keyword, searchLimit)
		if err != nil {
			return errWithProxy(err, proxy)
		}

		rows := make([]ui.RepoRow, 0, len(repos))
		for _, r := range repos {
			official := ui.Red("✗")
			if r.Official {
				official = ui.Green("✓")
			}
			rows = append(rows, ui.RepoRow{
				Name:        r.Name,
				Stars:       strconv.FormatInt(r.Stars, 10),
				Pulls:       strconv.FormatInt(r.Pulls, 10),
				Official:    official,
				Description: truncate(r.Description, 40),
			})
		}

		fmt.Print(ui.RenderRepoTable(rows))
		fmt.Printf("\n%s\n", ui.Green(fmt.Sprintf("找到 %d 个结果（共 %d）", len(repos), total)))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(searchCmd)
	searchCmd.Flags().IntVarP(&searchLimit, "limit", "n", 25, "max number of results")
}

// truncate clips s to max runes, appending "…" when truncated.
func truncate(s string, max int) string {
	if max <= 0 || len([]rune(s)) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}
