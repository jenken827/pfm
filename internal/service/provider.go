package service

import (
	"context"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// AddStorageProvider 添加存储提供者
func AddStorageProvider(ctx context.Context, req *native.StorageProvider) *models.Err {
	orm, err := req.ToORM(ctx)
	if err != nil {
		llog.L().Errorf("AddStorageProvider ToORM err: %v", err)
		return &models.Err{E: err, C: native.RespCode_PARAMS_ERROR}
	}

	err = db.Create[native.StorageProviderORM](ctx, &orm, db.FilterProviderCreate)
	if err != nil {
		llog.L().Errorf("CreateStorageProvider err: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
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
	err := db.Update[native.StorageProviderORM](ctx, orm, "id = ?", req.Id)
	if err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	return nil
}

// DeleteStorageProvider 删除存储提供者
func DeleteStorageProvider(ctx context.Context, req *native.StorageProviderDeleteReq) *models.Err {

	err := db.DeleteStorageProvider(req.GetIds())
	if err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	return nil
}
