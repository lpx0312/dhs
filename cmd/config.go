package cmd

import (
	"fmt"
	"net/url"
	"strconv"

	"dhs/internal/config"
	"dhs/internal/ui"

	"github.com/spf13/cobra"
)

// configKeys is the canonical key list (also feeds shell completion).
var configKeys = []string{"proxy", "no-color"}

// normalizeKey maps accepted aliases to a canonical key.
func normalizeKey(s string) (string, error) {
	switch s {
	case "proxy":
		return "proxy", nil
	case "no-color", "no_color":
		return "no-color", nil
	}
	return "", fmt.Errorf("未知的配置项 %q（可用: %v）", s, configKeys)
}

// applySetting validates value for key and returns a copy of cfg with it set.
func applySetting(cfg *config.Config, key, value string) (*config.Config, error) {
	next := *cfg
	switch key {
	case "proxy":
		if value == "" {
			return nil, fmt.Errorf("proxy 值不能为空；要清除代理请用 dhs config unset proxy")
		}
		u, err := url.Parse(value)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("代理地址格式不正确: %q（示例: http://192.168.0.225:7897）", value)
		}
		next.Proxy = value
	case "no-color":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("no-color 只接受 true/false，收到 %q", value)
		}
		next.NoColor = b
	default:
		return nil, fmt.Errorf("未知的配置项 %q（可用: %v）", key, configKeys)
	}
	return &next, nil
}

// clearSetting returns a copy of cfg with key reset to its zero value.
func clearSetting(cfg *config.Config, key string) *config.Config {
	next := *cfg
	switch key {
	case "proxy":
		next.Proxy = ""
	case "no-color":
		next.NoColor = false
	}
	return &next
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "查看或修改持久化配置（代理等，写入配置文件）",
	Long: `查看或修改 dhs 的持久化配置。写入一次，之后所有 dhs 命令自动生效。

配置项:
  proxy      代理地址，如 http://192.168.0.225:7897
  no-color   关闭彩色输出（true/false）

Examples:
  dhs config set proxy http://192.168.0.225:7897
  dhs config set no-color true
  dhs config get proxy
  dhs config list
  dhs config unset proxy`,
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "设置配置项并写入配置文件",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key, err := normalizeKey(args[0])
		if err != nil {
			return err
		}
		path, err := configPath()
		if err != nil {
			return err
		}
		cfg, err := config.Load(path)
		if err != nil {
			return err
		}
		applyColorSetting(cfg)
		next, err := applySetting(cfg, key, args[1])
		if err != nil {
			return err
		}
		if err := config.Save(path, next); err != nil {
			return err
		}
		fmt.Println(ui.Green(fmt.Sprintf("✓ 已写入 %s", path)))
		fmt.Printf("  %s = %s\n", key, args[1])
		fmt.Println(ui.Cyan("之后所有 dhs 命令自动使用该配置；单次覆盖可用 --proxy"))
		return nil
	},
}

var configGetCmd = &cobra.Command{
	Use:               "get <key>",
	Short:             "读取配置项的值（裸值输出，便于脚本）",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: cobra.FixedCompletions(configKeys, cobra.ShellCompDirectiveNoFileComp),
	RunE: func(cmd *cobra.Command, args []string) error {
		key, err := normalizeKey(args[0])
		if err != nil {
			return err
		}
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		applyColorSetting(cfg)
		switch key {
		case "proxy":
			if cfg.Proxy == "" {
				fmt.Println("(未设置)")
			} else {
				fmt.Println(cfg.Proxy)
			}
		case "no-color":
			fmt.Println(strconv.FormatBool(cfg.NoColor))
		}
		return nil
	},
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出配置文件路径与各配置项的值",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := configPath()
		if err != nil {
			return err
		}
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		applyColorSetting(cfg)

		fmt.Println(ui.Cyan(fmt.Sprintf("配置文件: %s", path)))
		fmt.Println()
		dash := func(s string) string {
			if s == "" {
				return "-"
			}
			return s
		}
		fmt.Printf("  proxy      文件值: %s\n", dash(cfg.Proxy))
		fmt.Printf("  no-color   文件值: %v\n", cfg.NoColor)
		fmt.Println()
		fmt.Printf("生效代理: %s\n", dash(cfg.EffectiveProxy(flagProxy, "")))
		fmt.Println(ui.Cyan("优先级: --proxy 参数 > 配置文件 > HTTPS_PROXY/HTTP_PROXY 环境变量 > 直连"))
		return nil
	},
}

var configUnsetCmd = &cobra.Command{
	Use:               "unset <key>",
	Short:             "清除配置项（恢复默认值）",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: cobra.FixedCompletions(configKeys, cobra.ShellCompDirectiveNoFileComp),
	RunE: func(cmd *cobra.Command, args []string) error {
		key, err := normalizeKey(args[0])
		if err != nil {
			return err
		}
		path, err := configPath()
		if err != nil {
			return err
		}
		cfg, err := config.Load(path)
		if err != nil {
			return err
		}
		applyColorSetting(cfg)
		if err := config.Save(path, clearSetting(cfg, key)); err != nil {
			return err
		}
		fmt.Println(ui.Green(fmt.Sprintf("✓ 已清除 %s（%s）", key, path)))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configSetCmd, configGetCmd, configListCmd, configUnsetCmd)
}
