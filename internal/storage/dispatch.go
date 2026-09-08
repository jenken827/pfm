package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

// Dispatch 分发处理（文件上传完成后调用）
// 支持两种场景：
// 1. 普通上传：有临时文件，从临时文件上传
// 2. 同步任务：无临时文件，从现有提供者同步
func Dispatch(ctx context.Context, ur *native.UploadRecordORM) {
	defer func() {
		if r := recover(); r != nil {
			var fileHash string
			if ur != nil && ur.FileHash != nil {
				fileHash = *ur.FileHash
			}
			llog.L().Errorf("Dispatch panicked, file_hash: %s, recover: %v", fileHash, r)
		}
	}()

	// 1. 根据 task_type 判断任务类型
	isSyncTask := ur.TaskType != nil && *ur.TaskType == int32(native.UploadTaskType_SYNC)

	// 2. 如果是普通上传任务，检查临时文件是否存在
	if !isSyncTask {
		if ur.TempPath == nil || *ur.TempPath == "" {
			markUploadFailed(ctx, ur, "temp file path is empty for upload task")
			return
		}
		if _, err := os.Stat(*ur.TempPath); err != nil {
			// Fallback: 精确路径不存在时，扫描 hash 目录查找任意可用文件
			// 场景：用户 rename 重试上传，新 temp 文件名与 upload_record 中的旧文件名不同
			if ur.FileHash != nil {
				if found, ok := utils.FindTempFile(*ur.FileHash); ok {
					llog.L().Infof("temp file fallback: %s -> %s", *ur.TempPath, found)
					ur.TempPath = &found
				} else {
					markUploadFailed(ctx, ur, fmt.Sprintf("temp file not found: %s", *ur.TempPath))
					return
				}
			} else {
				markUploadFailed(ctx, ur, fmt.Sprintf("temp file not found: %s", *ur.TempPath))
				return
			}
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

// uploadFromTempFile 从临时文件上传
func uploadFromTempFile(ctx context.Context, ur *native.UploadRecordORM, needUploadProviders []uint32) {
	// 生成缩略图（在分发前完成）
	generateThumbnailForUpload(ctx, ur)

	// 临时文件存在，直接从临时文件批量上传到所有目标提供者
	batchUploadFromSource(ctx, ur, needUploadProviders, *ur.TempPath)
}

// syncFromExistingProviders 从现有副本同步到新提供者

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
		pcfg := sourceStorage.GetProviderConfig()
		if pcfg != nil && pcfg.EncryptionKey != "" {
			salt := ""
			if sourceLocations[0].EncryptionSalt != nil {
				salt = *sourceLocations[0].EncryptionSalt
			}
			// 创建加密器用于解密
			encryptor, err = NewCryptEncryptor(pcfg.EncryptionKey, salt)
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

	// 更新 upload record 的 TempPath，驱动从这里读取文件路径
	ur.TempPath = &tempFilePath

	llog.L().Infof("file downloaded and merged to: %s", tempFilePath)

	// 5. 批量上传到所有目标提供者（并发）
	batchUploadFromSource(ctx, ur, needUploadProviders, tempFilePath)
}

// batchUploadFromSource 从源文件批量上传到多个目标提供者

// batchUploadFromSource 从源文件批量上传到多个目标提供者
func batchUploadFromSource(ctx context.Context, ur *native.UploadRecordORM, targetProviderIds []uint32, sourcePath string) {
	fileName, parentID, fileEncrypt := getUploadContext(ur)

	var wg sync.WaitGroup
	var failedCount int32
	var errorMessages []string
	var errorMu sync.Mutex

	for _, targetID := range targetProviderIds {
		// 获取全局信号量
		globalDispatcher.globalSemaphore <- struct{}{}
		// 获取存储提供者信号量
		providerSem := getProviderSemaphore(targetID)
		providerSem <- struct{}{}

		wg.Add(1)
		go func(tid uint32, pSem chan struct{}) {
			defer wg.Done()
			defer func() { <-pSem }()                             // 释放提供者信号量
			defer func() { <-globalDispatcher.globalSemaphore }() // 释放全局信号量

			if failed, errMsg := uploadToProviderAsync(ctx, ur, sourcePath, fileName, parentID, fileEncrypt, tid); failed {
				atomic.AddInt32(&failedCount, 1)
				if errMsg != "" {
					errorMu.Lock()
					errorMessages = append(errorMessages, errMsg)
					errorMu.Unlock()
				}
			}
		}(targetID, providerSem)
	}

	wg.Wait()
	finalizeBatchUpload(ur, failedCount, errorMessages)
}

// getUploadContext 获取上传上下文信息（文件名、父目录ID、加密设置）

// getUploadContext 获取上传上下文信息（文件名、父目录ID、加密设置）
func getUploadContext(ur *native.UploadRecordORM) (fileName, parentID string, fileEncrypt bool) {
	fileName = "unknown"
	if ur.FileName != nil {
		fileName = *ur.FileName
	}
	if ur.ParentId != nil {
		parentID = *ur.ParentId
	}

	if parentID != "" {
		parentFolder, err := db.GetFileByID(parentID)
		if err == nil && parentFolder != nil && parentFolder.EnableEncryption != nil {
			fileEncrypt = *parentFolder.EnableEncryption
			llog.L().Infof("parent folder encryption setting: %v, parent_id: %s, file: %s", fileEncrypt, parentID, fileName)
		}
	}
	return
}

// uploadToProviderAsync 异步上传到单个提供者，返回是否失败和错误信息

// uploadToProviderAsync 异步上传到单个提供者，返回是否失败和错误信息
func uploadToProviderAsync(ctx context.Context, ur *native.UploadRecordORM, sourcePath, fileName, parentID string, fileEncrypt bool, tid uint32) (failed bool, errMsg string) {
	// 捕获 panic 并打印堆栈
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			llog.L().Errorf("[PANIC] uploadToProviderAsync panic: %v\nstack: %s", r, string(buf[:n]))
			failed = true
			errMsg = fmt.Sprintf("panic: %v", r)
		}
	}()

	key := uploadKey{blake3Hash: *ur.FileHash, providerID: tid}
	if !acquireLock(key) {
		llog.L().Infof("upload already in progress, hash: %s, provider: %d", *ur.FileHash, tid)
		return false, ""
	}
	defer releaseLock(key)

	fl, flErr := getOrCreateFileLocation(tid, *ur.FileHash)
	if flErr != nil {
		return true, fmt.Sprintf("provider %d: %v", tid, flErr)
	}

	if *fl.Status == int32(native.LocationStatus_PCOMPLETED) {
		llog.L().Infof("file already exists in provider %d", tid)
		return false, ""
	}

	err := uploadToProvider(ctx, sourcePath, ur, tid, fl, fileEncrypt)
	if err != nil {
		llog.L().Errorf("upload to provider %d failed: %v", tid, err)
		return true, ""
	}

	// 上传成功，立即更新文件状态为就绪
	if parentID != "" && fileName != "" {
		if updateErr := db.UpdateFileIsReady(parentID, fileName, ""); updateErr != nil {
			llog.L().Errorf("update file is_ready failed: %v, parent_id: %s, file_name: %s", updateErr, parentID, fileName)
		} else {
			llog.L().Infof("file marked as ready, parent_id: %s, file_name: %s, provider: %d", parentID, fileName, tid)
		}
	}
	return false, ""
}

// finalizeBatchUpload 完成批量上传，处理结果

// finalizeBatchUpload 完成批量上传，处理结果
func finalizeBatchUpload(ur *native.UploadRecordORM, failedCount int32, errorMessages []string) {
	if failedCount > 0 {
		llog.L().Warnf("batch upload completed with %d failures, hash: %s", failedCount, *ur.FileHash)

		var errorMsg string
		if len(errorMessages) > 0 {
			errorMsg = fmt.Sprintf("%d providers failed: %s", failedCount, strings.Join(errorMessages, "; "))
			if len(errorMsg) > 500 {
				errorMsg = errorMsg[:500] + "..."
			}
		} else {
			errorMsg = fmt.Sprintf("%d providers failed, see file locations for details", failedCount)
		}

		failedStatus := int32(native.UploadStatus_FAILED)
		ur.TaskStatus = &failedStatus
		ur.Message = &errorMsg
		// 乐观锁：仅当非 COMPLETED 时才标记失败，防止覆盖已完成状态
		if ur.Id != nil {
			_ = db.TransitionUploadRecordTaskStatus(*ur.Id,
				[]native.UploadStatus{native.UploadStatus_PENDING, native.UploadStatus_UPLOADING, native.UploadStatus_FAILED},
				native.UploadStatus_FAILED, &errorMsg)
		} else {
			llog.L().Errorf("update upload record status to failed skipped: record id is nil")
		}

		// 部分失败但已有 provider 成功时，内容已安全落库：立即回收 staging 临时文件。
		// 全部失败时 temp 是内容的唯一副本，保留供排查/重试，由兜底 janitor 超龄回收。
		if ur.FileHash != nil && hasCompletedLocation(*ur.FileHash) {
			if err := utils.CleanupFileTempDir(*ur.FileHash); err != nil {
				llog.L().Warnf("cleanup file temp dir after partial success failed: %v", err)
			}
		}
	} else {
		completeUpload(ur)
	}
}

// hasCompletedLocation 检查文件 hash 是否存在任一已完成（PCOMPLETED）的 location
func hasCompletedLocation(fileHash string) bool {
	locations, err := db.GetFileLocationsByBlake3Hash(fileHash)
	if err != nil {
		return false
	}
	for _, loc := range locations {
		if loc.Status != nil && *loc.Status == int32(native.LocationStatus_PCOMPLETED) {
			return true
		}
	}
	return false
}

// uploadToProvider 上传到单个提供者
// sourcePath: 源文件路径（明文）
// providerID: 目标存储提供者ID
// fileEncrypt: 文件是否启用加密（文件夹设置）
func uploadToProvider(ctx context.Context, sourcePath string, ur *native.UploadRecordORM, providerID uint32, fl *native.FileLocationORM, fileEncrypt bool) error {
	llog.L().Infof("[uploadToProvider] start, provider: %d", providerID)

	fileName := ""
	if ur.FileName != nil {
		fileName = *ur.FileName
	}
	fileSize := uint64(0)
	if ur.FileSize != nil {
		fileSize = *ur.FileSize
	}
	fileHash := ""
	if ur.FileHash != nil {
		fileHash = *ur.FileHash
	}

	// 更新状态为上传中
	fl.Status = utils.EnumPtr(native.LocationStatus_PUPLOADING)
	db.UpdateFileLocation(fl, []string{"status"})

	// 获取存储实例
	st := GetStorage(providerID)
	if st == nil {
		llog.L().Errorf("[uploadToProvider] storage not found, provider: %d", providerID)
		return failFileLocation(fl, fmt.Sprintf("storage provider not found, id: %d", providerID))
	}

	// 获取类型化配置视图
	pcfg := st.GetProviderConfig()
	if pcfg == nil {
		llog.L().Errorf("[uploadToProvider] provider config is nil, provider: %d", providerID)
		return failFileLocation(fl, fmt.Sprintf("storage provider config is nil, id: %d", providerID))
	}
	if !pcfg.Enabled {
		llog.L().Errorf("[uploadToProvider] provider not enabled, provider: %d", providerID)
		return failFileLocation(fl, fmt.Sprintf("storage provider not enabled, id: %d", providerID))
	}

	// 检查源文件是否存在
	if _, err := os.Stat(sourcePath); err != nil {
		llog.L().Errorf("[uploadToProvider] source file not found: %s, err: %v", sourcePath, err)
		return failFileLocation(fl, fmt.Sprintf("source file not found: %s", sourcePath))
	}

	// 生成加密器（如果需要加密），并记录加密状态到 fl
	encryptor, salt, err := prepareEncryptor(fileEncrypt, pcfg)
	if err != nil {
		llog.L().Errorf("[uploadToProvider] prepare encryptor failed: %v, provider: %d", err, providerID)
		return failFileLocation(fl, err.Error())
	}
	encryptedFlag := salt != ""
	fl.Encrypted = &encryptedFlag
	if salt != "" {
		fl.EncryptionSalt = &salt
	}

	// 执行上传（DTO 化调用，驱动不再接触 ORM）
	refs, err := st.Upload(ctx, types.UploadSource{
		TempPath: sourcePath,
		FileName: fileName,
		FileSize: fileSize,
		FileHash: fileHash,
	}, types.UploadOptions{
		Encryptor:        encryptor,
		ObfuscateName:    encryptedFlag,
		EncryptionSuffix: pcfg.EncryptionSuffix,
	})
	if err != nil {
		llog.L().Errorf("[uploadToProvider] upload failed: %v, provider: %d", err, providerID)
		return failFileLocation(fl, err.Error())
	}
	llog.L().Infof("[uploadToProvider] upload returned, chunks: %d, provider: %d", len(refs), providerID)

	// 检查返回的分片信息
	if len(refs) == 0 {
		llog.L().Errorf("[uploadToProvider] no file info returned, provider: %d", providerID)
		return failFileLocation(fl, "no file info returned from upload")
	}

	// 处理分片上传结果
	totalUploadedSize := saveUploadChunks(fl, refs, providerID)

	// 更新存储提供者的已使用容量
	if totalUploadedSize > 0 {
		if err := db.IncrementUsedCapacity(providerID, totalUploadedSize); err != nil {
			llog.L().Errorf("[uploadToProvider] update used capacity failed: %v, provider_id: %d, size: %d", err, providerID, totalUploadedSize)
		}
	}

	hash := ""
	if fl.Blake3Hash != nil {
		hash = *fl.Blake3Hash
	}
	llog.L().Infof("[uploadToProvider] completed, hash: %s, provider: %d", hash, providerID)
	return nil
}

// completeUpload 完成上传，清理临时文件
func completeUpload(ur *native.UploadRecordORM) {
	// 幂等检查：如果已经是 COMPLETED，跳过
	if ur.TaskStatus != nil && *ur.TaskStatus == int32(native.UploadStatus_COMPLETED) {
		return
	}

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
		// 使用 DB 乐观锁防止并发更新
		if ur.Id != nil {
			result := db.GetDB().Model(&native.UploadRecordORM{}).
				Where("id = ? AND task_status != ?", *ur.Id, int32(native.UploadStatus_COMPLETED)).
				Updates(map[string]interface{}{
					"task_status": int32(native.UploadStatus_COMPLETED),
					"message":     "",
				})

			if result.Error != nil {
				llog.L().Errorf("update upload record to completed failed: %v, id: %d", result.Error, *ur.Id)
				return
			}
			if result.RowsAffected == 0 {
				// 已经被其他 goroutine 更新，跳过后续清理
				return
			}
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

// markUploadFailed 标记上传失败
func markUploadFailed(ctx context.Context, ur *native.UploadRecordORM, reason string) {
	if ur.Id == nil {
		llog.L().Errorf("upload failed (no record id): %s, hash: %s", reason, *ur.FileHash)
		return
	}

	// 乐观锁：仅当非 COMPLETED 时才标记失败，防止覆盖已完成状态
	_ = db.TransitionUploadRecordTaskStatus(*ur.Id,
		[]native.UploadStatus{native.UploadStatus_PENDING, native.UploadStatus_UPLOADING, native.UploadStatus_FAILED},
		native.UploadStatus_FAILED, &reason)
	llog.L().Errorf("upload failed: %s, hash: %s", reason, *ur.FileHash)
}

// markUploadUploading 标记上传中
func markUploadUploading(ctx context.Context, ur *native.UploadRecordORM) {
	if ur.Id == nil {
		return
	}
	// 乐观锁：仅当 PENDING 时才迁移到 UPLOADING
	if err := db.TransitionUploadRecordTaskStatus(*ur.Id,
		[]native.UploadStatus{native.UploadStatus_PENDING},
		native.UploadStatus_UPLOADING, nil); err != nil {
		llog.L().Errorf("mark uploading failed: %v, id: %d", err, *ur.Id)
		// 不 return — 允许 Dispatch 继续执行，至少尝试上传
	}
}

// resumeDispatch 恢复分发（系统启动时调用）
func resumeDispatch() {
	// 从数据库恢复未完成的上传任务
	records, err := db.GetUploadRecordsByStatus([]native.UploadStatus{native.UploadStatus_PENDING, native.UploadStatus_UPLOADING})
	if err != nil {
		llog.L().Errorf("get upload record failed: %v", err)
		return
	}

	if len(records) == 0 {
		return
	}

	llog.L().Infof("resuming %d pending/uploading upload records", len(records))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 10) // 最多同时 dispatch 10 个

	for _, record := range records {
		wg.Add(1)
		sem <- struct{}{}
		go func(r *native.UploadRecordORM) {
			defer wg.Done()
			defer func() { <-sem }()
			Dispatch(context.Background(), r)
		}(record)
	}

	wg.Wait()
	llog.L().Infof("resume dispatch completed, processed %d records", len(records))
}

// generateThumbnailForUpload 为上传的文件生成缩略图
