package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/thumbnail"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

const (
	// DefaultMaxConcurrentUploads 默认全局最大并发上传数
	DefaultMaxConcurrentUploads = 10
	// DefaultProviderConcurrentUploads 默认单个存储提供者最大并发上传数
	DefaultProviderConcurrentUploads = 3
)

// uploadKey 并发控制键
type uploadKey struct {
	blake3Hash string
	providerID uint32
}

// providerSemaphore 存储提供者信号量
type providerSemaphore struct {
	semaphore chan struct{}
}

// dispatcher 分发器
type dispatcher struct {
	processing          sync.Map // map[uploadKey]chan struct{}
	lockTime            sync.Map // map[uploadKey]time.Time — 锁创建时间，用于超时清理
	globalSemaphore     chan struct{}
	providerSemaphores  sync.Map // map[uint32]*providerSemaphore
	providerConcurrence sync.Map // map[uint32]int 存储提供者配置的并发数
}

var globalDispatcher = &dispatcher{}

func init() {
	// 初始化全局信号量，后续会被 config 覆盖
	maxConcurrent := DefaultMaxConcurrentUploads
	if cfg := config.GetConfig(); cfg != nil && cfg.Upload.MaxConcurrentUploads > 0 {
		maxConcurrent = cfg.Upload.MaxConcurrentUploads
	}
	globalDispatcher.globalSemaphore = make(chan struct{}, maxConcurrent)
}

// getProviderSemaphore 获取存储提供者的信号量
func getProviderSemaphore(providerID uint32) chan struct{} {
	if v, ok := globalDispatcher.providerSemaphores.Load(providerID); ok {
		return v.(*providerSemaphore).semaphore
	}

	// 从数据库读取配置
	concurrent := DefaultProviderConcurrentUploads
	if provider, err := db.GetProviderByID(providerID); err == nil && provider != nil {
		if provider.MaxConcurrentUploads != nil && *provider.MaxConcurrentUploads > 0 {
			concurrent = int(*provider.MaxConcurrentUploads)
		}
	}

	sem := make(chan struct{}, concurrent)
	if actual, loaded := globalDispatcher.providerSemaphores.LoadOrStore(providerID, &providerSemaphore{semaphore: sem}); loaded {
		return actual.(*providerSemaphore).semaphore
	}
	globalDispatcher.providerConcurrence.Store(providerID, concurrent)
	llog.L().Infof("Created semaphore for provider %d with concurrency %d", providerID, concurrent)
	return sem
}

// refreshProviderSemaphore 刷新存储提供者的信号量（配置变更时调用）
func RefreshProviderSemaphore(providerID uint32) {
	globalDispatcher.providerSemaphores.Delete(providerID)
	globalDispatcher.providerConcurrence.Delete(providerID)
}

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
	// 生成缩略图（在分发前完成）
	generateThumbnailForUpload(ctx, ur)

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

	// 更新 upload record 的 TempPath，驱动从这里读取文件路径
	ur.TempPath = &tempFilePath

	llog.L().Infof("file downloaded and merged to: %s", tempFilePath)

	// 5. 批量上传到所有目标提供者（并发）
	batchUploadFromSource(ctx, ur, needUploadProviders, tempFilePath)
}

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
			defer func() { <-pSem }()                  // 释放提供者信号量
			defer func() { <-globalDispatcher.globalSemaphore }() // 释放全局信号量

			if failed, errMsg := uploadToProviderAsync(ctx, ur, sourcePath, fileName, parentID, fileEncrypt, tid); failed {
				failedCount++
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
		if updateErr := db.UpdateFileIsReady(parentID, fileName); updateErr != nil {
			llog.L().Errorf("update file is_ready failed: %v, parent_id: %s, file_name: %s", updateErr, parentID, fileName)
		} else {
			llog.L().Infof("file marked as ready, parent_id: %s, file_name: %s, provider: %d", parentID, fileName, tid)
		}
	}
	return false, ""
}

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

		failedStatus := int32(native.LocationStatus_PFAILED)
		ur.Status = &failedStatus
		ur.Message = &errorMsg
		if err := db.UpdateUploadRecord(ur, []string{"status", "message"}); err != nil {
			llog.L().Errorf("update upload record status to failed error: %v, id: %d", err, *ur.Id)
		}
	} else {
		completeUpload(ur)
	}
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
	llog.L().Infof("[uploadToProvider] fileName: %s", fileName)

	// 更新状态为上传中
	fl.Status = utils.EnumPtr(native.LocationStatus_PUPLOADING)
	db.UpdateFileLocation(fl, []string{"status"})
	llog.L().Infof("[uploadToProvider] status updated to uploading, provider: %d", providerID)

	// 获取存储实例
	st := GetStorage(providerID)
	if st == nil {
		llog.L().Errorf("[uploadToProvider] storage not found, provider: %d", providerID)
		return failFileLocation(fl, fmt.Sprintf("storage provider not found, id: %d", providerID))
	}
	llog.L().Infof("[uploadToProvider] storage found, provider: %d", providerID)

	// 获取ORM对象
	orm := st.GetOrm()
	if orm == nil {
		llog.L().Errorf("[uploadToProvider] orm is nil, provider: %d", providerID)
		return failFileLocation(fl, fmt.Sprintf("storage provider orm is nil, id: %d", providerID))
	}
	if orm.Enabled == nil || !*orm.Enabled {
		llog.L().Errorf("[uploadToProvider] provider not enabled, provider: %d", providerID)
		return failFileLocation(fl, fmt.Sprintf("storage provider not enabled, id: %d", providerID))
	}
	llog.L().Infof("[uploadToProvider] orm check passed, provider: %d", providerID)

	// 检查源文件是否存在
	if _, err := os.Stat(sourcePath); err != nil {
		llog.L().Errorf("[uploadToProvider] source file not found: %s, err: %v", sourcePath, err)
		return failFileLocation(fl, fmt.Sprintf("source file not found: %s", sourcePath))
	}
	llog.L().Infof("[uploadToProvider] source file exists: %s", sourcePath)

	// 生成加密器（如果需要加密）
	llog.L().Infof("[uploadToProvider] preparing encryptor, provider: %d", providerID)
	encryptor, err := prepareEncryptor(fileEncrypt, orm, fl, providerID, fileName)
	if err != nil {
		llog.L().Errorf("[uploadToProvider] prepare encryptor failed: %v, provider: %d", err, providerID)
		return err
	}
	llog.L().Infof("[uploadToProvider] encryptor ready, provider: %d", providerID)

	// 执行上传
	llog.L().Infof("[uploadToProvider] calling Upload, provider: %d", providerID)
	pinfo, err := st.Upload(ctx, ur, encryptor)
	if err != nil {
		llog.L().Errorf("[uploadToProvider] upload failed: %v, provider: %d", err, providerID)
		return failFileLocation(fl, err.Error())
	}
	llog.L().Infof("[uploadToProvider] upload returned, chunks: %d, provider: %d", len(pinfo), providerID)

	// 检查返回的分片信息
	if len(pinfo) == 0 {
		llog.L().Errorf("[uploadToProvider] no file info returned, provider: %d", providerID)
		return failFileLocation(fl, "no file info returned from upload")
	}
	llog.L().Infof("[uploadToProvider] calling saveUploadChunks, provider: %d", providerID)

	// 处理分片上传结果
	totalUploadedSize := saveUploadChunks(fl, pinfo, providerID)
	llog.L().Infof("[uploadToProvider] saveUploadChunks done, size: %d, provider: %d", totalUploadedSize, providerID)

	// 更新存储提供者的已使用容量
	if totalUploadedSize > 0 {
		llog.L().Infof("[uploadToProvider] incrementing used capacity, provider: %d, size: %d", providerID, totalUploadedSize)
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

// failFileLocation 标记文件位置为失败状态
func failFileLocation(fl *native.FileLocationORM, msg string) error {
	fl.Message = &msg
	fl.Status = (*int32)(native.LocationStatus_PFAILED.Enum())
	db.UpdateFileLocation(fl, []string{"status", "message"})
	return errors.New(msg)
}

// prepareEncryptor 生成加密器（如果需要加密）
func prepareEncryptor(fileEncrypt bool, orm *native.StorageProviderORM, fl *native.FileLocationORM, providerID uint32, fileName string) (types.Encryptor, error) {
	if fileEncrypt && orm.EncryptionKey != nil && *orm.EncryptionKey != "" {
		llog.L().Infof("encryption enabled for file, provider: %d, file: %s", providerID, fileName)

		// 生成随机salt（32字节）
		saltBytes := make([]byte, 32)
		if _, err := rand.Read(saltBytes); err != nil {
			return nil, failFileLocation(fl, fmt.Sprintf("generate salt failed: %v", err))
		}
		saltStr := hex.EncodeToString(saltBytes)

		// 创建加密器
		enc, err := NewCryptEncryptor(*orm.EncryptionKey, saltStr)
		if err != nil {
			return nil, failFileLocation(fl, fmt.Sprintf("create encryptor failed: %v", err))
		}

		// 标记为已加密
		encrypted := true
		fl.Encrypted = &encrypted
		fl.EncryptionSalt = &saltStr

		llog.L().Infof("file will be encrypted, provider: %d, salt: %s...", providerID, saltStr[:16])
		return enc, nil
	}

	// 确保标记为未加密
	encrypted := false
	fl.Encrypted = &encrypted
	return &types.NullEncryptor{}, nil
}

// saveUploadChunks 处理上传结果，保存分片信息到数据库
func saveUploadChunks(fl *native.FileLocationORM, pinfo []*types.StorageProviderFileInfo, providerID uint32) uint64 {
	chunkTotal := uint32(len(pinfo))
	var totalUploadedSize uint64

	// 安全获取 hash
	hash := ""
	if fl.Blake3Hash != nil {
		hash = *fl.Blake3Hash
	}

	for i, chunkInfo := range pinfo {
		chunkNumber := uint32(i)

		if i == 0 {
			// 第一个分片：更新已有的 FileLocation 记录
			updateFirstChunkLocation(fl, chunkInfo, chunkTotal, chunkNumber)
			totalUploadedSize += chunkInfo.ChunkSize
		} else {
			// 后续分片：创建新的 FileLocation 记录
			if err := createChunkLocation(fl, chunkInfo, chunkTotal, chunkNumber); err != nil {
				llog.L().Errorf("create file location for chunk %d failed: %v", chunkNumber, err)
				continue
			}
			totalUploadedSize += chunkInfo.ChunkSize
		}

		llog.L().Infof("chunk %d/%d uploaded, hash: %s, provider: %d, file_id: %s",
			chunkNumber+1, chunkTotal, hash, providerID, chunkInfo.ID)
	}

	return totalUploadedSize
}

// updateFirstChunkLocation 更新第一个分片的 FileLocation 记录
func updateFirstChunkLocation(fl *native.FileLocationORM, chunkInfo *types.StorageProviderFileInfo, chunkTotal, chunkNumber uint32) {
	updateFields := []string{"provider_file_id", "status", "chunk_number", "chunk_total", "message"}
	fl.ProviderFileId = &chunkInfo.ID
	fl.Status = (*int32)(native.LocationStatus_PCOMPLETED.Enum())
	fl.ChunkTotal = &chunkTotal
	fl.ChunkNumber = &chunkNumber
	// 清除之前的错误信息
	emptyMsg := ""
	fl.Message = &emptyMsg

	if chunkInfo.Path != "" {
		fl.Path = &chunkInfo.Path
		updateFields = append(updateFields, "path")
	}
	if chunkInfo.Hash != "" {
		fl.Hash = &chunkInfo.Hash
		updateFields = append(updateFields, "hash")
	}
	if chunkInfo.Size > 0 {
		fl.Size = &chunkInfo.Size
		updateFields = append(updateFields, "size")
	}
	if chunkInfo.ChunkSize > 0 {
		fl.ChunkSize = &chunkInfo.ChunkSize
		updateFields = append(updateFields, "chunk_size")
	}
	if chunkInfo.ChunkHash != "" {
		fl.ChunkHash = &chunkInfo.ChunkHash
		updateFields = append(updateFields, "chunk_hash")
	}
	if chunkInfo.S3KeyFlag != "" {
		fl.S3KeyFlag = &chunkInfo.S3KeyFlag
		updateFields = append(updateFields, "s3_key_flag")
	}
	if chunkInfo.ChannelID != "" {
		fl.ChannelId = &chunkInfo.ChannelID
		updateFields = append(updateFields, "channel_id")
	}
	if chunkInfo.MessageID != 0 {
		fl.MessageId = utils.Int32Ptr(int32(chunkInfo.MessageID))
		updateFields = append(updateFields, "message_id")
	}
	if fl.Encrypted != nil {
		updateFields = append(updateFields, "encrypted", "encryption_salt")
	}

	db.UpdateFileLocation(fl, updateFields)
}

// createChunkLocation 创建后续分片的 FileLocation 记录
func createChunkLocation(fl *native.FileLocationORM, chunkInfo *types.StorageProviderFileInfo, chunkTotal, chunkNumber uint32) error {
	newFl := &native.FileLocationORM{
		Blake3Hash:     fl.Blake3Hash,
		ProviderId:     fl.ProviderId,
		ProviderFileId: &chunkInfo.ID,
		Status:         (*int32)(native.LocationStatus_PCOMPLETED.Enum()),
		ChunkNumber:    &chunkNumber,
		ChunkTotal:     &chunkTotal,
		// 复制加密字段，确保分片的加密状态一致
		Encrypted:      fl.Encrypted,
		EncryptionSalt: fl.EncryptionSalt,
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
	}
	if chunkInfo.ChunkHash != "" {
		newFl.ChunkHash = &chunkInfo.ChunkHash
	}
	if chunkInfo.S3KeyFlag != "" {
		newFl.S3KeyFlag = &chunkInfo.S3KeyFlag
	}
	if chunkInfo.ChannelID != "" {
		newFl.ChannelId = &chunkInfo.ChannelID
	}

	if chunkInfo.MessageID != 0 {
		newFl.MessageId = utils.Int32Ptr(int32(chunkInfo.MessageID))
	}

	return db.CreateFileLocation(newFl)
}

// getOrCreateFileLocation 获取或创建文件位置记录
func getOrCreateFileLocation(providerID uint32, blake3Hash string) (*native.FileLocationORM, error) {
	// 先查询是否已存在
	location, err := db.GetFileLocationByProviderAndHash(providerID, blake3Hash)
	if err == nil {
		return location, nil
	}

	// 不存在则创建，但先检查存储提供者是否可用
	st := GetStorage(providerID)
	if st == nil {
		return nil, fmt.Errorf("storage provider not found: %d", providerID)
	}

	orm := st.GetOrm()
	if orm == nil || !*orm.Enabled {
		return nil, fmt.Errorf("storage provider not enabled: %d", providerID)
	}

	// 创建文件位置记录
	fl := &native.FileLocationORM{
		ProviderId: &providerID,
		Blake3Hash: &blake3Hash,
		Status:     (*int32)(native.LocationStatus_PPENDING.Enum()),
	}
	if err := db.CreateFileLocation(fl); err != nil {
		return nil, fmt.Errorf("create file location failed: %v", err)
	}

	return fl, nil
}

// completeUpload 完成上传，清理临时文件
func completeUpload(ur *native.UploadRecordORM) {
	// 幂等检查：如果已经是 PCOMPLETED，跳过
	if ur.Status != nil && *ur.Status == int32(native.LocationStatus_PCOMPLETED) {
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
				Where("id = ? AND status != ?", *ur.Id, int32(native.LocationStatus_PCOMPLETED)).
				Updates(map[string]interface{}{
					"status":  int32(native.LocationStatus_PCOMPLETED),
					"message": "",
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
	if err := db.UpdateUploadRecord(
		&native.UploadRecordORM{Id: ur.Id, Status: utils.EnumPtr(native.LocationStatus_PUPLOADING)},
		[]string{"status"},
	); err != nil {
		llog.L().Errorf("mark uploading failed: %v, id: %d", err, *ur.Id)
		// 不 return — 允许 Dispatch 继续执行，至少尝试上传
	}
}

// acquireLock 获取并发控制锁（非阻塞），带过期锁自动清理
func acquireLock(key uploadKey) bool {
	cleanupExpiredLocks()

	ch := make(chan struct{})
	_, loaded := globalDispatcher.processing.LoadOrStore(key, ch)
	if loaded {
		// 已经有任务在处理，直接返回false，不等待
		return false
	}
	// 记录锁的创建时间
	globalDispatcher.lockTime.Store(key, time.Now())
	return true
}

// releaseLock 释放并发控制锁
func releaseLock(key uploadKey) {
	if ch, ok := globalDispatcher.processing.Load(key); ok {
		close(ch.(chan struct{}))
		globalDispatcher.processing.Delete(key)
		globalDispatcher.lockTime.Delete(key)
	}
}

// cleanupExpiredLocks 清理超时未释放的锁（防止崩溃后锁永久占用）
func cleanupExpiredLocks() {
	const lockTimeout = 30 * time.Minute
	globalDispatcher.lockTime.Range(func(key, value interface{}) bool {
		if t, ok := value.(time.Time); ok && time.Since(t) > lockTimeout {
			llog.L().Warnf("cleaning expired upload lock: hash=%s provider=%v",
				key.(uploadKey).blake3Hash[:8], key.(uploadKey).providerID)
			releaseLock(key.(uploadKey))
		}
		return true
	})
}

// resumeDispatch 恢复分发（系统启动时调用）
func resumeDispatch() {
	// 从数据库恢复未完成的上传任务
	records, err := db.GetUploadRecordsByStatus([]native.LocationStatus{native.LocationStatus_PPENDING, native.LocationStatus_PUPLOADING})
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
func generateThumbnailForUpload(ctx context.Context, ur *native.UploadRecordORM) {
	// 检查是否是图片文件
	if ur.FileName == nil || ur.TempPath == nil || ur.FileHash == nil {
		return
	}

	fileName := *ur.FileName
	tempPath := *ur.TempPath
	fileHash := *ur.FileHash

	// 检查是否是图片文件
	if !thumbnail.IsImageFile(fileName) {
		llog.L().Debugf("not an image file, skip thumbnail: %s", fileName)
		return
	}

	// 检查是否应该跳过（SVG等）
	if thumbnail.ShouldSkipThumbnail(fileName) {
		llog.L().Debugf("skip thumbnail for file: %s", fileName)
		return
	}

	// 获取图片信息
	imgInfo, err := thumbnail.GetImageInfo(tempPath)
	if err != nil {
		llog.L().Warnf("get image info failed, skip thumbnail: %s, err: %v", fileName, err)
		return
	}

	// 判断是否需要生成缩略图
	if !thumbnail.NeedThumbnail(imgInfo, fileName) {
		llog.L().Debugf("thumbnail not needed for file: %s, size: %dx%d", fileName, imgInfo.Width, imgInfo.Height)
		return
	}

	// 获取缩略图缓存目录
	cacheDir := getThumbnailCacheDir()
	generator := thumbnail.NewGenerator(cacheDir)

	// 生成所有尺寸的缩略图（200x200 和 400x400）
	thumbnailPaths, err := generator.GenerateAll(tempPath, fileHash)
	if err != nil {
		llog.L().Warnf("generate thumbnail failed: %s, err: %v", fileName, err)
		return
	}

	llog.L().Infof("thumbnails ready for file: %s, paths: %v", fileName, thumbnailPaths)
}

// getThumbnailCacheDir 获取缩略图缓存目录
func getThumbnailCacheDir() string {
	// 使用 data/thumbnails 目录
	return "data/thumbnails"
}
