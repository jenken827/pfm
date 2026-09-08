package bootstrap

import (
	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/backup"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/download"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/storage"
)

// Init 初始化
func Init() {

	// 初始化配置
	config.Init()

	// 初始化日志
	llog.Init()

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

}
