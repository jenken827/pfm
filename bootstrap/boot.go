package bootstrap

import (
	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/backup"
	"jenken827.com/jenken827/pfm/internal/casdoor"
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

	// 认证初始化：local 模式跳过 Casdoor（无需 Casdoor 配置），使用本地用户表
	if auth.IsLocalMode() {
		if err := auth.InitLocalAuth(); err != nil {
			panic(err)
		}
	} else {
		casdoor.Init()
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
