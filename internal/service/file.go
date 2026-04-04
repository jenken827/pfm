package service

import (
	"context"
	"encoding/json"
	"errors"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// GetFileList 获取文件列表
func GetFileList(ctx context.Context, req *native.FileListReq) ([]*native.FileORM, int64, *models.Err) {
	files, total, err := db.Query[native.FileORM](ctx, &db.DBQuery{Where: "parent_id = ?", Args: []any{req.ParentId}}, true)
	if err != nil {
		llog.L().Errorf("GetFileList failed, err: %v", err)
		return nil, 0, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	return files, total, nil
}

// CreateDir 创建文件夹
func CreateDir(ctx context.Context, req *native.CreateDirReq) *models.Err {
	providerIds := req.ProviderIds

	// 如果客户端未传 provider_ids，使用默认绑定的存储提供者
	if len(providerIds) == 0 {
		defaultProviders, err := getDefaultBoundProviders(ctx)
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
		providers, _, err := db.Query[native.StorageProvider](ctx, &db.DBQuery{
			Where: "id IN ? AND enabled = ?",
			Args:  []any{providerIds, true},
		}, false)
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

	t := int32(native.FileType_FOLDER.Number())
	f := &native.FileORM{
		Name:             req.Name,
		ParentId:         req.ParentId,
		Type:             &t,
		EnableEncryption: req.EnableEncryption,
		EncryptFilename:  req.EncryptFilename,
		BoundProviderIds: &boundProviderIds,
	}
	e := db.Create(ctx, f, nil)
	if e != nil {
		return &models.Err{E: e, C: native.RespCode_DB_ERROR}
	}
	return nil
}

// UpdateDir 更新文件夹
// 文件夹加密信息不可以更改
func UpdateDir(ctx context.Context, req *native.UpdateDirReq) *models.Err {
	orm := native.FileORM{
		Name:             req.Name,
		EnableEncryption: req.EnableEncryption,
		EncryptFilename:  req.EncryptFilename,
	}

	// 如果提供了 provider_ids，更新绑定关系
	if len(req.ProviderIds) > 0 {
		providerIds := req.ProviderIds

		// 批量查询验证提供者是否存在且启用
		providers, _, err := db.Query[native.StorageProvider](ctx, &db.DBQuery{
			Where: "id IN ? AND enabled = ?",
			Args:  []any{providerIds, true},
		}, false)
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

	if err := db.Update(ctx, &orm, "id=?", *req.Id); err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	return nil
}

// getDefaultBoundProviders 获取默认绑定的存储提供者
func getDefaultBoundProviders(ctx context.Context) ([]*native.StorageProvider, error) {
	providers, _, err := db.Query[native.StorageProvider](ctx, &db.DBQuery{
		Where: "default_bound = ? AND enabled = ?",
		Args:  []any{true, true},
	}, false)
	return providers, err
}

// DeleteFile 删除文件
func DeleteFile(ctx context.Context, req *native.DeleteFileReq) *models.Err {
	// Convert []string to []any for db.Delete
	ids := make([]any, len(req.Ids))
	for i, id := range req.Ids {
		ids[i] = id
	}
	if err := db.Delete[native.FileORM](ctx, ids); err != nil {
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
