package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"gorm.io/gorm"
)

// GetUserSpaceByUserID 根据用户ID获取用户空间信息
func GetUserSpaceByUserID(userID string) (*entity.UserSpaceORM, error) {
	ctx := context.Background()
	var space entity.UserSpaceORM
	if err := db.WithContext(ctx).Where("user_id = ?", userID).First(&space).Error; err != nil {
		return nil, err
	}
	return &space, nil
}

// CreateUserSpace 创建用户空间
func CreateUserSpace(space *entity.UserSpaceORM) error {
	return db.Create(space).Error
}

// UpdateUserSpace 更新用户空间
func UpdateUserSpace(space *entity.UserSpaceORM) error {
	ctx := context.Background()
	_, err := gorm.G[*entity.UserSpaceORM](db).
		Where("id = ?", *space.Id).
		Updates(ctx, space)
	return err
}

// UpdateUserSpaceUsedBytes 按 user_id 写回用户已用容量（容量异步更新器调用）
func UpdateUserSpaceUsedBytes(userID string, usedBytes uint64) error {
	ctx := context.Background()
	return db.WithContext(ctx).
		Model(&entity.UserSpaceORM{}).
		Where("user_id = ?", userID).
		Update("used_bytes", usedBytes).Error
}

// GetAllUserIDs 返回全部用户空间所属 user_id（供容量启动全量刷新用）
func GetAllUserIDs() ([]string, error) {
	ctx := context.Background()
	var ids []string
	if err := db.WithContext(ctx).
		Model(&entity.UserSpaceORM{}).
		Pluck("user_id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

// ListUserSpaces 获取用户空间列表（分页）
func ListUserSpaces(offset, pageSize uint32) ([]*entity.UserSpaceORM, int64, error) {
	ctx := context.Background()

	if pageSize == 0 {
		pageSize = 50
	}

	var spaces []*entity.UserSpaceORM
	var total int64

	// 获取总数
	if err := db.WithContext(ctx).Model(&entity.UserSpaceORM{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	if err := db.WithContext(ctx).
		Offset(int(offset)).
		Limit(int(pageSize)).
		Order("id ASC").
		Find(&spaces).Error; err != nil {
		return nil, 0, err
	}

	return spaces, total, nil
}

// GetBoundProviderIdsByOwnerID 通过 owner_id 查询 UserSpace 获取绑定的存储提供者
// 用于后台协程（无 context 的场景，如上传调度）及其他包调用
func GetBoundProviderIdsByOwnerID(ownerID string) ([]uint32, error) {
	space, err := GetUserSpaceByUserID(ownerID)
	if err != nil {
		return nil, fmt.Errorf("用户 %s 未配置 UserSpace: %w", ownerID, err)
	}
	if space.BoundProviderIds == nil || *space.BoundProviderIds == "" {
		return nil, fmt.Errorf("用户 %s 未绑定存储提供者", ownerID)
	}
	var ids []uint32
	if err := json.Unmarshal([]byte(*space.BoundProviderIds), &ids); err != nil {
		return nil, fmt.Errorf("解析 bound_provider_ids 失败: %w", err)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("用户 %s 未绑定存储提供者", ownerID)
	}
	return ids, nil
}
