package cmd

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/pidfile"
	"jenken827.com/jenken827/pfm/internal/plugin"
)

// pluginCmd 驱动插件管理
var pluginCmd = &cobra.Command{
	Use: "plugin",
	// 错误已经足够自解释，不再跟着打印一整页用法
	SilenceUsage: true,
	Short:        "驱动插件管理（与宿主同权限，只放可信来源的二进制）",
	Long: `驱动插件管理。

插件是一个目录：plugin.json（kind / exec / abi_version，driver 类型另需 driver_id）
+ 可执行文件，启动时向 stdout 打一行握手，之后用 loopback HTTP 与宿主通信。
插件类型（kind）：
  driver      存储驱动，按 driver_id 注册驱动覆盖
  downloader  离线下载后端，按 downloader_id 提供给 downloader.plugin 选用
启用插件需要在配置里打开 plugin.enabled（默认关闭）。

子命令:
  list              列出插件目录里的插件清单（不启动进程）
  verify            拉起每个插件做握手 + Describe（真正的可用性检查）
  install <name>    从签名索引安装 / 升级插件（默认取最高版本），装完自动热加载
  rollback <name>   回滚到上一个已归档的版本（同样自动热加载）
  reload            让运行中的 pfm 重新扫描插件目录（SIGHUP，不重启服务）
  keygen            生成索引签名密钥对（私钥写文件，公钥打印）
  sign-index        给 index.json 签名（生成 index.json.sig）
`,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

var (
	pluginListJSON bool
	pluginInstallV string
	// pluginNoReload 装完不通知运行中的服务（自动化脚本 / 服务没在跑时用）
	pluginNoReload bool
)

var pluginListCmd = &cobra.Command{
	Use:          "list",
	SilenceUsage: true,
	Short:        "列出插件目录里的插件清单（不启动进程）",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initPluginCommand(); err != nil {
			return err
		}
		cfg := config.GetConfig().Plugin
		dir := plugin.ResolveDir(cfg.Dir)
		infos, err := plugin.Scan(dir)
		if err != nil {
			return err
		}
		if pluginListJSON {
			raw, marshalErr := json.MarshalIndent(infos, "", "  ")
			if marshalErr != nil {
				return marshalErr
			}
			fmt.Println(string(raw))
			return nil
		}
		fmt.Printf("插件目录: %s（enabled=%v）\n", dir, cfg.Enabled)
		if len(infos) == 0 {
			fmt.Println("没有发现插件")
			return nil
		}
		installed, err := plugin.InstalledPlugins(dir)
		if err != nil {
			return err
		}
		installedByDir := make(map[string]plugin.InstalledInfo, len(installed))
		for _, item := range installed {
			installedByDir[item.Name] = item
		}
		writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(writer, "KIND\tID\tNAME\tVERSION\tABI\tSTATE")
		for _, info := range infos {
			state := "manual"
			if info.Error != "" {
				state = "invalid"
			}
			if record, ok := installedByDir[filepath.Base(info.Dir)]; ok {
				state = record.Status
			}
			id := info.DriverID
			if info.Kind == plugin.KindDownloader {
				id = info.DownloaderID
			}
			_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%d\t%s\n",
				info.Kind, id, info.Name, info.Version, info.AbiVersion, state)
			if info.Error != "" {
				_, _ = fmt.Fprintf(writer, "  └ %s\n", info.Error)
			}
		}
		return writer.Flush()
	},
}

// newInstaller 按配置组装安装器（路径、索引地址、可信公钥）
func newInstaller() (*plugin.Installer, error) {
	cfg := config.GetConfig().Plugin
	keys, err := plugin.ResolveTrustKeys(cfg.PublicKeys)
	if err != nil {
		return nil, err
	}
	return &plugin.Installer{
		Dir:        plugin.ResolveDir(cfg.Dir),
		IndexURL:   cfg.IndexURL,
		Keys:       keys,
		PfmVersion: globals.Version,
	}, nil
}

var pluginInstallCmd = &cobra.Command{
	Use:          "install <name>",
	Short:        "从签名索引安装 / 升级插件（默认取最高版本）",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initPluginCommand(); err != nil {
			return err
		}
		installer, err := newInstaller()
		if err != nil {
			return err
		}
		result, err := installer.Install(cmd.Context(), args[0], pluginInstallV)
		if err != nil {
			return err
		}
		fmt.Printf("已安装 %s v%s（kind=%s，id=%s，seq=%d）\n", result.Name, result.Version,
			result.Kind, pluginID(result.Kind, result.DriverID, result.DownloaderID), result.Seq)
		if result.PreviousVersion != "" {
			fmt.Printf("旧版本 v%s 已归档，可用 pfm plugin rollback %s 回滚\n",
				result.PreviousVersion, result.Name)
		}
		fmt.Println("提示：插件与宿主同权限运行，只安装可信来源")
		if pluginNoReload {
			fmt.Println("已跳过热加载（--no-reload）：运行中的服务需重启或 pfm plugin reload 才会加载")
			return nil
		}
		reloadRunningServer()
		return nil
	},
}

var pluginRollbackCmd = &cobra.Command{
	Use:          "rollback <name>",
	Short:        "回滚到上一个已归档的版本",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initPluginCommand(); err != nil {
			return err
		}
		installer, err := newInstaller()
		if err != nil {
			return err
		}
		result, err := installer.Rollback(args[0])
		if err != nil {
			return err
		}
		fmt.Printf("已回滚 %s 到 v%s（kind=%s，原 v%s 已归档）\n", result.Name, result.Version,
			result.Kind, result.PreviousVersion)
		if pluginNoReload {
			fmt.Println("已跳过热加载（--no-reload）：运行中的服务需重启或 pfm plugin reload 才会加载")
			return nil
		}
		reloadRunningServer()
		return nil
	},
}

// pluginReloadCmd 让运行中的服务重新扫描插件目录（不重启、不断连接）
var pluginReloadCmd = &cobra.Command{
	Use:          "reload",
	Short:        "让运行中的 pfm 重新扫描插件目录（SIGHUP，不重启服务）",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initPluginCommand(); err != nil {
			return err
		}
		if !reloadRunningServer() {
			return fmt.Errorf("没能通知到运行中的 pfm（见上方提示）")
		}
		fmt.Println("插件目录已重新扫描：新装的插件立即可用，被删掉的插件驱动已撤销")
		return nil
	},
}

// reloadRunningServer 通知运行中的 pfm 重载插件（SIGHUP）。
//
// pid 来自配置文件同目录的 pfm.pid（服务启动时写入）。读不到 / 进程已死 /
// 命令行不像 pfm 时只打印提示，返回 false —— 这不影响「插件已装好，只是要等重启」。
func reloadRunningServer() bool {
	if !config.GetConfig().Plugin.Enabled {
		fmt.Println("提示：plugin.enabled=false，服务不会加载任何插件；打开它并重启服务后才会生效")
		return false
	}
	configPath := globals.ConfigPath
	pidPath := pidfile.Path(configPath)
	pid, err := pidfile.Read(configPath)
	if err != nil {
		fmt.Printf("提示：没有运行中的 pfm 记录（%s），插件将在下次启动时加载\n", pidPath)
		return false
	}
	if !processAlive(pid) {
		fmt.Printf("提示：%s 里的 pid %d 已不在运行（可能已退出），插件将在下次启动时加载\n", pidPath, pid)
		return false
	}
	if !processLooksLikePfm(pid) {
		fmt.Printf("提示：pid %d 不是 pfm 进程（pid 被复用？），已跳过热加载；可删除 %s 后重试\n", pid, pidPath)
		return false
	}
	if err := signalReload(pid); err != nil {
		fmt.Printf("提示：通知 pid %d 重载失败: %v\n", pid, err)
		return false
	}
	fmt.Printf("已通知运行中的 pfm（pid=%d）热加载插件\n", pid)
	return true
}

var (
	pluginKeygenOut   string
	pluginSignKeyPath string
)

var pluginKeygenCmd = &cobra.Command{
	Use:          "keygen",
	Short:        "生成索引签名密钥对（私钥写文件，公钥打印出来填进配置或内置）",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return fmt.Errorf("生成密钥失败: %w", err)
		}
		out := pluginKeygenOut
		if out == "" {
			return fmt.Errorf("必须用 --out 指定私钥文件路径（不要放在仓库里）")
		}
		// 私钥以 base64 文本保存，权限 0600；发布索引时用 pfm plugin sign-index
		encoded := base64.StdEncoding.EncodeToString(privateKey.Seed())
		if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(out, []byte(encoded+"\n"), 0o600); err != nil {
			return fmt.Errorf("写私钥失败: %w", err)
		}
		fmt.Printf("私钥: %s（0600，勿入版本控制）\n", out)
		fmt.Printf("公钥: %s\n", base64.StdEncoding.EncodeToString(publicKey))
		fmt.Println("把公钥填进配置 plugin.public_keys，或（官方索引）填进 internal/plugin/trust.go")
		return nil
	},
}

var pluginSignIndexCmd = &cobra.Command{
	Use:          "sign-index <index.json>",
	Short:        "用私钥给索引签名（生成 index.json.sig）",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if pluginSignKeyPath == "" {
			return fmt.Errorf("必须用 --key 指定私钥文件")
		}
		privateKey, err := readPrivateKey(pluginSignKeyPath)
		if err != nil {
			return err
		}
		payload, err := os.ReadFile(args[0])
		if err != nil {
			return fmt.Errorf("读取索引失败: %w", err)
		}
		signature := ed25519.Sign(privateKey, payload)
		sigPath := args[0] + ".sig"
		if err := os.WriteFile(sigPath, []byte(base64.StdEncoding.EncodeToString(signature)+"\n"), 0o644); err != nil {
			return fmt.Errorf("写签名失败: %w", err)
		}
		fmt.Printf("已签名: %s\n", sigPath)
		return nil
	},
}

// readPrivateKey 支持两种私钥格式：base64 的 32 字节种子，或 64 字节完整私钥
func readPrivateKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取私钥失败: %w", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("私钥不是合法的 base64: %w", err)
	}
	switch len(decoded) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(decoded), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(decoded), nil
	default:
		return nil, fmt.Errorf("私钥长度不对（应为 %d 或 %d 字节）", ed25519.SeedSize, ed25519.PrivateKeySize)
	}
}

func init() {
	rootCmd.AddCommand(pluginCmd)
	pluginCmd.AddCommand(pluginListCmd, pluginVerifyCmd, pluginInstallCmd, pluginRollbackCmd,
		pluginReloadCmd, pluginKeygenCmd, pluginSignIndexCmd)
	pluginKeygenCmd.Flags().StringVar(&pluginKeygenOut, "out", "", "私钥输出路径（勿放仓库内）")
	pluginSignIndexCmd.Flags().StringVar(&pluginSignKeyPath, "key", "", "私钥文件")

	pluginInstallCmd.Flags().StringVar(&pluginInstallV, "version", "", "指定版本（默认取索引里的最高版本）")
	pluginInstallCmd.Flags().BoolVar(&pluginNoReload, "no-reload", false, "装完不通知运行中的 pfm（下次启动才加载）")
	pluginRollbackCmd.Flags().BoolVar(&pluginNoReload, "no-reload", false, "回滚完不通知运行中的 pfm（下次启动才加载）")
	pluginListCmd.Flags().BoolVar(&pluginListJSON, "json", false, "以 JSON 输出")
}

// initPluginCommand 插件子命令只需要配置（不碰数据库）
func initPluginCommand() error {
	if err := globals.ValidateConfig(); err != nil {
		return err
	}
	globals.NormalizePath()
	config.Init()
	return nil
}

var pluginVerifyCmd = &cobra.Command{
	Use:          "verify",
	SilenceUsage: true,
	Short:        "拉起每个插件做握手与 Describe（真正的可用性检查）",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initPluginCommand(); err != nil {
			return err
		}
		pluginCfg := config.GetConfig().Plugin
		downloaderCfg := config.GetConfig().Downloader
		dir := plugin.ResolveDir(pluginCfg.Dir)
		manager := plugin.NewManager(plugin.Options{
			Dir:               dir,
			HandshakeTimeout:  time.Duration(pluginCfg.HandshakeTimeoutSeconds) * time.Second,
			ConfigureTimeout:  time.Duration(pluginCfg.ConfigureTimeoutSeconds) * time.Second,
			DownloaderID:      downloaderCfg.Plugin,
			DownloaderOptions: downloaderCfg.Options,
		})
		loadErr := manager.Load(cmd.Context())
		defer manager.Close()

		plugins := manager.Plugins()
		writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(writer, "KIND\tID\tNAME\tVERSION\tCAPABILITIES")
		for _, loaded := range plugins {
			caps := make([]string, 0, len(loaded.Capabilities))
			for _, capability := range loaded.Capabilities {
				caps = append(caps, strings.TrimPrefix(capability.String(), "DRIVER_CAPABILITY_"))
			}
			_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n",
				loaded.Kind, loaded.ID, loaded.Name, loaded.Version, strings.Join(caps, ","))
		}
		if flushErr := writer.Flush(); flushErr != nil {
			return flushErr
		}
		if len(plugins) == 0 {
			fmt.Printf("插件目录 %s 下没有可用插件\n", dir)
		}
		// 单个插件的失败已经在 Load 里逐条打日志，这里把汇总错误返回给退出码
		return loadErr
	},
}

// pluginID 按类型取插件身份（list / install 输出用）
func pluginID(kind, driverID, downloaderID string) string {
	if kind == plugin.KindDownloader {
		return downloaderID
	}
	return driverID
}
