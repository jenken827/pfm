package db

import (
	"context"

	"jenken827.com/jenken827/pfm/proto/gen/native"
	"gorm.io/gorm"
)

// FilterProviderCreate 过滤掉创建时不可以设置的字段
func FilterProviderCreate(provider *native.StorageProviderORM) {
	filterProvider(provider, false)
}

// 过滤掉不可以设置的字段
func filterProvider(provider *native.StorageProviderORM, update bool) {
	provider.Id = nil
	provider.CreatedAt = nil
	provider.UpdatedAt = nil
	provider.DeletedAt = nil
	provider.UsedCapacity = nil
	if update {
		provider.ProviderType = nil
		provider.Config = nil
		provider.EncryptFilename = nil
		provider.EncryptionKey = nil
		provider.EncryptionSuffix = nil
	}
}

// UpdateStorageProvider 更新存储提供者
func UpdateStorageProvider(provider *native.StorageProviderORM) error {
	ctx := context.Background()
	id := provider.Id
	filterProvider(provider, true)
	_, err := gorm.G[native.StorageProviderORM](db).Where("id=?", id).Updates(ctx, *provider)
	return err
}

// DeleteStorageProvider 删除存储提供者
func DeleteStorageProvider(ids []uint32) error {
	ctx := context.Background()
	_, err := gorm.G[native.StorageProviderORM](db).Where("id in ?", ids).Delete(ctx)
	return err
}
