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

	// URL 离线下载任务（任务14）：url 非空且无中转 upload_record → 入库
	// 中转同步任务：provider→provider（有 upload_record_id）→ 原有恢复逻辑
	if dr.Url != nil && *dr.Url != "" && (dr.UploadRecordId == nil || *dr.UploadRecordId == 0) {
		importOfflineDownload(dr, filePath)
		return
	}

	// 恢复同步任务：从下载的文件批量上传到目标提供者
	resumeSyncFromDownload(dr, filePath)
}

// importOfflineDownload URL 离线下载完成后的入库链路（任务14）
//
// aria2 已把文件下载到 .offline/ 目录，这里：
//  1. 计算 blake3（增量，避免大文件整读内存）
//  2. 把文件移入 staging 临时目录（TempDir/users/<owner>/<hash>/chunks/，供 Dispatch 上传，
//     上传完成后由 completeUpload 清理——与普通上传生命周期一致）
//  3. 检查目标目录是否已存在同 hash 文件（秒传跳过上传）
//  4. 取 UserSpace 绑定 providers（取不到即返回，不留孤儿记录）
//  5. 位置行不变式：落库前为新 hash 预建 file_location 行（见
//     storage.PreallocateFileLocations），消除「记录已指向新 hash、位置行未落库」窗口的读取硬 500
//  6. 创建 File 记录（owner/parent 来自 DownloadRecord）
//  7. 创建 UploadRecord → Dispatch
func importOfflineDownload(dr *native.DownloadRecordORM, downloadedFilePath string) {
	if dr.OwnerId == nil || *dr.OwnerId == "" || dr.ParentId == nil || *dr.ParentId == "" {
		llog.L().Errorf("offline download record missing owner/parent, download_record_id: %v", dr.Id)
		return
	}
	ownerID := *dr.OwnerId
	parentID := *dr.ParentId

	// 1. 计算 hash（增量流式）
	hash, err := computeBlake3(downloadedFilePath)
	if err != nil {
		llog.L().Errorf("offline download compute hash failed, file: %s, err: %v", downloadedFilePath, err)
		updateDownloadRecordState(*dr.Gid, native.DownloadRecordState_DOWNLOAD_FAILED, "hash 计算失败: "+err.Error())
		return
	}
	llog.L().Infof("offline download hash computed: %s, file: %s", hash, downloadedFilePath)

	// 文件信息
	fileInfo, err := os.Stat(downloadedFilePath)
	if err != nil {
		llog.L().Errorf("offline download stat failed, file: %s, err: %v", downloadedFilePath, err)
		return
	}
	fileSize := uint64(fileInfo.Size())
	fileName := filepath.Base(downloadedFilePath)
	if dr.SourceName != nil && *dr.SourceName != "" {
		fileName = *dr.SourceName
	}

	// 2. 移入 staging（owner 隔离：TempDir/users/<ownerID>/<hash>/chunks/）：
	//    与普通上传分片目录同布局，避免多用户并发下载同一内容互相覆盖；
	//    Dispatch 上传完成后清理，进行中 janitor 不回收
	stagingDir := utils.GetChunkTempDir(hash, ownerID)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		llog.L().Errorf("offline download create staging dir failed: %v", err)
		return
	}
	stagingPath := filepath.Join(stagingDir, utils.MergedFileName(fileName))
	if err := os.Rename(downloadedFilePath, stagingPath); err != nil {
		// Windows/跨盘等 rename 失败场景退化为复制
		if cErr := copyFile(downloadedFilePath, stagingPath); cErr != nil {
			llog.L().Errorf("offline download move to staging failed: %v / %v", err, cErr)
			return
		}
		_ = os.Remove(downloadedFilePath)
	}

	// 3. 检查目标目录同名同 hash（秒传：内容已在该目录 → 无需上传）
	if existing, qErr := db.GetFileByParentAndName(parentID, fileName, ownerID); qErr == nil &&
		existing != nil && existing.Hash != nil && *existing.Hash == hash {
		llog.L().Infof("offline download: same content exists in target dir, skip upload: %s", fileName)
		_ = os.Remove(stagingPath)
		updateDownloadRecordState(*dr.Gid, native.DownloadRecordState_DOWNLOAD_COMPLETED, "")
		return
	}

	// 4. 创建 File 记录
	fileType, mime := fileutil.GetFileType(stagingPath, fileName)
	// 4. 绑定 providers（必须先取：位置行预建与 UploadRecord 都要用，
	//    且取不到时直接返回，避免留下「有文件记录但没有分发」的孤儿）
	// 目标目录绑定的存储提供者（目录绑定 → 祖先继承 → 用户分配集）
	providerIds, pErr := storage.ResolveProviderIds(ownerID, parentID)
	if pErr != nil || len(providerIds) == 0 {
		llog.L().Errorf("offline download: resolve providers failed for owner %s, dir %s, err: %v", ownerID, parentID, pErr)
		_ = os.Remove(stagingPath)
		return
	}

	// 5. 位置行不变式：落库（文件记录指向新 hash）之前先预建位置行，
	//    否则从落库到异步分发首次上传成功之间的读取会命中
	//    len(locations)==0 硬 500（详见 storage.PreallocateFileLocations）
	storage.PreallocateFileLocations(context.Background(), hash, providerIds)

	// 6. 创建 File 记录
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
		llog.L().Errorf("offline download create file record failed: %v", err)
		return
	}
	llog.L().Infof("offline download file record created, id: %s, name: %s", *newFile.Id, fileName)

	// 7. 创建 UploadRecord → Dispatch
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
		llog.L().Errorf("offline download create upload record failed: %v", err)
		return
	}
	llog.L().Infof("offline download dispatching, file: %s, hash: %s, providers: %s", fileName, hash, targetProviderIds)
	go storage.Dispatch(context.Background(), uploadRecord)
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

	_, err := downloader.TellStatus(gid, "errorMessage", "errorCode")
	if err != nil {
		return err.Error()
	}
	return "unknown error"
}
