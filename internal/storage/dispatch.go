package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

const (
	// MaxConcurrentUploads 最大并发上传数
	MaxConcurrentUploads = 10
)

// uploadKey 并发控制键
type uploadKey struct {
	blake3Hash string
	providerID uint32
}

// dispatcher 分发器
type dispatcher struct {
	processing sync.Map      // map[uploadKey]chan struct{}
	semaphore  chan struct{} // 并发控制信号量
}

var globalDispatcher = &dispatcher{
	semaphore: make(chan struct{}, MaxConcurrentUploads),
}

// Dispatch 分发处理（文件上传完成后调用）
// 支持两种场景：
// 1. 普通上传：有临时文件，从临时文件上传
// 2. 同步任务：无临时文件，从现有提供者同步
func Dispatch(ctx context.Context, ur *native.UploadRecordORM) {
	// 1. 根据 task_type 判断任务类型
	isSyncTask := ur.TaskType != nil && *ur.TaskType == int32(native.UploadTaskType_SYNC)

	// 2. 如果是普通上传任务，检查临时文件是否存在
	if !isSyncTask {
		if ur.TempPath == nil || *ur.TempPath == "" {
			markUploadFailed(ctx, ur, "temp file path is empty for upload task")
			return
		}
		if _, err := os.Stat(*ur.TempPath); err != nil {
			markUploadFailed(ctx, ur, fmt.Sprintf("temp file not found: %s", *ur.TempPath))
			return
		}
	}

	// 4. 解析目标提供者列表
	var targetProviderIds []uint32
	if ur.TargetProviderIds != nil && *ur.TargetProviderIds != "" {
		if err := json.Unmarshal([]byte(*ur.TargetProviderIds), &targetProviderIds); err != nil {
			markUploadFailed(ctx, ur, fmt.Sprintf("parse target provider ids failed: %v", err))
			return
		}
	}

	if len(targetProviderIds) == 0 {
		markUploadFailed(ctx, ur, "no target providers specified")
		return
	}

	// 5. 更新状态为上传中
	markUploadUploading(ctx, ur)

	// 6. 分发到各个存储提供者
	dispatchToProviders(ctx, ur, targetProviderIds, isSyncTask)
}

// dispatchToProviders 分发上传任务到各存储提供者
func dispatchToProviders(ctx context.Context, ur *native.UploadRecordORM, targetProviderIds []uint32, isSyncTask bool) {
	// 1. 查询文件已经在哪些存储提供者上
	existingLocations, err := db.GetFileLocationsByBlake3Hash(*ur.FileHash)
	if err != nil {
		llog.L().Errorf("get existing file locations failed: %v", err)
	}

	// 2. 构建已存在的提供者映射（按提供者分组）
	providerChunksMap := make(map[uint32][]*native.FileLocationORM)
	for _, loc := range existingLocations {
		providerChunksMap[*loc.ProviderId] = append(providerChunksMap[*loc.ProviderId], loc)
	}

	// 3. 检查每个提供者的所有分片是否都完成
	completedProviderMap := make(map[uint32]bool)
	for providerID, chunks := range providerChunksMap {
		if len(chunks) == 0 {
			continue
		}

		// 获取预期的分片总数（从第一个分片获取）
		expectedChunkTotal := *chunks[0].ChunkTotal

		// 检查分片数量是否正确
		if len(chunks) != int(expectedChunkTotal) {
			llog.L().Warnf("provider %d has incomplete chunks: expected %d, got %d",
				providerID, expectedChunkTotal, len(chunks))
			continue
		}

		// 检查所有分片是否都完成
		allCompleted := true
		for _, chunk := range chunks {
			if *chunk.Status != int32(native.LocationStatus_PCOMPLETED) {
				allCompleted = false
				llog.L().Debugf("provider %d chunk %d not completed, status: %d",
					providerID, *chunk.ChunkNumber, *chunk.Status)
				break
			}
		}

		if allCompleted {
			completedProviderMap[providerID] = true
			llog.L().Infof("provider %d is available with all %d chunks completed",
				providerID, expectedChunkTotal)
		}
	}

	// 3. 计算需要上传的提供者
	var needUploadProviders []uint32
	for _, pid := range targetProviderIds {
		if !completedProviderMap[pid] {
			needUploadProviders = append(needUploadProviders, pid)
		}
	}

	// 4. 如果所有提供者都已有文件，直接完成
	if len(needUploadProviders) == 0 {
		llog.L().Infof("file already exists in all target providers, hash: %s", *ur.FileHash)
		completeUpload(ur)
		return
	}

	// 5. 根据任务类型选择不同的上传策略
	if isSyncTask {
		// 同步任务：必须有已完成的提供者才能同步
		if len(completedProviderMap) == 0 {
			llog.L().Errorf("sync task has no source provider, hash: %s", *ur.FileHash)
			markUploadFailed(ctx, ur, "no available source provider for sync task")
			return
		}
		// 从现有提供者同步
		syncFromExistingProviders(ctx, ur, needUploadProviders, existingLocations)
	} else {
		// 普通上传：从临时文件上传
		uploadFromTempFile(ctx, ur, needUploadProviders)
	}
}

// uploadFromTempFile 从临时文件上传
func uploadFromTempFile(ctx context.Context, ur *native.UploadRecordORM, needUploadProviders []uint32) {
	// 临时文件存在，直接从临时文件批量上传到所有目标提供者
	batchUploadFromSource(ctx, ur, needUploadProviders, *ur.TempPath)
}

// syncFromExistingProviders 从现有副本同步到新提供者
func syncFromExistingProviders(ctx context.Context, ur *native.UploadRecordORM, needUploadProviders []uint32, existingLocations []*native.FileLocationORM) {
	// 1. 选择最优源提供者（支持分片完整性检查）
	sourceProviderID, sourceLocations, err := SelectBestSourceProvider(existingLocations)
	if err != nil {
		llog.L().Errorf("select best source provider failed: %v", err)
		markUploadFailed(ctx, ur, "no available source provider")
		return
	}

	llog.L().Infof("selected source provider: %d with %d chunks", sourceProviderID, len(sourceLocations))

	// 2. 获取源存储实例
	sourceStorage := GetStorage(sourceProviderID)
	if sourceStorage == nil {
		markUploadFailed(ctx, ur, fmt.Sprintf("source storage not found: %d", sourceProviderID))
		return
	}

	// 3. 创建加密器（用于解密，如果需要）
	var encryptor types.Encryptor
	if sourceLocations[0].Encrypted != nil && *sourceLocations[0].Encrypted {
		// 从存储提供者配置中获取加密密钥
		providerOrm := sourceStorage.GetOrm()
		if providerOrm != nil && providerOrm.EncryptionKey != nil {
			salt := ""
			if sourceLocations[0].EncryptionSalt != nil {
				salt = *sourceLocations[0].EncryptionSalt
			}
			// 创建加密器用于解密
			encryptor, err = NewCryptEncryptor(*providerOrm.EncryptionKey, salt)
			if err != nil {
				markUploadFailed(ctx, ur, fmt.Sprintf("create encryptor failed: %v", err))
				return
			}
		}
	}

	// 4. 下载并合并所有分片到临时文件
	tempFilePath, err := DownloadAndMergeFile(ctx, sourceStorage, sourceLocations, encryptor, *ur.FileHash)
	if err != nil {
		llog.L().Errorf("download and merge file failed: %v", err)
		markUploadFailed(ctx, ur, fmt.Sprintf("download file failed: %v", err))
		return
	}
	defer os.Remove(tempFilePath) // 确保临时文件最终被删除

	llog.L().Infof("file downloaded and merged to: %s", tempFilePath)

	// 5. 批量上传到所有目标提供者（并发）
	batchUploadFromSource(ctx, ur, needUploadProviders, tempFilePath)
}

// isLocalPath 判断路径是否为本地路径
// func isLocalPath(path string) bool {
// 	// 判断是否为本地文件路径
// 	// 支持以下格式：
// 	// - Unix路径: /path/to/file
// 	// - Windows路径: C:\path\to\file, D:/path/to/file
// 	// - 相对路径: ./path/to/file, path/to/file

// 	if path == "" {
// 		return false
// 	}

// 	// 检查是否包含 :// 协议标识符（排除 Windows 盘符如 C:）
// 	// Windows 盘符格式: C:\path 或 C:/path
// 	// URL 格式: http://, https://, ftp:// 等
// 	if len(path) >= 2 {
// 		// Windows 盘符检查：第二个字符是冒号且后面跟着反斜杠或斜杠
// 		if path[1] == ':' && (len(path) < 3 || path[2] == '\\' || path[2] == '/') {
// 			return true
// 		}
// 	}

// 	// 检查是否包含协议标识符
// 	for i := 0; i < len(path); i++ {
// 		if path[i] == ':' {
// 			// 如果冒号后面跟着 //，则是URL协议
// 			if i+2 < len(path) && path[i+1] == '/' && path[i+2] == '/' {
// 				return false
// 			}
// 		}
// 	}

// 	// 以 / 或 . 开头的路径视为本地路径
// 	if path[0] == '/' || path[0] == '.' {
// 		return true
// 	}

// 	// 其他情况，假设为本地路径（保守估计）
// 	return true
// }

// triggerAsyncDownload 触发异步下载任务
// func triggerAsyncDownload(ur *native.UploadRecordORM, downloadLink string, sourceLocation *native.FileLocationORM) error {
// 	// 1. 创建文件临时目录
// 	if err := utils.CreateFileTempDir(*ur.FileHash, false); err != nil {
// 		llog.L().Errorf("create file temp dir failed: %v", err)
// 		return err
// 	}

// 	// 2. 创建下载记录（只记录必要信息）
// 	downloadRecord := &native.DownloadRecordORM{
// 		Blake3Hash:       ur.FileHash,
// 		SourceLocationId: sourceLocation.Id,
// 		UploadRecordId:   ur.Id,
// 		State:            (*int32)(native.DownloadRecordState_DOWNLOAD_PENDING.Enum()),
// 	}

// 	if err := db.CreateDownloadRecord(downloadRecord); err != nil {
// 		llog.L().Errorf("create download record failed: %v", err)
// 		return err
// 	}

// 	// 3. 调用 aria2 添加下载任务
// 	if downloader.Aria2Downloader == nil {
// 		llog.L().Errorf("aria2 downloader not initialized")
// 		return fmt.Errorf("aria2 downloader not initialized")
// 	}

// 	// 下载到文件临时目录
// 	downloadDir := utils.GetFileTempDir(*ur.FileHash)
// 	gid, err := downloader.Aria2Downloader.AddURI([]string{downloadLink}, aria2.Option{
// 		"dir": downloadDir,
// 	})
// 	if err != nil {
// 		llog.L().Errorf("add aria2 download task failed: %v", err)
// 		return err
// 	}

// 	// 4. 更新下载记录的 GID
// 	downloadRecord.Gid = &gid
// 	downloadRecord.State = (*int32)(native.DownloadRecordState_DOWNLOADING.Enum())
// 	if err := db.UpdateDownloadRecord(downloadRecord, []string{"gid", "state"}); err != nil {
// 		llog.L().Errorf("update download record gid failed: %v", err)
// 		return err
// 	}

// 	llog.L().Infof("download task created, gid: %s, record_id: %d, hash: %s, dir: %s, waiting for aria2 callback", gid, *downloadRecord.Id, *ur.FileHash, downloadDir)

// 	// 5. 返回成功，等待 aria2 下载完成回调
// 	return nil
// }

// batchUploadFromSource 从源文件批量上传到多个目标提供者
func batchUploadFromSource(ctx context.Context, ur *native.UploadRecordORM, targetProviderIds []uint32, sourcePath string) {
	// 使用 WaitGroup 并发上传
	var wg sync.WaitGroup
	var failedCount int32
	var errorMessages []string
	var errorMu sync.Mutex // 保护 errorMessages 的并发访问

	// 获取文件名和父目录ID
	fileName := "unknown"
	parentID := ""
	if ur.FileName != nil {
		fileName = *ur.FileName
	}
	if ur.ParentId != nil {
		parentID = *ur.ParentId
	}

	// 查询父文件夹的加密设置
	var fileEncrypt bool
	if parentID != "" {
		parentFolder, err := db.GetFileByID(parentID)
		if err == nil && parentFolder != nil && parentFolder.EnableEncryption != nil {
			fileEncrypt = *parentFolder.EnableEncryption
			llog.L().Infof("parent folder encryption setting: %v, parent_id: %s, file: %s", fileEncrypt, parentID, fileName)
		}
	}

	for _, targetID := range targetProviderIds {
		// 获取并发控制信号量（阻塞等待可用槽位）
		globalDispatcher.semaphore <- struct{}{}

		wg.Add(1)
		go func(tid uint32) {
			defer wg.Done()
			defer func() { <-globalDispatcher.semaphore }() // 释放信号量

			// 并发控制
			key := uploadKey{
				blake3Hash: *ur.FileHash,
				providerID: tid,
			}

			if !acquireLock(key) {
				llog.L().Infof("upload already in progress, hash: %s, provider: %d", *ur.FileHash, tid)
				return
			}
			defer releaseLock(key)

			// 获取或创建文件位置记录
			fl := getOrCreateFileLocation(tid, *ur.FileHash)
			if fl == nil {
				failedCount++
				errorMu.Lock()
				errorMessages = append(errorMessages, fmt.Sprintf("provider %d: failed to create file location", tid))
				errorMu.Unlock()
				return
			}

			// 如果已经完成，跳过
			if *fl.Status == int32(native.LocationStatus_PCOMPLETED) {
				llog.L().Infof("file already exists in provider %d", tid)
				return
			}

			// 上传到目标提供者（加密器在uploadToProvider内部生成）
			// 错误信息已在 uploadToProvider 中记录到 FileLocation.message
			err := uploadToProvider(ctx, sourcePath, fileName, tid, fl, fileEncrypt)
			if err != nil {
				llog.L().Errorf("upload to provider %d failed: %v", tid, err)
				failedCount++
				// 错误信息已记录在 FileLocation.message，无需重复记录
			} else {
				// 上传成功，立即更新文件状态为就绪
				if parentID != "" && fileName != "" {
					if updateErr := db.UpdateFileIsReady(parentID, fileName); updateErr != nil {
						llog.L().Errorf("update file is_ready failed: %v, parent_id: %s, file_name: %s", updateErr, parentID, fileName)
					} else {
						llog.L().Infof("file marked as ready, parent_id: %s, file_name: %s, provider: %d", parentID, fileName, tid)
					}
				}
			}
		}(targetID)
	}

	// 等待所有上传完成
	wg.Wait()

	// 记录失败数量
	if failedCount > 0 {
		llog.L().Warnf("batch upload completed with %d failures, hash: %s", failedCount, *ur.FileHash)

		// 组合错误信息（仅记录那些没有创建 FileLocation 的错误）
		var errorMsg string
		if len(errorMessages) > 0 {
			errorMsg = fmt.Sprintf("%d providers failed: %s", failedCount, strings.Join(errorMessages, "; "))
			if len(errorMsg) > 500 {
				errorMsg = errorMsg[:500] + "..."
			}
		} else {
			// 所有失败都有对应的 FileLocation 记录，错误信息已在那里记录
			errorMsg = fmt.Sprintf("%d providers failed, see file locations for details", failedCount)
		}

		// 有失败的情况，更新上传记录状态为失败
		failedStatus := int32(native.LocationStatus_PFAILED)
		ur.Status = &failedStatus
		ur.Message = &errorMsg
		if err := db.UpdateUploadRecord(ur, []string{"status", "message"}); err != nil {
			llog.L().Errorf("update upload record status to failed error: %v, id: %d", err, *ur.Id)
		}
	} else {
		// 所有上传都完成，检查是否可以完成上传
		completeUpload(ur)
	}
}

// uploadToProvider 上传到单个提供者
// sourcePath: 源文件路径（明文）
// fileName: 目标文件名
// providerID: 目标存储提供者ID
// fileEncrypt: 文件是否启用加密（文件夹设置）
func uploadToProvider(ctx context.Context, sourcePath string, fileName string, providerID uint32, fl *native.FileLocationORM, fileEncrypt bool) error {
	// 更新状态为上传中
	fl.Status = utils.EnumPtr(native.LocationStatus_PUPLOADING)
	db.UpdateFileLocation(fl, []string{"status"})

	// 获取存储实例
	st := GetStorage(providerID)
	if st == nil {
		msg := fmt.Sprintf("storage provider not found, id: %d", providerID)
		fl.Message = &msg
		fl.Status = (*int32)(native.LocationStatus_PFAILED.Enum())
		db.UpdateFileLocation(fl, []string{"status", "message"})
		return errors.New(msg)
	}

	// 获取ORM对象
	orm := st.GetOrm()
	if orm == nil || !*orm.Enabled {
		msg := fmt.Sprintf("storage provider not enabled, id: %d", providerID)
		fl.Message = &msg
		fl.Status = (*int32)(native.LocationStatus_PFAILED.Enum())
		db.UpdateFileLocation(fl, []string{"status", "message"})
		return errors.New(msg)
	}

	// 检查源文件是否存在
	if _, err := os.Stat(sourcePath); err != nil {
		msg := fmt.Sprintf("source file not found: %s", sourcePath)
		fl.Message = &msg
		fl.Status = (*int32)(native.LocationStatus_PFAILED.Enum())
		db.UpdateFileLocation(fl, []string{"status", "message"})
		return errors.New(msg)
	}

	// 生成加密器（如果需要加密）
	// 加密条件：文件启用加密 且 存储提供者配置了加密密钥
	var encryptor types.Encryptor = &types.NullEncryptor{}
	var saltStr string

	if fileEncrypt && orm.EncryptionKey != nil && *orm.EncryptionKey != "" {
		// 文件启用加密 + 提供者配置密钥 = 加密

		llog.L().Infof("encryption enabled for file, provider: %d, file: %s", providerID, fileName)

		// 生成随机salt（32字节）
		saltBytes := make([]byte, 32)
		if _, err := rand.Read(saltBytes); err != nil {
			msg := fmt.Sprintf("generate salt failed: %v", err)
			fl.Message = &msg
			fl.Status = (*int32)(native.LocationStatus_PFAILED.Enum())
			db.UpdateFileLocation(fl, []string{"status", "message"})
			return errors.New(msg)
		}
		saltStr = hex.EncodeToString(saltBytes)

		// 创建加密器
		enc, err := NewCryptEncryptor(*orm.EncryptionKey, saltStr)
		if err != nil {
			msg := fmt.Sprintf("create encryptor failed: %v", err)
			fl.Message = &msg
			fl.Status = (*int32)(native.LocationStatus_PFAILED.Enum())
			db.UpdateFileLocation(fl, []string{"status", "message"})
			return errors.New(msg)
		}
		encryptor = enc

		// 标记为已加密
		encrypted := true
		fl.Encrypted = &encrypted
		fl.EncryptionSalt = &saltStr

		llog.L().Infof("file will be encrypted, provider: %d, salt: %s...", providerID, saltStr[:16])
	} else {
		// 确保标记为未加密
		encrypted := false
		fl.Encrypted = &encrypted
	}

	// 执行上传
	pinfo, err := st.Upload(ctx, sourcePath, fileName, encryptor)
	if err != nil {
		msg := err.Error()
		fl.Message = &msg
		fl.Status = (*int32)(native.LocationStatus_PFAILED.Enum())
		db.UpdateFileLocation(fl, []string{"status", "message"})
		return err
	}

	// 检查返回的分片信息
	if len(pinfo) == 0 {
		msg := "no file info returned from upload"
		fl.Message = &msg
		fl.Status = (*int32)(native.LocationStatus_PFAILED.Enum())
		db.UpdateFileLocation(fl, []string{"status", "message"})
		return errors.New(msg)
	}

	// 处理分片上传结果
	chunkTotal := uint32(len(pinfo))
	var totalUploadedSize uint64 // 统计总上传大小

	for i, chunkInfo := range pinfo {
		chunkNumber := uint32(i)

		if i == 0 {
			// 第一个分片：更新已有的 FileLocation 记录
			fl.ProviderFileId = &chunkInfo.ID
			if chunkInfo.Path != "" {
				fl.Path = &chunkInfo.Path
			}
			if chunkInfo.Hash != "" {
				fl.Hash = &chunkInfo.Hash
			}
			fl.Status = (*int32)(native.LocationStatus_PCOMPLETED.Enum())
			chunkTotalValue := chunkTotal
			fl.ChunkTotal = &chunkTotalValue
			fl.ChunkNumber = &chunkNumber
			if chunkInfo.Size > 0 {
				fl.Size = &chunkInfo.Size
			}
			if chunkInfo.ChunkSize > 0 {
				fl.ChunkSize = &chunkInfo.ChunkSize
				totalUploadedSize += chunkInfo.ChunkSize // 累加上传大小
			}

			// 更新字段列表
			updateFields := []string{"provider_file_id", "path", "hash", "status", "chunk_number", "chunk_total", "size", "chunk_size"}
			if fl.Encrypted != nil {
				updateFields = append(updateFields, "encrypted", "encryption_salt")
			}

			db.UpdateFileLocation(fl, updateFields)
			llog.L().Infof("chunk 1/%d uploaded, hash: %s, provider: %d, file_id: %s",
				chunkTotal, *fl.Blake3Hash, providerID, chunkInfo.ID)
		} else {
			// 后续分片：创建新的 FileLocation 记录
			newFl := &native.FileLocationORM{
				Blake3Hash:     fl.Blake3Hash,
				ProviderId:     fl.ProviderId,
				ProviderFileId: &chunkInfo.ID,
				Status:         (*int32)(native.LocationStatus_PCOMPLETED.Enum()),
				ChunkNumber:    &chunkNumber,
				ChunkTotal:     &chunkTotal,
			}

			if chunkInfo.Path != "" {
				newFl.Path = &chunkInfo.Path
			}
			if chunkInfo.Hash != "" {
				newFl.Hash = &chunkInfo.Hash
			}
			if chunkInfo.Size > 0 {
				newFl.Size = &chunkInfo.Size
			}
			if chunkInfo.ChunkSize > 0 {
				newFl.ChunkSize = &chunkInfo.ChunkSize
				totalUploadedSize += chunkInfo.ChunkSize // 累加上传大小
			}
			// // 复制加密信息
			// if fl.Encrypted != nil {
			// 	encrypted := *fl.Encrypted
			// 	newFl.Encrypted = &encrypted
			// 	newFl.EncryptionSalt = fl.EncryptionSalt
			// }

			if err := db.CreateFileLocation(newFl); err != nil {
				llog.L().Errorf("create file location for chunk %d failed: %v", chunkNumber, err)
				return err
			}

			llog.L().Infof("chunk %d/%d uploaded, hash: %s, provider: %d, file_id: %s",
				chunkNumber, chunkTotal, *fl.Blake3Hash, providerID, chunkInfo.ID)
		}
	}

	// 更新存储提供者的已使用容量
	if totalUploadedSize > 0 {
		if err := db.IncrementUsedCapacity(providerID, totalUploadedSize); err != nil {
			llog.L().Errorf("update used capacity failed: %v, provider_id: %d, size: %d", err, providerID, totalUploadedSize)
			// 不影响上传结果，仅记录错误
		}
	}

	llog.L().Infof("upload completed, hash: %s, provider: %d, encrypted: %v, total chunks: %d, size: %d",
		*fl.Blake3Hash, providerID, fl.Encrypted != nil, chunkTotal, totalUploadedSize)
	return nil
}

// getOrCreateFileLocation 获取或创建文件位置记录
func getOrCreateFileLocation(providerID uint32, blake3Hash string) *native.FileLocationORM {
	// 先查询是否已存在
	location, err := db.GetFileLocationByProviderAndHash(providerID, blake3Hash)
	if err == nil {
		return location
	}

	// 不存在则创建
	fl := &native.FileLocationORM{
		ProviderId: &providerID,
		Blake3Hash: &blake3Hash,
		Status:     (*int32)(native.LocationStatus_PPENDING.Enum()),
	}
	if err := db.CreateFileLocation(fl); err != nil {
		llog.L().Errorf("create file location failed: %v", err)
		return nil
	}

	return fl
}

// completeUpload 完成上传，清理临时文件
func completeUpload(ur *native.UploadRecordORM) {
	// 解析目标提供者列表
	var targetProviderIds []uint32
	if ur.TargetProviderIds != nil && *ur.TargetProviderIds != "" {
		if err := json.Unmarshal([]byte(*ur.TargetProviderIds), &targetProviderIds); err != nil {
			llog.L().Errorf("parse target provider ids failed: %v", err)
			return
		}
	}

	// 查询文件位置记录
	locations, err := db.GetFileLocationsByProviderAndHash(targetProviderIds, *ur.FileHash)
	if err != nil {
		llog.L().Errorf("get file locations failed: %v", err)
		return
	}

	// 检查是否所有目标提供者都已完成
	completedCount := 0
	for _, loc := range locations {
		if *loc.Status == int32(native.LocationStatus_PCOMPLETED) {
			completedCount++
		}
	}

	// 更新上传记录状态
	if completedCount == len(targetProviderIds) {
		// 所有上传都完成
		if ur.Id != nil {
			db.UpdateUploadRecord(&native.UploadRecordORM{Id: ur.Id, Status: utils.EnumPtr(native.LocationStatus_PCOMPLETED)}, []string{"status"})
		}

		// 清理整个文件临时目录
		if err := utils.CleanupFileTempDir(*ur.FileHash); err != nil {
			llog.L().Warnf("cleanup file temp dir failed: %v", err)

		}

		// 删除上传记录（仅当有ID时）
		if ur.Id != nil {
			if err := db.ClearUploadRecords(*ur.Id); err != nil {
				llog.L().Errorf("clear upload records failed: %v", err)
			}
		}

		llog.L().Infof("all uploads completed, hash: %s", *ur.FileHash)
	} else {
		llog.L().Infof("upload progress: %d/%d, hash: %s", completedCount, len(targetProviderIds), *ur.FileHash)
	}
}

// markUploadFailed 标记上传失败
func markUploadFailed(ctx context.Context, ur *native.UploadRecordORM, reason string) {
	if ur.Id == nil {
		llog.L().Errorf("upload failed (no record id): %s, hash: %s", reason, *ur.FileHash)
		return
	}

	// 更新状态和错误信息
	ur.Status = utils.EnumPtr(native.LocationStatus_PFAILED)
	ur.Message = &reason
	db.UpdateUploadRecord(ur, []string{"status", "message"})
	llog.L().Errorf("upload failed: %s, hash: %s", reason, *ur.FileHash)
}

// markUploadUploading 标记上传中
func markUploadUploading(ctx context.Context, ur *native.UploadRecordORM) {
	if ur.Id == nil {
		return
	}

	db.UpdateUploadRecord(&native.UploadRecordORM{Id: ur.Id, Status: utils.EnumPtr(native.LocationStatus_PUPLOADING)}, []string{"status"})
}

// acquireLock 获取并发控制锁（非阻塞）
func acquireLock(key uploadKey) bool {
	ch := make(chan struct{})
	_, loaded := globalDispatcher.processing.LoadOrStore(key, ch)
	if loaded {
		// 已经有任务在处理，直接返回false，不等待
		return false
	}
	return true
}

// releaseLock 释放并发控制锁
func releaseLock(key uploadKey) {
	if ch, ok := globalDispatcher.processing.Load(key); ok {
		close(ch.(chan struct{}))
		globalDispatcher.processing.Delete(key)
	}
}

// resumeDispatch 恢复分发（系统启动时调用）
func resumeDispatch() {
	// 从数据库恢复未完成的上传任务
	records, err := db.GetUploadRecordsByStatus([]native.LocationStatus{native.LocationStatus_PPENDING, native.LocationStatus_PUPLOADING})
	if err != nil {
		llog.L().Errorf("get upload record failed: %v", err)
		return
	}

	for _, record := range records {
		go Dispatch(context.Background(), record)
	}
}
