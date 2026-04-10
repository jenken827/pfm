package db

import (
	"context"

	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// GetFileLocations 获取文件位置记录
// func GetFileLocations(query *DBQuery) ([]*native.FileLocationORM, error) {
// 	ctx := context.Background()
// 	var locations []*native.FileLocationORM

// 	dbQuery := GetDB().WithContext(ctx)
// 	if query != nil {
// 		dbQuery = dbQuery.Where(query.Where, query.Args...)
// 	}

// 	if err := dbQuery.Find(&locations).Error; err != nil {
// 		llog.L().Errorf("get file locations failed: %v", err)
// 		return nil, err
// 	}

// 	return locations, nil
// }

// CreateFileLocation 创建文件位置记录
func CreateFileLocation(fl *native.FileLocationORM) error {
	ctx := context.Background()
	if err := GetDB().WithContext(ctx).Create(fl).Error; err != nil {
		llog.L().Errorf("create file location failed: %v", err)
		return err
	}
	return nil
}

// UpdateFileLocation 更新文件位置记录
func UpdateFileLocation(fl *native.FileLocationORM, updateFields []string) error {
	ctx := context.Background()
	if err := GetDB().WithContext(ctx).Model(fl).Select(updateFields).Updates(fl).Error; err != nil {
		llog.L().Errorf("update file location failed: %v", err)
		return err
	}
	return nil
}

// UpdateFileLocationCacheStatus 更新文件位置的缓存状态
func UpdateFileLocationCacheStatus(id uint64, status native.CacheStatus) error {
	ctx := context.Background()
	if err := GetDB().WithContext(ctx).Model(&native.FileLocationORM{}).
		Where("id = ?", id).
		Update("cache_status", status).Error; err != nil {
		llog.L().Errorf("update file location cache status failed: %v", err)
		return err
	}
	return nil
}

// UpdateFileLocationCache 更新文件位置的缓存路径和状态
func UpdateFileLocationCache(id uint64, path string, status native.CacheStatus) error {
	ctx := context.Background()
	if err := GetDB().WithContext(ctx).Model(&native.FileLocationORM{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"path":         path,
			"cache_status": status,
		}).Error; err != nil {
		llog.L().Errorf("update file location cache failed: %v", err)
		return err
	}
	return nil
}

// GetFileLocationByProviderAndHash 根据提供者ID和文件哈希获取文件位置
func GetFileLocationByProviderAndHash(providerId uint32, blake3Hash string) (*native.FileLocationORM, error) {
	ctx := context.Background()
	var fl native.FileLocationORM
	if err := GetDB().WithContext(ctx).Where("provider_id = ? AND blake3_hash = ?", providerId, blake3Hash).First(&fl).Error; err != nil {
		return nil, err
	}
	return &fl, nil
}

// GetFileLocationByID 根据ID获取文件位置
func GetFileLocationByID(id uint64) (*native.FileLocationORM, error) {

	ctx := context.Background()

	var fl native.FileLocationORM

	if err := GetDB().WithContext(ctx).Where("id = ?", id).First(&fl).Error; err != nil {

		return nil, err

	}

	return &fl, nil

}

// GetFileLocationsByBlake3Hash 根据blake3哈希获取文件位置
func GetFileLocationsByBlake3Hash(hash string) ([]*native.FileLocationORM, error) {
	ctx := context.Background()
	var locations []*native.FileLocationORM

	if err := GetDB().WithContext(ctx).Where("blake3_hash = ?", hash).Find(&locations).Error; err != nil {
		return nil, err
	}

	return locations, nil
}

// GetFileLocationsByProviderAndHash 根据提供者ID和blake3哈希获取文件位置
func GetFileLocationsByProviderAndHash(providerIDs []uint32, blake3Hash string) ([]*native.FileLocationORM, error) {
	ctx := context.Background()
	var locations []*native.FileLocationORM

	if err := GetDB().WithContext(ctx).Where("provider_id IN ? AND blake3_hash = ?", providerIDs, blake3Hash).Find(&locations).Error; err != nil {
		return nil, err
	}

	return locations, nil
}

// GetFileHistoriesByFileID 根据文件ID获取文件历史
func GetFileHistoriesByFileID(fileID string) ([]*native.FileHistoryORM, error) {
	ctx := context.Background()
	var histories []*native.FileHistoryORM

	if err := GetDB().WithContext(ctx).Where("file_id = ?", fileID).Order("version_number DESC").Find(&histories).Error; err != nil {
		return nil, err
	}

	return histories, nil
}
