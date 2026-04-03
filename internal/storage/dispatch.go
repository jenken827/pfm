package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sync"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/downloader"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/utils"
	"jenken827.com/jenken827/pfm/pkg/downloader/aria2"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

const (
	// MaxConcurrentUploads 最大并发上传数
	MaxConcurrentUploads = 10
)

// uploadKey 并发控制键
type uploadKey struct {
	blake3Hash string
	providerId uint32
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
		downloadRecords, err := db.GetDownloadRecordsByUploadRecordId(*ur.Id)
		if err == nil && len(downloadRecords) > 0 {
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
	existingLocations, err := db.GetFileLocations(&db.DBQuery{
		Where: "blake3_hash = ?",
		Args:  []any{*ur.FileHash},
	})
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
	// 1. 创建下载临时目录
	downloadDir, err := utils.CreateDownloadTempDir(*ur.FileHash)
	if err != nil {
		llog.L().Errorf("create download temp dir failed: %v", err)
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

	llog.L().Infof("download task created, gid: %s, record_id: %d, hash: %s, waiting for aria2 callback", gid, *downloadRecord.Id, *ur.FileHash)

	// 5. 返回成功，等待 aria2 下载完成回调
	return nil
}

// batchUploadFromSource 从源文件批量上传到多个目标提供者
func batchUploadFromSource(ctx context.Context, ur *native.UploadRecordORM, targetProviderIds []uint32, sourcePath string) {
	// 使用 WaitGroup 并发上传
	var wg sync.WaitGroup
	var failedCount int32

	for _, targetId := range targetProviderIds {
		// 获取并发控制信号量（阻塞等待可用槽位）
		globalDispatcher.semaphore <- struct{}{}

		wg.Add(1)
		go func(tid uint32) {
			defer wg.Done()
			defer func() { <-globalDispatcher.semaphore }() // 释放信号量

			// 并发控制
			key := uploadKey{
				blake3Hash: *ur.FileHash,
				providerId: tid,
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

			// 上传到目标提供者
			if err := uploadToProvider(ctx, ur, sourcePath, tid, fl); err != nil {
				llog.L().Errorf("upload to provider %d failed: %v", tid, err)
				failedCount++
			}
		}(targetId)
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
func uploadToProvider(ctx context.Context, ur *native.UploadRecordORM, sourcePath string, providerId uint32, fl *native.FileLocationORM) error {
	// 更新状态为上传中
	status := int32(native.UploadProviderStatus_PUPLOADING)
	fl.Status = &status
	db.UpdateFileLocation(fl, []string{"status"})

	// 获取存储实例
	st := GetStorage(providerId)
	if st == nil || !*st.Enabled {
		msg := fmt.Sprintf("storage provider not found or disabled, id: %d", providerId)
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

	// 执行上传
	pinfo, err := st.Upload(ctx, ur, nil)
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
	db.UpdateFileLocation(fl, []string{"provider_file_id", "path", "hash", "status"})

	llog.L().Infof("upload completed, hash: %s, provider: %d", *ur.FileHash, providerId)
	return nil
}

// getOrCreateFileLocation 获取或创建文件位置记录
func getOrCreateFileLocation(providerId uint32, blake3Hash string) *native.FileLocationORM {
	// 先查询是否已存在
	locations, err := db.GetFileLocations(&db.DBQuery{
		Where: "provider_id = ? AND blake3_hash = ?",
		Args:  []any{providerId, blake3Hash},
	})
	if err == nil && len(locations) > 0 {
		return locations[0]
	}

	// 不存在则创建
	fl := &native.FileLocationORM{
		ProviderId: &providerId,
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
		if st == nil || !*st.Enabled {
			continue
		}

		order := uint32(0)
		if st.Order != nil {
			order = *st.Order
		}

		isLocal := st.ProviderType != nil && *st.ProviderType == int32(native.StorageProviderType_LOCAL)
		isEncrypted := st.EncryptionKey != nil && *st.EncryptionKey != ""

		c := &candidate{
			provider:    st.StorageProviderORM,
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
	locations, err := db.GetFileLocations(&db.DBQuery{
		Where: "blake3_hash = ? AND provider_id IN ?",
		Args:  []any{*ur.FileHash, targetProviderIds},
	})
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
			status := int32(native.GroupUploadStatus_GCOMPLETED)
			db.Update[native.UploadRecordORM](ctx, &native.UploadRecordORM{Status: &status}, "id=?", *ur.Id)
		}

		// 清理临时文件（仅当有临时文件时）
		if ur.TempPath != nil && *ur.TempPath != "" {
			if err := os.RemoveAll(path.Dir(*ur.TempPath)); err != nil {
				llog.L().Warnf("remove temp file failed: %v", err)
			}
		}

		// 删除上传记录（仅当有ID时）
		if ur.Id != nil {
			if err := db.ClearUploadRecords(ctx, *ur.Id); err != nil {
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

	status := int32(native.GroupUploadStatus_GFAILED)
	db.Update[native.UploadRecordORM](ctx, &native.UploadRecordORM{Status: &status}, "id=?", *ur.Id)
	llog.L().Errorf("upload failed: %s, hash: %s", reason, *ur.FileHash)
}

// markUploadUploading 标记上传中
func markUploadUploading(ctx context.Context, ur *native.UploadRecordORM) {
	if ur.Id == nil {
		return
	}

	status := int32(native.GroupUploadStatus_GUPLOADING)
	db.Update[native.UploadRecordORM](ctx, &native.UploadRecordORM{Status: &status}, "id=?", *ur.Id)
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
	records, _, err := db.Query[native.UploadRecordORM](context.Background(), &db.DBQuery{
		Where: "status IN ?",
		Args:  []any{int32(native.GroupUploadStatus_GPENDING), int32(native.GroupUploadStatus_GUPLOADING)},
	}, false)
	if err != nil {
		llog.L().Errorf("get upload record failed: %v", err)
		return
	}

	for _, record := range records {
		go Dispatch(context.Background(), record)
	}
}
