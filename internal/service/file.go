package service

import (
	"context"
	"encoding/json"
	"errors"
	"maps"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/internal/storage"
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
func CreateDir(ctx context.Context, req *native.CreateDirReq) *models.Err {
	providerIds := req.ProviderIds
	// 如果客户端未传 provider_ids，使用默认绑定的存储提供者
	if len(providerIds) == 0 {
		defaultProviders, err := db.GetDefaultBoundEnabledProviders()
		if err != nil {
			llog.L().Errorf("get default bound providers failed: %v", err)
			return &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}
		if len(defaultProviders) == 0 {
			return &models.Err{
				E: errors.New("no default bound provider available, please specify provider_ids or set default_bound in storage providers"),
				C: native.RespCode_PARAMS_ERROR,
			}
		}

		// 提取ID列表
		providerIds = make([]uint32, len(defaultProviders))
		for i, p := range defaultProviders {
			providerIds[i] = *p.Id
		}
	} else {
		// 验证提供者是否存在且启用（批量查询）
		providers, err := db.GetEnabledProvidersByIds(providerIds)
		if err != nil {
			llog.L().Errorf("query providers failed: %v", err)
			return &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}
		// 验证所有提供的ID都有效
		if len(providers) != len(providerIds) {
			return &models.Err{
				E: errors.New("some providers are invalid or disabled"),
				C: native.RespCode_PARAMS_ERROR,
			}
		}
	}

	// 转换为JSON字符串存储
	providerIdsJSON, err := json.Marshal(providerIds)
	if err != nil {
		llog.L().Errorf("marshal provider_ids failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
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
		return &models.Err{E: e, C: native.RespCode_DB_ERROR}
	}
	return nil
}

// UpdateDir 更新文件夹
func UpdateDir(ctx context.Context, req *native.UpdateDirReq) *models.Err {
	orm := native.FileORM{
		Id:               req.Id,
		Name:             req.Name,
		EnableEncryption: req.EnableEncryption,
	}

	// 如果提供了 provider_ids，更新绑定关系
	if len(req.ProviderIds) > 0 {
		providerIds := req.ProviderIds

		// 批量查询验证提供者是否存在且启用
		providers, err := db.GetEnabledProvidersByIds(providerIds)
		if err != nil {
			llog.L().Errorf("query providers failed: %v", err)
			return &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}

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
func DeleteFile(ctx context.Context, req *native.DeleteFileReq) *models.Err {
	ids := req.Ids
	if len(ids) == 0 {
		return nil
	}

	// 移动到回收站（软删除）
	if err := db.DeleteFiles(ids); err != nil {
		llog.L().Errorf("DeleteFiles failed, err: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

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
func RestoreFromTrash(ctx context.Context, req *native.RestoreFromTrashReq) *models.Err {
	if len(req.Ids) == 0 {
		return nil
	}

	if err := db.RestoreFromTrash(req.Ids); err != nil {
		llog.L().Errorf("restore from trash failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	llog.L().Infof("restored %d files from trash", len(req.Ids))
	return nil
}

// deleteFilesFromProviders 从存储提供者批量删除文件（内部函数）
// 按 provider_id 分组后批量删除，提高效率
func deleteFilesFromProviders(ctx context.Context, fileIDs []string) {
	if len(fileIDs) == 0 {
		return
	}
	llog.L().Infof("delet files  from providers,id length: %d", len(fileIDs))

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
		llog.L().Debugf("success get fil locatione by hash: %s,locations len: %d", fileHash, len(locations))

		for _, loc := range locations {
			if loc.ProviderId == nil {
				llog.L().Debugf("location provider id is nil, locaiton id: %d", loc.Id)
				continue
			}
			providerID := *loc.ProviderId
			providerLocations[providerID] = append(providerLocations[providerID], loc)
		}
	}
	llog.L().Infof("delet files  from providers,provider len: %d", len(providerLocations))

	// 4. 按 provider_id 批量删除文件，并跟踪删除结果
	providerDeletedSizes := make(map[uint32]uint64) // providerID -> deletedSize
	locationIDsToDelete := make([]uint64, 0)
	locationIDsFailed := make([]uint64, 0) // 删除失败的 location ID

	for providerID, locations := range providerLocations {
		llog.L().Infof("delet files loop providerlocations,providerID: %d", providerID)
		st := storage.GetStorage(providerID)
		if st == nil {
			llog.L().Warnf("storage provider not found, id: %d", providerID)
			// 将所有 location 标记为删除失败
			for _, loc := range locations {
				if loc.Id != nil {
					locationIDsFailed = append(locationIDsFailed, *loc.Id)
				}
			}
			continue
		}
		llog.L().Infof("delet files  get storage success,name: %s", st.GetName())

		// 尝试使用批量删除接口
		if batchDeleter, ok := st.(types.BatchDeleter); ok {
			llog.L().Infof("provider id:%d support batch delete", providerID)
			results, err := batchDeleter.BatchDelete(ctx, locations)

			// 处理返回结果
			if results == nil && err == nil {
				// 全部删除成功
				llog.L().Infof("batch deleted all %d files from provider %d", len(locations), providerID)
				var deletedSize uint64
				for _, loc := range locations {
					if loc.Id != nil {
						locationIDsToDelete = append(locationIDsToDelete, *loc.Id)
					}
					if loc.ChunkSize != nil && *loc.Status == int32(native.LocationStatus_PCOMPLETED) {
						deletedSize += *loc.ChunkSize
					}
				}
				providerDeletedSizes[providerID] = deletedSize
			} else if results == nil && err != nil {
				// 全部删除失败
				llog.L().Errorf("batch delete all failed: %v, provider_id: %d", err, providerID)
				for _, loc := range locations {
					if loc.Id != nil {
						locationIDsFailed = append(locationIDsFailed, *loc.Id)
					}
				}
			} else {
				// 部分成功，处理每条记录的状态
				llog.L().Infof("batch delete partial success: %d/%d from provider %d",
					countSuccess(results), len(results), providerID)

				var deletedSize uint64
				for i, result := range results {
					if i >= len(locations) {
						break
					}
					loc := locations[i]

					if result.Success {
						// 删除成功
						if loc.Id != nil {
							locationIDsToDelete = append(locationIDsToDelete, *loc.Id)
						}
						if loc.ChunkSize != nil && *loc.Status == int32(native.LocationStatus_PCOMPLETED) {
							deletedSize += *loc.ChunkSize
						}
					} else {
						// 删除失败
						if loc.Id != nil {
							locationIDsFailed = append(locationIDsFailed, *loc.Id)
						}
						llog.L().Errorf("delete location %d failed: %v", *loc.Id, result.Error)
					}
				}
				if deletedSize > 0 {
					providerDeletedSizes[providerID] = deletedSize
				}
			}
		} else {
			// 不支持 BatchDeleter，跳过
			llog.L().Warnf("provider %d does not support BatchDeleter, skipping", providerID)
			for _, loc := range locations {
				if loc.Id != nil {
					locationIDsFailed = append(locationIDsFailed, *loc.Id)
				}
			}
		}
	}

	// 5. 更新存储提供者的已使用容量（只更新成功删除的容量）
	for providerID, size := range providerDeletedSizes {
		if err := db.DecrementUsedCapacity(providerID, size); err != nil {
			llog.L().Errorf("decrement used capacity failed: %v, provider_id: %d", err, providerID)
		}
	}

	// 6. 批量删除成功删除的文件位置记录
	if len(locationIDsToDelete) > 0 {
		if err := db.DeleteFileLocations(locationIDsToDelete); err != nil {
			llog.L().Errorf("batch delete file locations failed: %v", err)
		}
	}

	// 7. 更新删除失败的文件位置记录状态
	if len(locationIDsFailed) > 0 {
		if err := db.UpdateFileLocationStatus(locationIDsFailed, int32(native.LocationStatus_PDELETED_FAILED)); err != nil {
			llog.L().Errorf("update file location status to DELETED_FAILED failed: %v", err)
		} else {
			llog.L().Warnf("marked %d file locations as DELETED_FAILED", len(locationIDsFailed))
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

	// 异步执行删除操作
	go func() {
		defer func() {
			if r := recover(); r != nil {
				llog.L().Errorf("delete files panic: %v", r)
			}
		}()

		// 使用新的 context，因为原 context 可能在请求结束后失效
		asyncCtx := context.Background()

		// 1. 删除文件的历史记录和对应的存储提供者上的文件
		deleteFileHistories(asyncCtx, fileIDs)

		// 2. 从存储提供者删除当前文件
		deleteFilesFromProviders(asyncCtx, fileIDs)

		// 3. 永久删除数据库记录
		if err := db.DeletePermanently(fileIDs); err != nil {
			llog.L().Errorf("delete permanently failed: %v", err)
			return
		}

		llog.L().Infof("permanently deleted %d files", len(fileIDs))
	}()

	return nil
}

// deleteFileHistories 删除文件的历史记录和对应的存储提供者上的文件
func deleteFileHistories(ctx context.Context, fileIDs []string) {
	if len(fileIDs) == 0 {
		return
	}
	llog.L().Infof("delete file history ids length: %d", len(fileIDs))
	// 1. 批量获取所有文件的历史记录
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

	// 2. 收集所有历史记录的 hash
	hashSet := make(map[string]bool)
	for _, history := range allHistories {
		if history.Hash != nil && *history.Hash != "" {
			hashSet[*history.Hash] = true
		}
	}

	// 3. 根据 hash 批量获取文件位置记录
	providerLocations := make(map[uint32][]*native.FileLocationORM)
	for hash := range hashSet {
		llog.L().Infof("delete history get locations by hash:%s", hash)
		locations, err := db.GetFileLocationsByBlake3Hash(hash)
		if err != nil {
			llog.L().Errorf("get file locations by hash failed, hash: %s, err: %v", hash, err)
			continue
		}

		for _, loc := range locations {
			if loc.ProviderId == nil {
				continue
			}
			providerID := *loc.ProviderId
			providerLocations[providerID] = append(providerLocations[providerID], loc)
		}
	}

	llog.L().Infof("delete history provider ids:%+v", maps.Keys(providerLocations))

	// 4. 按 provider 批量删除历史文件
	providerDeletedSizes := make(map[uint32]uint64)

	for providerID, locations := range providerLocations {
		st := storage.GetStorage(providerID)
		if st == nil {
			llog.L().Warnf("storage provider not found, id: %d", providerID)
			continue
		}

		// 尝试使用批量删除接口
		if batchDeleter, ok := st.(types.BatchDeleter); ok {
			results, err := batchDeleter.BatchDelete(ctx, locations)

			// 处理返回结果
			if results == nil && err == nil {
				// 全部删除成功
				llog.L().Infof("batch deleted all %d history files from provider %d", len(locations), providerID)
				var deletedSize uint64
				for _, loc := range locations {
					if loc.ChunkSize != nil && *loc.Status == int32(native.LocationStatus_PCOMPLETED) {
						deletedSize += *loc.ChunkSize
					}
				}
				providerDeletedSizes[providerID] = deletedSize
			} else if results == nil && err != nil {
				// 全部删除失败
				llog.L().Errorf("batch delete history files all failed: %v, provider_id: %d", err, providerID)
			} else {
				// 部分成功
				var deletedSize uint64
				for i, result := range results {
					if i >= len(locations) {
						break
					}
					loc := locations[i]

					if result.Success {
						if loc.ChunkSize != nil && *loc.Status == int32(native.LocationStatus_PCOMPLETED) {
							deletedSize += *loc.ChunkSize
						}
					} else {
						llog.L().Errorf("delete history location %d failed: %v", *loc.Id, result.Error)
					}
				}
				if deletedSize > 0 {
					providerDeletedSizes[providerID] = deletedSize
				}
			}
		}
	}

	// 5. 更新存储提供者的已使用容量
	for providerID, size := range providerDeletedSizes {
		if err := db.DecrementUsedCapacity(providerID, size); err != nil {
			llog.L().Errorf("decrement used capacity failed for history: %v, provider_id: %d", err, providerID)
		}
	}

	// 6. 删除数据库中的历史记录
	if err := db.DeleteFileHistoryByFileIDs(fileIDs); err != nil {
		llog.L().Errorf("delete file histories failed: %v", err)
	} else {
		llog.L().Infof("deleted %d file history records", len(allHistories))
	}
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

	// 3. 异步执行删除操作
	go func() {
		defer func() {
			if r := recover(); r != nil {
				llog.L().Errorf("empty trash panic: %v", r)
			}
		}()

		asyncCtx := context.Background()

		// 从存储提供者删除文件
		deleteFilesFromProviders(asyncCtx, fileIDs)

		// 清空回收站数据库记录
		if err := db.EmptyTrash(); err != nil {
			llog.L().Errorf("empty trash failed: %v", err)
			return
		}

		llog.L().Infof("emptied trash, deleted %d files", len(fileIDs))
	}()

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
	for i, id := range req.ProviderIds {
		providerIDs[i] = id
	}

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
