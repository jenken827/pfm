package service

import (
	"context"
	"encoding/json"
	"errors"

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

// DeleteFile 删除文件
func DeleteFile(ctx context.Context, req *native.DeleteFileReq) *models.Err {
	ids := req.Ids
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
		if loc.Status != nil && *loc.Status == int32(native.UploadProviderStatus_PCOMPLETED) {
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
			if existingLoc.Status != nil && *existingLoc.Status == int32(native.UploadProviderStatus_PCOMPLETED) {
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
		Status:            utils.Int32Ptr(int32(native.UploadProviderStatus_PPENDING)),
		TargetProviderIds: utils.StringPtr(string(providerIDsJSON)), // JSON array like [1,3,5]
		TaskType:          utils.Int32Ptr(int32(native.UploadTaskType_SYNC)),
	}

	if err := db.CreateUploadRecord(uploadRecord); err != nil {
		llog.L().Errorf("create upload record failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 7. Trigger storage dispatch (will handle multi-provider distribution)
	go storage.Dispatch(ctx, uploadRecord)

	llog.L().Infof("created sync task for file %s to %d providers", *file.Id, len(validProviderIDs))
	return nil
}
