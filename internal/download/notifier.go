package download

import (
	"context"
	"path/filepath"
	"strconv"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/downloader"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/pkg/downloader/aria2"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// DNotifier 下载通知器
type DNotifier struct {
}

var _ = (aria2.Notifier)((*DNotifier)(nil))

// OnDownloadStart will be sent when a download is started.
func (d *DNotifier) OnDownloadStart(events []aria2.Event) {
	for _, e := range events {
		llog.L().Infof("download started, gid: %s", e.Gid)
	}
}

// OnDownloadPause will be sent when a download is paused.
func (d *DNotifier) OnDownloadPause(events []aria2.Event) {
	for _, e := range events {
		llog.L().Infof("download paused, gid: %s", e.Gid)
	}
}

// OnDownloadStop will be sent when a download is stopped by the user.
func (d *DNotifier) OnDownloadStop(events []aria2.Event) {
	for _, e := range events {
		llog.L().Infof("download stopped, gid: %s", e.Gid)
		// 更新下载记录状态
		updateDownloadRecordState(e.Gid, native.DownloadRecordState_DOWNLOAD_FAILED, "download stopped by user")
	}
}

// OnDownloadComplete will be sent when a download is complete. For BitTorrent downloads, this notification is sent when the download is complete and seeding is over.
func (d *DNotifier) OnDownloadComplete(events []aria2.Event) {
	for _, e := range events {
		llog.L().Infof("download completed, gid: %s", e.Gid)
		// 处理下载完成事件
		handleDownloadComplete(e.Gid)
	}
}

// OnDownloadError will be sent when a download is stopped due to an error.
func (d *DNotifier) OnDownloadError(events []aria2.Event) {
	for _, e := range events {
		llog.L().Errorf("download error, gid: %s", e.Gid)
		// 获取错误信息
		errMsg := getAria2ErrorMessage(e.Gid)
		updateDownloadRecordState(e.Gid, native.DownloadRecordState_DOWNLOAD_FAILED, errMsg)
	}
}

// OnBtDownloadComplete will be sent when a torrent download is complete but seeding is still going on.
func (d *DNotifier) OnBtDownloadComplete(events []aria2.Event) {
	for _, e := range events {
		llog.L().Infof("bt download completed (seeding), gid: %s", e.Gid)
		handleDownloadComplete(e.Gid)
	}
}

// handleDownloadComplete 处理下载完成事件
func handleDownloadComplete(gid string) {
	// 根据gid获取下载记录
	dr, err := db.GetDownloadRecordByGid(gid)
	if err != nil {
		llog.L().Errorf("get download record by gid failed, gid: %s, err: %v", gid, err)
		return
	}

	// 获取aria2下载的文件信息
	downloader := downloader.Aria2Downloader
	if downloader == nil {
		llog.L().Errorf("downloader not initialized")
		updateDownloadRecordState(gid, native.DownloadRecordState_DOWNLOAD_FAILED, "downloader not initialized")
		return
	}

	statusInfo, err := downloader.TellStatus(gid, "dir", "files", "totalLength")
	if err != nil {
		llog.L().Errorf("get aria2 status failed, gid: %s, err: %v", gid, err)
		updateDownloadRecordState(gid, native.DownloadRecordState_DOWNLOAD_FAILED, err.Error())
		return
	}
	files := statusInfo.Files
	dir := statusInfo.Dir
	totalLength := statusInfo.TotalLength

	// 获取下载的文件路径
	var filePath string
	if len(files) > 0 {
		filePath = filepath.Join(dir, files[0].Path)
	}

	// 更新下载记录
	dr.FilePath = &filePath
	dr.State = (*int32)(native.DownloadRecordState_DOWNLOAD_COMPLETED.Enum())

	// 解析文件大小
	if totalLength != "" {
		if size, err := strconv.ParseUint(totalLength, 10, 64); err == nil {
			dr.FileSize = &size
		}
	}

	if err := db.UpdateDownloadRecord(dr, []string{"file_path", "state", "file_size"}); err != nil {
		llog.L().Errorf("update download record failed, gid: %s, err: %v", gid, err)
		return
	}




	llog.L().Infof("download record updated, gid: %s, file: %s, size: %s", gid, filePath, totalLength)

	// 恢复同步任务：从下载的文件批量上传到目标提供者
	resumeSyncFromDownload(dr, filePath)
}

// resumeSyncFromDownload 从下载的文件恢复同步任务
func resumeSyncFromDownload(dr *native.DownloadRecordORM, downloadedFilePath string) {
	// 1. 获取上传记录
	var uploadRecord *native.UploadRecordORM
	if dr.UploadRecordId != nil {
		records, _, err := db.Query[native.UploadRecordORM](context.Background(), &db.DBQuery{
			Where: "id = ?",
			Args:  []any{*dr.UploadRecordId},
		}, false)
		if err == nil && len(records) > 0 {
			uploadRecord = records[0]
		}
	}

	// 2. 如果没有上传记录，无法继续同步
	if uploadRecord == nil {
		llog.L().Errorf("upload record not found, cannot resume sync, download_record_id: %d", *dr.Id)
		return
	}

	// 3. 更新上传记录的临时文件路径
	uploadRecord.TempPath = &downloadedFilePath
	syncStatus := int32(native.GroupUploadStatus_GUPLOADING)
	uploadRecord.Status = &syncStatus
	if err := db.GetDB().Save(uploadRecord).Error; err != nil {
		llog.L().Errorf("update upload record failed: %v", err)
		return
	}

	llog.L().Infof("resuming sync from download, hash: %s, file: %s", *dr.Blake3Hash, downloadedFilePath)

	// 4. 调用 storage.Dispatch 继续上传流程
	// Dispatch 会检测到 TempPath 存在，从该文件批量上传到所有目标提供者
	storage.Dispatch(context.Background(), uploadRecord)

	llog.L().Infof("sync task resumed successfully, hash: %s", *dr.Blake3Hash)
}


// updateDownloadRecordState 更新下载记录状态
func updateDownloadRecordState(gid string, state native.DownloadRecordState, errMsg string) {
	dr, err := db.GetDownloadRecordByGid(gid)
	if err != nil {
		llog.L().Errorf("get download record by gid failed, gid: %s, err: %v", gid, err)
		return
	}

	dr.State = (*int32)(state.Enum())
	if errMsg != "" {
		dr.ErrorMessage = &errMsg
	}

	updateFields := []string{"state"}
	if errMsg != "" {
		updateFields = append(updateFields, "error_message")
	}

	if err := db.UpdateDownloadRecord(dr, updateFields); err != nil {
		llog.L().Errorf("update download record state failed, gid: %s, err: %v", gid, err)
	}
}

// getAria2ErrorMessage 获取aria2错误信息
func getAria2ErrorMessage(gid string) string {
	downloader := downloader.Aria2Downloader
	if downloader == nil {
		return "downloader not initialized"
	}

	_, err := downloader.TellStatus(gid, "errorMessage", "errorCode")
	if err != nil {
		return err.Error()
	}
	return "unknown error"
}
