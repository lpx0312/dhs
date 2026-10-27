package cmd

import (
	"fmt"
	"net/url"
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
	// SilenceUsage keeps runtime errors (network/HTTP/bad YAML) from dumping
	// the whole usage text after the "Error:" line. Cobra still prints the
	// "Error:" line itself. Verified experimentally on cobra v1.10.2: the
	// suppression also applies to flag syntax errors and argument-count
	// errors (cobra checks SilenceUsage uniformly in ExecuteC for every
	// error returned from execute(), including FlagErrorFunc-wrapped ones) —
	// those errors keep their self-descriptive "Error:" message but no longer
	// print usage either. See TestE2E_RuntimeErrorNoUsageDump and
	// TestE2E_FlagErrorStillExplainsItself.
	SilenceUsage: true,
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

// configPath resolves the config file path: --config flag or platform default.
func configPath() (string, error) {
	if flagConfig != "" {
		return flagConfig, nil
	}
	return config.DefaultConfigPath()
}

// loadConfig resolves the config path (flag or default) and loads it.
func loadConfig() (*config.Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, fmt.Errorf("resolving default config path: %w", err)
	}
	return config.Load(path)
}

// applyColorSetting disables color when config, flag, or NO_COLOR require it.
// It never force-enables color: when no no-color source applies, fatih/color
// keeps its own NO_COLOR / non-TTY auto-detection untouched.
func applyColorSetting(cfg *config.Config) {
	if cfg.EffectiveNoColor(flagNoColor) {
		ui.SetEnabled(false)
	}
}

// errWithProxy augments a network error with the active proxy for debugging.
// The proxy URL is redacted before printing (url.URL.Redacted): a
// http://user:pass@host form would otherwise leak credentials to stderr.
// URLs that fail to parse are printed as-is.
func errWithProxy(err error, proxy string) error {
	if proxy == "" {
		return err
	}
	if u, perr := url.Parse(proxy); perr == nil {
		proxy = u.Redacted() // user:pass@ → user:xxxxx@
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
