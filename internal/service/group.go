package service

import (
	"context"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"gorm.io/gorm"
)

// ====== StorageGroup Service ======

// AddStorageGroup 创建存储组
func AddStorageGroup(ctx context.Context, req *native.StorageGroupAddReq) (*native.StorageGroup, *models.Err) {
	group := &native.StorageGroupORM{
		Name:        req.Name,
		Description: req.Description,
	}

	err := db.CreateStorageGroup(group)
	if err != nil {
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	pb, err := group.ToPB(ctx)
	if err != nil {
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	return &pb, nil
}

// GetStorageGroup 获取存储组详情
func GetStorageGroup(ctx context.Context, req *native.StorageGroupGetReq) (*native.StorageGroupGetRespData, *models.Err) {
	group, err := db.GetStorageGroupByID(*req.Id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &models.Err{E: err, Code: native.RespCode_PARAMS_ERROR}
		}
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	pb, err := group.ToPB(ctx)
	if err != nil {
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	// Get member count and capacity stats
	members, _, err := db.ListStorageProviders(0, -1, "", nil, req.Id, false, "", false)
	if err != nil {
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	var totalCapacity, remainingCapacity uint64
	for _, member := range members {
		totalCapacity += *member.TotalCapacity
		remainingCapacity += *member.TotalCapacity
	}

	return &native.StorageGroupGetRespData{
		Id:                pb.Id,
		Name:              pb.Name,
		Description:       pb.Description,
		MemberCount:       func() *uint32 { c := uint32(len(members)); return &c }(),
		TotalCapacity:     &totalCapacity,
		RemainingCapacity: &remainingCapacity,
		CreatedAt:         pb.CreatedAt,
	}, nil
}

// ListStorageGroups 获取存储组列表
func ListStorageGroups(ctx context.Context, req *native.StorageGroupListReq) (*native.StorageGroupListRespData, *models.Err) {
	page := int(req.GetPage())
	if page == 0 {
		page = 1
	}
	pageSize := int(req.GetPageSize())
	if pageSize == 0 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	groups, total, err := db.ListStorageGroups(
		offset,
		pageSize,
		req.GetKeyword(),
		req.GetSortBy(),
		req.GetDesc(),
	)
	if err != nil {
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	items := make([]*native.StorageGroupItem, 0, len(groups))
	for _, group := range groups {
		pb, err := group.ToPB(ctx)
		if err != nil {
			return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
		}

		item := &native.StorageGroupItem{
			Id:          pb.Id,
			Name:        pb.Name,
			Description: pb.Description,
			CreatedAt:   pb.CreatedAt,
		}

		items = append(items, item)
	}

	return &native.StorageGroupListRespData{
		Groups: items,
		Total:  func() *uint32 { t := uint32(total); return &t }(),
	}, nil
}

// UpdateStorageGroup 更新存储组
func UpdateStorageGroup(ctx context.Context, req *native.StorageGroupUpdateReq) *models.Err {
	group, err := db.GetStorageGroupByID(*req.Id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return &models.Err{E: err, Code: native.RespCode_PARAMS_ERROR}
		}
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	if req.Name != nil {
		group.Name = req.Name
	}
	if req.Description != nil {
		group.Description = req.Description
	}

	err = db.UpdateStorageGroup(group)
	if err != nil {
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	return nil
}

// DeleteStorageGroup 删除存储组
func DeleteStorageGroup(ctx context.Context, req *native.StorageGroupDeleteReq) *models.Err {
	_, err := db.GetStorageGroupByID(*req.Id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return &models.Err{E: err, Code: native.RespCode_PARAMS_ERROR}
		}
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	// Check if group has members
	members, _, err := db.ListStorageProviders(0, -1, "", nil, req.Id, false, "", false)
	if err != nil {
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	if len(members) > 0 && !req.GetForce() {
		return &models.Err{E: err, Code: native.RespCode_PERMISSION_DENIED}
	}

	err = db.DeleteStorageGroup(*req.Id)
	if err != nil {
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	return nil
}
