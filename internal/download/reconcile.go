package download

import (
	"fmt"
	"strings"
	"time"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/downloader"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/utils"
)

const (
	// offlineReconcileInterval 周期对齐间隔。
	// 宿主不订阅下载器推送（后端是插件，只有 RPC），完成/失败/暂停都靠轮询发现；
	// 5s 与前端进度轮询同量级，代价只是一次插件 RPC。
	offlineReconcileInterval = 5 * time.Second
	// offlineTaskGracePeriod 新建记录在下载器里暂时查不到时的宽限期（避免刚提交就误判失败）
	offlineTaskGracePeriod = 5 * time.Minute
)

// reconcileDownloads 把 DB 里的未终态记录与下载器真实状态对齐。
//
// 这是**唯一**的完成检测路径：宿主不订阅下载器推送（后端是插件，只有 RPC），
// pfm 停机期间完成、下载器重启换 gid、用户在 AriaNg 里改选文件后再继续，
// 都由这一轮轮询收敛。
func reconcileDownloads() {
	completeMu.Lock()
	defer completeMu.Unlock()

	c := downloader.Current()
	if c == nil {
		return
	}
	records, err := db.ListUnfinishedDownloadRecords()
	if err != nil || len(records) == 0 {
		return
	}
	snap := fetchTaskSnapshot(c)
	if len(snap.tasks) == 0 && len(records) > 0 {
		llog.L().Warnf("reconcile downloads: 下载器返回 0 个任务, 保留 %d 条未完成记录", len(records))
	}
	for _, dr := range records {
		reconcileOneRecord(dr, snap)
	}
}

// startReconcileLoop 周期对齐（唯一的完成检测路径）
func startReconcileLoop() {
	utils.Go(func() {
		ticker := time.NewTicker(offlineReconcileInterval)
		defer ticker.Stop()
		for range ticker.C {
			reconcileDownloads()
		}
	})
}

// reconcileOneRecord 对齐单条记录
func reconcileOneRecord(dr *entity.DownloadRecordORM, snap *taskSnapshot) {
	task, ok := snap.targetFor(dr)
	if !ok {
		// 刚提交的任务可能还没出现在下载器列表里，宽限期内先不动
		if withinGrace(dr) {
			return
		}
		markDownloadFailed(dr, "下载器中已不存在该任务（被移除或重启后未恢复），可重试")
		return
	}

	// 下载器里真实的 gid 才是后续对齐的键（磁力：元数据 gid → 产物 gid）
	relinkGid(dr, task.Gid)
	rememberInfoHash(dr, task)

	if isFailedTask(task) {
		markDownloadFailed(dr, firstNonEmpty(task.ErrorMessage, "下载器错误码: "+task.ErrorCode))
		return
	}

	switch classifyTask(task) {
	case roleFinal:
		if isFinished(task) {
			importFinalTask(dr, task)
			return
		}
		markDownloadRunning(dr, task.TotalLength)
	case roleMetadata, roleTorrent, roleUnknown:
		// 元数据 / 种子文件阶段：真正的 BT 任务还没派生出来，等下一轮对齐
		markDownloadRunning(dr, "")
	}
}

// relinkGid 把记录里的 gid 换成下载器里真实的产物 gid，
// 否则后续对齐仍然匹配不上（磁力任务记录的 gid 是元数据那一环）
func relinkGid(dr *entity.DownloadRecordORM, gid string) {
	if gid == "" || derefString(dr.Gid) == gid {
		return
	}
	dr.Gid = utils.StringPtr(gid)
	if err := db.UpdateDownloadRecord(dr, []string{"gid"}); err != nil {
		llog.L().Errorf("reconcile: relink gid failed, record: %v, new gid: %s, err: %v", dr.Id, gid, err)
		return
	}
	llog.L().Infof("reconcile: relinked gid, record: %v, new gid: %s", dr.Id, gid)
}

// rememberInfoHash 把产物任务的 infohash 记进 callback_info。
//
// .torrent URL 的 infohash 不体现在 URL 上，下载器重启后 gid 全变、种子文件也
// 可能已被清理，只有记下来才能重新关联到对应的 BT 任务。
func rememberInfoHash(dr *entity.DownloadRecordORM, task downloader.Task) {
	ih := strings.ToLower(task.InfoHash)
	if ih == "" || recordInfoHash(dr) != "" {
		return
	}
	payload := fmt.Sprintf(`{"info_hash":%q}`, ih)
	dr.CallbackInfo = &payload
	if err := db.UpdateDownloadRecord(dr, []string{"callback_info"}); err != nil {
		llog.L().Errorf("reconcile: remember info hash failed, record: %v, err: %v", dr.Id, err)
		return
	}
	llog.L().Infof("reconcile: remembered info hash, record: %v, info_hash: %s", dr.Id, ih)
}

// importFinalTask 产物任务已完成 → 落完成信息并入库 / 恢复同步
func importFinalTask(dr *entity.DownloadRecordORM, task downloader.Task) {
	paths := importableFiles(task.Dir, task.Files)
	if len(paths) == 0 {
		markDownloadFailed(dr, "下载已完成但没有可导入的文件（可能文件全部未选中）")
		return
	}
	if err := markDownloadCompleted(dr, task.Gid, paths[0], task.TotalLength); err != nil {
		return
	}
	llog.L().Infof("offline download finished, record: %v, gid: %s, files: %d, primary: %s",
		dr.Id, task.Gid, len(paths), paths[0])

	if isURLOfflineDownload(dr) {
		importOfflineDownload(dr, task, paths)
	} else {
		// 中转同步任务：从下载的文件恢复上传
		resumeSyncFromDownload(dr, paths[0])
	}
	cleanupTorrentLeftover(task)
}

// isURLOfflineDownload URL 离线下载任务（有 url、且不是 provider→provider 中转）
func isURLOfflineDownload(dr *entity.DownloadRecordORM) bool {
	return derefString(dr.Url) != "" && (dr.UploadRecordId == nil || *dr.UploadRecordId == 0)
}

// withinGrace 记录是否还在「刚提交、可能尚未进入下载器」的宽限期内
func withinGrace(dr *entity.DownloadRecordORM) bool {
	return dr.CreatedAt == nil || time.Since(*dr.CreatedAt) < offlineTaskGracePeriod
}

// markDownloadRunning 置为下载中（并同步 下载器 已知总大小）
func markDownloadRunning(dr *entity.DownloadRecordORM, totalLength string) {
	changed := false
	if size, err := parseSize(totalLength); err == nil && size > 0 {
		if dr.FileSize == nil || *dr.FileSize != size {
			dr.FileSize = &size
			if err := db.UpdateDownloadRecord(dr, []string{"file_size"}); err != nil {
				llog.L().Errorf("reconcile: update file size failed: %v", err)
			}
		}
	}
	state := int32(native.DownloadRecordState_DOWNLOADING)
	if dr.State == nil || *dr.State != state {
		dr.State = &state
		changed = true
	}
	if changed {
		if err := db.UpdateDownloadRecord(dr, []string{"state"}); err != nil {
			llog.L().Errorf("reconcile: update state to downloading failed: %v", err)
		}
	}
}

// markDownloadFailed 置为失败并写入可读原因
func markDownloadFailed(dr *entity.DownloadRecordORM, msg string) {
	state := int32(native.DownloadRecordState_DOWNLOAD_FAILED)
	dr.State = &state
	dr.ErrorMessage = &msg
	if err := db.UpdateDownloadRecord(dr, []string{"state", "error_message"}); err != nil {
		llog.L().Errorf("reconcile: mark failed error: %v", err)
		return
	}
	llog.L().Infof("reconcile: marked failed, record: %v, gid: %v, reason: %s", dr.Id, derefString(dr.Gid), msg)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
