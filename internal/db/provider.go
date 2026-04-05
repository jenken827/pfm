package db

import (
	"context"

	"jenken827.com/jenken827/pfm/proto/gen/native"
	"gorm.io/gorm"
)

// CreateStorageProvider 创建存储提供者
func CreateStorageProvider(provider *native.StorageProviderORM) error {
	ctx := context.Background()
	return gorm.G[native.StorageProviderORM](db).Create(ctx, provider)
}

// UpdateStorageProvider 更新存储提供者
func UpdateStorageProvider(provider *native.StorageProviderORM) error {
	ctx := context.Background()
	id := *provider.Id
	fields := []string{}

	if provider.DefaultBound != nil {
		fields = append(fields, "default_bound")
	}
	if provider.Description != nil {
		fields = append(fields, "description")
	}
	if provider.Enabled != nil {
		fields = append(fields, "enabled")
	}
	if provider.FileSizeLimit != nil {
		fields = append(fields, "file_size_limit")
	}
	if provider.Name != nil {
		fields = append(fields, "name")
	}
	if provider.Order != nil {
		fields = append(fields, "order")
	}
	if provider.TotalCapacity != nil {
		fields = append(fields, "total_capacity")
	}
	if provider.Proxy != nil {
		fields = append(fields, "proxy")
	}

	// GORM 泛型 API 的 Select 签名为 Select(query string, args ...interface{})
	// 需要正确展开字段列表
	if len(fields) == 0 {
		_, err := gorm.G[*native.StorageProviderORM](db).Where("id=?", id).Updates(ctx, provider)
		return err
	}
	// 将剩余字段转换为 []interface{}
	args := make([]any, len(fields)-1)
	for i, f := range fields[1:] {
		args[i] = f
	}
	_, err := gorm.G[*native.StorageProviderORM](db).Where("id=?", id).Select(fields[0], args...).Updates(ctx, provider)
	return err
}

// DeleteStorageProvider 删除存储提供者
func DeleteStorageProvider(ids []uint32) error {
	ctx := context.Background()
	_, err := gorm.G[native.StorageProviderORM](db).Where("id in ?", ids).Delete(ctx)
	return err
}

// GetEnabledProviders 获取启用的存储提供者
func GetEnabledProviders() ([]*native.StorageProviderORM, error) {
	ctx := context.Background()
	return gorm.G[*native.StorageProviderORM](db).Where("enabled = ?", true).Find(ctx)
}

// GetEnabledProvidersByIds 获取启用的存储提供者
func GetEnabledProvidersByIds(ids []uint32) ([]*native.StorageProviderORM, error) {
	ctx := context.Background()
	return gorm.G[*native.StorageProviderORM](db).Where("id in ? and enabled = ?", ids, true).Find(ctx)
}

// GetDefaultBoundEnabledProviders 获取默认绑定启用的存储提供者
func GetDefaultBoundEnabledProviders() ([]*native.StorageProviderORM, error) {
	ctx := context.Background()
	return gorm.G[*native.StorageProviderORM](db).Where("default_bound = ? and enabled = ?", true, true).Find(ctx)
}

// GetProviderByID 获取存储提供者
func GetProviderByID(id uint32) (*native.StorageProviderORM, error) {
	ctx := context.Background()
	provider, err := gorm.G[*native.StorageProviderORM](db).Where("id = ?", id).First(ctx)
	if err != nil {
		return nil, err
	}
	return provider, nil
}

// GetAllProviders 获取所有存储提供者
func GetAllProviders() ([]*native.StorageProviderORM, error) {
	ctx := context.Background()
	return gorm.G[*native.StorageProviderORM](db).Find(ctx)
}
