package db

import (
	"context"

	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"gorm.io/gorm"
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
func GetFileLocationByProviderAndHash(providerID uint32, blake3Hash string) (*native.FileLocationORM, error) {
	ctx := context.Background()
	var fl native.FileLocationORM
	if err := GetDB().WithContext(ctx).Where("provider_id = ? AND blake3_hash = ?", providerID, blake3Hash).First(&fl).Error; err != nil {
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

// GetFileLocationsByFileHash 根据文件哈希查询文件位置记录（按 provider_id 排序）
func GetFileLocationsByFileHash(fileHash string) ([]*native.FileLocationORM, error) {
	ctx := context.Background()
	locations, err := gorm.G[*native.FileLocationORM](db).Where("blake3_hash = ?", fileHash).Order("provider_id").Find(ctx)
	if err != nil {
		llog.L().Errorf("query file locations failed: %v", err)
		return nil, err
	}
	return locations, nil
}

// GetFileLocationByFileHashAndProvider 根据文件哈希和提供者查询
func GetFileLocationByFileHashAndProvider(fileHash string, providerID uint32) (*native.FileLocationORM, error) {
	ctx := context.Background()
	location, err := gorm.G[*native.FileLocationORM](db).Where("blake3_hash = ? AND provider_id = ?", fileHash, providerID).First(ctx)
	if err != nil {
		llog.L().Errorf("query file location failed: %v", err)
		return nil, err
	}
	return location, nil
}

// DeleteFileLocation 删除文件位置记录
func DeleteFileLocation(id uint64) error {
	ctx := context.Background()
	_, err := gorm.G[*native.FileLocationORM](db).Where("id = ?", id).Delete(ctx)
	if err != nil {
		llog.L().Errorf("delete file location failed, id: %d, err: %v", id, err)
		return err
	}
	return nil
}

// DeleteFileLocations 批量删除文件位置记录
func DeleteFileLocations(ids []uint64) error {
	if len(ids) == 0 {
		return nil
	}
	ctx := context.Background()
	_, err := gorm.G[*native.FileLocationORM](db).Where("id IN ?", ids).Delete(ctx)
	if err != nil {
		llog.L().Errorf("batch delete file locations failed, err: %v", err)
		return err
	}
	return nil
}
