package bootstrap

import (
	"path/filepath"

	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/backup"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/download"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/internal/thumbnail"
	"jenken827.com/jenken827/pfm/internal/transcode"
)

// Init 初始化
func Init() {

	// 初始化配置
	config.Init()

	// 初始化日志
	llog.Init()

	// 缩略图尺寸（配置 thumbnail.small / thumbnail.large，非法值回落默认 200/400）
	thumbnail.Configure(config.GetConfig().Thumbnail.Small, config.GetConfig().Thumbnail.Large)

	// 视频转码（配置 transcode）：宿主没有 ffmpeg 时自动禁用，
	// 缓存落在数据目录下的 transcodes/，与缩略图同级。
	transcodeCfg := config.GetConfig().Transcode
	transcode.Init(transcode.ConfigFromModel(transcodeCfg), filepath.Join(globals.DataDir, "transcodes"))

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

	// 初始化备份调度器
	backup.Init()

	// 容量统计异步更新器：文件变动触发防抖重算（used_bytes / 文件夹大小写回）
	service.StartCapacityStatsUpdater()
	// 启动异步全量刷新，补齐历史存量数据（不阻塞启动）
	service.RefreshAllCapacityStats()

}
