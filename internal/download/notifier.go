package download

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/downloader"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/pkg/downloader/aria2"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
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
		record, _, err := db.GetUploadRecordByID(*dr.UploadRecordId, false)
		if err == nil {
			uploadRecord = record
		}
	}

	// 2. 如果没有上传记录，无法继续同步
	if uploadRecord == nil {
		llog.L().Errorf("upload record not found, cannot resume sync, download_record_id: %d", *dr.Id)
		return
	}

	// 3. 获取源文件位置记录
	var sourceLocation *native.FileLocationORM
	if dr.SourceLocationId != nil {
		location, err := db.GetFileLocationByID(*dr.SourceLocationId)
		if err == nil {
			sourceLocation = location
		}
	}

	// 4. 检查是否需要解密
	blake3Hash := *dr.Blake3Hash
	var finalFilePath string = downloadedFilePath

	if sourceLocation != nil && sourceLocation.Encrypted != nil && *sourceLocation.Encrypted {
		// 需要解密
		llog.L().Infof("source file is encrypted, decrypting...")

		// 4.1 获取加密盐
		if sourceLocation.EncryptionSalt == nil || *sourceLocation.EncryptionSalt == "" {
			llog.L().Errorf("encryption salt not found in source location")
			return
		}
		salt := *sourceLocation.EncryptionSalt

		// 4.2 获取存储提供者的加密密钥
		st := storage.GetStorage(*sourceLocation.ProviderId)
		if st == nil {
			llog.L().Errorf("storage provider not found: %d", *sourceLocation.ProviderId)
			return
		}

		orm := st.GetOrm()
		if orm == nil || orm.EncryptionKey == nil || *orm.EncryptionKey == "" {
			llog.L().Errorf("encryption key not found in storage provider")
			return
		}
		encryptionKey := *orm.EncryptionKey

		// 4.3 创建解密器
		decryptor, err := storage.NewCryptEncryptor(encryptionKey, salt)
		if err != nil {
			llog.L().Errorf("create decryptor failed: %v", err)
			return
		}

		// 4.4 解密文件到同一目录，添加"decrypted_"前缀
		tempDir := utils.GetFileTempDir(blake3Hash)
		originalFileName := filepath.Base(downloadedFilePath)
		decryptedFileName := "decrypted_" + originalFileName
		decryptedFilePath := filepath.Join(tempDir, decryptedFileName)

		if err := decryptFileTo(downloadedFilePath, decryptedFilePath, decryptor); err != nil {
			llog.L().Errorf("decrypt file failed: %v", err)
			return
		}
		finalFilePath = decryptedFilePath

		llog.L().Infof("file decrypted successfully: %s -> %s", downloadedFilePath, decryptedFilePath)
	}

	// 5. 更新上传记录的临时文件路径
	uploadRecord.TempPath = &finalFilePath
	uploadRecord.Status = utils.EnumPtr(native.LocationStatus_PUPLOADING)
	if err := db.GetDB().Save(uploadRecord).Error; err != nil {
		llog.L().Errorf("update upload record failed: %v", err)
		return
	}

	llog.L().Infof("resuming sync from download, hash: %s, file: %s", blake3Hash, finalFilePath)

	// 6. 调用 storage.Dispatch 继续上传流程
	// Dispatch 会检测到 TempPath 存在，从该文件批量上传到所有目标提供者
	storage.Dispatch(context.Background(), uploadRecord)

	llog.L().Infof("sync task resumed successfully, hash: %s", blake3Hash)
}

// decryptFileTo 解密文件到指定路径
func decryptFileTo(encryptedFilePath string, decryptedFilePath string, decryptor types.Encryptor) error {
	// 1. 打开加密文件
	encryptedFile, err := os.Open(encryptedFilePath)
	if err != nil {
		return fmt.Errorf("open encrypted file failed: %w", err)
	}
	defer encryptedFile.Close()

	// 2. 创建解密后的文件
	decryptedFile, err := os.Create(decryptedFilePath)
	if err != nil {
		return fmt.Errorf("create decrypted file failed: %w", err)
	}
	defer decryptedFile.Close()

	// 3. 解密数据
	decryptedReader, err := decryptor.Decrypt(encryptedFile)
	if err != nil {
		return fmt.Errorf("create decrypt reader failed: %w", err)
	}
	defer decryptedReader.Close()

	// 4. 复制解密后的数据到文件
	_, err = io.Copy(decryptedFile, decryptedReader)
	if err != nil {
		return fmt.Errorf("copy decrypted data failed: %w", err)
	}

	return nil
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
