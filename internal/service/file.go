package service

import (
	"context"
	"encoding/json"
	"errors"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/internal/thumbnail"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

// GetFileList 获取文件列表
func GetFileList(ctx context.Context, req *native.FileListReq) ([]*native.FileORM, int64, bool, *models.Err) {
	// 获取分页参数
	offset := req.GetOffset()
	pageSize := req.GetPageSize()

	files, total, hasMore, err := db.GetFileList(req.GetParentId(), offset, pageSize)
	if err != nil {
		llog.L().Errorf("GetFileList failed, err: %v", err)
		return nil, 0, false, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	return files, total, hasMore, nil
}

// GetFolderList 获取文件夹列表（用于移动文件选择目标文件夹）
func GetFolderList(ctx context.Context, req *native.FolderListReq) ([]*native.FileORM, *models.Err) {
	folders, err := db.GetFolderList(req.GetParentId())
	if err != nil {
		llog.L().Errorf("GetFolderList failed, err: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	return folders, nil
}

// CreateDir 创建文件夹
func CreateDir(ctx context.Context, req *native.CreateDirReq) (string, *models.Err) {
	providerIds := req.ProviderIds

	// 如果客户端未传 provider_ids，按优先级选择存储提供者
	if len(providerIds) == 0 {
		var mErr *models.Err
		providerIds, mErr = getInheritedProviderIds(req.ParentId)
		if mErr != nil {
			return "", mErr
		}
	} else {
		// 验证提供者是否存在且启用（批量查询）
		providers, err := db.GetEnabledProvidersByIds(providerIds)
		if err != nil {
			llog.L().Errorf("query providers failed: %v", err)
			return "", &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}
		// 验证所有提供的ID都有效
		if len(providers) != len(providerIds) {
			return "", &models.Err{
				E: errors.New("some providers are invalid or disabled"),
				C: native.RespCode_PARAMS_ERROR,
			}
		}
	}

	// 转换为JSON字符串存储
	providerIdsJSON, err := json.Marshal(providerIds)
	if err != nil {
		llog.L().Errorf("marshal provider_ids failed: %v", err)
		return "", &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	boundProviderIds := string(providerIdsJSON)

	f := &native.FileORM{
		Name:             req.Name,
		ParentId:         req.ParentId,
		Type:             utils.EnumPtr(native.FileType_FOLDER),
		EnableEncryption: req.EnableEncryption,
		BoundProviderIds: &boundProviderIds,
	}
	e := db.CreateFile(f)
	if e != nil {
		return "", &models.Err{E: e, C: native.RespCode_DB_ERROR}
	}

	// 返回新创建的目录 ID
	if f.Id == nil {
		return "", &models.Err{E: errors.New("failed to get created directory ID"), C: native.RespCode_INTERNAL_ERROR}
	}
	return *f.Id, nil
}

// getInheritedProviderIds 获取继承的存储提供者ID列表
// 优先级：父文件夹绑定 > 默认绑定
func getInheritedProviderIds(parentID *string) ([]uint32, *models.Err) {
	// 1. 尝试从父文件夹获取绑定的存储提供者
	if parentID != nil && *parentID != "" {
		parentFile, err := db.GetFileByID(*parentID)
		if err == nil && parentFile != nil && parentFile.BoundProviderIds != nil && *parentFile.BoundProviderIds != "" {
			// 解析父文件夹的绑定
			var providerIds []uint32
			if err := json.Unmarshal([]byte(*parentFile.BoundProviderIds), &providerIds); err == nil && len(providerIds) > 0 {
				// 验证这些提供者是否仍然有效
				providers, dbErr := db.GetEnabledProvidersByIds(providerIds)
				if dbErr == nil && len(providers) > 0 {
					llog.L().Infof("using parent folder's bound providers: %v", providerIds)
					return providerIds, nil
				}
			}
		}
	}

	// 2. 父文件夹没有绑定或无效，使用默认绑定的存储提供者
	defaultProviders, err := db.GetDefaultBoundEnabledProviders()
	if err != nil {
		llog.L().Errorf("get default bound providers failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	if len(defaultProviders) == 0 {
		return nil, &models.Err{
			E: errors.New("no default bound provider available, please specify provider_ids or set default_bound in storage providers"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 提取ID列表
	providerIds := make([]uint32, len(defaultProviders))
	for i, p := range defaultProviders {
		providerIds[i] = *p.Id
	}
	llog.L().Infof("using default bound providers: %v", providerIds)
	return providerIds, nil
}

// UpdateDir 更新文件夹
func UpdateDir(ctx context.Context, req *native.UpdateDirReq) *models.Err {
	orm := native.FileORM{
		Id:               req.Id,
		Name:             req.Name,
		EnableEncryption: req.EnableEncryption,
	}
	llog.L().Infof("update dir req:%+v", req)

	// 如果提供了 provider_ids，更新绑定关系
	if len(req.ProviderIds) > 0 {
		providerIds := req.ProviderIds

		// 批量查询验证提供者是否存在且启用
		providers, err := db.GetEnabledProvidersByIds(providerIds)
		if err != nil {
			llog.L().Errorf("query providers failed: %v", err)
			return &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}
		llog.L().Infof("update dir providers:%+v", providers)

		// 验证所有提供的ID都有效
		if len(providers) != len(providerIds) {
			return &models.Err{
				E: errors.New("some providers are invalid or disabled"),
				C: native.RespCode_PARAMS_ERROR,
			}
		}

		// 转换为JSON字符串存储
		providerIdsJSON, err := json.Marshal(providerIds)
		if err != nil {
			llog.L().Errorf("marshal provider_ids failed: %v", err)
			return &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
		}
		boundProviderIds := string(providerIdsJSON)
		orm.BoundProviderIds = &boundProviderIds
	}

	if err := db.UpdateDir(&orm); err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	return nil
}

// DeleteFile 删除文件（移动到回收站）
// 只软删除指定的文件，不递归删除子文件
// 回收站中只显示被删除的文件夹本身，不显示子文件
func DeleteFile(ctx context.Context, req *native.DeleteFileReq) *models.Err {
	ids := req.Ids
	if len(ids) == 0 {
		return nil
	}

	// 只软删除指定的文件，不递归删除子文件
	// 子文件会通过 parent_id 关联到已删除的文件夹
	if err := db.DeleteFiles(ids); err != nil {
		llog.L().Errorf("DeleteFiles failed, err: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	llog.L().Infof("deleted %d files", len(ids))
	return nil
}

// RenameFile 重命名文件
func RenameFile(ctx context.Context, req *native.RenameFileReq) *models.Err {
	if err := db.RenameFiles(req.RenameObjs); err != nil {
		llog.L().Errorf("RenameFiles failed, err: %v", err)
		return &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	return nil
}

// GetAvailableProvidersForSync 获取可用于同步的存储提供者
// GetAvailableProvidersForSync 获取可用于同步的存储提供者
// 返回：已启用的提供者，排除文件已存在且完成的提供者
func GetAvailableProvidersForSync(ctx context.Context, fileHash string) ([]*native.StorageProvider, *models.Err) {
	// 1. Validate file hash
	if fileHash == "" {
		return nil, &models.Err{
			E: errors.New("file hash is required"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 2. Query available providers from database
	providers, err := db.GetAvailableProvidersForSync(fileHash)
	if err != nil {
		llog.L().Errorf("get available providers failed: %v", err)
		return nil, &models.Err{
			E: err,
			C: native.RespCode_DB_ERROR,
		}
	}

	// 3. Convert ORM to PB
	items := make([]*native.StorageProvider, 0, len(providers))
	for _, p := range providers {
		pb, err := p.ToPB(ctx)
		if err != nil {
			llog.L().Errorf("convert provider to PB failed: %v", err)
			continue
		}
		items = append(items, &pb)
	}

	return items, nil
}

// RestoreFromTrash 从回收站恢复文件
// 恢复时需要递归恢复所有父级目录
func RestoreFromTrash(ctx context.Context, req *native.RestoreFromTrashReq) *models.Err {
	if len(req.Ids) == 0 {
		return nil
	}

	// 收集所有需要恢复的文件ID（包括父级目录）
	allRestoreIDs := make([]string, 0)
	allRestoreIDs = append(allRestoreIDs, req.Ids...)

	// 递归恢复父级目录
	for _, fileID := range req.Ids {
		parentIDs, err := getDeletedParentIDs(fileID)
		if err != nil {
			llog.L().Errorf("get deleted parent IDs failed: %v", err)
			return &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}
		allRestoreIDs = append(allRestoreIDs, parentIDs...)
	}

	llog.L().Infof("restoring %d files (including %d parents)", len(allRestoreIDs), len(allRestoreIDs)-len(req.Ids))

	if err := db.RestoreFromTrash(allRestoreIDs); err != nil {
		llog.L().Errorf("restore from trash failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	llog.L().Infof("restored %d files from trash", len(allRestoreIDs))
	return nil
}

// getDeletedParentIDs 获取所有被删除的父级目录ID
func getDeletedParentIDs(fileID string) ([]string, error) {
	parentIDs := make([]string, 0)
	currentID := fileID

	for {
		// 获取当前文件信息
		file, err := db.GetFileByID(currentID)
		if err != nil {
			break
		}

		// 检查 parent_id
		if file.ParentId == nil || *file.ParentId == "" {
			break
		}

		parentID := *file.ParentId

		// 获取父文件信息
		parentFile, err := db.GetFileByID(parentID)
		if err != nil {
			break
		}

		// 检查父文件是否被软删除
		if parentFile.DeletedAt != nil {
			parentIDs = append(parentIDs, parentID)
			currentID = parentID
		} else {
			break
		}
	}

	return parentIDs, nil
}

// batchDeleteFromProvider 从单个存储提供者批量删除文件位置
// 返回：成功删除的 location ID 列表、删除失败的 location ID 列表、删除的总大小
func batchDeleteFromProvider(
	ctx context.Context,
	providerID uint32,
	locations []*native.FileLocationORM,
) (deletedIDs []uint64, failedIDs []uint64, deletedSize uint64) {
	st := storage.GetStorage(providerID)
	if st == nil {
		llog.L().Warnf("storage provider not found, id: %d", providerID)
		for _, loc := range locations {
			if loc.Id != nil {
				failedIDs = append(failedIDs, *loc.Id)
			}
		}
		return
	}

	batchDeleter, ok := st.(types.BatchDeleter)
	if !ok {
		llog.L().Warnf("provider %d does not support BatchDeleter, skipping", providerID)
		for _, loc := range locations {
			if loc.Id != nil {
				failedIDs = append(failedIDs, *loc.Id)
			}
		}
		return
	}

	results, err := batchDeleter.BatchDelete(ctx, locations)

	// 全部成功
	if results == nil && err == nil {
		llog.L().Infof("batch deleted all %d files from provider %d", len(locations), providerID)
		for _, loc := range locations {
			if loc.Id != nil {
				deletedIDs = append(deletedIDs, *loc.Id)
			}
			if loc.ChunkSize != nil && *loc.Status == int32(native.LocationStatus_PCOMPLETED) {
				deletedSize += *loc.ChunkSize
			}
		}
		return
	}

	// 全部失败
	if results == nil && err != nil {
		llog.L().Errorf("batch delete all failed: %v, provider_id: %d", err, providerID)
		for _, loc := range locations {
			if loc.Id != nil {
				failedIDs = append(failedIDs, *loc.Id)
			}
		}
		return
	}

	// 部分成功
	llog.L().Infof("batch delete partial success: %d/%d from provider %d",
		countSuccess(results), len(results), providerID)
	for i, result := range results {
		if i >= len(locations) {
			break
		}
		loc := locations[i]
		if result.Success {
			if loc.Id != nil {
				deletedIDs = append(deletedIDs, *loc.Id)
			}
			if loc.ChunkSize != nil && *loc.Status == int32(native.LocationStatus_PCOMPLETED) {
				deletedSize += *loc.ChunkSize
			}
		} else {
			if loc.Id != nil {
				failedIDs = append(failedIDs, *loc.Id)
			}
			llog.L().Errorf("delete location %d failed: %v", *loc.Id, result.Error)
		}
	}
	return
}

// deleteFilesFromProviders 从存储提供者批量删除文件（内部函数）
// 按 provider_id 分组后批量删除，提高效率
func deleteFilesFromProviders(ctx context.Context, fileIDs []string) {
	if len(fileIDs) == 0 {
		return
	}
	llog.L().Infof("delete files from providers, id length: %d", len(fileIDs))

	// 1. 批量获取文件信息（包含软删除的记录，因为可能是回收站文件）
	files, err := db.GetFilesByIDsUnscoped(fileIDs)
	if err != nil {
		llog.L().Errorf("batch get files failed: %v", err)
		return
	}

	// 2. 收集文件hash，用于查询位置记录
	fileHashMap := make(map[string]string) // fileID -> fileHash
	hashSet := make(map[string]bool)       // 用于去重查询
	for _, file := range files {
		if file.Id != nil && file.Hash != nil && *file.Hash != "" {
			fileHashMap[*file.Id] = *file.Hash
			hashSet[*file.Hash] = true
			llog.L().Debugf("get file hash success %s", *file.Hash)
		}
	}

	// 3. 批量获取所有文件位置记录，按 provider_id 分组
	providerLocations := make(map[uint32][]*native.FileLocationORM) // providerID -> locations
	for fileHash := range hashSet {
		locations, err := db.GetFileLocationsByBlake3Hash(fileHash)
		if err != nil {
			llog.L().Errorf("get file locations failed: %v, hash: %s", err, fileHash)
			continue
		}
		llog.L().Debugf("success get file location by hash: %s, locations len: %d", fileHash, len(locations))

		for _, loc := range locations {
			if loc.ProviderId == nil {
				llog.L().Debugf("location provider id is nil, location id: %d", loc.Id)
				continue
			}
			providerID := *loc.ProviderId
			providerLocations[providerID] = append(providerLocations[providerID], loc)
		}
	}
	llog.L().Infof("delete files from providers, provider len: %d", len(providerLocations))

	// 4. 按 provider_id 批量删除文件
	providerDeletedSizes := make(map[uint32]uint64)
	var allDeletedIDs, allFailedIDs []uint64

	for providerID, locations := range providerLocations {
		llog.L().Infof("delete files loop providerlocations, providerID: %d", providerID)
		deletedIDs, failedIDs, deletedSize := batchDeleteFromProvider(ctx, providerID, locations)
		allDeletedIDs = append(allDeletedIDs, deletedIDs...)
		allFailedIDs = append(allFailedIDs, failedIDs...)
		if deletedSize > 0 {
			providerDeletedSizes[providerID] = deletedSize
		}
	}

	// 5. 更新存储提供者的已使用容量
	for providerID, size := range providerDeletedSizes {
		if err := db.DecrementUsedCapacity(providerID, size); err != nil {
			llog.L().Errorf("decrement used capacity failed: %v, provider_id: %d", err, providerID)
		}
	}

	// 6. 批量删除成功删除的文件位置记录
	if len(allDeletedIDs) > 0 {
		if err := db.DeleteFileLocations(allDeletedIDs); err != nil {
			llog.L().Errorf("batch delete file locations failed: %v", err)
		}
	}

	// 7. 更新删除失败的文件位置记录状态
	if len(allFailedIDs) > 0 {
		if err := db.UpdateFileLocationStatus(allFailedIDs, int32(native.LocationStatus_PDELETED_FAILED)); err != nil {
			llog.L().Errorf("update file location status to DELETED_FAILED failed: %v", err)
		} else {
			llog.L().Warnf("marked %d file locations as DELETED_FAILED", len(allFailedIDs))
		}
	}
}

// countSuccess 计算删除结果中成功的数量
func countSuccess(results []types.DeleteResult) int {
	count := 0
	for _, r := range results {
		if r.Success {
			count++
		}
	}
	return count
}

// DeletePermanently 彻底删除文件（从回收站永久删除）
func DeletePermanently(ctx context.Context, req *native.DeletePermanentlyReq) *models.Err {
	if len(req.Ids) == 0 {
		return nil
	}

	// 复制 IDs 避免异步访问问题
	fileIDs := make([]string, len(req.Ids))
	copy(fileIDs, req.Ids)

	// 获取所有子文件和子文件夹的ID（包括软删除的）
	allChildrenIDs, err := db.GetAllChildrenIDs(fileIDs)
	if err != nil {
		llog.L().Errorf("get all children IDs failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 合并指定的文件ID和所有子文件ID
	allFileIDs := append(fileIDs, allChildrenIDs...)
	llog.L().Infof("permanently deleting %d files (including %d children)", len(allFileIDs), len(allChildrenIDs))

	permanentlyDeleteFilesAsync(allFileIDs, "permanently deleted")
	return nil
}

// deleteFileHistories 删除文件的历史记录和对应的存储提供者上的文件
func deleteFileHistories(ctx context.Context, fileIDs []string) {
	if len(fileIDs) == 0 {
		return
	}
	llog.L().Infof("delete file history ids length: %d", len(fileIDs))
	// 批量获取所有文件的历史记录
	allHistories := make([]*native.FileHistoryORM, 0)
	for _, fileID := range fileIDs {
		histories, err := db.GetFileHistoryByFileID(fileID)
		if err != nil {
			llog.L().Errorf("get file history failed, file_id: %s, err: %v", fileID, err)
			continue
		}
		allHistories = append(allHistories, histories...)
	}

	if len(allHistories) == 0 {
		llog.L().Infof("no file history need delete")
		return
	}

	// 从存储提供者删除历史文件并更新容量
	deleteHistoryFromProviders(ctx, allHistories)

	// 删除数据库中的历史记录
	if err := db.DeleteFileHistoryByFileIDs(fileIDs); err != nil {
		llog.L().Errorf("delete file histories failed: %v", err)
	} else {
		llog.L().Infof("deleted %d file history records", len(allHistories))
	}
}

// DeleteFileHistory 删除文件历史版本
func DeleteFileHistory(ctx context.Context, ids []uint64) *models.Err {
	if len(ids) == 0 {
		return nil
	}

	// 获取所有历史记录
	var histories []*native.FileHistoryORM
	for _, id := range ids {
		history, err := db.GetFileHistoryByID(id)
		if err != nil {
			llog.L().Errorf("get file history failed, id: %d, err: %v", id, err)
			continue
		}
		histories = append(histories, history)
	}

	if len(histories) == 0 {
		return nil
	}

	// 从存储提供者删除历史文件并更新容量
	deleteHistoryFromProviders(ctx, histories)

	// 删除数据库中的历史记录
	if err := db.DeleteFileHistoryByIDs(ids); err != nil {
		llog.L().Errorf("delete file history failed, err: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	llog.L().Infof("deleted %d file history records", len(histories))
	return nil
}

// deleteHistoryFromProviders 从存储提供者删除历史文件（公共逻辑）
// 收集 hash -> 查询 FileLocation -> 按 provider 分组 -> 批量删除 -> 更新容量 -> 删除 FileLocation 记录
func deleteHistoryFromProviders(ctx context.Context, histories []*native.FileHistoryORM) {
	// 收集所有历史记录的 hash
	hashSet := make(map[string]bool)
	for _, history := range histories {
		if history.Hash != nil && *history.Hash != "" {
			hashSet[*history.Hash] = true
		}
	}

	// 根据 hash 批量获取文件位置记录，按 provider 分组
	providerLocations := make(map[uint32][]*native.FileLocationORM)
	for hash := range hashSet {
		locations, err := db.GetFileLocationsByBlake3Hash(hash)
		if err != nil {
			llog.L().Errorf("get file locations by hash failed, hash: %s, err: %v", hash, err)
			continue
		}

		for _, loc := range locations {
			if loc.ProviderId == nil {
				continue
			}
			providerLocations[*loc.ProviderId] = append(providerLocations[*loc.ProviderId], loc)
		}
	}

	// 按 provider 批量删除并记录容量变化
	providerDeletedSizes := make(map[uint32]uint64)
	for providerID, locations := range providerLocations {
		_, _, deletedSize := batchDeleteFromProvider(ctx, providerID, locations)
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

	// 删除文件位置记录
	for hash := range hashSet {
		if err := db.DeleteFileLocationsByBlake3Hash(hash); err != nil {
			llog.L().Errorf("delete file locations failed, hash: %s, err: %v", hash, err)
		}
	}
}

// permanentlyDeleteFilesAsync 异步永久删除文件（历史记录 + 存储提供者文件 + 数据库记录）
func permanentlyDeleteFilesAsync(allFileIDs []string, logLabel string) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				llog.L().Errorf("delete files panic: %v", r)
			}
		}()

		asyncCtx := context.Background()

		// 删除缩略图缓存
		deleteThumbnailsForFiles(allFileIDs)

		deleteFileHistories(asyncCtx, allFileIDs)
		deleteFilesFromProviders(asyncCtx, allFileIDs)

		if err := db.DeletePermanently(allFileIDs); err != nil {
			llog.L().Errorf("delete permanently failed: %v", err)
			return
		}

		llog.L().Infof("%s %d files", logLabel, len(allFileIDs))
	}()
}

// EmptyTrash 清空回收站
func EmptyTrash(ctx context.Context, req *native.EmptyTrashReq) *models.Err {
	// 1. 获取所有回收站文件
	files, _, err := db.GetTrashList(0, 10000) // 获取所有文件
	if err != nil {
		llog.L().Errorf("get trash list failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	if len(files) == 0 {
		return nil
	}

	// 2. 提取文件ID
	fileIDs := make([]string, len(files))
	for i, file := range files {
		fileIDs[i] = *file.Id
	}

	// 3. 获取所有子文件和子文件夹的ID（包括软删除的）
	allChildrenIDs, err := db.GetAllChildrenIDs(fileIDs)
	if err != nil {
		llog.L().Errorf("get all children IDs failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 合并指定的文件ID和所有子文件ID
	allFileIDs := append(fileIDs, allChildrenIDs...)
	llog.L().Infof("emptying trash: %d files (including %d children)", len(allFileIDs), len(allChildrenIDs))

	permanentlyDeleteFilesAsync(allFileIDs, "emptied trash, deleted")
	return nil
}

// GetTrashList 获取回收站文件列表
func GetTrashList(ctx context.Context) (*native.TrashListRespData, *models.Err) {
	files, total, err := db.GetTrashList(0, 500)
	if err != nil {
		llog.L().Errorf("get trash list failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 转换为 PB
	items := make([]*native.File, 0, len(files))
	for _, f := range files {
		pb, err := f.ToPB(ctx)
		if err != nil {
			llog.L().Errorf("convert file to PB failed: %v", err)
			continue
		}
		items = append(items, &pb)
	}

	totalUint32 := uint32(total)
	return &native.TrashListRespData{
		Files: items,
		Total: &totalUint32,
	}, nil
}

// SyncFileToProviders 同步文件到指定存储提供者
func SyncFileToProviders(ctx context.Context, req *native.SyncFileToProvidersReq) *models.Err {
	// 1. Validate file exists
	file, err := db.GetFileByID(*req.FileId)
	if err != nil {
		llog.L().Errorf("get file failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 2. Check file has at least one completed location
	locations, err := db.GetFileLocationsByFileHash(*file.Hash)
	if err != nil {
		llog.L().Errorf("get file locations failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	hasCompleted := false
	for _, loc := range locations {
		if loc.Status != nil && *loc.Status == int32(native.LocationStatus_PCOMPLETED) {
			hasCompleted = true
			break
		}
	}

	if !hasCompleted {
		return &models.Err{
			E: errors.New("file has no completed locations"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 3. Validate target providers
	providerIDs := make([]uint32, len(req.ProviderIds))
	copy(providerIDs, req.ProviderIds)

	providers, err := db.GetStorageProvidersByIDs(providerIDs)
	if err != nil {
		llog.L().Errorf("get providers failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	if len(providers) != len(providerIDs) {
		return &models.Err{
			E: errors.New("some providers not found or disabled"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 4. Filter out providers where file already exists and is completed
	var validProviderIDs []uint32
	for _, provider := range providers {
		existingLoc, err := db.GetFileLocationByFileHashAndProvider(*file.Hash, *provider.Id)
		if err == nil && existingLoc != nil {
			// File already exists on this provider
			if existingLoc.Status != nil && *existingLoc.Status == int32(native.LocationStatus_PCOMPLETED) {
				llog.L().Warnf("file already exists on provider %d, skipping", *provider.Id)
				continue // Skip this provider
			}
		}
		validProviderIDs = append(validProviderIDs, *provider.Id)
	}

	if len(validProviderIDs) == 0 {
		return &models.Err{
			E: errors.New("all selected providers already have this file"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 5. Convert provider IDs to JSON array string
	providerIDsJSON, err := json.Marshal(validProviderIDs)
	if err != nil {
		llog.L().Errorf("marshal provider ids failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}

	// 6. Create ONE sync upload record with all target providers
	uploadRecord := &native.UploadRecordORM{
		FileHash:          file.Hash,
		FileName:          file.Name,
		FileSize:          file.Size,
		ParentId:          file.ParentId,
		ChunkTotal:        utils.Uint32Ptr(1),
		ChunkSize:         utils.Uint64Ptr(0),
		TempPath:          utils.StringPtr(""), // Will be filled by dispatcher
		Status:            utils.Int32Ptr(int32(native.LocationStatus_PPENDING)),
		TargetProviderIds: utils.StringPtr(string(providerIDsJSON)), // JSON array like [1,3,5]
		TaskType:          utils.Int32Ptr(int32(native.UploadTaskType_SYNC)),
	}

	if err := db.CreateUploadRecord(uploadRecord); err != nil {
		llog.L().Errorf("create upload record failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 7. Trigger storage dispatch (will handle multi-provider distribution)
	// 使用独立的 context，避免 HTTP context 在请求结束后被取消
	go storage.Dispatch(context.Background(), uploadRecord)

	llog.L().Infof("created sync task for file %s to %d providers", *file.Id, len(validProviderIDs))
	return nil
}

// UpdateFileTags 更新文件标签
func UpdateFileTags(ctx context.Context, req *native.UpdateFileTagsReq) *models.Err {
	return UpdateFileTagsImpl(ctx, req)
}

// deleteThumbnailsForFiles 删除文件的缩略图缓存
func deleteThumbnailsForFiles(fileIDs []string) {
	cacheDir := "data/thumbnails"
	generator := thumbnail.NewGenerator(cacheDir)

	for _, fileID := range fileIDs {
		file, err := db.GetFileByID(fileID)
		if err != nil {
			continue
		}

		if file.Hash != nil && *file.Hash != "" {
			// 删除所有尺寸的缩略图
			if err := generator.DeleteAllThumbnails(*file.Hash); err != nil {
				llog.L().Warnf("delete thumbnails failed: hash=%s, err=%v", *file.Hash, err)
			}
		}
	}
}
