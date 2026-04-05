package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/downloader"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/pkg/downloader/aria2"
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
	// 1. 检查是否是同步任务（没有临时文件路径或为空）
	isSyncTask := ur.TempPath == nil || *ur.TempPath == ""

	// 2. 如果是同步任务，检查是否已经触发过异步下载
	if isSyncTask && ur.Id != nil {
		_, err := db.GetDownloadRecordByUploadID(*ur.Id)
		if err == nil {
			return
		}
	}

	// 3. 如果是普通上传任务，检查临时文件是否存在
	if !isSyncTask {
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

	// 2. 构建已存在的提供者映射
	existingProviderMap := make(map[uint32]*native.FileLocationORM)
	completedProviderMap := make(map[uint32]bool)
	for _, loc := range existingLocations {
		existingProviderMap[*loc.ProviderId] = loc
		if *loc.Status == int32(native.UploadProviderStatus_PCOMPLETED) {
			completedProviderMap[*loc.ProviderId] = true
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
		completeUpload(ctx, ur)
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
	// 1. 选择最优源提供者（只选择一次）
	sourceProvider, sourceLocation := selectBestSourceStorage(existingLocations)
	if sourceProvider == nil || sourceLocation == nil {
		llog.L().Errorf("no available source provider found")
		markUploadFailed(ctx, ur, "no available source provider")
		return
	}

	// 2. 获取源存储实例
	sourceStorage := GetStorage(*sourceLocation.ProviderId)
	if sourceStorage == nil {
		markUploadFailed(ctx, ur, fmt.Sprintf("source storage not found: %d", *sourceLocation.ProviderId))
		return
	}

	// 3. 获取下载链接
	downloadLink, err := sourceStorage.Link(ctx, sourceLocation)
	if err != nil {
		markUploadFailed(ctx, ur, fmt.Sprintf("get download link failed: %v", err))
		return
	}

	// 4. 判断链接是否为本地路径
	isLocalPath := isLocalPath(downloadLink)

	var sourcePath string
	if isLocalPath {
		// 本地路径：直接使用
		sourcePath = downloadLink
		llog.L().Infof("using local path for sync, hash: %s, path: %s", *ur.FileHash, sourcePath)

		// 批量上传到所有目标提供者
		batchUploadFromSource(ctx, ur, needUploadProviders, sourcePath)
	} else {
		// 远程链接：触发异步下载，结束当前流程
		llog.L().Infof("triggering async download from remote, hash: %s, link: %s", *ur.FileHash, downloadLink)
		if err := triggerAsyncDownload(ur, downloadLink, sourceLocation); err != nil {
			markUploadFailed(ctx, ur, fmt.Sprintf("trigger async download failed: %v", err))
			return
		}
		// 下载任务是异步的，这里直接返回，等待aria2完成通知
		// 下载完成后，notifier 会调用 resumeSyncFromDownload 恢复上传流程
		return
	}
}

// isLocalPath 判断路径是否为本地路径
func isLocalPath(path string) bool {
	// 判断是否为本地文件路径
	// 支持以下格式：
	// - Unix路径: /path/to/file
	// - Windows路径: C:\path\to\file, D:/path/to/file
	// - 相对路径: ./path/to/file, path/to/file

	if path == "" {
		return false
	}

	// 检查是否包含 :// 协议标识符（排除 Windows 盘符如 C:）
	// Windows 盘符格式: C:\path 或 C:/path
	// URL 格式: http://, https://, ftp:// 等
	if len(path) >= 2 {
		// Windows 盘符检查：第二个字符是冒号且后面跟着反斜杠或斜杠
		if path[1] == ':' && (len(path) < 3 || path[2] == '\\' || path[2] == '/') {
			return true
		}
	}

	// 检查是否包含协议标识符
	for i := 0; i < len(path); i++ {
		if path[i] == ':' {
			// 如果冒号后面跟着 //，则是URL协议
			if i+2 < len(path) && path[i+1] == '/' && path[i+2] == '/' {
				return false
			}
		}
	}

	// 以 / 或 . 开头的路径视为本地路径
	if path[0] == '/' || path[0] == '.' {
		return true
	}

	// 其他情况，假设为本地路径（保守估计）
	return true
}

// triggerAsyncDownload 触发异步下载任务
func triggerAsyncDownload(ur *native.UploadRecordORM, downloadLink string, sourceLocation *native.FileLocationORM) error {
	// 1. 创建文件临时目录
	if err := utils.CreateFileTempDir(*ur.FileHash, false); err != nil {
		llog.L().Errorf("create file temp dir failed: %v", err)
		return err
	}

	// 2. 创建下载记录（只记录必要信息）
	downloadRecord := &native.DownloadRecordORM{
		Blake3Hash:       ur.FileHash,
		SourceLocationId: sourceLocation.Id,
		UploadRecordId:   ur.Id,
		State:            (*int32)(native.DownloadRecordState_DOWNLOAD_PENDING.Enum()),
	}

	if err := db.CreateDownloadRecord(downloadRecord); err != nil {
		llog.L().Errorf("create download record failed: %v", err)
		return err
	}

	// 3. 调用 aria2 添加下载任务
	if downloader.Aria2Downloader == nil {
		llog.L().Errorf("aria2 downloader not initialized")
		return fmt.Errorf("aria2 downloader not initialized")
	}

	// 下载到文件临时目录
	downloadDir := utils.GetFileTempDir(*ur.FileHash)
	gid, err := downloader.Aria2Downloader.AddURI([]string{downloadLink}, aria2.Option{
		"dir": downloadDir,
	})
	if err != nil {
		llog.L().Errorf("add aria2 download task failed: %v", err)
		return err
	}

	// 4. 更新下载记录的 GID
	downloadRecord.Gid = &gid
	downloadRecord.State = (*int32)(native.DownloadRecordState_DOWNLOADING.Enum())
	if err := db.UpdateDownloadRecord(downloadRecord, []string{"gid", "state"}); err != nil {
		llog.L().Errorf("update download record gid failed: %v", err)
		return err
	}

	llog.L().Infof("download task created, gid: %s, record_id: %d, hash: %s, dir: %s, waiting for aria2 callback", gid, *downloadRecord.Id, *ur.FileHash, downloadDir)

	// 5. 返回成功，等待 aria2 下载完成回调
	return nil
}

// batchUploadFromSource 从源文件批量上传到多个目标提供者
func batchUploadFromSource(ctx context.Context, ur *native.UploadRecordORM, targetProviderIds []uint32, sourcePath string) {
	// 使用 WaitGroup 并发上传
	var wg sync.WaitGroup
	var failedCount int32

	// 获取文件名
	fileName := "unknown"
	if ur.FileName != nil {
		fileName = *ur.FileName
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
				return
			}

			// 如果已经完成，跳过
			if *fl.Status == int32(native.UploadProviderStatus_PCOMPLETED) {
				llog.L().Infof("file already exists in provider %d", tid)
				return
			}

			// 上传到目标提供者（加密器在uploadToProvider内部生成）
			if err := uploadToProvider(ctx, sourcePath, fileName, tid, fl); err != nil {
				llog.L().Errorf("upload to provider %d failed: %v", tid, err)
				failedCount++
			}
		}(targetID)
	}

	// 等待所有上传完成
	wg.Wait()

	// 记录失败数量
	if failedCount > 0 {
		llog.L().Warnf("batch upload completed with %d failures, hash: %s", failedCount, *ur.FileHash)
	}

	// 检查是否所有上传都完成
	completeUpload(ctx, ur)
}

// uploadToProvider 上传到单个提供者
// sourcePath: 源文件路径（明文）
// fileName: 目标文件名
// providerId: 目标存储提供者ID
func uploadToProvider(ctx context.Context, sourcePath string, fileName string, providerID uint32, fl *native.FileLocationORM) error {
	// 更新状态为上传中
	fl.Status = utils.EnumPtr(native.UploadProviderStatus_PUPLOADING)
	db.UpdateFileLocation(fl, []string{"status"})

	// 获取存储实例
	st := GetStorage(providerID)
	if st == nil {
		msg := fmt.Sprintf("storage provider not found, id: %d", providerID)
		fl.Message = &msg
		fl.Status = (*int32)(native.UploadProviderStatus_PFAILED.Enum())
		db.UpdateFileLocation(fl, []string{"status", "message"})
		return errors.New(msg)
	}

	// 获取ORM对象
	orm := st.GetOrm()
	if orm == nil || !*orm.Enabled {
		msg := fmt.Sprintf("storage provider not enabled, id: %d", providerID)
		fl.Message = &msg
		fl.Status = (*int32)(native.UploadProviderStatus_PFAILED.Enum())
		db.UpdateFileLocation(fl, []string{"status", "message"})
		return errors.New(msg)
	}

	// 检查源文件是否存在
	if _, err := os.Stat(sourcePath); err != nil {
		msg := fmt.Sprintf("source file not found: %s", sourcePath)
		fl.Message = &msg
		fl.Status = (*int32)(native.UploadProviderStatus_PFAILED.Enum())
		db.UpdateFileLocation(fl, []string{"status", "message"})
		return errors.New(msg)
	}

	// 生成加密器（如果需要加密）
	var encryptor types.Encryptor = &types.NullEncryptor{}
	var saltStr string

	if orm.EncryptionKey != nil && *orm.EncryptionKey != "" {
		// 生成随机salt（32字节）
		saltBytes := make([]byte, 32)
		if _, err := rand.Read(saltBytes); err != nil {
			msg := fmt.Sprintf("generate salt failed: %v", err)
			fl.Message = &msg
			fl.Status = (*int32)(native.UploadProviderStatus_PFAILED.Enum())
			db.UpdateFileLocation(fl, []string{"status", "message"})
			return errors.New(msg)
		}
		saltStr = hex.EncodeToString(saltBytes)

		// 创建加密器
		enc, err := NewCryptEncryptor(*orm.EncryptionKey, saltStr)
		if err != nil {
			msg := fmt.Sprintf("create encryptor failed: %v", err)
			fl.Message = &msg
			fl.Status = (*int32)(native.UploadProviderStatus_PFAILED.Enum())
			db.UpdateFileLocation(fl, []string{"status", "message"})
			return errors.New(msg)
		}
		encryptor = enc

		// 标记为已加密
		encrypted := true
		fl.Encrypted = &encrypted
		fl.EncryptionSalt = &saltStr
	}

	// 执行上传
	pinfo, err := st.Upload(ctx, sourcePath, fileName, encryptor)
	if err != nil {
		msg := err.Error()
		fl.Message = &msg
		fl.Status = (*int32)(native.UploadProviderStatus_PFAILED.Enum())
		db.UpdateFileLocation(fl, []string{"status", "message"})
		return err
	}

	// 更新文件位置信息
	fl.ProviderFileId = &pinfo.ID
	fl.Path = &pinfo.Path
	if pinfo.Hash != "" {
		fl.Hash = &pinfo.Hash
	}
	fl.Status = (*int32)(native.UploadProviderStatus_PCOMPLETED.Enum())

	// 更新字段列表
	updateFields := []string{"provider_file_id", "path", "hash", "status"}
	if fl.Encrypted != nil {
		updateFields = append(updateFields, "encrypted", "encryption_salt")
	}

	db.UpdateFileLocation(fl, updateFields)

	llog.L().Infof("upload completed, hash: %s, provider: %d, encrypted: %v", *fl.Blake3Hash, providerID, fl.Encrypted != nil)
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
		Status:     (*int32)(native.UploadProviderStatus_PPENDING.Enum()),
	}
	if err := db.CreateFileLocation(fl); err != nil {
		llog.L().Errorf("create file location failed: %v", err)
		return nil
	}

	return fl
}

// selectBestSourceStorage 选择最优源存储提供者
func selectBestSourceStorage(sourceLocations []*native.FileLocationORM) (*native.StorageProviderORM, *native.FileLocationORM) {
	type candidate struct {
		provider    *native.StorageProviderORM
		location    *native.FileLocationORM
		order       uint32
		isLocal     bool
		isEncrypted bool
	}

	var (
		localCandidate     *candidate
		orderedUnencrypted *candidate
		unencrypted        *candidate
		ordered            *candidate
	)

	// 遍历所有源位置，按优先级分类
	for _, loc := range sourceLocations {
		if *loc.Status != int32(native.UploadProviderStatus_PCOMPLETED) {
			continue
		}

		st := GetStorage(*loc.ProviderId)
		if st == nil {
			continue
		}

		orm := st.GetOrm()
		if orm == nil || !*orm.Enabled {
			continue
		}

		order := uint32(0)
		if orm.Order != nil {
			order = *orm.Order
		}

		isLocal := orm.ProviderType != nil && *orm.ProviderType == int32(native.StorageProviderType_LOCAL)
		isEncrypted := orm.EncryptionKey != nil && *orm.EncryptionKey != ""

		c := &candidate{
			provider:    orm,
			location:    loc,
			order:       order,
			isLocal:     isLocal,
			isEncrypted: isEncrypted,
		}

		if isLocal {
			// 优先级1: 本地存储
			if localCandidate == nil || order < localCandidate.order {
				localCandidate = c
			}
		} else if !isEncrypted {
			// 优先级2: 顺序靠前且未加密
			if orderedUnencrypted == nil || order < orderedUnencrypted.order {
				orderedUnencrypted = c
			}
			// 优先级3: 未加密
			if unencrypted == nil {
				unencrypted = c
			}
		} else {
			// 优先级4: 顺序靠前
			if ordered == nil || order < ordered.order {
				ordered = c
			}
		}
	}

	// 按优先级返回
	if localCandidate != nil {
		return localCandidate.provider, localCandidate.location
	}
	if orderedUnencrypted != nil {
		return orderedUnencrypted.provider, orderedUnencrypted.location
	}
	if unencrypted != nil {
		return unencrypted.provider, unencrypted.location
	}
	if ordered != nil {
		return ordered.provider, ordered.location
	}

	return nil, nil
}

// completeUpload 完成上传，清理临时文件
func completeUpload(ctx context.Context, ur *native.UploadRecordORM) {
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
		if *loc.Status == int32(native.UploadProviderStatus_PCOMPLETED) {
			completedCount++
		}
	}

	// 更新上传记录状态
	if completedCount == len(targetProviderIds) {
		// 所有上传都完成
		if ur.Id != nil {
			db.UpdateUploadRecord(&native.UploadRecordORM{Id: ur.Id, Status: utils.EnumPtr(native.UploadProviderStatus_PCOMPLETED)}, []string{"status"})
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

	db.UpdateUploadRecord(&native.UploadRecordORM{Id: ur.Id, Status: utils.EnumPtr(native.UploadProviderStatus_PFAILED)}, []string{"status"})
	llog.L().Errorf("upload failed: %s, hash: %s", reason, *ur.FileHash)
}

// markUploadUploading 标记上传中
func markUploadUploading(ctx context.Context, ur *native.UploadRecordORM) {
	if ur.Id == nil {
		return
	}

	db.UpdateUploadRecord(&native.UploadRecordORM{Id: ur.Id, Status: utils.EnumPtr(native.UploadProviderStatus_PUPLOADING)}, []string{"status"})
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
	records, err := db.GetUploadRecordsByStatus([]native.UploadProviderStatus{native.UploadProviderStatus_PPENDING, native.UploadProviderStatus_PUPLOADING})
	if err != nil {
		llog.L().Errorf("get upload record failed: %v", err)
		return
	}

	for _, record := range records {
		go Dispatch(context.Background(), record)
	}
}
