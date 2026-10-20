package download

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"lukechampine.com/blake3"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/downloader"
	"jenken827.com/jenken827/pfm/internal/fileutil"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/pkg/downloader/aria2"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

// completeMu 串行化「完成处理」：完成通知与启动对齐（reconcile）可能同时进来
var completeMu sync.Mutex

// DNotifier 下载通知器
type DNotifier struct {
}

var _ = (aria2.Notifier)((*DNotifier)(nil))

// OnDownloadStart will be sent when a download is started.
func (d *DNotifier) OnDownloadStart(events []aria2.Event) {
	for _, e := range events {
		llog.L().Infof("download started, gid: %s", e.Gid)
		// 置「下载中」，否则列表在完成前一直显示「等待中」
		markDownloadStarted(e.Gid)
	}
}

// markDownloadStarted 置「下载中」；查不到记录（非 pfm 发起的 aria2 任务）静默跳过
func markDownloadStarted(gid string) {
	dr, err := db.FindDownloadRecordByGid(gid)
	if err != nil || dr == nil {
		return
	}
	state := int32(native.DownloadRecordState_DOWNLOADING)
	if dr.State != nil && *dr.State == state {
		return
	}
	dr.State = &state
	if err := db.UpdateDownloadRecord(dr, []string{"state"}); err != nil {
		llog.L().Errorf("update download record to downloading failed, gid: %s, err: %v", gid, err)
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
	reconcileDownloads()
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
	// gid 可能只是元数据那一环；真正的产物任务出错时只能靠对齐发现
	reconcileDownloads()
}

// OnBtDownloadComplete will be sent when a torrent download is complete but seeding is still going on.
func (d *DNotifier) OnBtDownloadComplete(events []aria2.Event) {
	for _, e := range events {
		llog.L().Infof("bt download completed (seeding), gid: %s", e.Gid)
		handleDownloadComplete(e.Gid)
	}
}

// handleDownloadComplete 处理下载完成事件。
//
// 不直接按 gid 导入：磁力 / .torrent 的 gid 可能只是「元数据 / 种子文件」那一环，
// 真正的产物在 followedBy 派生出的任务里。统一交给 reconcileDownloads 用
// infoHash + 任务形态解析产物（幂等：已是终态的记录不会再被处理）。
func handleDownloadComplete(_ string) {
	reconcileDownloads()
}

// markDownloadCompleted 落库完成信息（主文件路径 + 总大小），成功返回 nil
func markDownloadCompleted(dr *native.DownloadRecordORM, gid, primaryPath, totalLength string) error {
	dr.FilePath = &primaryPath
	dr.State = (*int32)(native.DownloadRecordState_DOWNLOAD_COMPLETED.Enum())
	if totalLength != "" {
		if size, err := strconv.ParseUint(totalLength, 10, 64); err == nil {
			dr.FileSize = &size
		}
	}
	if err := db.UpdateDownloadRecord(dr, []string{"file_path", "state", "file_size"}); err != nil {
		llog.L().Errorf("update download record failed, gid: %s, err: %v", gid, err)
		return err
	}
	return nil
}

// importOfflineDownload URL 离线下载完成后的入库链路（任务14）
//
// aria2 已把文件下载到 .offline/ 目录，单文件直接入库；多文件种子
// （magnet/BT）在目标目录下建一个以种子名命名的子目录，再按相对路径还原结构。
// 每个文件的入库链路：
//  1. 计算 blake3（增量，避免大文件整读内存）
//  2. 把文件移入 staging 临时目录（TempDir/users/<owner>/<hash>/chunks/，供 Dispatch 上传，
//     上传完成后由 completeUpload 清理——与普通上传生命周期一致）
//  3. 检查目标目录是否已存在同 hash 文件（秒传跳过上传）
//  4. 取 UserSpace 绑定 providers（取不到即返回，不留孤儿记录）
//  5. 位置行不变式：落库前为新 hash 预建 file_location 行（见
//     storage.PreallocateFileLocations），消除「记录已指向新 hash、位置行未落库」窗口的读取硬 500
//  6. 创建 File 记录（owner/parent 来自 DownloadRecord）
//  7. 创建 UploadRecord → Dispatch
func importOfflineDownload(dr *native.DownloadRecordORM, task aria2.StatusInfo, files []string) {
	if dr.OwnerId == nil || *dr.OwnerId == "" || dr.ParentId == nil || *dr.ParentId == "" {
		llog.L().Errorf("offline download record missing owner/parent, download_record_id: %v", dr.Id)
		return
	}
	if len(files) == 0 {
		markImportFailed(dr, fmt.Errorf("没有可导入的文件"))
		return
	}
	if len(files) == 1 {
		// 单文件：沿用 sourceName（URL 推断的名字通常比磁盘文件名更准确）
		fileName := filepath.Base(files[0])
		if dr.SourceName != nil {
			if name := safeSegment(*dr.SourceName); name != "" {
				fileName = name
			}
		}
		if err := importOneFile(dr, files[0], *dr.ParentId, fileName); err != nil {
			markImportFailed(dr, err)
			return
		}
		markImportDone(dr)
		return
	}
	importTorrentFiles(dr, task, files)
}

// importTorrentFiles 多文件种子导入：目标目录下建种子名子目录，按相对路径逐级还原子目录
func importTorrentFiles(dr *native.DownloadRecordORM, task aria2.StatusInfo, files []string) {
	ownerID := *dr.OwnerId
	rootName := torrentRootName(dr, task.BitTorrent.Info.Name, files, task.Dir)
	rootID, err := ensureFolderPath(ownerID, *dr.ParentId, []string{rootName})
	if err != nil {
		markImportFailed(dr, fmt.Errorf("创建种子目录 %s 失败: %w", rootName, err))
		return
	}

	imported := 0
	var firstErr error
	for _, filePath := range files {
		if importErr := importTorrentEntry(dr, task.Dir, rootID, rootName, filePath); importErr != nil {
			llog.L().Errorf("offline download import file failed, file: %s, err: %v", filePath, importErr)
			if firstErr == nil {
				firstErr = importErr
			}
			continue
		}
		imported++
	}
	llog.L().Infof("offline download torrent import done, record: %v, root: %s, total: %d, imported: %d",
		dr.Id, rootName, len(files), imported)

	switch {
	case imported == 0:
		markImportFailed(dr, fmt.Errorf("种子内 %d 个文件全部入库失败: %v", len(files), firstErr))
	case imported < len(files):
		markImportDone(dr)
		llog.L().Warnf("offline download torrent partially imported, record: %v, failed: %d/%d, first error: %v",
			dr.Id, len(files)-imported, len(files), firstErr)
	default:
		markImportDone(dr)
	}
	// 清理 aria2 留下的空目录（文件已 rename 进 staging，只剩目录骨架）
	_ = os.RemoveAll(filepath.Join(task.Dir, rootName))
}

// importTorrentEntry 导入种子里的单个文件（自动建中间目录）
func importTorrentEntry(dr *native.DownloadRecordORM, dir, rootID, rootName, filePath string) error {
	segs := relativeSegments(dir, filePath)
	if len(segs) > 1 && segs[0] == rootName {
		segs = segs[1:]
	}
	fileName := filepath.Base(filePath)
	if len(segs) > 0 {
		fileName = segs[len(segs)-1]
	}
	parentID := rootID
	if len(segs) > 1 {
		var err error
		parentID, err = ensureFolderPath(*dr.OwnerId, rootID, segs[:len(segs)-1])
		if err != nil {
			return fmt.Errorf("创建子目录失败: %w", err)
		}
	}
	return importOneFile(dr, filePath, parentID, fileName)
}

// importOneFile 单个文件入库：hash → staging → 秒传 → File 记录 → UploadRecord → Dispatch
func importOneFile(dr *native.DownloadRecordORM, downloadedFilePath, parentID, fileName string) error {
	ownerID := *dr.OwnerId

	fileInfo, err := os.Stat(downloadedFilePath)
	if err != nil {
		return fmt.Errorf("读取文件信息失败: %w", err)
	}
	fileSize := uint64(fileInfo.Size())
	if fileSize == 0 {
		// aria2 对空文件不会真正落盘验证，空文件入网盘没有意义，直接跳过
		return fmt.Errorf("文件为空: %s", downloadedFilePath)
	}

	hash, err := computeBlake3(downloadedFilePath)
	if err != nil {
		return fmt.Errorf("hash 计算失败: %w", err)
	}
	llog.L().Infof("offline download hash computed: %s, file: %s", hash, downloadedFilePath)

	stagingPath, err := moveToStaging(downloadedFilePath, hash, ownerID, fileName)
	if err != nil {
		return err
	}

	// 秒传：目标目录已存在同名同 hash → 无需上传
	if existing, qErr := db.GetFileByParentAndName(parentID, fileName, ownerID); qErr == nil &&
		existing != nil && existing.Hash != nil && *existing.Hash == hash {
		llog.L().Infof("offline download: same content exists in target dir, skip upload: %s", fileName)
		_ = os.Remove(stagingPath)
		return nil
	}
	return createFileAndDispatch(ownerID, parentID, fileName, hash, fileSize, stagingPath)
}

// moveToStaging 把下载文件移入 staging（owner 隔离，与普通上传共用生命周期）
func moveToStaging(downloadedFilePath, hash, ownerID, fileName string) (string, error) {
	stagingDir := utils.GetChunkTempDir(hash, ownerID)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return "", fmt.Errorf("创建 staging 目录失败: %w", err)
	}
	stagingPath := filepath.Join(stagingDir, utils.MergedFileName(fileName))
	if err := os.Rename(downloadedFilePath, stagingPath); err != nil {
		// Windows/跨盘等 rename 失败场景退化为复制
		if cErr := copyFile(downloadedFilePath, stagingPath); cErr != nil {
			return "", fmt.Errorf("移动到 staging 失败: %v / %v", err, cErr)
		}
		_ = os.Remove(downloadedFilePath)
	}
	return stagingPath, nil
}

// createFileAndDispatch 建 File 记录 + UploadRecord，交给 Dispatch 分发
func createFileAndDispatch(ownerID, parentID, fileName, hash string, fileSize uint64, stagingPath string) error {
	providerIds, pErr := storage.ResolveProviderIds(ownerID, parentID)
	if pErr != nil || len(providerIds) == 0 {
		_ = os.Remove(stagingPath)
		return fmt.Errorf("解析目标目录存储提供者失败: %v", pErr)
	}

	// 位置行不变式：落库前先预建位置行（见 storage.PreallocateFileLocations）
	storage.PreallocateFileLocations(context.Background(), hash, providerIds)

	fileType, mime := fileutil.GetFileType(stagingPath, fileName)
	isReady := false
	newFile := &native.FileORM{
		Name:     &fileName,
		ParentId: &parentID,
		Size:     &fileSize,
		Hash:     &hash,
		Type:     utils.Int32Ptr(fileType),
		Mime:     &mime,
		IsReady:  &isReady,
		OwnerId:  &ownerID,
	}
	if err := db.CreateFile(newFile); err != nil {
		_ = os.Remove(stagingPath)
		return fmt.Errorf("创建文件记录失败: %w", err)
	}
	llog.L().Infof("offline download file record created, id: %s, name: %s", *newFile.Id, fileName)

	providerIdsJSON, _ := json.Marshal(providerIds)
	targetProviderIds := string(providerIdsJSON)
	uploadRecord := &native.UploadRecordORM{
		FileHash:          &hash,
		FileName:          &fileName,
		FileSize:          &fileSize,
		ParentId:          &parentID,
		TempPath:          &stagingPath,
		OverwriteId:       utils.StringPtr(""),
		Status:            utils.EnumPtr(native.LocationStatus_PPENDING),
		TaskStatus:        utils.EnumPtr(native.UploadStatus_PENDING),
		TargetProviderIds: &targetProviderIds,
		TaskType:          utils.EnumPtr(native.UploadTaskType_UPLOAD),
		OwnerId:           &ownerID,
	}
	if err := db.GetDB().Create(uploadRecord).Error; err != nil {
		return fmt.Errorf("创建上传记录失败: %w", err)
	}
	llog.L().Infof("offline download dispatching, file: %s, hash: %s, providers: %s", fileName, hash, targetProviderIds)
	go storage.Dispatch(context.Background(), uploadRecord)
	return nil
}

// markImportDone 入库成功 → 记录置完成（按记录更新，元数据 gid 换过也不影响）
func markImportDone(dr *native.DownloadRecordORM) {
	state := int32(native.DownloadRecordState_DOWNLOAD_COMPLETED)
	dr.State = &state
	if err := db.UpdateDownloadRecord(dr, []string{"state"}); err != nil {
		llog.L().Errorf("offline download mark completed failed, record: %v, err: %v", dr.Id, err)
	}
}

// markImportFailed 入库失败 → 记录置失败并写入可读原因（前端可据此提示，也可重试）
func markImportFailed(dr *native.DownloadRecordORM, err error) {
	llog.L().Errorf("offline download import failed, record: %v, err: %v", dr.Id, err)
	msg := err.Error()
	state := int32(native.DownloadRecordState_DOWNLOAD_FAILED)
	dr.State = &state
	dr.ErrorMessage = &msg
	if uErr := db.UpdateDownloadRecord(dr, []string{"state", "error_message"}); uErr != nil {
		llog.L().Errorf("offline download mark failed error, record: %v, err: %v", dr.Id, uErr)
	}
}

// computeBlake3 增量计算文件 blake3（大文件安全）
func computeBlake3(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := blake3.New(32, nil)
	buf := make([]byte, 1024*1024)
	for {
		n, rErr := f.Read(buf)
		if n > 0 {
			_, _ = h.Write(buf[:n])
		}
		if rErr == io.EOF {
			break
		}
		if rErr != nil {
			return "", rErr
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyFile 复制文件（rename 跨设备失败时兜底）
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
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

		pcfg := st.GetProviderConfig()
		if pcfg == nil || pcfg.EncryptionKey == "" {
			llog.L().Errorf("encryption key not found in storage provider")
			return
		}
		encryptionKey := pcfg.EncryptionKey

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
	uploadRecord.TaskStatus = utils.EnumPtr(native.UploadStatus_UPLOADING)
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

	info, err := downloader.TellStatus(gid, "errorMessage", "errorCode")
	if err != nil {
		return err.Error()
	}
	if info.ErrorMessage != "" {
		return info.ErrorMessage
	}
	if info.ErrorCode != "" && info.ErrorCode != "0" {
		return "aria2 error code: " + info.ErrorCode
	}
	return "unknown error"
}
