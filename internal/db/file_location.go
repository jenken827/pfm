package db

import (
	"context"

	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// GetFileLocations 获取文件位置记录
func GetFileLocations(query *DBQuery) ([]*native.FileLocationORM, error) {
	ctx := context.Background()
	var locations []*native.FileLocationORM

	dbQuery := GetDB().WithContext(ctx)
	if query != nil {
		dbQuery = dbQuery.Where(query.Where, query.Args...)
	}

	if err := dbQuery.Find(&locations).Error; err != nil {
		llog.L().Errorf("get file locations failed: %v", err)
		return nil, err
	}

	return locations, nil
}

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

// GetFileLocationByProviderAndHash 根据提供者ID和文件哈希获取文件位置
func GetFileLocationByProviderAndHash(providerId uint32, blake3Hash string) (*native.FileLocationORM, error) {
	ctx := context.Background()
	var fl native.FileLocationORM
	if err := GetDB().WithContext(ctx).Where("provider_id = ? AND blake3_hash = ?", providerId, blake3Hash).First(&fl).Error; err != nil {
		return nil, err
	}
	return &fl, nil
}
