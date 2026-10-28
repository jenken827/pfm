package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/gorm"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/internal/thumbnail"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/utils"
	"lukechampine.com/blake3"
)

const (
	// DefaultChunkSize default 10MB
	DefaultChunkSize = 10 * 1024 * 1024
	// SmallFileThreshold 小文件阈值 10MB
	SmallFileThreshold = 10 * 1024 * 1024
)

// chunkUploadKey 分片上传并发控制键
type chunkUploadKey struct {
	uploadID   uint32
	chunkIndex uint32
}

// chunkUploadLocks 分片上传锁管理
var chunkUploadLocks sync.Map // map[chunkUploadKey]chan struct{}

// acquireChunkLock 获取分片上传锁（非阻塞）
func acquireChunkLock(key chunkUploadKey) bool {
	ch := make(chan struct{})
	_, loaded := chunkUploadLocks.LoadOrStore(key, ch)
	if loaded {
		// 已经有任务在处理
		return false
	}
	return true
}

// releaseChunkLock 释放分片上传锁
func releaseChunkLock(key chunkUploadKey) {
	if ch, ok := chunkUploadLocks.Load(key); ok {
		close(ch.(chan struct{}))
		chunkUploadLocks.Delete(key)
	}
}

// parseAndCreateTags 解析标签字符串并创建文件标签
func parseAndCreateTags(fileID string, tagsStr *string) {
	if tagsStr == nil || *tagsStr == "" {
		return
	}
	// 按逗号分隔标签
	tags := strings.Split(*tagsStr, ",")
	// 去除空白
	var cleanTags []string
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			cleanTags = append(cleanTags, tag)
		}
	}
	if len(cleanTags) == 0 {
		return
	}
	if err := db.CreateFileTags(fileID, cleanTags); err != nil {
		llog.L().Errorf("create file tags failed: %v", err)
	} else {
		llog.L().Infof("created %d tags for file: %s", len(cleanTags), fileID)
	}
}

// validateAndGetParent 验证父文件夹存在且为文件夹类型
func validateAndGetParent(parentID string) (*entity.FileORM, *models.Err) {
	parentFile, err := db.GetFileByID(parentID)
	if err != nil {
		llog.L().Errorf("get parent folder failed: %v", err)
		return nil, &models.Err{
			E: errors.New("parent folder not found"),
			C: native.RespCode_DB_ERROR,
		}
	}

	if parentFile.Type == nil || native.FileType(*parentFile.Type) != native.FileType_FOLDER {
		return nil, &models.Err{
			E: errors.New("parent is not a folder"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	return parentFile, nil
}

// checkFileExists 检查文件是否已存在
// 返回三个文件：
//   - ownerFile:  自己名下同 hash 的记录（用于复用判断，避免全局命中其他用户）
//   - sameNameFile: 同目录同名的记录（用于同名冲突/覆盖判断）
//   - globalFile:  全局同 hash 的记录（仅当 ownerFile 不存在时查询，
//     用于判断存储层数据是否已存在，决定秒传还是完整上传）
func checkFileExists(parentID, fileName, fileHash, ownerID string) (*entity.FileORM, *entity.FileORM, *entity.FileORM, *models.Err) {
	ownerFile, err := db.GetFileByHashAndOwner(fileHash, ownerID)
	if err != nil && err != gorm.ErrRecordNotFound {
		llog.L().Errorf("check file exists by hash and owner failed: %v", err)
		return nil, nil, nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	sameNameFile, err := db.GetFileByParentAndName(parentID, fileName, ownerID)
	if err != nil && err != gorm.ErrRecordNotFound {
		llog.L().Errorf("check same name file failed: %v", err)
		return nil, nil, nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 仅当自己名下没有同 hash 记录时，才需要全局查询判断数据是否存在
	var globalFile *entity.FileORM
	if ownerFile == nil {
		globalFile, err = db.GetFileByHash(fileHash)
		if err != nil && err != gorm.ErrRecordNotFound {
			llog.L().Errorf("check global file by hash failed: %v", err)
			return nil, nil, nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}
	}

	return ownerFile, sameNameFile, globalFile, nil
}

// createSyncTask 创建同步任务（支持 UPSERT：如果同 hash 记录已存在则合并 provider 列表并重试）
//
// owner_id 必须写入发起者：秒传路径（handleInstantUpload）产出的 SYNC 记录同样
// 是"某人发起的任务"，缺 owner 会让该记录对任何用户都不可见/不可操作
// （列表能查到但详情/重试/删除全部 PERMISSION_DENIED）。ctx 由调用方传入以取
// 发起者身份；取不到身份时从父目录继承，与 createSmallFileRecord 保持一致。
func createSyncTask(ctx context.Context, fileName *string, fileSize *uint64, parentID *string, fileHash string, providerIds []uint32) {
	providerIdsJSON, _ := json.Marshal(providerIds)
	targetProviderIds := string(providerIdsJSON)
	emptyStr := ""
	ownerID := getCurrentUserIDFromCtx(ctx)
	if ownerID == "" && parentID != nil && *parentID != "" {
		if parent, pErr := db.GetFileByID(*parentID); pErr == nil && parent != nil && parent.OwnerId != nil {
			ownerID = *parent.OwnerId
		}
	}
	if ownerID == "" {
		llog.C(ctx).Warnf("sync task has no resolvable owner (parent: %s, hash: %s), record will be inaccessible",
			strPtrVal(parentID), fileHash)
	}

	syncRecord := &entity.UploadRecordORM{
		FileName:          fileName,
		FileSize:          fileSize,
		ParentId:          parentID,
		FileHash:          &fileHash,
		TargetProviderIds: &targetProviderIds,
		TempPath:          &emptyStr,
		Status:            utils.EnumPtr(native.LocationStatus_PPENDING),
		TaskStatus:        utils.EnumPtr(native.UploadStatus_PENDING),
		TaskType:          utils.EnumPtr(native.UploadTaskType_SYNC),
		OwnerId:           utils.StringPtr(ownerID),
	}

	gormDB := db.GetDB()
	if err := gormDB.Create(syncRecord).Error; err != nil {
		// 唯一约束冲突 → 查询已有记录，合并 provider 列表并重新 dispatch。
		// 必须带 owner_id 限定：复合唯一索引是 (file_hash, owner_id)，但查询
		// 若只按 file_hash，会命中他人同名 hash 记录并覆写其 target_provider_ids
		// 后重新 dispatch（跨用户串台，与 upload.go:761 同类防护）。
		var existing entity.UploadRecordORM
		if qErr := gormDB.Where("file_hash = ? AND owner_id = ?", fileHash, ownerID).First(&existing).Error; qErr != nil {
			llog.L().Errorf("create sync record failed and lookup also failed: %v / %v", err, qErr)
			return
		}

		// 防误复用：同 hash 冲突的记录必须是 SYNC 类型才允许合并复用。
		// 若是 UPLOAD 类型（真实上传任务，可能正在 Dispatch 中），复用会导致
		// 同一 upload record 被上传与同步两个流程并行处理，两次 prepareEncryptor
		// 竞争写入 file_locations.encryption_salt，最终 DB 里的盐与实际加密用的
		// 随机盐不一致 → 密文无法解密（8/21 相册备份截图损坏事故根因）。
		if existing.TaskType == nil || *existing.TaskType != int32(native.UploadTaskType_SYNC) {
			llog.L().Warnf("sync record conflict with non-sync record #%d (task_type=%v), skip merge to avoid double dispatch",
				*existing.Id, existing.TaskType)
			return
		}

		// 合并新旧 providerIds（去重）
		merged := mergeProviderIds(existing.TargetProviderIds, providerIds)
		mergedJSON, _ := json.Marshal(merged)
		mergedStr := string(mergedJSON)

		// 重置状态为 PPENDING，更新 provider 列表，清除旧错误信息
		if uErr := gormDB.Model(&existing).Updates(map[string]interface{}{
			"target_provider_ids": mergedStr,
			"task_status":         int32(native.UploadStatus_PENDING),
			"message":             "",
		}).Error; uErr != nil {
			llog.L().Errorf("update existing sync record failed: %v", uErr)
			return
		}

		existing.TargetProviderIds = &mergedStr
		existing.TaskStatus = utils.EnumPtr(native.UploadStatus_PENDING)
		msg := ""
		existing.Message = &msg
		llog.L().Infof("reusing existing sync record #%d, merged providers: %s", *existing.Id, mergedStr)
		go storage.Dispatch(context.Background(), &existing)
	} else {
		go storage.Dispatch(context.Background(), syncRecord)
	}
}

// mergeProviderIds 合并已有 JSON 数组与新 providerIds，去重返回
func mergeProviderIds(existingJSON *string, newIds []uint32) []uint32 {
	set := make(map[uint32]bool)
	for _, id := range newIds {
		set[id] = true
	}
	if existingJSON != nil && *existingJSON != "" {
		var existing []uint32
		if err := json.Unmarshal([]byte(*existingJSON), &existing); err == nil {
			for _, id := range existing {
				set[id] = true
			}
		}
	}
	result := make([]uint32, 0, len(set))
	for id := range set {
		result = append(result, id)
	}
	return result
}

// buildExistingEncryptMap 构建每个 provider 的加密状态映射
// 取每个 provider 第一个已完成分片的 Encrypted 字段作为该 provider 的加密状态
func buildExistingEncryptMap(locations []*entity.FileLocationORM) map[uint32]bool {
	result := make(map[uint32]bool)
	for _, loc := range locations {
		if loc.ProviderId == nil || loc.Status == nil || *loc.Status != int32(native.LocationStatus_PCOMPLETED) {
			continue
		}
		pid := *loc.ProviderId
		if _, exists := result[pid]; !exists {
			encrypted := loc.Encrypted != nil && *loc.Encrypted
			result[pid] = encrypted
		}
	}
	return result
}

// handleInstantUpload 处理秒传逻辑
// 返回值：handled 表示是否已处理，需要直接返回；fileToUse 表示应该使用的文件记录
//
// 判定规则（ownerFile 是自己名下同 hash 的记录，globalFile 是全局同 hash 的记录）：
//   - 同目录同名且同 hash → 秒传复用（保留原同名覆盖/冲突语义）
//   - 同目录同名但不同 hash → 覆盖/冲突（由调用方与 overwrite 标志决定）
//   - 自己名下同 hash 但不在目标目录 → 秒传 + 目标目录新增记录
//     （2024-09 起不再做跨目录同名去重：上传按年/月归档后，同一内容出现在
//     不同目录是合法需求；同目录不重名由唯一索引 (owner_id,name,parent_id) 保证）
//   - 自己名下无同 hash，但全局存在 → 秒传 + 新增记录
//   - 全局也不存在 → 完整上传流程（handled=false）
func handleInstantUpload(
	ctx context.Context,
	fileHash string,
	fileName *string,
	fileSize *uint64,
	parentID *string,
	providerIds []uint32,
	ownerFile *entity.FileORM,
	sameNameFile *entity.FileORM,
	globalFile *entity.FileORM,
	overwrite bool,
	tags *string,
) (handled bool, fileToUse *entity.FileORM, err *models.Err) {
	// 秒传数据源：新内容是否已存在于存储（同 hash 的文件记录）。需在
	// 同名分支之前确定 —— 同名覆盖若指向全新内容，不能走"只改记录"捷径，
	// 否则文件会指向没有 file_location 的 blob（读取报 no file location found）。
	dataFile := ownerFile
	if dataFile == nil {
		dataFile = globalFile
	}

	// 处理同名文件（保留原有语义：同目录同名同 hash 秒传，不同 hash 覆盖/冲突）
	if sameNameFile != nil {
		if *sameNameFile.Hash == fileHash {
			// 同名且同 hash，直接返回秒传成功（记录就是目标目录里的，parent 正确）
			llog.C(ctx).Infof("same name and same hash, instant upload: %s", *fileName)
			return true, sameNameFile, nil
		}
		// 同名但 hash 不同，检查覆盖标志
		if !overwrite {
			llog.C(ctx).Infof("same name file exists, overwrite=false: %s", *fileName)
			return true, nil, &models.Err{
				E: fmt.Errorf("file with same name already exists: %s", *fileName),
				C: native.RespCode_ALREADY_EXISTS,
			}
		}
		// 覆盖的新内容在存储中不存在 → 交还 UploadSmall 走真实上传路径
		// （写 temp → createSmallFileRecord/handleOverwriteFile → 异步分发，
		// 由 handleOverwriteFile 统一处理 max_histories 与旧位置清理）。
		if dataFile == nil {
			llog.C(ctx).Infof("same name overwrite with new content, full upload: %s", *fileName)
			return false, nil, nil
		}
		// 需要覆盖，更新文件记录
		// 与 handleOverwriteFile 对齐：CAS 抢占版本（后到者赢），旧内容
		// 始终写入历史，超限裁剪交给 cleanupOldHistories 统一处理。
		// 注意：这里不直接删除旧 hash 的存储位置——旧 hash 可能正处于
		// 分发窗口期（upload_record UPLOADING），直接删位置会使其卡死；
		// 物理回收由 cleanupOldHistories 裁剪历史后做引用检查（含
		// upload_record 活跃检查）决定。
		f := &entity.FileORM{
			Id:   sameNameFile.Id,
			Hash: &fileHash,
			Size: fileSize,
		}
		myVersion := getUploadVersion(ctx)
		taken, updateErr := db.UpdateFileWithHistoryCAS(ctx, f, sameNameFile, myVersion)
		if updateErr != nil {
			llog.C(ctx).Errorf("update file with history failed: %v", updateErr)
			return true, nil, &models.Err{E: updateErr, C: native.RespCode_DB_ERROR}
		}
		if !taken {
			// 版本落后：有更新的上传已接管该文件。重新读取当前记录返回
			// （内容归属已由更新者决定，本次秒传数据在存储中已有副本，无孤儿）
			llog.C(ctx).Warnf("instant overwrite rejected (version stale), id: %s, my_version: %d", *sameNameFile.Id, myVersion)
			latest, gErr := db.GetFileByID(*sameNameFile.Id)
			if gErr == nil && latest != nil {
				return true, latest, nil
			}
			return true, sameNameFile, nil
		}
		if sameNameFile.Id != nil {
			go cleanupOldHistories(*sameNameFile.Id, getMaxHistoriesOfParent(parentID))
		}
		sameNameFile.Hash = &fileHash
		sameNameFile.Size = fileSize
		sameNameFile.Version = &myVersion
		return true, sameNameFile, nil
	}

	// 全局也不存在同 hash → 完整上传流程（dataFile 已在函数开头确定）
	if dataFile == nil {
		return false, nil, nil
	}

	llog.C(ctx).Infof("file already exists by hash: %s", fileHash)

	// 注：此处不再做"自己名下同 hash 且同名 → 直接复用"的跨目录去重。
	// 旧策略：同 hash 文件即使不在同一目录也不允许同时存在（自动备份/照片
	// 归档前无日期识别，靠全局 hash 去重防重复）。现在上传按 年/月 目录归档，
	// 同一内容出现在不同目录是合法需求（同目录内不重名由上方 sameNameFile
	// 分支 + files 唯一索引 (owner_id, name, parent_id) 保证）。因此走下方
	// "秒传 + 新增记录"：目标目录建立自己的条目，物理数据仍按 hash 共享。

	// 检查文件已经在哪些存储提供者上
	existingLocations, dbErr := db.GetFileLocationsByBlake3Hash(fileHash)
	if dbErr != nil {
		llog.C(ctx).Errorf("get existing file locations failed: %v", dbErr)
		return true, nil, &models.Err{E: dbErr, C: native.RespCode_DB_ERROR}
	}

	// ✅ 使用公共方法构建已存在的完整提供者映射（支持分片）
	completeProviderMap := storage.BuildCompleteProviderMap(existingLocations)

	// 检查目标目录的加密设置
	var targetEncrypt bool
	if parentID != nil && *parentID != "" {
		if parentFolder, err := db.GetFileByID(*parentID); err == nil && parentFolder != nil && parentFolder.EnableEncryption != nil {
			targetEncrypt = *parentFolder.EnableEncryption
		}
	}

	// 检查已有副本的加密状态是否与目标目录匹配
	// 如果不匹配，需要重新上传（不能复用加密状态不同的副本）
	existingEncryptMap := buildExistingEncryptMap(existingLocations)
	for pid, isComplete := range completeProviderMap {
		if !isComplete {
			continue
		}
		existingEncrypted := existingEncryptMap[pid]
		if existingEncrypted != targetEncrypt {
			llog.C(ctx).Infof("provider %d encryption mismatch: existing=%v, target=%v, need re-upload",
				pid, existingEncrypted, targetEncrypt)
			delete(completeProviderMap, pid)
		}
	}

	// 计算需要同步到哪些新的存储提供者
	var needSyncProviders []uint32
	for _, pid := range providerIds {
		if !completeProviderMap[pid] {
			needSyncProviders = append(needSyncProviders, pid)
		}
	}

	// 如果需要同步，创建同步任务（只传需要同步的提供者，不是全部）
	if len(needSyncProviders) > 0 {
		llog.C(ctx).Infof("file exists but needs sync to %d new providers, hash: %s", len(needSyncProviders), fileHash)
		createSyncTask(ctx, fileName, fileSize, parentID, fileHash, needSyncProviders)
	}

	// 没有同名文件，创建新的文件记录（秒传：数据已存在，仅新增目录条目）
	myVersion := getUploadVersion(ctx)
	newFileRecord := &entity.FileORM{
		Name:     fileName,
		ParentId: parentID,
		Size:     dataFile.Size,
		Hash:     dataFile.Hash,
		Type:     dataFile.Type,
		Mime:     dataFile.Mime,
		IsReady:  utils.BoolPtr(true), // 秒传：文件已存在，标记为就绪
		Version:  &myVersion,
	}
	ownerID := getCurrentUserIDFromCtx(ctx)
	newFileRecord.OwnerId = &ownerID
	if createErr := db.CreateFile(newFileRecord); createErr != nil {
		llog.C(ctx).Errorf("create file record failed: %v", createErr)
		return true, nil, &models.Err{E: createErr, C: native.RespCode_DB_ERROR}
	}
	// 容量统计：秒传新增文件影响用户占用
	if newFileRecord.Id != nil {
		MarkFileCapacityChanged(ownerID)
	}
	// 创建文件标签
	if newFileRecord.Id != nil {
		parseAndCreateTags(*newFileRecord.Id, tags)
	}
	return true, newFileRecord, nil
}

// handleOverwriteFile 处理覆盖上传或新建文件记录
// 返回创建的文件记录和错误
//
// 并发上传仲裁：file 记录一律携带版本号（version），落库使用 CAS
// （当前 version < 本次上传分配的 myVersion 才生效）。后到达的 PUT
// 拿到更大的版本号，因此"后到者赢"；版本落后的请求 CAS 失败，不会
// 覆盖已被更新的内容。覆盖时旧内容始终写入 file_histories（供分发中
// 的旧 hash 位置保留引用，物理清理交给 cleanupOldHistories 的引用检查），
// 不再在覆盖路径直接删除旧存储位置。
//
// 位置行不变式：落库（把文件记录指向新 hash）之前，先为 providerIDs 预建
// 新 hash 的未完成 file_location 行，使「文件记录指向的 hash 必有位置行」
// 恒成立——否则从记录落库到异步分发首次上传成功之间（可达数秒）读取端
// 会命中 len(locations)==0 的硬 500（详见 storage.PreallocateFileLocations）。
func handleOverwriteFile(
	ctx context.Context,
	fileRecord *entity.FileORM,
	overwriteID string,
	cleanupPath string,
	providerIDs []uint32,
) (*entity.FileORM, *models.Err) {
	myVersion := getUploadVersion(ctx)

	storage.PreallocateFileLocations(ctx, strPtrVal(fileRecord.Hash), providerIDs)

	// 容量统计标记（新建/覆盖都改变用户占用）
	markUploadedFile := func() {
		owner := strPtrVal(fileRecord.OwnerId)
		if owner == "" {
			owner = getCurrentUserIDFromCtx(ctx)
		}
		MarkFileCapacityChanged(owner)
	}

	if overwriteID == "" {
		// 新上传：创建新的文件记录（记录当前版本号）
		fileRecord.Version = &myVersion
		if err := db.CreateFile(fileRecord); err != nil {
			cleanupUploadTempIfOrphan(cleanupPath, strPtrVal(fileRecord.Hash))
			llog.C(ctx).Errorf("create file record failed: %v", err)
			return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
		}
		llog.C(ctx).Infof("new file created, id: %s, hash: %s, version: %d", *fileRecord.Id, *fileRecord.Hash, myVersion)
		markUploadedFile()
		return fileRecord, nil
	}

	// 覆盖上传
	fileRecord.Id = &overwriteID
	oldFile, err := db.GetFileByID(overwriteID)
	if err != nil && err != gorm.ErrRecordNotFound {
		cleanupUploadTempIfOrphan(cleanupPath, strPtrVal(fileRecord.Hash))
		llog.C(ctx).Errorf("get old file record failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	if oldFile != nil {
		// 获取父文件夹的 max_histories 配置
		parentID := fileRecord.ParentId
		maxHistories := uint32(0)
		if parentID != nil && *parentID != "" {
			parentFolder, err := db.GetFileByID(*parentID)
			if err == nil && parentFolder != nil && parentFolder.MaxHistories != nil {
				maxHistories = *parentFolder.MaxHistories
			}
		}

		// CAS 抢占版本 + 旧内容始终写入历史（事务内）。
		// 无论 max_histories 是否 0 都先写历史：覆盖瞬间旧 hash 需要被
		// file_histories 引用（其分发可能仍在途），物理位置是否回收由
		// cleanupOldHistories 按 max_histories 裁剪后做引用检查决定。
		taken, uErr := db.UpdateFileWithHistoryCAS(ctx, fileRecord, oldFile, myVersion)
		if uErr != nil {
			cleanupUploadTempIfOrphan(cleanupPath, strPtrVal(fileRecord.Hash))
			llog.C(ctx).Errorf("update file with history failed: %v", uErr)
			return nil, &models.Err{E: uErr, C: native.RespCode_DB_ERROR}
		}
		if !taken {
			// 版本落后：有更新的上传已接管该文件（并发 PUT）。
			// 本次内容不落库；调用方负责清理临时文件。返回错误通知上层：
			// WebDAV/客户端可重试，重试会拿到更新的版本号并最终生效。
			llog.C(ctx).Warnf("file takeover rejected (version stale), id: %s, my_version: %d, current_version: %d, new_hash: %s",
				*fileRecord.Id, myVersion, derefVersion(oldFile.Version), *fileRecord.Hash)
			return nil, &models.Err{
				E: fmt.Errorf("file has been updated by a concurrent upload, please retry"),
				C: native.RespCode_ALREADY_EXISTS,
			}
		}
		llog.C(ctx).Infof("file updated with history, id: %s, old_hash: %s, new_hash: %s, version: %d",
			*fileRecord.Id, *oldFile.Hash, *fileRecord.Hash, myVersion)

		// 按 max_histories 异步裁剪历史（超限旧历史被移除后，其 hash 的
		// 存储位置由 cleanupUnreferencedHashLocations 引用检查后回收；
		// 分发在途的 hash 会被 upload_record 活跃检查拦下，不会误删）
		go cleanupOldHistories(*fileRecord.Id, maxHistories)
		markUploadedFile()
		return fileRecord, nil
	}

	// 旧文件不存在（可能被删除了），创建新记录
	fileRecord.Version = &myVersion
	if err := db.CreateFile(fileRecord); err != nil {
		cleanupUploadTempIfOrphan(cleanupPath, strPtrVal(fileRecord.Hash))
		llog.C(ctx).Errorf("create file record failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	llog.C(ctx).Warnf("overwrite file not found, created new record, id: %s, version: %d", *fileRecord.Id, myVersion)
	markUploadedFile()
	return fileRecord, nil
}

// cleanupUploadTempIfOrphan 清理本次上传的临时文件，但同 hash 还有别的在途
// 上传（PENDING/UPLOADING）时保留。
//
// 临时文件按内容寻址共享：小文件落 TempDir/<hash>/<name>，同一内容被并发上传
// （客户端重复投递、WebDAV PUT 与 App 同时写同一文件）时两个请求用的是同一份
// 文件。此前错误分支无条件删掉它，等于把另一路上传正等着分发的源文件删了，
// 对方随后在 Dispatch 里报 temp file not found 并整单判失败。
// 查不到在途记录时保守保留，交给 temp janitor 按超龄回收。
func cleanupUploadTempIfOrphan(cleanupPath, fileHash string) {
	if cleanupPath == "" {
		return
	}
	if otherActiveUploadsForHash(fileHash) {
		llog.L().Infof("keep shared temp file, other active upload(s) still use hash: %s", fileHash)
		return
	}
	os.Remove(cleanupPath)
}

// otherActiveUploadsForHash 是否还有在途（PENDING/UPLOADING）上传记录在用这个 hash
// 的临时文件。查询失败按"有"处理（保守保留），交给 temp janitor 兜底回收。
func otherActiveUploadsForHash(fileHash string) bool {
	if fileHash == "" {
		return false
	}
	active, err := db.CountActiveUploadRecordsByHash(fileHash)
	if err != nil {
		llog.L().Warnf("check active uploads failed: %v, hash: %s", err, fileHash)
		return true
	}
	return active > 0
}

// derefVersion 安全解引用版本号指针（nil 视为 0）
func derefVersion(v *uint64) uint64 {
	if v == nil {
		return 0
	}
	return *v
}

// getMaxHistoriesOfParent 读取父文件夹的 max_histories 配置（读不到返回 0）
func getMaxHistoriesOfParent(parentID *string) uint32 {
	if parentID == nil || *parentID == "" {
		return 0
	}
	parentFolder, err := db.GetFileByID(*parentID)
	if err == nil && parentFolder != nil && parentFolder.MaxHistories != nil {
		return *parentFolder.MaxHistories
	}
	return 0
}

// checkSameNameFile 检查同名文件并返回 overwriteID
func checkSameNameFile(sameNameFile *entity.FileORM, fileName string, overwrite bool) (string, *models.Err) {
	if sameNameFile == nil {
		return "", nil
	}
	if !overwrite {
		llog.L().Infof("same name file exists, overwrite=false: %s", fileName)
		return "", &models.Err{
			E: fmt.Errorf("file with same name already exists: %s", fileName),
			C: native.RespCode_ALREADY_EXISTS,
		}
	}
	return *sameNameFile.Id, nil
}

// InitUpload init upload flow
func InitUpload(ctx context.Context, req *native.InitUploadReq) (*native.InitUploadRespData, *models.Err) {
	if *req.ParentId == "" {
		return nil, &models.Err{
			E: errors.New("cannot upload file to root directory, please create a directory first"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 权限检查：检查对父目录的上传权限
	if !CheckOwnership(ctx, *req.ParentId) {
		return nil, &models.Err{
			E: fmt.Errorf("permission denied: no upload access to directory %s", *req.ParentId),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	blake3Hash := *req.Hash

	// 如果前端传递了 upload_id，根据 upload_id 获取上传记录
	if req.UploadId != nil && *req.UploadId > 0 {
		record, chunks, err := db.GetUploadRecordByID(*req.UploadId, true)
		if err != nil && err != gorm.ErrRecordNotFound {
			llog.C(ctx).Errorf("get upload record by ID failed: %v", err)
			return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
		}
		if record != nil {
			// 比对 hash 是否相等
			if record.FileHash != nil && *record.FileHash == blake3Hash {
				// hash 相等，认为是续传，更新名称（如果不同的话）
				if record.FileName != nil && *record.FileName != *req.Name {
					llog.C(ctx).Infof("update filename from %s to %s for upload record %d", *record.FileName, *req.Name, *record.Id)
					record.FileName = req.Name
					if err := db.GetDB().Save(record).Error; err != nil {
						llog.C(ctx).Errorf("update filename failed: %v", err)
						return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
					}
				}
				return resumeExistingUpload(record, chunks, blake3Hash)
			} else {
				// hash 不相等，创建新的上传记录
				llog.C(ctx).Infof("hash mismatch for upload record %d, creating new upload", *record.Id)
			}
		}
	}

	// 检查是否有未完成的上传任务（断点续传）
	record, chunks, err := db.GetUploadRecordByHash(blake3Hash, true)
	if err != nil && err != gorm.ErrRecordNotFound {
		llog.C(ctx).Errorf("get uncomplete uploads failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}

	// 如果存在未完成的上传任务，返回续传信息
	if record != nil {
		return resumeExistingUpload(record, chunks, blake3Hash)
	}

	// 验证父文件夹
	_, mErr := validateAndGetParent(*req.ParentId)
	if mErr != nil {
		return nil, mErr
	}

	// 按目标目录的绑定解析落盘存储提供者（目录绑定 → 祖先继承 → 用户分配集）
	providerIds, mErr := resolveProviderIdsForFolder(ctx, *req.ParentId)
	if mErr != nil {
		return nil, mErr
	}

	// 请求到达即发号（并发上传仲裁，同 key 后到者版本号更大）
	userID := getCurrentUserIDFromCtx(ctx)
	ctx = withUploadVersion(ctx, AllocateFileVersion(userID, *req.ParentId, *req.Name))

	// 检查文件是否已存在
	ownerFile, sameNameFile, globalFile, mErr := checkFileExists(*req.ParentId, *req.Name, blake3Hash, userID)
	if mErr != nil {
		return nil, mErr
	}

	// 处理秒传逻辑
	handled, _, mErr := handleInstantUpload(
		ctx, blake3Hash, req.Name, req.Size, req.ParentId,
		providerIds, ownerFile, sameNameFile, globalFile, req.GetOverwrite(),
		req.Tags,
	)
	if mErr != nil {
		return nil, mErr
	}
	if handled {
		exists := true
		return &native.InitUploadRespData{Exists: &exists}, nil
	}

	// 文件不存在，创建新的上传任务
	return createNewUploadTask(ctx, req, blake3Hash, providerIds, sameNameFile)
}

// resumeExistingUpload 处理断点续传
func resumeExistingUpload(record *entity.UploadRecordORM, chunks []*entity.UploadChunkRecordORM, blake3Hash string) (*native.InitUploadRespData, *models.Err) {
	// 根据 task_type 区分任务类型
	if record.TaskType != nil && *record.TaskType == int32(native.UploadTaskType_SYNC) {
		llog.L().Infof("found sync task for hash: %s, file already exists", blake3Hash)
		exists := true
		return &native.InitUploadRespData{Exists: &exists}, nil
	}

	// 普通上传任务：返回 upload_id 和已上传分片（位图）
	llog.L().Infof("resume upload: %d, chunk_total: %d, chunk_size: %d",
		*record.Id, *record.ChunkTotal, *record.ChunkSize)
	uploadID := *record.Id
	chunkTotal := *record.ChunkTotal
	chunkSize := *record.ChunkSize

	uploadedChunksBitmap := make([]byte, (chunkTotal+7)/8)
	if chunks != nil {
		uploadedChunksBitmap = buildUploadedBitmap(chunks, chunkTotal)
	}

	return &native.InitUploadRespData{
		UploadId:       &uploadID,
		UploadedChunks: uploadedChunksBitmap,
		ChunkTotal:     &chunkTotal,
		ChunkSize:      &chunkSize,
	}, nil
}

// createNewUploadTask 创建新的上传任务
func createNewUploadTask(ctx context.Context, req *native.InitUploadReq, blake3Hash string, providerIds []uint32, sameNameFile *entity.FileORM) (*native.InitUploadRespData, *models.Err) {
	overwriteID, mErr := checkSameNameFile(sameNameFile, *req.Name, req.GetOverwrite())
	if mErr != nil {
		return nil, mErr
	}

	// 计算分片大小和总数（优先使用客户端指定的分片大小）
	chunkSize := uint64(DefaultChunkSize)
	if req.ChunkSize != nil && *req.ChunkSize > 0 {
		chunkSize = *req.ChunkSize
	}
	chunkTotal := uint32((*req.Size + chunkSize - 1) / chunkSize)
	if chunkTotal == 0 {
		chunkTotal = 1
	}

	// 创建分片临时目录（按 owner 隔离，避免多用户同 hash 并发上传互相覆盖）
	if err := utils.CreateFileTempDir(blake3Hash, getCurrentUserIDFromCtx(ctx), true); err != nil {
		llog.C(ctx).Errorf("create temp dir failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}

	tp := utils.GetFileTempPath(blake3Hash, *req.Name)
	// 将 provider IDs 转为 JSON 字符串存储
	providerIdsJSON, _ := json.Marshal(providerIds)
	targetProviderIds := string(providerIdsJSON)

	// 记录本次上传的版本号（由入口发号器分配，贯穿到分发完成回调做 CAS）
	urVersion := getUploadVersion(ctx)

	ur := &entity.UploadRecordORM{
		FileHash:          &blake3Hash,
		FileName:          req.Name,
		FileSize:          req.Size,
		ParentId:          req.ParentId,
		ChunkTotal:        &chunkTotal,
		ChunkSize:         &chunkSize,
		TempPath:          &tp,
		OverwriteId:       &overwriteID,
		Status:            utils.EnumPtr(native.LocationStatus_PPENDING),
		TaskStatus:        utils.EnumPtr(native.UploadStatus_PENDING),
		TargetProviderIds: &targetProviderIds,
		TaskType:          utils.EnumPtr(native.UploadTaskType_UPLOAD),
		Tags:              req.Tags,
		Version:           &urVersion,
		OwnerId:           utils.StringPtr(getCurrentUserIDFromCtx(ctx)),
	}

	if err := db.GetDB().Create(ur).Error; err != nil {
		// 唯一约束冲突（复合 hash+owner 已存在）→ 查询本人已有记录并复用。
		// 注意必须带 owner_id 限定：老实现的全局 file_hash 唯一约束会让
		// B 命中 A 的记录并篡改其 file_name/parent_id（跨用户串台）。
		ownerID := getCurrentUserIDFromCtx(ctx)
		var existing entity.UploadRecordORM
		if qErr := db.GetDB().Where("file_hash = ? AND owner_id = ?", blake3Hash, ownerID).First(&existing).Error; qErr != nil {
			llog.C(ctx).Errorf("create upload record failed and lookup also failed: %v / %v", err, qErr)
			return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
		}

		// 更新已有记录的文件名、父目录、覆盖ID等信息
		updates := map[string]interface{}{
			"file_name":    *req.Name,
			"parent_id":    *req.ParentId,
			"overwrite_id": overwriteID,
			"task_status":  int32(native.UploadStatus_PENDING),
			"message":      "",
			"tags":         req.Tags,
		}
		if uErr := db.GetDB().Model(&existing).Updates(updates).Error; uErr != nil {
			llog.C(ctx).Errorf("update existing upload record failed: %v", uErr)
			return nil, &models.Err{E: uErr, C: native.RespCode_INTERNAL_ERROR}
		}

		llog.C(ctx).Infof("reuse existing upload record: %d, new filename: %s", *existing.Id, *req.Name)
		uploadID := *existing.Id
		chunkTotal := *existing.ChunkTotal
		chunkSize := *existing.ChunkSize
		return &native.InitUploadRespData{
			UploadId:       &uploadID,
			UploadedChunks: make([]byte, (chunkTotal+7)/8),
			ChunkTotal:     &chunkTotal,
			ChunkSize:      &chunkSize,
		}, nil
	}
	llog.C(ctx).Infof("init upload: %d, chunk_total: %d, chunk_size: %d",
		*ur.Id, chunkTotal, chunkSize)

	return &native.InitUploadRespData{
		UploadId:       ur.Id,
		UploadedChunks: make([]byte, (chunkTotal+7)/8),
		ChunkTotal:     &chunkTotal,
		ChunkSize:      &chunkSize,
	}, nil
}

func buildUploadedBitmap(chunks []*entity.UploadChunkRecordORM, chunkTotal uint32) []byte {
	// 构建已上传分片位图
	uploadedChunksBitmap := make([]byte, (chunkTotal+7)/8)
	for _, r := range chunks {
		chunkIdx := *r.Index // 位图索引从0开始
		uploadedChunksBitmap[chunkIdx/8] |= (1 << (chunkIdx % 8))
	}
	return uploadedChunksBitmap

}

// UploadChunkSingle 上传单个分片（非流式）
func UploadChunkSingle(ctx context.Context, req *native.UploadChunkReq) *models.Err {
	uploadID := *req.UploadId
	chunkIndex := *req.ChunkIndex
	chunkHash := *req.ChunkHash

	llog.C(ctx).Infof("upload chunk: upload_id=%d, chunk_index=%d", uploadID, chunkIndex)

	// 检查上传是否已进入异步完成阶段（防止前端重试报错）
	if uploadDone.IsDone(uploadID) {
		llog.C(ctx).Infof("upload already processing/done, ignoring chunk: upload_id=%d, chunk_index=%d", uploadID, chunkIndex)
		return nil
	}

	// 并发控制：防止同时上传同一个分片
	key := chunkUploadKey{
		uploadID:   uint32(uploadID),
		chunkIndex: chunkIndex,
	}
	if !acquireChunkLock(key) {
		llog.C(ctx).Warnf("chunk upload already in progress: upload_id=%d, chunk_index=%d", uploadID, chunkIndex)
		return &models.Err{
			E: errors.New("chunk upload already in progress"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}
	defer releaseChunkLock(key)

	// 获取上传记录
	uploadRecord, _, err := db.GetUploadRecordByID(uploadID, false)
	if err != nil {
		if err == gorm.ErrRecordNotFound && uploadDone.IsDone(uploadID) {
			llog.C(ctx).Infof("upload record cleaned up but marked done, ignoring chunk: upload_id=%d", uploadID)
			return nil
		}
		llog.C(ctx).Errorf("get upload record failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 检查分片记录是否已存在（幂等性）
	existingChunk, err := db.GetChunkRecord(uploadID, chunkIndex)
	if err == nil && existingChunk != nil {
		llog.C(ctx).Infof("chunk record already exists, skip: upload_id=%d, chunk_index=%d", uploadID, chunkIndex)
		return nil
	}

	chunkPath := utils.GetChunkTempPath(*uploadRecord.FileHash, strPtrVal(uploadRecord.OwnerId), int(chunkIndex))

	// 写入分片文件
	chunkFile, err := os.Create(chunkPath)
	if err != nil {
		llog.C(ctx).Errorf("create chunk file failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	defer chunkFile.Close()

	// 计算 hash 并写入
	hasher := blake3.New(32, nil)
	var totalSize int64

	// 写入分片数据
	if len(req.Chunk) > 0 {
		n, err := chunkFile.Write(req.Chunk)
		if err != nil {
			return &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
		}
		hasher.Write(req.Chunk)
		totalSize += int64(n)
	}

	// 验证分片 hash（如果提供了）
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if chunkHash != "" && chunkHash != actualHash {
		os.Remove(chunkPath)
		llog.C(ctx).Errorf("chunk hash mismatch: expected=%s, actual=%s", chunkHash, actualHash)
		return &models.Err{E: errors.New("chunk hash mismatch"), C: native.RespCode_PARAMS_ERROR}
	}

	// 创建新的分片记录
	newRecord := &entity.UploadChunkRecordORM{
		UploadId: &uploadID,
		Hash:     &actualHash,
		Index:    &chunkIndex,
	}
	if err := db.GetDB().Create(newRecord).Error; err != nil {
		llog.C(ctx).Errorf("create chunk record failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}

	llog.C(ctx).Infof("chunk uploaded successfully: upload_id=%d, chunk_index=%d, size=%d", uploadID, chunkIndex, totalSize)
	return nil
}

// CompleteUpload 完成上传
func CompleteUpload(ctx context.Context, req *native.CompleteUploadReq) *models.Err {
	uploadID := *req.UploadId

	llog.C(ctx).Infof("complete upload: %d", uploadID)

	// 检查上传是否已进入异步完成阶段
	if uploadDone.IsDone(uploadID) {
		llog.C(ctx).Infof("upload already processing/done, skipping complete: %d", uploadID)
		return nil
	}

	uploadRecord, chunks, err := db.GetUploadRecordByID(uploadID, true)
	if err != nil {
		if err == gorm.ErrRecordNotFound && uploadDone.IsDone(uploadID) {
			llog.C(ctx).Infof("upload record cleaned up but marked done: %d", uploadID)
			return nil
		}
		llog.C(ctx).Errorf("get upload record failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	if len(chunks) == 0 {
		llog.C(ctx).Errorf("no chunk records found")
		return &models.Err{E: errors.New("no upload records found"), C: native.RespCode_PARTIAL_UPLOAD_ERROR}
	}

	// 检查所有分片是否都已上传
	if len(chunks) < int(*uploadRecord.ChunkTotal) {
		llog.C(ctx).Errorf("not all chunks uploaded: %d/%d", len(chunks), *uploadRecord.ChunkTotal)
		return &models.Err{
			E: fmt.Errorf("not all chunks uploaded: %d/%d", len(chunks), *uploadRecord.ChunkTotal),
			C: native.RespCode_PARTIAL_UPLOAD_ERROR,
		}
	}

	// ✅ 异步处理：立即返回，后台合并文件
	go processCompleteUpload(context.Background(), uploadRecord, chunks)

	return nil
}

// processCompleteUpload 后台处理文件合并和上传
func processCompleteUpload(ctx context.Context, uploadRecord *entity.UploadRecordORM, chunks []*entity.UploadChunkRecordORM) {
	// 标记上传已进入异步处理阶段，前端重试时可识别
	uploadDone.MarkDone(*uploadRecord.Id)
	llog.C(ctx).Infof("start processing complete upload: %d", *uploadRecord.Id)

	// 同一目标文件串行化：合并后的临时文件路径按 hash+owner 共享，并发合并会
	// 互相截断/交错；且同一目标文件的两条记录会互相撞唯一索引并在错误清理时
	// 删掉对方要分发的源文件。后台协程持锁，不阻塞 HTTP 请求。
	unlock := lockUploadTarget(strPtrVal(uploadRecord.OwnerId),
		strPtrVal(uploadRecord.ParentId), strPtrVal(uploadRecord.FileName))
	defer unlock()

	// 1. 合并所有分片到 owner 隔离目录
	// 注意：合并输出也必须按 owner 隔离。若写内容寻址层 TempDir/<hash>/<name>，
	// 同一 hash 的两个用户并发上传时两次 os.Create + 写入会交错，产出损坏文件。
	ownerID := strPtrVal(uploadRecord.OwnerId)
	mergeDir := utils.GetChunkTempDir(*uploadRecord.FileHash, ownerID)
	if mkErr := os.MkdirAll(mergeDir, 0755); mkErr != nil {
		markUploadFailedSync(uploadRecord, fmt.Sprintf("create merge dir failed: %v", mkErr))
		return
	}
	mergePath := filepath.Join(mergeDir, utils.MergedFileName(strPtrVal(uploadRecord.FileName)))
	uploadRecord.TempPath = &mergePath

	outputFile, err := os.Create(mergePath)
	if err != nil {
		markUploadFailedSync(uploadRecord, fmt.Sprintf("create output file failed: %v", err))
		return
	}
	defer outputFile.Close()

	// 使用 blake3 计算文件哈希（与前端一致）
	hasher := blake3.New(32, nil)
	var totalSize int64

	for i := range chunks {
		f := utils.GetChunkTempPath(*uploadRecord.FileHash, strPtrVal(uploadRecord.OwnerId), i)
		chunkFile, err := os.Open(f)
		if err != nil {
			markUploadFailedSync(uploadRecord, fmt.Sprintf("open chunk file failed: %v", err))
			cleanupChunks(uploadRecord)
			return
		}

		n, err := io.Copy(io.MultiWriter(outputFile, hasher), chunkFile)
		chunkFile.Close()
		if err != nil {
			markUploadFailedSync(uploadRecord, fmt.Sprintf("copy chunk to output failed: %v", err))
			cleanupChunks(uploadRecord)
			return
		}
		totalSize += n
	}

	finalHash := hex.EncodeToString(hasher.Sum(nil))

	if finalHash != *uploadRecord.FileHash {
		markUploadFailedSync(uploadRecord, fmt.Sprintf("file hash mismatch: expected %s, got %s", *uploadRecord.FileHash, finalHash))
		cleanupChunks(uploadRecord)
		return
	}

	llog.C(ctx).Infof("file merged successfully: %d bytes", totalSize)

	// 2. 获取文件类型
	filetype, mime := GetFileType(utils.GetChunkTempPath(*uploadRecord.FileHash, strPtrVal(uploadRecord.OwnerId), 0), *uploadRecord.FileName)

	// 3. 创建文件记录
	// 从父目录获取 owner_id（goroutine 中无用户上下文）
	ownerID = ""
	if uploadRecord.ParentId != nil {
		if parent, pErr := db.GetFileByID(*uploadRecord.ParentId); pErr == nil && parent != nil && parent.OwnerId != nil {
			ownerID = *parent.OwnerId
		}
	}
	fileRecord := &entity.FileORM{
		Name:     uploadRecord.FileName,
		ParentId: uploadRecord.ParentId,
		Size:     func() *uint64 { v := uint64(totalSize); return &v }(),
		Hash:     &finalHash,
		Type:     &filetype,
		Mime:     &mime,
		IsReady:  utils.BoolPtr(false), // 文件创建时标记为未就绪
	}
	fileRecord.OwnerId = &ownerID

	// 恢复本次上传的版本号（分片上传的合并流程运行在后台 goroutine 中，
	// 无原请求 ctx，版本号从 upload_record 读取后注入，供覆盖 CAS 使用）
	myVersion := uint64(0)
	if uploadRecord.Version != nil {
		myVersion = *uploadRecord.Version
	}
	ctx = withUploadVersion(ctx, myVersion)

	// 处理文件记录（新建或覆盖）
	overwriteID := ""
	if uploadRecord.OverwriteId != nil {
		overwriteID = *uploadRecord.OverwriteId
	}
	createdFile, mErr := handleOverwriteFile(ctx, fileRecord, overwriteID, "", targetProviderIDsOf(uploadRecord))
	if mErr != nil {
		markUploadFailedSync(uploadRecord, fmt.Sprintf("create file record failed: %v", mErr))
		cleanupChunks(uploadRecord)
		return
	}

	// 创建文件标签
	if createdFile.Id != nil {
		parseAndCreateTags(*createdFile.Id, uploadRecord.Tags)
	}

	llog.C(ctx).Infof("file record created: %s", *createdFile.Id)

	// 4. 移除分片临时文件（只删 chunk_*，保留合并明文 merged_<name>）
	// 合并明文与分片同处 chunks/，而紧接着的异步分发正是从它读源
	// （uploadRecord.TempPath = mergePath）——整目录删除会让 Dispatch 报
	// "temp file not found"，上传编排失败且文件记录已落库（该 hash 永远没有
	// file_location 行）。整目录清理由 dispatch 终态的 cleanupUploadTemp 负责。
	if err := utils.RemoveChunkTempFiles(*uploadRecord.FileHash, strPtrVal(uploadRecord.OwnerId)); err != nil {
		llog.C(ctx).Warnf("remove chunk temp files failed: %v", err)
	}

	// 5. 上传到存储（异步，在独立协程中执行）
	// 注意：必须在独立协程中执行，否则 outputFile 的 defer Close() 无法及时释放文件句柄
	go storage.Dispatch(context.Background(), uploadRecord)
}

// targetProviderIDsOf 解析 upload_record 的目标提供者列表
// 供「位置行预建」（handleOverwriteFile）与分发共用，保证预建行与分发目标一致。
func targetProviderIDsOf(ur *entity.UploadRecordORM) []uint32 {
	if ur == nil || ur.TargetProviderIds == nil || *ur.TargetProviderIds == "" {
		return nil
	}
	var ids []uint32
	if err := json.Unmarshal([]byte(*ur.TargetProviderIds), &ids); err != nil {
		llog.L().Warnf("parse target provider ids failed: %v", err)
		return nil
	}
	return ids
}

// markUploadFailedSync 标记分块上传失败（同步版本，用于 processCompleteUpload 错误路径）
func markUploadFailedSync(ur *entity.UploadRecordORM, reason string) {
	if ur.Id != nil {
		failedStatus := int32(native.UploadStatus_FAILED)
		ur.TaskStatus = &failedStatus
		ur.Message = &reason
		// 乐观锁：合并分片阶段 record 仍为 PENDING，仅当 PENDING 时才标记失败
		if err := db.TransitionUploadRecordTaskStatus(*ur.Id,
			[]native.UploadStatus{native.UploadStatus_PENDING},
			native.UploadStatus_FAILED, &reason); err != nil {
			llog.L().Errorf("mark upload failed error: %v, id: %d", err, *ur.Id)
		}
	}
	llog.L().Errorf("chunk upload failed: %s, hash: %s", reason, *ur.FileHash)
}

// cleanupChunks 清理分片临时文件
// 分片目录按 (owner, hash) 共享：同 hash 还有别的在途上传时保留，
// 否则会删掉对方正在合并的源分片（同上一条 temp file not found 事故）。
func cleanupChunks(ur *entity.UploadRecordORM) {
	if otherActiveUploadsForHash(strPtrVal(ur.FileHash)) {
		llog.L().Infof("keep chunk temp dir, other active upload(s) still use hash: %s", strPtrVal(ur.FileHash))
		return
	}
	if err := os.RemoveAll(utils.GetChunkTempDir(*ur.FileHash, strPtrVal(ur.OwnerId))); err != nil {
		llog.L().Warnf("remove chunks failed: %v", err)
	}
}

// checkUploadRecordOwnership 校验当前用户是否为上传记录的发起者
// 管理/普通操作一律按 owner 判定：上传记录属个人任务，管理员也不能
// 越权取消/删除/重试他人上传。返回 false 时调用方应拒绝请求。
// 与 ListUploadRecords / RetryAllFailedUploadRecords 同为"仅本人"口径，
// 保证列表可见即可操作（owner 为空的历史脏记录不可见也不可操作）。
func checkUploadRecordOwnership(ctx context.Context, record *entity.UploadRecordORM) bool {
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return false
	}
	return record.OwnerId != nil && *record.OwnerId == userID
}

// CancelUpload 取消上传
func CancelUpload(ctx context.Context, req *native.CancelUploadReq) *models.Err {
	uploadID := *req.UploadId

	llog.C(ctx).Infof("cancel upload: %d", uploadID)

	// 上传已完成/处理中，无需取消
	if uploadDone.IsDone(uploadID) {
		llog.C(ctx).Infof("upload already processing/done, ignoring cancel: %d", uploadID)
		return nil
	}

	uploadRecord, _, err := db.GetUploadRecordByID(uploadID, true)
	if err != nil && err != gorm.ErrRecordNotFound {
		llog.C(ctx).Errorf("get upload record failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	if err == gorm.ErrRecordNotFound {
		return nil
	}

	// 归属校验：仅发起者可取消自己的上传
	if !checkUploadRecordOwnership(ctx, uploadRecord) {
		return &models.Err{E: fmt.Errorf("permission denied: not owner of upload record"), C: native.RespCode_PERMISSION_DENIED}
	}

	// 删除数据库记录后再清理临时文件：记录本身此时还是 PENDING/UPLOADING，
	// 先删记录才能用引用检查区分「是否还有别的在途上传共用这个 hash 的临时文
	// 件」——按 hash 共享的目录不能因为取消了一条就整目录删掉。
	if err := db.ClearUploadRecords(uploadID); err != nil {
		llog.C(ctx).Errorf("delete upload record failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	if uploadRecord.TempPath != nil && *uploadRecord.TempPath != "" && !otherActiveUploadsForHash(strPtrVal(uploadRecord.FileHash)) {
		os.RemoveAll(path.Dir(*uploadRecord.TempPath))
	}

	return nil
}

// UploadSmall 小文件上传（< 10MB）
func UploadSmall(ctx context.Context, req *native.UploadSmallReq) (*native.UploadSmallRespData, *models.Err) {
	// 权限检查：检查对父目录的上传权限
	if !CheckOwnership(ctx, *req.ParentId) {
		return nil, &models.Err{
			E: fmt.Errorf("permission denied: no upload access to directory %s", *req.ParentId),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 验证文件大小（必须小于 10MB）
	if *req.Size > SmallFileThreshold {
		llog.C(ctx).Errorf("file too large for small file upload: %d > %d", *req.Size, SmallFileThreshold)
		return nil, &models.Err{
			E: fmt.Errorf("file too large for small file upload (max %dMB), use chunk upload instead", SmallFileThreshold/1024/1024),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	blake3Hash := *req.Hash
	llog.C(ctx).Infof("small file upload started: name=%s, size=%d, hash=%s", *req.Name, *req.Size, blake3Hash)

	// 同一目标文件串行化：并发上传同一文件时，后到者必须能看到前者刚建的记录，
	// 走「同名同 hash → 秒传」直接返回，而不是各自建记录、互相删临时文件。
	unlock := lockUploadTarget(getCurrentUserIDFromCtx(ctx), *req.ParentId, *req.Name)
	defer unlock()

	// 验证父文件夹
	_, mErr := validateAndGetParent(*req.ParentId)
	if mErr != nil {
		return nil, mErr
	}

	// 按目标目录的绑定解析落盘存储提供者（目录绑定 → 祖先继承 → 用户分配集）
	providerIds, mErr := resolveProviderIdsForFolder(ctx, *req.ParentId)
	if mErr != nil {
		return nil, mErr
	}

	// 请求到达即发号（并发上传仲裁，同 key 后到者版本号更大）
	userID := getCurrentUserIDFromCtx(ctx)
	ctx = withUploadVersion(ctx, AllocateFileVersion(userID, *req.ParentId, *req.Name))

	// 检查文件是否已存在
	ownerFile, sameNameFile, globalFile, mErr := checkFileExists(*req.ParentId, *req.Name, blake3Hash, userID)
	if mErr != nil {
		return nil, mErr
	}

	// 处理秒传逻辑
	handled, fileRecord, mErr := handleInstantUpload(
		ctx, blake3Hash, req.Name, req.Size, req.ParentId,
		providerIds, ownerFile, sameNameFile, globalFile, req.GetOverwrite(),
		req.Tags,
	)
	if mErr != nil {
		return nil, mErr
	}
	if handled {
		exists := true
		return &native.UploadSmallRespData{
			FileId: fileRecord.Id,
			Name:   fileRecord.Name,
			Size:   fileRecord.Size,
			Hash:   fileRecord.Hash,
			Exists: &exists,
		}, nil
	}

	// 文件不存在，需要实际上传
	if mErr := validateSmallFileContent(req.Content, blake3Hash, *req.Size); mErr != nil {
		return nil, mErr
	}

	// 处理同名文件
	overwriteID, mErr := checkSameNameFile(sameNameFile, *req.Name, req.GetOverwrite())
	if mErr != nil {
		return nil, mErr
	}

	// 创建临时文件
	tempPath, mErr := writeSmallFileTemp(blake3Hash, *req.Name, req.Content)
	if mErr != nil {
		return nil, mErr
	}

	// 创建文件记录
	newFileRecord, mErr := createSmallFileRecord(ctx, req, blake3Hash, tempPath, overwriteID, providerIds)
	if mErr != nil {
		return nil, mErr
	}

	// 触发后台存储上传：优先复用 initUpload 已创建的 upload record
	providerIdsJSON, _ := json.Marshal(providerIds)
	targetProviderIds := string(providerIdsJSON)
	if req.UploadId != nil && *req.UploadId > 0 {
		if err := reuseAndDispatchUpload(ctx, *req.UploadId, tempPath, *req.Name, *req.ParentId, overwriteID, *req.Size, &targetProviderIds); err != nil {
			return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}
	} else {
		if err := smallFileUploadToProviders(ctx, blake3Hash, req, providerIds, tempPath, overwriteID); err != nil {
			return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}
	}

	exists := false
	return &native.UploadSmallRespData{
		FileId: newFileRecord.Id,
		Name:   newFileRecord.Name,
		Size:   newFileRecord.Size,
		Hash:   newFileRecord.Hash,
		Exists: &exists,
	}, nil
}

// UploadWebdavFromTemp WebDAV 上传专用入口：请求体已在 webdav 层流式落盘为
// 临时文件（blake3 hash 在写入时同步计算），这里直接入库并异步分发到存储提供者。
//
// 与 UploadSmall 刻意分开，不共用其 10MB 上限与整读内存的语义：
//   - 无大小限制（webdav PUT 的 body 天然是流，大文件应直接入库分发）；
//   - 不把内容整读进内存（UploadSmall 的 req.Content 是内存切片，不适合大文件）。
//
// 秒传 / 同名冲突 / 覆盖历史 / upload-record+异步分发等语义与 UploadSmall 一致
// （共用 checkFileExists / handleInstantUpload / checkSameNameFile /
// createSmallFileRecord / smallFileUploadToProviders）。
// tempPath 必须在规范临时目录（TempDir/<hash>/）下：各 helper 的错误清理路径
// 依赖该目录的隔离性（RemoveAll 目录是安全的），storage.Dispatch 也按该布局兜底。
func UploadWebdavFromTemp(ctx context.Context, req *native.UploadSmallReq, tempPath string) (*entity.FileORM, *models.Err) {
	// 权限检查：检查对父目录的上传权限
	if !CheckOwnership(ctx, *req.ParentId) {
		return nil, &models.Err{
			E: fmt.Errorf("permission denied: no upload access to directory %s", *req.ParentId),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	blake3Hash := *req.Hash
	llog.C(ctx).Infof("webdav upload started: name=%s, size=%d, hash=%s", *req.Name, *req.Size, blake3Hash)

	// 与 UploadSmall 同一把串行化锁：WebDAV PUT 与 App 上传可能同时写同一目标文件
	unlock := lockUploadTarget(getCurrentUserIDFromCtx(ctx), *req.ParentId, *req.Name)
	defer unlock()

	// 按目标目录的绑定解析落盘存储提供者（目录绑定 → 祖先继承 → 用户分配集）
	providerIds, mErr := resolveProviderIdsForFolder(ctx, *req.ParentId)
	if mErr != nil {
		removeWebdavTemp(tempPath)
		return nil, mErr
	}

	// 检查文件是否已存在（秒传/同名冲突判定）
	userID := getCurrentUserIDFromCtx(ctx)
	ownerFile, sameNameFile, globalFile, mErr := checkFileExists(*req.ParentId, *req.Name, blake3Hash, userID)
	if mErr != nil {
		removeWebdavTemp(tempPath)
		return nil, mErr
	}

	// 秒传 / 同名覆盖处理。内容已落盘：若新内容不在存储中，同名覆盖分支会
	// 回落（handled=false）走下面的真实上传，保证记录不会指向无位置的新 hash。
	handled, fileRecord, mErr := handleInstantUpload(
		ctx, blake3Hash, req.Name, req.Size, req.ParentId,
		providerIds, ownerFile, sameNameFile, globalFile, req.GetOverwrite(), req.Tags,
	)
	if mErr != nil {
		removeWebdavTemp(tempPath)
		return nil, mErr
	}
	if handled {
		// 记录已就绪（秒传复用），临时文件不再需要
		removeWebdavTemp(tempPath)
		return fileRecord, nil
	}

	// 同名文件覆盖判定（内容不在存储时这里拿到 overwriteID，走完整上传）
	overwriteID, mErr := checkSameNameFile(sameNameFile, *req.Name, req.GetOverwrite())
	if mErr != nil {
		removeWebdavTemp(tempPath)
		return nil, mErr
	}

	// 创建文件记录（覆盖时按父目录 max_histories 决定建历史还是直接更新）
	newFileRecord, mErr := createSmallFileRecord(ctx, req, blake3Hash, tempPath, overwriteID, providerIds)
	if mErr != nil {
		removeWebdavTemp(tempPath)
		return nil, mErr
	}

	// 触发后台存储上传（从临时文件上传；内部失败会清理 temp 目录）
	if err := smallFileUploadToProviders(ctx, blake3Hash, req, providerIds, tempPath, overwriteID); err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	return newFileRecord, nil
}

// removeWebdavTemp 清理 webdav 上传的规范临时目录 TempDir/<hash>/
func removeWebdavTemp(tempPath string) {
	if err := os.RemoveAll(filepath.Dir(tempPath)); err != nil {
		llog.L().Warnf("cleanup webdav temp dir failed: %v", err)
	}
}

// validateSmallFileContent 验证小文件内容hash和大小
func validateSmallFileContent(content []byte, expectedHash string, expectedSize uint64) *models.Err {
	hasher := blake3.New(32, nil)
	hasher.Write(content)
	actualHash := hex.EncodeToString(hasher.Sum(nil))

	if actualHash != expectedHash {
		llog.L().Errorf("file hash mismatch: expected %s, got %s", expectedHash, actualHash)
		return &models.Err{E: errors.New("file hash mismatch"), C: native.RespCode_PARAMS_ERROR}
	}

	if uint64(len(content)) != expectedSize {
		llog.L().Errorf("file size mismatch: expected %d, got %d", expectedSize, len(content))
		return &models.Err{
			E: fmt.Errorf("file size mismatch: expected %d, got %d", expectedSize, len(content)),
			C: native.RespCode_PARAMS_ERROR,
		}
	}
	return nil
}

// writeSmallFileTemp 将小文件内容写入临时文件（内容寻址层 TempDir/<hash>/）
//
// 目录由 tempPath 推导（os.MkdirAll(filepath.Dir(tempPath))），
// 从结构上保证「创建目录」与「写入文件」永远指向同一位置，不会再脱节。
//
// 历史坑：此处原先调用 CreateFileTempDir(hash, owner, true) 创建分片目录
// （TempDir/users/<owner>/<hash>/chunks）。owner 隔离改造前，该分片目录位于
// TempDir/<hash>/chunks，顺带创建了父目录 TempDir/<hash>，因而写入碰巧成功；
// 改造后分片目录移到 users/ 下，TempDir/<hash>/ 不再被创建，
// GetFileTempPath 指向的路径父目录缺失，小文件上传写入必然失败。
//
// 小文件不涉及分片，故无需创建 owner 隔离的分片目录。
func writeSmallFileTemp(blake3Hash, fileName string, content []byte) (string, *models.Err) {
	tempPath := utils.GetFileTempPath(blake3Hash, fileName)
	if err := os.MkdirAll(filepath.Dir(tempPath), 0755); err != nil {
		llog.L().Errorf("create temp dir failed: %v", err)
		return "", &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	if err := os.WriteFile(tempPath, content, 0644); err != nil {
		llog.L().Errorf("write temp file failed: %v", err)
		return "", &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	return tempPath, nil
}

// createSmallFileRecord 创建小文件的文件记录
func createSmallFileRecord(ctx context.Context, req *native.UploadSmallReq, blake3Hash, tempPath, overwriteID string, providerIDs []uint32) (*entity.FileORM, *models.Err) {
	filetype, mime := GetFileType(tempPath, *req.Name)

	newFileRecord := &entity.FileORM{
		Name:     req.Name,
		ParentId: req.ParentId,
		Size:     req.Size,
		Hash:     &blake3Hash,
		Type:     &filetype,
		Mime:     &mime,
		IsReady:  utils.BoolPtr(false),
	}

	// 设置 owner_id：优先取当前用户，取不到则从父目录继承（与 CompleteUpload 一致）
	ownerID := getCurrentUserIDFromCtx(ctx)
	if ownerID == "" && req.ParentId != nil && *req.ParentId != "" {
		if parent, pErr := db.GetFileByID(*req.ParentId); pErr == nil && parent != nil && parent.OwnerId != nil {
			ownerID = *parent.OwnerId
		}
	}
	newFileRecord.OwnerId = &ownerID

	newFileRecord, mErr := handleOverwriteFile(ctx, newFileRecord, overwriteID, tempPath, providerIDs)
	if mErr != nil {
		return nil, mErr
	}

	if newFileRecord.Id != nil {
		parseAndCreateTags(*newFileRecord.Id, req.Tags)
	}

	return newFileRecord, nil
}

// smallFileUploadToProviders 创建上传记录并触发后台存储上传
func smallFileUploadToProviders(ctx context.Context, blake3Hash string, req *native.UploadSmallReq, providerIds []uint32, tempPath, overwriteID string) error {
	providerIdsJSON, _ := json.Marshal(providerIds)
	targetProviderIds := string(providerIdsJSON)

	// 记录本次上传对应的文件版本号（分发完成回调用它做 CAS：仅当文件仍为
	// 本次上传的内容时才标记 is_ready，防止旧分发把新 PUT 接管的内容标就绪）
	urVersion := getUploadVersion(ctx)

	uploadRecord := &entity.UploadRecordORM{
		FileHash:          &blake3Hash,
		FileName:          req.Name,
		FileSize:          req.Size,
		ParentId:          req.ParentId,
		TempPath:          &tempPath,
		OverwriteId:       &overwriteID,
		Status:            utils.EnumPtr(native.LocationStatus_PPENDING),
		TaskStatus:        utils.EnumPtr(native.UploadStatus_PENDING),
		TargetProviderIds: &targetProviderIds,
		TaskType:          utils.EnumPtr(native.UploadTaskType_UPLOAD),
		Version:           &urVersion,
	}

	if err := db.GetDB().Create(uploadRecord).Error; err != nil {
		llog.C(ctx).Errorf("create upload record failed: %v", err)
		// 清理 temp 文件
		os.RemoveAll(filepath.Dir(tempPath))
		return err
	}

	go storage.Dispatch(context.Background(), uploadRecord)
	return nil
}

// uploadRecordReuseUpdates 组装"复用旧上传记录"时要刷新的字段。
//
// version 必须用本次请求发号的值（传 0 表示无版本上下文，保持原值不动）：
// 分发完成时 markFileReadyIfVersionMatches 拿它做 CAS，留着旧版本号会让
// is_ready 永远标不上（三个源都已完成却显示未就绪）。
func uploadRecordReuseUpdates(tempPath, fileName, parentID string, fileSize uint64, overwriteID string, version uint64, targetProviderIds *string) map[string]interface{} {
	updates := map[string]interface{}{
		"temp_path":           tempPath,
		"file_name":           fileName,
		"parent_id":           parentID,
		"file_size":           fileSize,
		"overwrite_id":        overwriteID,
		"target_provider_ids": targetProviderIds,
		"task_status":         int32(native.UploadStatus_PENDING),
		"message":             "",
	}
	if version > 0 {
		updates["version"] = version
	}
	return updates
}

// reuseAndDispatchUpload 复用 initUpload 已创建的 upload record 并触发后台存储上传。
//
// 复用的记录可能是上一次断点续传 / 失败重试留下的**旧记录**，它的文件名字、父目录、
// 大小、尤其是 version 都是旧的。其中 version 必须刷新成本次请求发号得到的值：
// 分发完成时 markFileReadyIfVersionMatches 用它做 CAS（WHERE version = ur.version），
// 版本对不上就静默命中 0 行，文件会永远停在"未就绪"（位置行全部 PCOMPLETED 也一样）。
func reuseAndDispatchUpload(ctx context.Context, uploadID uint32, tempPath, fileName, parentID, overwriteID string, fileSize uint64, targetProviderIds *string) error {
	uploadRecord, _, err := db.GetUploadRecordByID(uploadID, false)
	if err != nil {
		llog.L().Errorf("get upload record by id failed: %v", err)
		return err
	}

	// 更新 upload record：指向新的临时文件路径 + 本次请求的元信息与版本号
	myVersion := getUploadVersion(ctx)
	updates := uploadRecordReuseUpdates(tempPath, fileName, parentID, fileSize, overwriteID, myVersion, targetProviderIds)
	if err := db.GetDB().Model(uploadRecord).Updates(updates).Error; err != nil {
		llog.L().Errorf("update upload record failed: %v", err)
		return err
	}

	// 刷新记录以获取更新后的字段
	uploadRecord.TempPath = &tempPath
	uploadRecord.FileName = &fileName
	uploadRecord.ParentId = &parentID
	uploadRecord.FileSize = &fileSize
	uploadRecord.OverwriteId = &overwriteID
	uploadRecord.TargetProviderIds = targetProviderIds
	uploadRecord.TaskStatus = utils.EnumPtr(native.UploadStatus_PENDING)
	if myVersion > 0 {
		uploadRecord.Version = &myVersion
	}
	msg := ""
	uploadRecord.Message = &msg

	llog.L().Infof("reuse upload record %d for small file: %s, version: %d", uploadID, fileName, myVersion)
	utils.Go(func() { storage.Dispatch(context.Background(), uploadRecord) })
	return nil
}

// ListUploadRecords 获取上传记录列表
func ListUploadRecords(ctx context.Context, req *native.ListUploadRecordsReq) (*native.ListUploadRecordsResp, *models.Err) {
	// 默认参数
	page := int32(1)
	if req.Page != nil {
		page = int32(*req.Page)
	}
	pageSize := int32(20)
	if req.PageSize != nil {
		pageSize = int32(*req.PageSize)
	}

	// 查询数据库：一律按 owner 过滤（普通用户仅本人；管理员亦同）。
	// 上传记录是个人任务，四件套（详情/重试/删除/取消）按 owner 校验，
	// 列表若对管理员放开全局会出现"看得见却点不开"的矛盾，故策略统一为收紧。
	// 管理员的全局视角请走管理专用数据（如 backup_tasks 等）。
	ownerFilter := getCurrentUserIDFromCtx(ctx)
	if ownerFilter == "" {
		return nil, &models.Err{E: fmt.Errorf("unauthorized"), C: native.RespCode_PERMISSION_DENIED}
	}
	records, total, err := db.ListUploadRecords(page, pageSize, (*int32)(req.Status), ownerFilter)
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 查询失败记录总数（用于状态栏统计）
	failedCount, err := db.CountUploadRecordsByStatus(int32(native.UploadStatus_FAILED), ownerFilter)
	if err != nil {
		llog.C(ctx).Warnf("count failed upload records error: %v", err)
		// 不影响主查询，继续返回
		failedCount = 0
	}

	// 转换为 Proto
	var uploadRecords []*native.UploadRecordInfo
	for _, record := range records {
		var createdAt *timestamppb.Timestamp
		if record.CreatedAt != nil {
			createdAt = timestamppb.New(*record.CreatedAt)
		}
		var updatedAt *timestamppb.Timestamp
		if record.UpdatedAt != nil {
			updatedAt = timestamppb.New(*record.UpdatedAt)
		}

		info := &native.UploadRecordInfo{
			Id:                record.Id,
			FileName:          record.FileName,
			FileSize:          record.FileSize,
			FileHash:          record.FileHash,
			ParentId:          record.ParentId,
			Status:            (*native.UploadStatus)(record.TaskStatus),
			TargetProviderIds: record.TargetProviderIds,
			TaskType:          (*native.UploadTaskType)(record.TaskType),
			CreatedAt:         createdAt,
			UpdatedAt:         updatedAt,
			Message:           record.Message,
		}
		uploadRecords = append(uploadRecords, info)
	}

	totalUint32 := uint32(total)
	failedCountUint32 := uint32(failedCount)
	return &native.ListUploadRecordsResp{
		Records:     uploadRecords,
		Total:       &totalUint32,
		FailedCount: &failedCountUint32,
	}, nil
}

// GetUploadRecordDetail 获取上传记录详情
func GetUploadRecordDetail(ctx context.Context, req *native.GetUploadRecordDetailReq) (*native.GetUploadRecordDetailResp, *models.Err) {
	// 查询上传记录
	record, _, err := db.GetUploadRecordByID(*req.Id, false)
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 归属校验：仅发起者可查看详情（管理员不可越权查看他人上传）
	if !checkUploadRecordOwnership(ctx, record) {
		return nil, &models.Err{E: fmt.Errorf("permission denied: not owner of upload record"), C: native.RespCode_PERMISSION_DENIED}
	}

	// 查询文件位置记录
	locations, err := db.GetFileLocationsByFileHash(*record.FileHash)
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 查询存储提供者名称
	providerIDs := []uint32{}
	for _, loc := range locations {
		providerIDs = append(providerIDs, *loc.ProviderId)
	}

	providers, err := db.GetStorageProvidersByIDs(providerIDs)
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	providerMap := make(map[uint32]string)
	for _, p := range providers {
		providerMap[*p.Id] = *p.Name
	}

	// 转换为 Proto
	var locationInfos []*native.FileLocationInfo
	for _, loc := range locations {
		providerName := providerMap[*loc.ProviderId]

		var expiredAt *timestamppb.Timestamp
		if loc.ExpiredAt != nil {
			expiredAt = timestamppb.New(*loc.ExpiredAt)
		}
		var createdAt *timestamppb.Timestamp
		if loc.CreatedAt != nil {
			createdAt = timestamppb.New(*loc.CreatedAt)
		}
		var updatedAt *timestamppb.Timestamp
		if loc.UpdatedAt != nil {
			updatedAt = timestamppb.New(*loc.UpdatedAt)
		}

		info := &native.FileLocationInfo{
			Id:             loc.Id,
			ProviderId:     loc.ProviderId,
			ProviderName:   &providerName,
			Status:         (*native.LocationStatus)(loc.Status),
			Message:        loc.Message,
			Progress:       loc.Progress,
			Encrypted:      loc.Encrypted,
			Size:           loc.Size,
			ProviderFileId: loc.ProviderFileId,
			CacheStatus:    (*native.CacheStatus)(loc.CacheStatus),
			ExpiredAt:      expiredAt,
			CreatedAt:      createdAt,
			UpdatedAt:      updatedAt,
		}
		locationInfos = append(locationInfos, info)
	}

	// 构建上传记录信息
	var recordCreatedAt *timestamppb.Timestamp
	if record.CreatedAt != nil {
		recordCreatedAt = timestamppb.New(*record.CreatedAt)
	}
	var recordUpdatedAt *timestamppb.Timestamp
	if record.UpdatedAt != nil {
		recordUpdatedAt = timestamppb.New(*record.UpdatedAt)
	}

	recordInfo := &native.UploadRecordInfo{
		Id:                record.Id,
		FileName:          record.FileName,
		FileSize:          record.FileSize,
		FileHash:          record.FileHash,
		ParentId:          record.ParentId,
		Status:            (*native.UploadStatus)(record.TaskStatus),
		TargetProviderIds: record.TargetProviderIds,
		TaskType:          (*native.UploadTaskType)(record.TaskType),
		CreatedAt:         recordCreatedAt,
		UpdatedAt:         recordUpdatedAt,
		Message:           record.Message,
	}

	return &native.GetUploadRecordDetailResp{
		Record:    recordInfo,
		Locations: locationInfos,
	}, nil
}

// DeleteUploadRecord 删除上传记录
func DeleteUploadRecord(ctx context.Context, req *native.DeleteUploadRecordReq) *models.Err {
	// 上传已完成/处理中，无需删除
	if uploadDone.IsDone(*req.Id) {
		llog.C(ctx).Infof("upload already processing/done, ignoring delete: %d", *req.Id)
		return nil
	}

	// 查询上传记录
	record, _, err := db.GetUploadRecordByID(*req.Id, false)
	if err != nil {
		if err == gorm.ErrRecordNotFound && uploadDone.IsDone(*req.Id) {
			return nil
		}
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 归属校验：仅发起者可删除自己的上传记录
	if !checkUploadRecordOwnership(ctx, record) {
		return &models.Err{E: fmt.Errorf("permission denied: not owner of upload record"), C: native.RespCode_PERMISSION_DENIED}
	}

	// 删除失败的 FileLocation 记录
	if record.FileHash != nil && *record.FileHash != "" {
		// 查询该文件的所有位置记录
		locations, err := db.GetFileLocationsByFileHash(*record.FileHash)
		if err != nil {
			return &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}

		// 删除失败的记录
		for _, loc := range locations {
			if loc.Status != nil && *loc.Status == int32(native.LocationStatus_PFAILED) {
				if err := db.DeleteFileLocation(*loc.Id); err != nil {
					llog.C(ctx).Errorf("delete file location failed: %v", err)
					// 继续删除其他记录
				}
			}
		}
	}

	// 删除上传记录
	if err := db.ClearUploadRecords(*req.Id); err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	return nil
}

// RetryUploadRecord 重试上传记录
func RetryUploadRecord(ctx context.Context, req *native.RetryUploadRecordReq) *models.Err {
	// 上传已完成/处理中，无需重试
	if uploadDone.IsDone(*req.Id) {
		llog.C(ctx).Infof("upload already processing/done, ignoring retry: %d", *req.Id)
		return nil
	}

	// 查询上传记录
	record, _, err := db.GetUploadRecordByID(*req.Id, false)
	if err != nil {
		if err == gorm.ErrRecordNotFound && uploadDone.IsDone(*req.Id) {
			return nil
		}
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 归属校验：仅发起者可重试自己的上传
	if !checkUploadRecordOwnership(ctx, record) {
		return &models.Err{E: fmt.Errorf("permission denied: not owner of upload record"), C: native.RespCode_PERMISSION_DENIED}
	}

	// 重置状态为等待中
	pendingStatus := int32(native.UploadStatus_PENDING)
	record.TaskStatus = &pendingStatus

	// 更新数据库
	if err := db.UpdateUploadRecord(record, []string{"task_status"}); err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 重新分发上传任务（使用独立的 context，避免 HTTP context 在请求结束后被取消）
	go storage.Dispatch(context.Background(), record)

	return nil
}

// RetryAllFailedUploadRecords 重试所有失败的上传记录
func RetryAllFailedUploadRecords(ctx context.Context) *models.Err {
	// 查询失败的上传记录：一律仅本人（管理员亦同，与 ListUploadRecords /
	// checkUploadRecordOwnership 的收紧口径一致，避免批量操作他人任务）
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return &models.Err{E: fmt.Errorf("unauthorized"), C: native.RespCode_PERMISSION_DENIED}
	}
	records, err := db.GetUploadRecordsByStatus([]native.UploadStatus{native.UploadStatus_FAILED})
	if err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 仅重试自己的记录
	own := records[:0]
	for _, r := range records {
		if r.OwnerId != nil && *r.OwnerId == userID {
			own = append(own, r)
		}
	}
	records = own

	if len(records) == 0 {
		return nil
	}

	// 逐个重试
	for _, record := range records {
		if uploadDone.IsDone(*record.Id) {
			continue
		}

		// 重置状态为等待中
		pendingStatus := int32(native.UploadStatus_PENDING)
		record.TaskStatus = &pendingStatus

		// 更新数据库
		if err := db.UpdateUploadRecord(record, []string{"task_status"}); err != nil {
			llog.C(ctx).Errorf("RetryAllFailedUploadRecords: update record %d failed: %v", *record.Id, err)
			continue
		}

		// 重新分发上传任务
		go storage.Dispatch(context.Background(), record)
	}

	llog.C(ctx).Infof("RetryAllFailedUploadRecords: retried %d records", len(records))
	return nil
}

// cleanOldFileLocations 异步删除旧文件在存储提供者上的位置
func cleanOldFileLocations(locations []*entity.FileLocationORM, oldHash *string) {
	if len(locations) == 0 {
		return
	}

	ctx := context.Background()

	// 按 provider_id 分组
	providerLocations := make(map[uint32][]*entity.FileLocationORM)
	for _, loc := range locations {
		if loc.ProviderId != nil {
			providerID := *loc.ProviderId
			providerLocations[providerID] = append(providerLocations[providerID], loc)
		}
	}

	llog.L().Infof("cleaning old file locations from providers, count: %d", len(locations))

	// 按 provider_id 批量删除文件
	providerDeletedSizes := make(map[uint32]uint64)
	var allDeletedIDs []uint64

	for providerID, locs := range providerLocations {
		deletedIDs, failedIDs, deletedSize := batchDeleteFromProvider(ctx, providerID, locs)
		allDeletedIDs = append(allDeletedIDs, deletedIDs...)
		// 删除失败的位置记录标记为 DELETED_FAILED，供后续重试/巡检清理，
		// 避免静默残留成无引用的孤儿记录
		if len(failedIDs) > 0 {
			if err := db.UpdateFileLocationStatus(failedIDs, int32(native.LocationStatus_PDELETED_FAILED)); err != nil {
				llog.L().Errorf("mark file locations DELETED_FAILED failed: %v", err)
			} else {
				llog.L().Warnf("marked %d file locations as DELETED_FAILED, provider: %d", len(failedIDs), providerID)
			}
		}
		if deletedSize > 0 {
			providerDeletedSizes[providerID] = deletedSize
		}
	}

	// 更新存储提供者的已使用容量
	for providerID, size := range providerDeletedSizes {
		if err := db.DecrementUsedCapacity(providerID, size); err != nil {
			llog.L().Errorf("decrement used capacity failed: %v, provider_id: %d", err, providerID)
		}
	}

	// 批量删除成功删除的文件位置记录
	if len(allDeletedIDs) > 0 {
		if err := db.DeleteFileLocations(allDeletedIDs); err != nil {
			llog.L().Errorf("batch delete file locations failed: %v", err)
		}
	}

	if oldHash != nil && *oldHash != "" {
		llog.L().Infof("old file locations cleaned, hash: %s", *oldHash)
	} else {
		llog.L().Infof("old file locations cleaned")
	}
}

// cleanupOldHistories 异步清理超出限制的历史记录
// 修复：旧实现把"当前覆盖的旧版本"的 locations 当成了被裁剪历史的 locations 删除，
// 导致被裁剪 hash 的位置永久残留成孤儿、而新历史版本的 provider 文件被误删。
// 现在只清理被裁剪 history 条目对应的 hash，且仅当该 hash 不再被任何
// files / file_histories 引用时才删除其存储位置（避免误删被共享的副本）。
func cleanupOldHistories(fileID string, maxHistories uint32) {
	// 获取该文件的历史记录（version_number DESC，最旧的排在末尾）
	histories, err := db.GetFileHistoriesByFileID(fileID)
	if err != nil {
		llog.L().Errorf("get file histories failed: %v", err)
		return
	}

	currentCount := uint32(len(histories))
	if currentCount <= maxHistories {
		llog.L().Infof("histories count %d <= max_histories %d, no cleanup needed", currentCount, maxHistories)
		return
	}

	// 需要清理的数量
	toDelete := currentCount - maxHistories
	llog.L().Infof("need to cleanup %d histories, current: %d, max: %d", toDelete, currentCount, maxHistories)

	// 删除最旧的 toDelete 条历史记录，并收集其 hash
	var deletedHashes []string
	for i := uint32(0); i < toDelete; i++ {
		history := histories[currentCount-1-i]
		if history == nil || history.Id == nil {
			continue
		}
		if err := db.DeleteFileHistory(*history.Id); err != nil {
			llog.L().Errorf("delete history failed: %v, id: %d", err, *history.Id)
			continue
		}
		if history.Hash != nil && *history.Hash != "" {
			deletedHashes = append(deletedHashes, *history.Hash)
		}
	}
	llog.L().Infof("histories cleaned, file_id: %s, deleted: %d, remaining: %d", fileID, toDelete, maxHistories)

	// 异步清理不再被引用的 hash 的存储位置
	if len(deletedHashes) > 0 {
		go cleanupUnreferencedHashLocations(deletedHashes)
	}
}

// cleanupUnreferencedHashLocations 清理不再被任何文件/历史引用（孤儿）的 hash 的存储位置
func cleanupUnreferencedHashLocations(hashes []string) {
	cacheDir := "data/thumbnails"
	generator := thumbnail.NewGenerator(cacheDir)

	for _, hash := range hashes {
		// 引用检查1：files 表活跃记录 + file_histories 表剩余记录
		fileRefs, err := db.CountActiveFilesByHashExcludingIDs(hash, nil)
		if err != nil {
			llog.L().Errorf("count active files by hash failed: %v, hash: %s", err, hash)
			continue
		}
		if fileRefs > 0 {
			llog.L().Debugf("hash %s still referenced by %d files, skip location cleanup", hash, fileRefs)
			continue
		}
		historyRefs, err := db.CountFileHistoriesByHash(hash)
		if err != nil {
			llog.L().Errorf("count file histories by hash failed: %v, hash: %s", err, hash)
			continue
		}
		if historyRefs > 0 {
			llog.L().Debugf("hash %s still referenced by %d histories, skip location cleanup", hash, historyRefs)
			continue
		}

		// 引用检查2：upload_records 活跃任务（PENDING/UPLOADING）
		// 并发覆盖场景：旧 hash 可能刚被新 PUT 接管（file/histories 引用已移除），
		// 但其分发任务仍在途——此时删除 locations 会让 completeUpload 永久卡住。
		// 必须等活跃 upload_record 结束后才能回收物理位置：注册延迟重试，
		// 分发通常数秒完成，30s 后重试大概率已无活跃任务。
		activeUploads, err := db.CountActiveUploadRecordsByHash(hash)
		if err != nil {
			llog.L().Errorf("count active upload records by hash failed: %v, hash: %s", err, hash)
			continue
		}
		if activeUploads > 0 {
			llog.L().Debugf("hash %s still has %d active uploads, schedule retry cleanup", hash, activeUploads)
			time.AfterFunc(30*time.Second, func() {
				cleanupUnreferencedHashLocations([]string{hash})
			})
			continue
		}

		locations, err := db.GetFileLocationsByBlake3Hash(hash)
		if err != nil {
			llog.L().Errorf("get file locations failed: %v, hash: %s", err, hash)
			continue
		}
		if len(locations) == 0 {
			continue
		}

		llog.L().Infof("cleaning unreferenced hash locations, hash: %s, count: %d", hash, len(locations))
		cleanOldFileLocations(locations, &hash)

		// 顺带清理该 hash 的缩略图缓存（hash 已确认无任何引用，缩略图按 hash 命名可安全删除）
		if err := generator.DeleteAllThumbnails(hash); err != nil {
			llog.L().Warnf("delete thumbnails failed: hash=%s, err=%v", hash, err)
		}
	}
}
