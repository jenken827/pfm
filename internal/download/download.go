package download

import (
	"context"
	"time"

	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/downloader"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/pkg/downloader/aria2"
)

// Init 初始化下载器
func Init() {
	cfg := config.GetConfig().Aria2
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

}
