package download

import (
	"context"
	"time"

	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/downloader"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/pkg/downloader/aria2"
	"jenken827.com/jenken827/pfm/utils"
)

// Init 初始化下载器
func Init() {
	cfg := config.GetConfig().Aria2
	if cfg.Endpoint == "" {
		// 空 endpoint 会让 aria2.New 直接返回 invalid parameter；这里给出可操作的提示
		llog.L().Errorf("aria2 endpoint is empty, offline download disabled (set aria2.endpoint/token in config)")
		return
	}
	c, err := aria2.New(context.Background(), cfg.Endpoint, cfg.Token, time.Duration(cfg.Timeout*int64(time.Second)), &DNotifier{})
	if err != nil {
		llog.L().Errorf("failed to init aria2 client: %v", err)
		return
	}
	v, err := c.GetVersion()
	if err != nil {
		llog.L().Errorf("failed to get aria2 version: %v", err)
		return
	}
	llog.L().Infof("aria2 version: %s", v)
	downloader.Aria2Downloader = c

	// 启动时与 aria2 真实状态对齐一次，补回停机期间漏掉的完成/失败通知
	utils.Go(reconcileDownloads)
	// 之后周期对齐：用户在 AriaNg 里暂停 / 选文件 / 继续，pfm 收不到任何通知
	startReconcileLoop()
}
