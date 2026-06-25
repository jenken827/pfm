package db

import (
	"context"

	"jenken827.com/jenken827/pfm/proto/gen/native"
	"gorm.io/gorm"
)

// GetUserSpaceByUserID 根据用户ID获取用户空间信息
func GetUserSpaceByUserID(userID string) (*native.UserSpaceORM, error) {
	ctx := context.Background()
	var space native.UserSpaceORM
	if err := db.WithContext(ctx).Where("user_id = ?", userID).First(&space).Error; err != nil {
		return nil, err
	}
	return &space, nil
}

// CreateUserSpace 创建用户空间
func CreateUserSpace(space *native.UserSpaceORM) error {
	return db.Create(space).Error
}

// UpdateUserSpace 更新用户空间
func UpdateUserSpace(space *native.UserSpaceORM) error {
	ctx := context.Background()
	_, err := gorm.G[*native.UserSpaceORM](db).
		Where("id = ?", *space.Id).
		Updates(ctx, space)
	return err
}

// ListUserSpaces 获取用户空间列表（分页）
func ListUserSpaces(offset, pageSize uint32) ([]*native.UserSpaceORM, int64, error) {
	ctx := context.Background()

	if pageSize == 0 {
		pageSize = 50
	}

	var spaces []*native.UserSpaceORM
	var total int64

	// 获取总数
	if err := db.WithContext(ctx).Model(&native.UserSpaceORM{}).Count(&total).Error; err != nil {
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
