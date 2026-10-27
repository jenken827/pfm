package bootstrap

import (
	"context"
	"time"

	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/backup"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/download"
	"jenken827.com/jenken827/pfm/internal/plugin"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/internal/thumbnail"
	"jenken827.com/jenken827/pfm/pkg/llog"
)

// plugins 插件管理器（未启用时为 nil）
var plugins *plugin.Manager

// initPlugins 按配置加载插件；单个插件失败只记日志，不影响启动
func initPlugins() {
	pluginCfg := config.GetConfig().Plugin
	if !pluginCfg.Enabled {
		return
	}
	manager := plugin.NewManager(plugin.Options{
		Dir:              plugin.ResolveDir(pluginCfg.Dir),
		HandshakeTimeout: time.Duration(pluginCfg.HandshakeTimeoutSeconds) * time.Second,
		ConfigureTimeout: time.Duration(pluginCfg.ConfigureTimeoutSeconds) * time.Second,
	})
	if err := manager.Load(context.Background()); err != nil {
		llog.L().Errorf("加载插件失败: %v", err)
	}
	plugins = manager
}

// ClosePlugins 结束插件进程（服务退出时调用）
func ClosePlugins() {
	if plugins != nil {
		plugins.Close()
		plugins = nil
	}
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

	// 初始化下载
	download.Init()

	// 初始化存储
	storage.Init()

	// 插件驱动（默认关闭）：在存储之后加载，插件会覆盖对应 driver_id 的构造器
	initPlugins()

	// 初始化备份调度器
	backup.Init()

	// 容量统计异步更新器：文件变动触发防抖重算（used_bytes / 文件夹大小写回）
	service.StartCapacityStatsUpdater()
	// 启动异步全量刷新，补齐历史存量数据（不阻塞启动）
	service.RefreshAllCapacityStats()

}
