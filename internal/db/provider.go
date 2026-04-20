package db

import (
	"context"

	"jenken827.com/jenken827/pfm/internal/llog"
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
	if provider.Config != nil {
		fields = append(fields, "config")
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
	if provider.SortOrder != nil {
		fields = append(fields, "sort_order")
	}
	if provider.TotalCapacity != nil {
		fields = append(fields, "total_capacity")
	}
	if provider.Proxy != nil {
		fields = append(fields, "proxy")
	}

	if len(fields) == 0 {
		_, err := gorm.G[*native.StorageProviderORM](db).Where("id=?", id).Updates(ctx, provider)
		return err
	}
	query, args := selectFields(fields)
	_, err := gorm.G[*native.StorageProviderORM](db).Where("id=?", id).Select(query, args...).Updates(ctx, provider)
	return err
}

// DeleteStorageProvider 删除存储提供者
func DeleteStorageProvider(ids []uint32) error {
	ctx := context.Background()
	return db.Transaction(func(tx *gorm.DB) error {
		// 先删除关联的 FileLocation 记录，避免孤儿记录
		if _, err := gorm.G[*native.FileLocationORM](tx).Where("provider_id IN ?", ids).Delete(ctx); err != nil {
			llog.L().Errorf("delete file locations for providers failed, ids: %v, err: %v", ids, err)
			return err
		}
		// 再删除存储提供者
		if _, err := gorm.G[native.StorageProviderORM](tx).Where("id in ?", ids).Delete(ctx); err != nil {
			llog.L().Errorf("delete storage providers failed, ids: %v, err: %v", ids, err)
			return err
		}
		return nil
	})
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

// UpdateStorageProviderConfig 更新存储提供者的配置
func UpdateStorageProviderConfig(id uint32, config string) error {
	ctx := context.Background()
	// 使用原生 GORM 的 Update 方法，确保更新成功
	result := db.WithContext(ctx).Model(&native.StorageProviderORM{}).
		Where("id = ?", id).
		Update("config", config)

	if result.Error != nil {
		llog.L().Errorf("update storage provider config failed: id=%d, err=%v", id, result.Error)
		return result.Error
	}

	if result.RowsAffected == 0 {
		llog.L().Warnf("update storage provider config: no rows affected, id=%d may not exist", id)
	} else {
		llog.L().Infof("update storage provider config successful: id=%d, rows_affected=%d", id, result.RowsAffected)
	}

	return nil
}

// GetAvailableProvidersForSync 获取可用于同步的存储提供者
// 返回：已启用的提供者，排除文件已存在且完成的提供者
func GetAvailableProvidersForSync(fileHash string) ([]*native.StorageProviderORM, error) {
	ctx := context.Background()

	// Get all enabled providers
	enabledProviders, err := gorm.G[*native.StorageProviderORM](db).
		Where("enabled = ?", true).
		Order("sort_order ASC").
		Find(ctx)
	if err != nil {
		llog.L().Errorf("get enabled providers failed: %v", err)
		return nil, err
	}

	// Filter out providers where file already exists and is completed
	var available []*native.StorageProviderORM
	for _, provider := range enabledProviders {
		loc, err := gorm.G[*native.FileLocationORM](db).
			Where("blake3_hash = ? AND provider_id = ?", fileHash, *provider.Id).
			First(ctx)

		// If no location found, or location is not completed, include this provider
		if err != nil || loc == nil || *loc.Status != int32(native.LocationStatus_PCOMPLETED) {
			available = append(available, provider)
		}
	}

	return available, nil
}

// IncrementUsedCapacity 增加存储提供者的已使用容量
func IncrementUsedCapacity(providerID uint32, size uint64) error {
	ctx := context.Background()

	// 使用原子操作增加已使用容量
	result := db.WithContext(ctx).Model(&native.StorageProviderORM{}).
		Where("id = ?", providerID).
		Update("used_capacity", gorm.Expr("used_capacity + ?", size))

	if result.Error != nil {
		llog.L().Errorf("increment used capacity failed: provider_id=%d, size=%d, err=%v", providerID, size, result.Error)
		return result.Error
	}

	if result.RowsAffected == 0 {
		llog.L().Warnf("increment used capacity: no rows affected, provider_id=%d may not exist", providerID)
	} else {
		llog.L().Infof("increment used capacity successful: provider_id=%d, size=%d", providerID, size)
	}

	return nil
}

// DecrementUsedCapacity 减少存储提供者的已使用容量
func DecrementUsedCapacity(providerID uint32, size uint64) error {
	ctx := context.Background()

	// 使用原子操作减少已使用容量
	result := db.WithContext(ctx).Model(&native.StorageProviderORM{}).
		Where("id = ?", providerID).
		Update("used_capacity", gorm.Expr("used_capacity - ?", size))

	if result.Error != nil {
		llog.L().Errorf("decrement used capacity failed: provider_id=%d, size=%d, err=%v", providerID, size, result.Error)
		return result.Error
	}

	if result.RowsAffected == 0 {
		llog.L().Warnf("decrement used capacity: no rows affected, provider_id=%d may not exist", providerID)
	} else {
		llog.L().Infof("decrement used capacity successful: provider_id=%d, size=%d", providerID, size)
	}

	return nil
}

// GetStorageProvidersByIDs 批量查询存储提供者
func GetStorageProvidersByIDs(ids []uint32) ([]*native.StorageProviderORM, error) {
	if len(ids) == 0 {
		return []*native.StorageProviderORM{}, nil
	}

	ctx := context.Background()
	providers, err := gorm.G[*native.StorageProviderORM](db).Where("id IN ?", ids).Find(ctx)
	if err != nil {
		llog.L().Errorf("query storage providers failed: %v", err)
		return nil, err
	}
	return providers, nil
}
