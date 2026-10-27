package bootstrap

import (
	"context"
	"fmt"
	"sync"
	"time"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/backup"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/download"
	"jenken827.com/jenken827/pfm/internal/downloader"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/internal/plugin"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/internal/thumbnail"
	"jenken827.com/jenken827/pfm/pkg/llog"
)

// plugins 插件管理器（未启用时为 nil）
var plugins *plugin.Manager

// pluginMu 串行化插件的加载 / 热加载 / 关闭（SIGHUP 与退出可能同时到达）
var pluginMu sync.Mutex

// newPluginManager 按配置组装插件管理器（不加载）
func newPluginManager(pluginCfg models.PluginConfig) *plugin.Manager {
	downloaderCfg := config.GetConfig().Downloader
	return plugin.NewManager(plugin.Options{
		Dir:               plugin.ResolveDir(pluginCfg.Dir),
		HandshakeTimeout:  time.Duration(pluginCfg.HandshakeTimeoutSeconds) * time.Second,
		ConfigureTimeout:  time.Duration(pluginCfg.ConfigureTimeoutSeconds) * time.Second,
		DownloaderID:      downloaderCfg.Plugin,
		DownloaderOptions: downloaderCfg.Options,
	})
}

// loadPlugins 拉起插件目录里的全部插件；单个插件失败只记日志，不影响启动
func loadPlugins(pluginCfg models.PluginConfig) *plugin.Manager {
	manager := newPluginManager(pluginCfg)
	if err := manager.Load(context.Background()); err != nil {
		llog.L().Errorf("加载插件失败: %v", err)
	}
	return manager
}

// initPlugins 按配置加载插件（默认关闭）
func initPlugins() {
	pluginCfg := config.GetConfig().Plugin
	if !pluginCfg.Enabled {
		return
	}
	plugins = loadPlugins(pluginCfg)
}

// ClosePlugins 结束插件进程（服务退出时调用）
func ClosePlugins() {
	pluginMu.Lock()
	defer pluginMu.Unlock()
	if plugins != nil {
		plugins.Close()
		plugins = nil
	}
}

// ReloadPlugins 重新扫描插件目录并重建受影响的 provider 实例（SIGHUP →
// server 调用；也可由 `pfm plugin reload` 通过信号触发）。
//
// 顺序：先把新插件全部拉起并注册覆盖 → 再关掉旧进程 → 最后按驱动身份重建
// provider 实例。这样热加载期间新实例已经可用，旧实例只在末尾才失效。
//
// 目录里已经不存在的插件会撤销驱动覆盖（回退到内置实现，没有内置的该
// driver_id 就变成"未安装"）；重建失败的 provider 只记日志、不阻断其它 provider。
func ReloadPlugins() error {
	pluginMu.Lock()
	defer pluginMu.Unlock()

	pluginCfg := config.GetConfig().Plugin
	if !pluginCfg.Enabled {
		return fmt.Errorf("插件未启用（plugin.enabled=false），无需热加载")
	}

	old := plugins
	var oldIDs []string
	if old != nil {
		oldIDs = old.DriverIDs()
	}

	manager := loadPlugins(pluginCfg)
	newIDs := manager.DriverIDs()

	// 插件目录里已经删掉的：撤销覆盖，别让旧实现继续被当成可用
	newSet := make(map[string]struct{}, len(newIDs))
	for _, id := range newIDs {
		newSet[id] = struct{}{}
	}
	for _, id := range oldIDs {
		if _, ok := newSet[id]; !ok {
			types.ClearDriverOverride(id)
			llog.L().Infof("插件已移除: driver=%s，驱动覆盖已撤销", id)
		}
	}

	plugins = manager
	if old != nil {
		old.Close()
	}

	affected := make(map[string]struct{}, len(oldIDs)+len(newIDs))
	for _, id := range oldIDs {
		affected[id] = struct{}{}
	}
	for _, id := range newIDs {
		affected[id] = struct{}{}
	}
	rebuilt, err := storage.ReloadDrivers(affected)
	if err != nil {
		llog.L().Errorf("插件重载后重建 provider 实例失败: %v", err)
	}
	backend := downloader.Current()
	backendDesc := "无（离线下载不可用）"
	if backend != nil {
		backendDesc = backend.Name()
	}
	llog.L().Infof("插件重载完成: 插件 %d 个 %v，重建 provider 实例 %d 个，离线下载后端: %s",
		len(newIDs), newIDs, rebuilt, backendDesc)
	if err != nil {
		return err
	}
	return nil
}

// Init 初始化
func Init() {

	// 初始化配置
	config.Init()

	// 初始化日志
	llog.Init()

	// 缩略图尺寸（配置 thumbnail.small / thumbnail.large，非法值回落默认 200/400）
	thumbnail.Configure(config.GetConfig().Thumbnail.Small, config.GetConfig().Thumbnail.Large)

	// 认证初始化：本地认证（users 表 + HS256 JWT），密钥缺失时自动生成
	if err := auth.InitLocalAuth(); err != nil {
		panic(err)
	}

	// 初始化数据库
	db.Init()

	// 初始化缓存
	// cache.Init()

	// 插件（默认关闭）：必须早于 download.Init / storage.Init ——
	// 驱动插件要在存储初始化前注册好覆盖，下载器插件要在离线下载启动前挂上后端
	initPlugins()

	// 初始化下载
	download.Init()

	// 初始化存储
	storage.Init()

	// 初始化备份调度器
	backup.Init()

	// 容量统计异步更新器：文件变动触发防抖重算（used_bytes / 文件夹大小写回）
	service.StartCapacityStatsUpdater()
	// 启动异步全量刷新，补齐历史存量数据（不阻塞启动）
	service.RefreshAllCapacityStats()

}
