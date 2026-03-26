package service

import (
	"context"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"gorm.io/gorm"
)

// ====== StorageProvider Service ======

// AddStorageProvider 添加存储提供者
func AddStorageProvider(ctx context.Context, req *native.StorageProviderAddReq) (*native.StorageProvider, *models.Err) {
	t := int32(*req.ProviderType)
	provider := &native.StorageProviderORM{
		ProviderType:  &t,
		Name:          req.Name,
		Description:   req.Description,
		Config:        req.Config,
		GroupId:       req.GroupId,
		Enabled:       req.Enabled,
		Order:         req.Order,
		TotalCapacity: req.TotalCapacity,
		FileSizeLimit: req.FileSizeLimit,
	}

	err := db.CreateStorageProvider(provider)
	if err != nil {
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	pb, err := provider.ToPB(ctx)
	if err != nil {
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	return &pb, nil
}

// GetStorageProvider 获取存储提供者详情
func GetStorageProvider(ctx context.Context, req *native.StorageProviderGetReq) (*native.StorageProviderGetRespData, *models.Err) {
	provider, err := db.GetStorageProviderByID(*req.Id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &models.Err{E: err, Code: native.RespCode_PARAMS_ERROR}
		}
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	pb, err := provider.ToPB(ctx)
	if err != nil {
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	var groupName *string
	if provider.GroupId != nil && *provider.GroupId > 0 {
		group, groupErr := db.GetStorageGroupByID(*provider.GroupId)
		if groupErr == nil {
			groupName = group.Name
		}
	}

	return &native.StorageProviderGetRespData{
		Detail:    &pb,
		GroupName: groupName,
	}, nil
}

// ListStorageProviders 获取存储提供者列表
func ListStorageProviders(ctx context.Context, req *native.StorageProviderListReq) (*native.StorageProviderListRespData, *models.Err) {
	page := int(req.GetPage())
	if page == 0 {
		page = 1
	}
	pageSize := int(req.GetPageSize())
	if pageSize == 0 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	var providerType *native.StorageProviderType
	if req.ProviderType != nil && *req.ProviderType != native.StorageProviderType_PROVIDER_UNSPECIFIED {
		providerType = req.ProviderType
	}

	var groupID *uint32
	if req.GroupId != nil && *req.GroupId > 0 {
		groupID = req.GroupId
	}

	providers, total, err := db.ListStorageProviders(
		offset,
		pageSize,
		req.GetKeyword(),
		providerType,
		groupID,
		req.GetEnabledOnly(),
		req.GetSortBy(),
		req.GetDesc(),
	)
	if err != nil {
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	items := make([]*native.StorageProviderItem, 0, len(providers))

	// 批量获取所有组 ID
	groupIDs := make(map[uint32]bool)
	for _, p := range providers {
		if p.GroupId != nil && *p.GroupId > 0 {
			groupIDs[*p.GroupId] = true
		}
	}

	// 一次性获取所有组信息
	groupNames := make(map[uint32]string)
	if len(groupIDs) > 0 {
		for groupID := range groupIDs {
			group, err := db.GetStorageGroupByID(groupID)
			if err == nil && group.Name != nil {
				groupNames[groupID] = *group.Name
			}
		}
	}

	// 构建返回结果
	for _, p := range providers {
		pb, err := p.ToPB(ctx)
		if err != nil {
			return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
		}

		item := &native.StorageProviderItem{
			Id:                pb.Id,
			ProviderType:      pb.ProviderType,
			Name:              pb.Name,
			Description:       pb.Description,
			GroupId:           p.GroupId,
			TotalCapacity:     pb.TotalCapacity,
			RemainingCapacity: pb.RemainingCapacity,
			Enabled:           pb.Enabled,
			Order:             pb.Order,
			CreatedAt:         pb.CreatedAt,
			Config:            pb.Config,
		}

		// 从缓存的 map 中获取组名
		if p.GroupId != nil && *p.GroupId > 0 {
			if groupName, ok := groupNames[*p.GroupId]; ok {
				item.GroupName = &groupName
			}
		}

		items = append(items, item)
	}

	return &native.StorageProviderListRespData{
		Providers: items,
		Total:     func() *uint32 { t := uint32(total); return &t }(),
	}, nil
}

// UpdateStorageProvider 更新存储提供者
func UpdateStorageProvider(ctx context.Context, req *native.StorageProviderUpdateReq) *models.Err {
	provider, err := db.GetStorageProviderByID(*req.Id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return &models.Err{E: err, Code: native.RespCode_PARAMS_ERROR}
		}
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	if req.Name != nil {
		provider.Name = req.Name
	}
	if req.Description != nil {
		provider.Description = req.Description
	}
	if req.Config != nil {
		provider.Config = req.Config
	}
	if req.GroupId != nil {
		provider.GroupId = req.GroupId
	}
	if req.TotalCapacity != nil {
		provider.TotalCapacity = req.TotalCapacity
	}
	if req.RemainingCapacity != nil {
		provider.RemainingCapacity = req.RemainingCapacity
	}
	if req.Enabled != nil {
		provider.Enabled = req.Enabled
	}
	if req.Order != nil {
		provider.Order = req.Order
	}

	err = db.UpdateStorageProvider(provider)
	if err != nil {
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	return nil
}

// DeleteStorageProvider 删除存储提供者
func DeleteStorageProvider(ctx context.Context, req *native.StorageProviderDeleteReq) *models.Err {
	_, err := db.GetStorageProviderByID(*req.Id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return &models.Err{E: err, Code: native.RespCode_PARAMS_ERROR}
		}
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	err = db.DeleteStorageProvider(*req.Id)
	if err != nil {
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	return nil
}

// GetStorageProviderStats 获取存储提供者统计信息
func GetStorageProviderStats(ctx context.Context, req *native.StorageProviderStatsReq) (*native.StorageProviderStatsRespData, *models.Err) {
	_, err := db.GetStorageProviderByID(*req.Id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &models.Err{E: err, Code: native.RespCode_PARAMS_ERROR}
		}
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	// TODO: Implement actual statistics from database
	// For now, return placeholder data
	return &native.StorageProviderStatsRespData{
		FileCount:    func() *uint32 { c := uint32(0); return &c }(),
		TotalSize:    func() *uint64 { s := uint64(0); return &s }(),
		VersionCount: func() *uint32 { c := uint32(0); return &c }(),
		LastUsedAt:   nil,
	}, nil
}
