package db

import (
	"context"

	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"gorm.io/gorm"
)

// GetUploadRecordByID 获取上传记录ByID
func GetUploadRecordByID(id uint32, withChunk bool) (*native.UploadRecordORM, []*native.UploadChunkRecordORM, error) {
	return getUploadRecord("id = ?", withChunk, id)
}

// GetUploadRecordByHash 获取上传记录
func GetUploadRecordByHash(hash string, withChunk bool) (*native.UploadRecordORM, []*native.UploadChunkRecordORM, error) {
	return getUploadRecord("file_hash = ?", withChunk, hash)
}

// GetUploadRecordsByStatus 获取上传记录
func GetUploadRecordsByStatus(status []native.LocationStatus) ([]*native.UploadRecordORM, error) {
	ints := []int32{}
	for _, s := range status {

		ints = append(ints, int32(s))

	}

	return gorm.G[*native.UploadRecordORM](db).Where("status in ?", ints).Find(context.Background())

}

// GetChunkRecord 获取分片记录
func GetChunkRecord(uploadID uint32, chunkIndex uint32) (*native.UploadChunkRecordORM, error) {
	ctx := context.Background()
	chunk, err := gorm.G[*native.UploadChunkRecordORM](db).
		Where("upload_id = ? AND `index` = ?", uploadID, chunkIndex).
		First(ctx)
	if err != nil {
		return nil, err
	}
	return chunk, nil
}

func getUploadRecord(expression string, withChunk bool, args ...any) (*native.UploadRecordORM, []*native.UploadChunkRecordORM, error) {
	record, err := gorm.G[*native.UploadRecordORM](db).Where(expression, args...).First(context.Background())
	if err != nil {
		llog.L().Errorf("query upload record failed: %v", err)
		return nil, nil, err
	}
	if withChunk {
		chunks, err := gorm.G[*native.UploadChunkRecordORM](db).Where("upload_id = ?", *record.Id).Find(context.Background())
		if err != nil {
			llog.L().Warnf("get uploading chunks failed, uploadID: %d, err: %v", *record.Id, err)
			return record, nil, err
		}

		return record, chunks, nil
	}
	return record, nil, nil
}

// CreateUploadRecord 创建上传记录
func CreateUploadRecord(record *native.UploadRecordORM) error {
	return db.Create(record).Error
}

// DelUploadRecord 删除上传记录
// func DelUploadRecord(id uint32) error {
// 	ctx := context.Background()
// 	err := db.Transaction(func(tx *gorm.DB) error {
// 		if _, err := gorm.G[*native.UploadRecordORM](tx).Where("id = ?", id).Delete(ctx); err != nil {
// 			llog.L().Errorf("delete upload record failed, id: %d, err: %v", id, err)
// 			return err
// 		}
// 		if _, err := gorm.G[*native.UploadChunkRecordORM](tx).Where("upload_id = ?", id).Delete(ctx); err != nil {
// 			llog.L().Errorf("delete upload chunk record failed, id: %d, err: %v", id, err)
// 			return err
// 		}
// 		return nil
// 	})
// 	return err
// }

// UpdateUploadRecord 更新上传记录
func UpdateUploadRecord(r *native.UploadRecordORM, updateFields []string) error {
	ctx := context.Background()
	// GORM 泛型 API 的 Select 签名为 Select(query string, args ...interface{})
	// 需要正确展开字段列表
	if len(updateFields) == 0 {
		if _, err := gorm.G[*native.UploadRecordORM](db).Where("id = ?", *r.Id).Updates(ctx, r); err != nil {
			llog.L().Errorf("update upload record failed: %v", err)
			return err
		}
		return nil
	}
	// 将剩余字段转换为 []interface{}
	args := make([]any, len(updateFields)-1)
	for i, f := range updateFields[1:] {
		args[i] = f
	}
	if _, err := gorm.G[*native.UploadRecordORM](db).Where("id = ?", *r.Id).Select(updateFields[0], args...).Updates(ctx, r); err != nil {
		llog.L().Errorf("update upload record failed: %v", err)
		return err
	}
	return nil
}

// ClearUploadRecords 清理上传记录
func ClearUploadRecords(id uint32) error {
	ctx := context.Background()
	return db.Transaction(func(tx *gorm.DB) error {
		if _, err := gorm.G[*native.UploadRecordORM](tx).Where("id = ?", id).Delete(ctx); err != nil {
			llog.L().Errorf("delete upload record failed, id: %d, err: %v", id, err)
			return err
		}
		if _, err := gorm.G[*native.UploadChunkRecordORM](tx).Where("upload_id = ?", id).Delete(ctx); err != nil {
			llog.L().Errorf("delete upload chunk record failed, id: %d, err: %v", id, err)
			return err
		}
		return nil
	})
}

// ListUploadRecords 分页查询上传记录
func ListUploadRecords(page, pageSize int32, status *int32) ([]*native.UploadRecordORM, int64, error) {
	var total int64

	// 构建基础查询
	query := db.Model(&native.UploadRecordORM{})
	if status != nil {
		query = query.Where("status = ?", *status)
	}

	// 统计总数
	if err := query.Count(&total).Error; err != nil {
		llog.L().Errorf("count upload records failed: %v", err)
		return nil, 0, err
	}

	// 分页查询
	var records []*native.UploadRecordORM
	offset := (page - 1) * pageSize
	if err := db.Model(&native.UploadRecordORM{}).
		Scopes(func(d *gorm.DB) *gorm.DB {
			if status != nil {
				return d.Where("status = ?", *status)
			}
			return d
		}).
		Order("created_at DESC").
		Limit(int(pageSize)).
		Offset(int(offset)).
		Find(&records).Error; err != nil {
		llog.L().Errorf("query upload records failed: %v", err)
		return nil, 0, err
	}

	return records, total, nil
}

// GetFileLocationsByFileHash 根据文件哈希查询文件位置记录
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
