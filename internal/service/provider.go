package service

import (
	"context"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

// GetStorageProviderTypes 获取支持的存储提供者类型列表
func GetStorageProviderTypes(ctx context.Context, req *native.GetStorageProviderTypesReq) (*native.GetStorageProviderTypesRespData, *models.Err) {
	metadatas := types.GetAllDriverMetadata()

	typeInfos := make([]*native.StorageProviderTypeInfo, 0, len(metadatas))
	for _, metadata := range metadatas {
		typeInfo := &native.StorageProviderTypeInfo{
			Type:         metadata.Type,
			Name:         metadata.Name,
			DisplayName:  metadata.DisplayName,
			Description:  metadata.Description,
			IconName:     metadata.IconName,
			ConfigFields: metadata.ConfigFields,
		}
		typeInfos = append(typeInfos, typeInfo)
	}

	return &native.GetStorageProviderTypesRespData{
		Types: typeInfos,
	}, nil
}

// AddStorageProvider 添加存储提供者
func AddStorageProvider(ctx context.Context, req *native.AddStorageProviderReq) *models.Err {
	orm := &native.StorageProviderORM{
		ProviderType:  utils.EnumPtr(*req.ProviderType),
		Name:          req.Name,
		Config:        req.Config,
		TotalCapacity: req.TotalCapacity,
		UsedCapacity:  new(uint64),
		Enabled:       req.Enabled,
		SortOrder:     req.SortOrder,
		FileSizeLimit: req.FileSizeLimit,
		DefaultBound:  req.DefaultBound,
	}

	if req.Description != nil {
		orm.Description = req.Description
	}
	if req.EncryptionKey != nil {
		orm.EncryptionKey = req.EncryptionKey
	}
	if req.EncryptionSuffix != nil {
		orm.EncryptionSuffix = req.EncryptionSuffix
	}
	if req.Proxy != nil {
		orm.Proxy = req.Proxy
	}
	if req.MaxConcurrentUploads != nil {
		orm.MaxConcurrentUploads = req.MaxConcurrentUploads
	}

	err := db.CreateStorageProvider(orm)
	if err != nil {
		llog.C(ctx).Errorf("CreateStorageProvider err: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 异步添加存储实例（不阻塞请求）
	storage.AddStorage(orm)

	return nil
}

// GetStorageProvider 获取存储提供者详情
func GetStorageProvider(ctx context.Context, req *native.StorageProviderGetReq) (*native.StorageProviderORM, *models.Err) {
	provider, err := db.GetProviderByID(req.GetId())
	if err != nil {
		llog.C(ctx).Errorf("GetStorageProviders, id: %d, err: %v", req.GetId(), err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	return provider, nil
}

// ListStorageProviders 获取存储提供者列表
func ListStorageProviders(ctx context.Context) ([]*native.StorageProviderORM, *models.Err) {
	providers, err := db.GetAllProviders()
	if err != nil {
		llog.C(ctx).Errorf("GetStorageProviders, err: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	return providers, nil
}

// UpdateStorageProvider update
func UpdateStorageProvider(ctx context.Context, req *native.UpdateStorageProviderReq) *models.Err {
	orm := &native.StorageProviderORM{
		Id: req.Id,
	}

	if req.Name != nil {
		orm.Name = req.Name
	}
	if req.Description != nil {
		orm.Description = req.Description
	}
	if req.Config != nil {
		orm.Config = req.Config
	}
	if req.Enabled != nil {
		orm.Enabled = req.Enabled
	}
	if req.SortOrder != nil {
		orm.SortOrder = req.SortOrder
	}
	if req.TotalCapacity != nil {
		orm.TotalCapacity = req.TotalCapacity
	}
	if req.FileSizeLimit != nil {
		orm.FileSizeLimit = req.FileSizeLimit
	}
	if req.DefaultBound != nil {
		orm.DefaultBound = req.DefaultBound
	}
	if req.Proxy != nil {
		orm.Proxy = req.Proxy
	}
	if req.MaxConcurrentUploads != nil {
		orm.MaxConcurrentUploads = req.MaxConcurrentUploads
	}

	err := db.UpdateStorageProvider(orm)
	if err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 查询完整数据后异步更新存储实例（不阻塞请求）
	fullOrm, err := db.GetProviderByID(req.GetId())
	if err != nil {
		llog.C(ctx).Errorf("GetProviderByID err: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	storage.UpdateStorage(fullOrm)

	// 刷新并发信号量缓存（max_concurrent_uploads 变更后需要重建信号量）
	storage.RefreshProviderSemaphore(req.GetId())

	return nil
}

// DeleteStorageProvider 删除存储提供者
func DeleteStorageProvider(ctx context.Context, req *native.StorageProviderDeleteReq) *models.Err {
	err := db.DeleteStorageProvider(req.GetIds())
	if err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 同步删除存储实例
	storage.DeleteStorage(req.GetIds())

	return nil
}
