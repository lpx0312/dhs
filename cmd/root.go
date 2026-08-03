package cmd

import (
	"fmt"
	"os"
	"strings"

	"dhs/internal/config"
	"dhs/internal/ui"

	"github.com/spf13/cobra"
)

var (
	flagProxy   string
	flagConfig  string
	flagNoColor bool
)

var rootCmd = &cobra.Command{
	Use:   "dhs",
	Short: "Docker Hub Search - search images and tags",
	Long:  "dhs is a CLI tool to search Docker Hub repositories and query image tags, with proxy support.",
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&flagProxy, "proxy", "P", "", "proxy URL, e.g. http://host:port")
	rootCmd.PersistentFlags().StringVar(&flagConfig, "config", "", "config file path (default: platform config dir)")
	rootCmd.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "disable colored output")
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// loadConfig resolves the config path (flag or default) and loads it.
func loadConfig() (*config.Config, error) {
	path := flagConfig
	if path == "" {
		p, err := config.DefaultConfigPath()
		if err != nil {
			return nil, fmt.Errorf("resolving default config path: %w", err)
		}
		path = p
	}
	return config.Load(path)
}

// applyColorSetting enables/disables color based on config + flag.
func applyColorSetting(cfg *config.Config) {
	// fatih/color already auto-detects non-TTY; honor explicit no-color.
	ui.SetEnabled(!cfg.EffectiveNoColor(flagNoColor))
}

// errWithProxy augments a network error with the active proxy for debugging.
func errWithProxy(err error, proxy string) error {
	if proxy == "" {
		return err
	}
	return fmt.Errorf("%w\n（当前代理: %s）", err, proxy)
}

// shortDate trims an ISO timestamp to its date prefix "YYYY-MM-DD".
func shortDate(s string) string {
	if i := strings.Index(s, "T"); i > 0 {
		return s[:i]
	}
	return s
}

// osWriteFile is a seam over os.WriteFile (overridable in tests).
var osWriteFile = os.WriteFile
