package db

import (
	"context"

	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"gorm.io/gorm"
)

// GetStorageProvider 获取存储提供者
func GetStorageProvider(expression string, args ...any) (*native.StorageProviderORM, error) {
	var provider *native.StorageProviderORM
	var err error
	ctx := context.Background()
	d := gorm.G[*native.StorageProviderORM](db)

	if expression != "" {
		provider, err = d.Where(expression, args...).First(ctx)
	} else {
		provider, err = d.First(ctx)
	}
	if err != nil {
		llog.GetLogger().Errorf("get storage provider failed, err: %v", err)
		return nil, err
	}
	return provider, nil
}

// UpdateFile 更新文件信息
func UpdateFile(file *native.FileORM) error {
	ctx := context.Background()
	_, err := gorm.G[native.FileORM](db).Where("id=?", file.Id).Updates(ctx, *file)
	return err
}

// GetStorageGroupByID 根据 ID 获取存储组
func GetStorageGroupByID(id uint32) (*native.StorageGroupORM, error) {
	ctx := context.Background()
	group, err := gorm.G[*native.StorageGroupORM](db).Where("id = ? AND deleted_at IS NULL", id).First(ctx)
	if err != nil {
		llog.GetLogger().Errorf("get storage group by id failed, id: %d, err: %v", id, err)
		return nil, err
	}
	return group, nil
}

// ListStorageGroups 获取存储组列表
func ListStorageGroups(offset, limit int, keyword string, sortBy string, desc bool) ([]*native.StorageGroupORM, int64, error) {
	ctx := context.Background()
	var total int64

	query := gorm.G[*native.StorageGroupORM](db).Where("deleted_at IS NULL")

	if keyword != "" {
		query = query.Where("name LIKE ? OR description LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}

	// Count total
	total, err := query.Count(ctx, "*")
	if err != nil {
		return nil, 0, err
	}

	// Order
	order := "created_at ASC"
	if sortBy != "" {
		order = sortBy + " ASC"
		if desc {
			order = sortBy + " DESC"
		}
	}

	// Find with pagination
	groups, err := query.Order(order).Offset(offset).Limit(limit).Find(ctx)
	if err != nil {
		llog.GetLogger().Errorf("list storage groups failed, err: %v", err)
		return nil, 0, err
	}

	return groups, total, nil
}

// CreateStorageGroup 创建存储组
func CreateStorageGroup(group *native.StorageGroupORM) error {
	ctx := context.Background()
	err := gorm.G[native.StorageGroupORM](db).Create(ctx, group)
	return err
}

// UpdateStorageGroup 更新存储组
func UpdateStorageGroup(group *native.StorageGroupORM) error {
	ctx := context.Background()
	_, err := gorm.G[native.StorageGroupORM](db).Updates(ctx, *group)
	return err
}

// DeleteStorageGroup 删除存储组（软删除）
func DeleteStorageGroup(id uint32) error {
	ctx := context.Background()
	_, err := gorm.G[native.StorageGroupORM](db).Where("id = ?", id).Delete(ctx)
	return err
}

// GetStorageProviderByID 根据 ID 获取存储提供者
func GetStorageProviderByID(id uint32) (*native.StorageProviderORM, error) {
	ctx := context.Background()
	provider, err := gorm.G[*native.StorageProviderORM](db).Where("id = ? AND deleted_at IS NULL", id).First(ctx)
	if err != nil {
		llog.GetLogger().Errorf("get storage provider by id failed, id: %d, err: %v", id, err)
		return nil, err
	}
	return provider, nil
}

// ListStorageProviders 获取存储提供者列表
func ListStorageProviders(offset, limit int, keyword string, providerType *native.StorageProviderType, groupID *uint32, enabledOnly bool, sortBy string, desc bool) ([]*native.StorageProviderORM, int64, error) {
	ctx := context.Background()

	query := gorm.G[*native.StorageProviderORM](db).Where("deleted_at IS NULL")

	if keyword != "" {
		query = query.Where("(name LIKE ? OR description LIKE ?)", "%"+keyword+"%", "%"+keyword+"%")
	}

	if providerType != nil {
		query = query.Where("provider_type = ?", *providerType)
	}

	if groupID != nil && *groupID > 0 {
		query = query.Where("group_id = ?", *groupID)
	}

	if enabledOnly {
		query = query.Where("enabled = ?", true)
	}

	// Count total
	total, err := query.Count(ctx, "*")
	if err != nil {
		return nil, 0, err
	}

	// Order
	order := "created_at ASC"
	if sortBy != "" {
		order = sortBy + " ASC"
		if desc {
			order = sortBy + " DESC"
		}
	}

	// Find with pagination
	providers, err := query.Order(order).Offset(offset).Limit(limit).Find(ctx)
	if err != nil {
		llog.GetLogger().Errorf("list storage providers failed, err: %v", err)
		return nil, 0, err
	}

	return providers, total, nil
}

// CreateStorageProvider 创建存储提供者
func CreateStorageProvider(provider *native.StorageProviderORM) error {
	ctx := context.Background()
	err := gorm.G[native.StorageProviderORM](db).Create(ctx, provider)
	return err
}

// UpdateStorageProvider 更新存储提供者
func UpdateStorageProvider(provider *native.StorageProviderORM) error {
	ctx := context.Background()
	_, err := gorm.G[native.StorageProviderORM](db).Updates(ctx, *provider)
	return err
}

// DeleteStorageProvider 删除存储提供者（软删除）
func DeleteStorageProvider(id uint32) error {
	ctx := context.Background()
	_, err := gorm.G[native.StorageProviderORM](db).Where("id=?", id).Delete(ctx)
	return err
}
