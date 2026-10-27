package download

import (
	"context"
	"time"

	"jenken827.com/jenken827/pfm/internal/downloader"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/utils"
)

// Init 初始化离线下载：后端由 kind=downloader 插件在插件加载时登记（downloader.SetBackend）。
//
// 这里不建任何连接——连不连得上 aria2 / qBittorrent 是插件的事；宿主只负责
// 发现「有没有后端」并按周期对齐任务状态。没有后端时离线下载整体不可用，
// 记一条能指导操作的错误（配置 downloader.plugin + 装对应插件）。
func Init() {
	backend := downloader.Current()
	if backend == nil {
		llog.L().Errorf("离线下载未启用：配置 downloader.plugin（如 aria2）并安装对应的 kind=downloader 插件")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	version, err := backend.Version(ctx)
	if err != nil {
		// 连不上也得继续跑：用户可能稍后把下载器拉起来，周期对齐会自己恢复
		llog.L().Errorf("离线下载后端 %s 不可用（%v），任务将在其恢复后继续对齐", backend.Name(), err)
	} else {
		llog.L().Infof("离线下载后端: %s %s", backend.Name(), version)
	}

	// 启动时与下载器真实状态对齐一次，补回停机期间漏掉的完成/失败
	utils.Go(reconcileDownloads)
	// 之后周期对齐：宿主不订阅推送，完成/暂停/选文件都靠这里收敛
	startReconcileLoop()
}
