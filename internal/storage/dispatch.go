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

	"github.com/jenken827/pfm-plugin-sdk/driver/cryptostream"
	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/pkg/llog"
	"github.com/jenken827/pfm/utils"
)

// dispatchingRecords 正在分发的上传记录 id（同一条记录并发重复 Dispatch 的去重表）
var dispatchingRecords sync.Map // map[uint32]struct{}

// Dispatch 分发处理（文件上传完成后调用）
// 支持两种场景：
// 1. 普通上传：有临时文件，从临时文件上传
// 2. 同步任务：无临时文件，从现有提供者同步
func Dispatch(ctx context.Context, ur *entity.UploadRecordORM) {
	defer func() {
		if r := recover(); r != nil {
			var fileHash string
			if ur != nil && ur.FileHash != nil {
				fileHash = *ur.FileHash
			}
			llog.C(ctx).Errorf("Dispatch panicked, file_hash: %s, recover: %v", fileHash, r)
		}
	}()

	// 同一条上传记录只允许一个分发在跑：客户端重复投递 / 重试 / 复用 init 记录
	// 都可能对同一条记录连发两次 Dispatch，两边并发上传会重复计容量、互相
	// 覆盖位置行状态，先到者完成后的 temp 清理还会让后到者撞 temp file not found。
	if ur.Id != nil {
		if _, loaded := dispatchingRecords.LoadOrStore(*ur.Id, struct{}{}); loaded {
			llog.C(ctx).Infof("dispatch already running, skip duplicate: %d", *ur.Id)
			return
		}
		defer dispatchingRecords.Delete(*ur.Id)
	}

	// 1. 根据 task_type 判断任务类型
	isSyncTask := ur.TaskType != nil && *ur.TaskType == int32(native.UploadTaskType_SYNC)

	// 2. 如果是普通上传任务，检查临时文件是否存在
	if !isSyncTask {
		if ur.TempPath == nil || *ur.TempPath == "" {
			markUploadFailed(ctx, ur, "temp file path is empty for upload task")
			return
		}
		if _, err := os.Stat(*ur.TempPath); err != nil {
			// Fallback: 精确路径不存在时，在本人隔离目录内查找可用文件
			// 场景：用户 rename 重试上传，新 temp 文件名与 upload_record 中的旧文件名不同
			// 先查 owner 隔离目录（当前布局），再回退内容寻址层（旧布局兼容）
			if ur.FileHash != nil {
				var p string
				ok := false
				if ur.OwnerId != nil && *ur.OwnerId != "" {
					p, ok = utils.FindTempFileInDir(utils.GetChunkTempDir(*ur.FileHash, *ur.OwnerId))
				}
				if !ok {
					p, ok = utils.FindTempFile(*ur.FileHash)
				}
				if ok {
					llog.C(ctx).Infof("temp file fallback: %s -> %s", *ur.TempPath, p)
					ur.TempPath = &p
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
func dispatchToProviders(ctx context.Context, ur *entity.UploadRecordORM, targetProviderIds []uint32, isSyncTask bool) {
	// 1. 查询文件已经在哪些存储提供者上
	existingLocations, err := db.GetFileLocationsByBlake3Hash(*ur.FileHash)
	if err != nil {
		llog.C(ctx).Errorf("get existing file locations failed: %v", err)
	}

	// 2. 构建已存在的提供者映射（按提供者分组）
	providerChunksMap := make(map[uint32][]*entity.FileLocationORM)
	for _, loc := range existingLocations {
		providerChunksMap[*loc.ProviderId] = append(providerChunksMap[*loc.ProviderId], loc)
	}

	// 3. 检查每个提供者的所有分片是否都完成
	// 用 CheckProviderComplete 统一判定：它容忍 chunk_total 为 nil 的行
	// （未完成/预建的位置行没有 chunk_total，直接解引用会 panic）。
	completedProviderMap := make(map[uint32]bool)
	for providerID, chunks := range providerChunksMap {
		complete, expectedTotal, actualTotal := CheckProviderComplete(chunks)
		if complete {
			completedProviderMap[providerID] = true
			llog.C(ctx).Infof("provider %d is available with all %d chunks completed",
				providerID, expectedTotal)
			continue
		}
		llog.C(ctx).Debugf("provider %d incomplete: expected %d chunks, got %d",
			providerID, expectedTotal, actualTotal)
	}

	// 3. 计算需要上传的提供者
	var needUploadProviders []uint32
	for _, pid := range targetProviderIds {
		if !completedProviderMap[pid] {
			needUploadProviders = append(needUploadProviders, pid)
		}
	}

	// 4. 如果所有提供者都已有文件，直接完成。
	// 注意：此时并没有真正上传，但文件记录仍必须标为就绪——同一份内容以不同文件名
	// 再上传一次（或离线下载改名入库）时，内容早就在 provider 里，若不补标，
	// 新记录会永远停在"未就绪"（前端显示未就绪、无法预览）。
	if len(needUploadProviders) == 0 {
		llog.C(ctx).Infof("file already exists in all target providers, hash: %s", *ur.FileHash)
		markFileReadyIfVersionMatches(ctx, ur)
		completeUpload(ur)
		return
	}

	// 5. 根据任务类型选择不同的上传策略
	if isSyncTask {
		// 同步任务：必须有已完成的提供者才能同步
		if len(completedProviderMap) == 0 {
			llog.C(ctx).Errorf("sync task has no source provider, hash: %s", *ur.FileHash)
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
func uploadFromTempFile(ctx context.Context, ur *entity.UploadRecordORM, needUploadProviders []uint32) {
	// 生成缩略图（在分发前完成）
	generateThumbnailForUpload(ctx, ur)

	// 临时文件存在，直接从临时文件批量上传到所有目标提供者
	batchUploadFromSource(ctx, ur, needUploadProviders, *ur.TempPath)
}

// syncFromExistingProviders 从现有副本同步到新提供者

// syncFromExistingProviders 从现有副本同步到新提供者
func syncFromExistingProviders(ctx context.Context, ur *entity.UploadRecordORM, needUploadProviders []uint32, existingLocations []*entity.FileLocationORM) {
	// 1. 选择最优源提供者（支持分片完整性检查）
	sourceProviderID, sourceLocations, err := SelectBestSourceProvider(existingLocations)
	if err != nil {
		llog.C(ctx).Errorf("select best source provider failed: %v", err)
		markUploadFailed(ctx, ur, "no available source provider")
		return
	}

	llog.C(ctx).Infof("selected source provider: %d with %d chunks", sourceProviderID, len(sourceLocations))

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
			encryptor, err = cryptostream.NewCryptEncryptor(pcfg.EncryptionKey, salt)
			if err != nil {
				markUploadFailed(ctx, ur, fmt.Sprintf("create encryptor failed: %v", err))
				return
			}
		}
	}

	// 4. 下载并合并所有分片到临时文件
	tempFilePath, err := DownloadAndMergeFile(ctx, sourceStorage, sourceLocations, encryptor, *ur.FileHash)
	if err != nil {
		llog.C(ctx).Errorf("download and merge file failed: %v", err)
		markUploadFailed(ctx, ur, fmt.Sprintf("download file failed: %v", err))
		return
	}
	defer os.Remove(tempFilePath) // 确保临时文件最终被删除

	// 更新 upload record 的 TempPath，驱动从这里读取文件路径
	ur.TempPath = &tempFilePath

	llog.C(ctx).Infof("file downloaded and merged to: %s", tempFilePath)

	// 5. 批量上传到所有目标提供者（并发）
	batchUploadFromSource(ctx, ur, needUploadProviders, tempFilePath)
}

// batchUploadFromSource 从源文件批量上传到多个目标提供者

// batchUploadFromSource 从源文件批量上传到多个目标提供者
func batchUploadFromSource(ctx context.Context, ur *entity.UploadRecordORM, targetProviderIds []uint32, sourcePath string) {
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
func getUploadContext(ur *entity.UploadRecordORM) (fileName, parentID string, fileEncrypt bool) {
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

// markFileReadyIfVersionMatches 把上传记录对应的文件记录标为就绪。
//
// CAS：仅当文件当前 version 仍等于本次上传创建时的版本（ur.Version）才标记——
// 若期间同名文件已被更新的 PUT 接管（version 已递增），本次分发属于旧内容，
// 不能把新内容误标为就绪。
//
// 注意 ur.Version 必须是**本次上传**发号得到的版本：复用旧 upload record 时若
// 不刷新版本号，CAS 会静默命中 0 行，文件永远停在"未就绪"。
func markFileReadyIfVersionMatches(ctx context.Context, ur *entity.UploadRecordORM) {
	if ur.ParentId == nil || *ur.ParentId == "" || ur.FileName == nil || *ur.FileName == "" {
		return
	}
	urVersion := uint64(0)
	if ur.Version != nil {
		urVersion = *ur.Version
	}
	affected, err := db.UpdateFileIsReady(*ur.ParentId, *ur.FileName, "", urVersion)
	if err != nil {
		llog.C(ctx).Errorf("update file is_ready failed: %v, parent_id: %s, file_name: %s",
			err, *ur.ParentId, *ur.FileName)
		return
	}
	if affected == 0 {
		// 版本对不上（上传记录的 version 落后于文件记录）或文件已被移动 / 改名。
		// 这里必须告警：内容其实已经写完，但前端会一直显示"未就绪"。
		llog.C(ctx).Warnf("file ready flag not applied (no row matched): parent_id: %s, file_name: %s, version: %d",
			*ur.ParentId, *ur.FileName, urVersion)
		return
	}
	llog.C(ctx).Infof("file marked as ready, parent_id: %s, file_name: %s, version: %d",
		*ur.ParentId, *ur.FileName, urVersion)
}

// uploadToProviderAsync 异步上传到单个提供者，返回是否失败和错误信息

// uploadToProviderAsync 异步上传到单个提供者，返回是否失败和错误信息
func uploadToProviderAsync(ctx context.Context, ur *entity.UploadRecordORM, sourcePath, fileName, parentID string, fileEncrypt bool, tid uint32) (failed bool, errMsg string) {
	// 捕获 panic 并打印堆栈
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			llog.C(ctx).Errorf("[PANIC] uploadToProviderAsync panic: %v\nstack: %s", r, string(buf[:n]))
			failed = true
			errMsg = fmt.Sprintf("panic: %v", r)
		}
	}()

	key := uploadKey{blake3Hash: *ur.FileHash, providerID: tid}
	if !acquireLock(key) {
		llog.C(ctx).Infof("upload already in progress, hash: %s, provider: %d", *ur.FileHash, tid)
		return false, ""
	}
	defer releaseLock(key)

	fl, flErr := getOrCreateFileLocation(tid, *ur.FileHash)
	if flErr != nil {
		return true, fmt.Sprintf("provider %d: %v", tid, flErr)
	}

	if *fl.Status == int32(native.LocationStatus_PCOMPLETED) {
		llog.C(ctx).Infof("file already exists in provider %d", tid)
		// 内容已在 provider 里，但文件记录本身可能还是未就绪（同内容换了文件名的情况），
		// 这里补标——否则该记录永远显示"未就绪"，前端无法预览 / 下载。
		markFileReadyIfVersionMatches(ctx, ur)
		return false, ""
	}

	err := uploadToProvider(ctx, sourcePath, ur, tid, fl, fileEncrypt)
	if err != nil {
		llog.C(ctx).Errorf("upload to provider %d failed: %v", tid, err)
		return true, ""
	}

	// 上传成功，立即更新文件状态为就绪
	markFileReadyIfVersionMatches(ctx, ur)
	return false, ""
}

// finalizeBatchUpload 完成批量上传，处理结果

// finalizeBatchUpload 完成批量上传，处理结果
func finalizeBatchUpload(ur *entity.UploadRecordORM, failedCount int32, errorMessages []string) {
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

		// 只要有 provider 失败，就保留 staging 临时文件：重试要重新走
		// Dispatch → uploadFromTempFile → 读 ur.TempPath（或按 hash 在 owner
		// 隔离层/内容寻址层兜底查找），删掉后重试必然报 "temp file not found"。
		//
		// 历史实现在"已有任一 provider 成功"时就删（理由是内容已安全落库），
		// 结果部分失败的上传记录永远无法重试——2026-10-03 的 139 授权事故里，
		// 用户修好授权后点重试，就是被这条清理挡住而失败的。
		//
		// 代价是失败记录的临时文件会多留一段时间；由兜底 janitor 回收：
		// FAILED 不是进行中状态，不受 hasActiveUploadRecord 保护，超过 7 天
		// 即被删除（见 temp_cleanup.go）。重试成功走 completeUpload 立即清理。
	} else {
		completeUpload(ur)
	}
}

// cleanupUploadTemp 清理上传任务的临时文件
//
// 临时文件分布在两处，都要清理：
//   - owner 隔离层 TempDir/users/<owner>/<hash>/  （新布局：分片 + 合并明文）
//   - 内容寻址层 TempDir/<hash>/                  （旧布局 / 兼容残留）
//
// 同一 hash 归不同 owner 的分片互不影响，隔离层只删本 owner 的 hash 目录。
//
// 临时文件按 hash 共享：同一条内容可能还有别的在途上传记录（PENDING/UPLOADING）
// 正等着它分发，此时保留，等最后一条记录完成时再清理（兜底由 temp janitor 回收）。
func cleanupUploadTemp(ur *entity.UploadRecordORM) {
	if ur.FileHash == nil || *ur.FileHash == "" {
		return
	}
	others, err := db.CountActiveUploadRecordsByHash(*ur.FileHash)
	if err != nil {
		llog.L().Warnf("check active uploads before temp cleanup failed: %v, hash: %s", err, *ur.FileHash)
		return
	}
	if others > 0 {
		llog.L().Infof("keep temp files, %d active upload(s) still use hash: %s", others, *ur.FileHash)
		return
	}
	ownerID := ""
	if ur.OwnerId != nil {
		ownerID = *ur.OwnerId
	}
	if err := utils.CleanupOwnerHashTempDir(*ur.FileHash, ownerID); err != nil {
		llog.L().Warnf("cleanup owner hash temp dir failed: %v", err)
	}
	if err := utils.CleanupFileTempDir(*ur.FileHash); err != nil {
		llog.L().Warnf("cleanup file temp dir failed: %v", err)
	}
}

// uploadToProvider 上传到单个提供者
// sourcePath: 源文件路径（明文）
// providerID: 目标存储提供者ID
// fileEncrypt: 文件是否启用加密（文件夹设置）
func uploadToProvider(ctx context.Context, sourcePath string, ur *entity.UploadRecordORM, providerID uint32, fl *entity.FileLocationORM, fileEncrypt bool) error {
	llog.C(ctx).Infof("[uploadToProvider] start, provider: %d", providerID)

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
		llog.C(ctx).Errorf("[uploadToProvider] storage not found, provider: %d", providerID)
		return failFileLocation(fl, fmt.Sprintf("storage provider not found, id: %d", providerID))
	}

	// 获取类型化配置视图
	pcfg := st.GetProviderConfig()
	if pcfg == nil {
		llog.C(ctx).Errorf("[uploadToProvider] provider config is nil, provider: %d", providerID)
		return failFileLocation(fl, fmt.Sprintf("storage provider config is nil, id: %d", providerID))
	}
	if !pcfg.Enabled {
		llog.C(ctx).Errorf("[uploadToProvider] provider not enabled, provider: %d", providerID)
		return failFileLocation(fl, fmt.Sprintf("storage provider not enabled, id: %d", providerID))
	}

	// 检查源文件是否存在
	if _, err := os.Stat(sourcePath); err != nil {
		llog.C(ctx).Errorf("[uploadToProvider] source file not found: %s, err: %v", sourcePath, err)
		return failFileLocation(fl, fmt.Sprintf("source file not found: %s", sourcePath))
	}

	// 生成加密器（如果需要加密），并记录加密状态到 fl
	encryptor, salt, err := prepareEncryptor(fileEncrypt, pcfg)
	if err != nil {
		llog.C(ctx).Errorf("[uploadToProvider] prepare encryptor failed: %v, provider: %d", err, providerID)
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
		llog.C(ctx).Errorf("[uploadToProvider] upload failed: %v, provider: %d", err, providerID)
		return failFileLocation(fl, err.Error())
	}
	llog.C(ctx).Infof("[uploadToProvider] upload returned, chunks: %d, provider: %d", len(refs), providerID)

	// 检查返回的分片信息
	if len(refs) == 0 {
		llog.C(ctx).Errorf("[uploadToProvider] no file info returned, provider: %d", providerID)
		return failFileLocation(fl, "no file info returned from upload")
	}

	// 处理分片上传结果
	totalUploadedSize := saveUploadChunks(fl, refs, providerID)

	// 更新存储提供者的已使用容量
	if totalUploadedSize > 0 {
		if err := db.IncrementUsedCapacity(providerID, totalUploadedSize); err != nil {
			llog.C(ctx).Errorf("[uploadToProvider] update used capacity failed: %v, provider_id: %d, size: %d", err, providerID, totalUploadedSize)
		}
	}

	hash := ""
	if fl.Blake3Hash != nil {
		hash = *fl.Blake3Hash
	}
	llog.C(ctx).Infof("[uploadToProvider] completed, hash: %s, provider: %d", hash, providerID)
	return nil
}

// completeUpload 完成上传，清理临时文件
func completeUpload(ur *entity.UploadRecordORM) {
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
			result := db.GetDB().Model(&entity.UploadRecordORM{}).
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

		// 清理整个文件临时目录（owner 隔离层 + 内容寻址层，兼容新旧布局）
		cleanupUploadTemp(ur)

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
func markUploadFailed(ctx context.Context, ur *entity.UploadRecordORM, reason string) {
	if ur.Id == nil {
		llog.C(ctx).Errorf("upload failed (no record id): %s, hash: %s", reason, *ur.FileHash)
		return
	}

	// 乐观锁：仅当非 COMPLETED 时才标记失败，防止覆盖已完成状态
	_ = db.TransitionUploadRecordTaskStatus(*ur.Id,
		[]native.UploadStatus{native.UploadStatus_PENDING, native.UploadStatus_UPLOADING, native.UploadStatus_FAILED},
		native.UploadStatus_FAILED, &reason)
	llog.C(ctx).Errorf("upload failed: %s, hash: %s", reason, *ur.FileHash)
}

// markUploadUploading 标记上传中
func markUploadUploading(ctx context.Context, ur *entity.UploadRecordORM) {
	if ur.Id == nil {
		return
	}
	// 乐观锁：仅当 PENDING 时才迁移到 UPLOADING
	if err := db.TransitionUploadRecordTaskStatus(*ur.Id,
		[]native.UploadStatus{native.UploadStatus_PENDING},
		native.UploadStatus_UPLOADING, nil); err != nil {
		llog.C(ctx).Errorf("mark uploading failed: %v, id: %d", err, *ur.Id)
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
		go func(r *entity.UploadRecordORM) {
			defer wg.Done()
			defer func() { <-sem }()
			Dispatch(context.Background(), r)
		}(record)
	}

	wg.Wait()
	llog.L().Infof("resume dispatch completed, processed %d records", len(records))
}

// generateThumbnailForUpload 为上传的文件生成缩略图
