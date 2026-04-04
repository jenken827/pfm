package service

import (
	"context"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// GetStorageProviderTypes 获取支持的存储提供者类型列表
func GetStorageProviderTypes(ctx context.Context, req *native.GetStorageProviderTypesReq) (*native.GetStorageProviderTypesRespData, *models.Err) {
	metadatas := types.GetAllDriverMetadata()

	typeInfos := make([]*native.StorageProviderTypeInfo, 0, len(metadatas))
	for _, metadata := range metadatas {
		typeInfo := &native.StorageProviderTypeInfo{
			Type:          metadata.Type,
			Name:          metadata.Name,
			DisplayName:   metadata.DisplayName,
			Description:   metadata.Description,
			IconName:      metadata.IconName,
			ConfigFields:  metadata.ConfigFields,
		}
		typeInfos = append(typeInfos, typeInfo)
	}

	return &native.GetStorageProviderTypesRespData{
		Types: typeInfos,
	}, nil
}

// AddStorageProvider 添加存储提供者
func AddStorageProvider(ctx context.Context, req *native.StorageProvider) *models.Err {
	orm, err := req.ToORM(ctx)
	if err != nil {
		llog.L().Errorf("AddStorageProvider ToORM err: %v", err)
		return &models.Err{E: err, C: native.RespCode_PARAMS_ERROR}
	}

	err = db.Create(ctx, &orm, db.FilterProviderCreate)
	if err != nil {
		llog.L().Errorf("CreateStorageProvider err: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 同步添加存储实例
	if err := storage.AddStorage(&orm); err != nil {
		llog.L().Errorf("AddStorage err: %v", err)
	}

	return nil
}

// GetStorageProvider 获取存储提供者详情
func GetStorageProvider(ctx context.Context, req *native.StorageProviderGetReq) (*native.StorageProviderORM, *models.Err) {
	provider, _, err := db.Query[native.StorageProviderORM](ctx, &db.DBQuery{Where: "id = ?", Args: []any{req.GetId()}}, false)
	if err != nil {
		llog.L().Errorf("GetStorageProviders, id: %d, err: %v", req.GetId(), err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	if len(provider) == 0 {
		return nil, &models.Err{E: err, C: native.RespCode_NOT_FOUND}
	}
	return provider[0], nil
}

// ListStorageProviders 获取存储提供者列表
func ListStorageProviders(ctx context.Context) ([]*native.StorageProviderORM, *models.Err) {
	providers, _, err := db.Query[native.StorageProviderORM](ctx, nil, false)
	if err != nil {
		llog.L().Errorf("GetStorageProviders, err: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	return providers, nil
}

// UpdateStorageProvider update
func UpdateStorageProvider(ctx context.Context, req *native.StorageProvider) *models.Err {
	orm := &native.StorageProviderORM{
		Name:          req.Name,
		Description:   req.Description,
		Enabled:       req.Enabled,
		Order:         req.Order,
		FileSizeLimit: req.FileSizeLimit,
	}
	err := db.Update(ctx, orm, "id = ?", req.Id)
	if err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 查询完整数据后同步更新存储实例
	fullOrm, _, err := db.Query[native.StorageProviderORM](ctx, &db.DBQuery{Where: "id = ?", Args: []any{req.Id}}, false)
	if err == nil && len(fullOrm) > 0 {
		if err := storage.UpdateStorage(fullOrm[0]); err != nil {
			llog.L().Errorf("UpdateStorage err: %v", err)
		}
	}

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
