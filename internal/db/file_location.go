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

	// 使用 map 进行更新，避免 GORM 的零值检查问题
	updateMap := make(map[string]interface{})
	for _, field := range updateFields {
		switch field {
		case "status":
			if fl.Status != nil {
				updateMap["status"] = *fl.Status
			}
		case "message":
			if fl.Message != nil {
				updateMap["message"] = *fl.Message
			}
		case "progress":
			if fl.Progress != nil {
				updateMap["progress"] = *fl.Progress
			}
		case "provider_file_id":
			if fl.ProviderFileId != nil {
				updateMap["provider_file_id"] = *fl.ProviderFileId
			}
		case "path":
			if fl.Path != nil {
				updateMap["path"] = *fl.Path
			}
		case "size":
			if fl.Size != nil {
				updateMap["size"] = *fl.Size
			}
		case "hash":
			if fl.Hash != nil {
				updateMap["hash"] = *fl.Hash
			}
		case "blake3_hash":
			if fl.Blake3Hash != nil {
				updateMap["blake3_hash"] = *fl.Blake3Hash
			}
		case "provider_id":
			if fl.ProviderId != nil {
				updateMap["provider_id"] = *fl.ProviderId
			}
		case "channel_id":
			if fl.ChannelId != nil {
				updateMap["channel_id"] = *fl.ChannelId
			}
		case "chunk_number":
			if fl.ChunkNumber != nil {
				updateMap["chunk_number"] = *fl.ChunkNumber
			}
		case "chunk_size":
			if fl.ChunkSize != nil {
				updateMap["chunk_size"] = *fl.ChunkSize
			}
		case "chunk_total":
			if fl.ChunkTotal != nil {
				updateMap["chunk_total"] = *fl.ChunkTotal
			}
		case "chunk_hash":
			if fl.ChunkHash != nil {
				updateMap["chunk_hash"] = *fl.ChunkHash
			}
		case "encrypted":
			if fl.Encrypted != nil {
				updateMap["encrypted"] = *fl.Encrypted
			}
		case "encryption_salt":
			if fl.EncryptionSalt != nil {
				updateMap["encryption_salt"] = *fl.EncryptionSalt
			}
		case "cache_status":
			if fl.CacheStatus != nil {
				updateMap["cache_status"] = *fl.CacheStatus
			}
		case "expired_at":
			if fl.ExpiredAt != nil {
				updateMap["expired_at"] = *fl.ExpiredAt
			}
		case "message_id":
			if fl.MessageId != nil {
				updateMap["message_id"] = *fl.MessageId
			}
		case "s3_key_flag":
			if fl.S3KeyFlag != nil {
				updateMap["s3_key_flag"] = *fl.S3KeyFlag
			}
		}
	}

	if err := GetDB().WithContext(ctx).Model(&native.FileLocationORM{}).Where("id = ?", *fl.Id).Updates(updateMap).Error; err != nil {
		llog.L().Errorf("update file location failed: %v", err)
		return err
	}
	return nil
}

// TransitionFileLocationStatus 状态迁移（带乐观锁）
// 仅当记录当前 status 在 prevStatuses 中时才更新，防止并发覆盖已完成状态
func TransitionFileLocationStatus(id uint64, prevStatuses []native.LocationStatus, newStatus native.LocationStatus, message *string) error {
	ctx := context.Background()
	updateMap := map[string]interface{}{"status": int32(newStatus)}
	if message != nil {
		updateMap["message"] = *message
	}
	prevInts := make([]int32, 0, len(prevStatuses))
	for _, s := range prevStatuses {
		prevInts = append(prevInts, int32(s))
	}
	if err := GetDB().WithContext(ctx).Model(&native.FileLocationORM{}).
		Where("id = ? AND status IN ?", id, prevInts).
		Updates(updateMap).Error; err != nil {
		llog.L().Errorf("transition file location status failed: %v, id: %d", err, id)
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

	if err := GetDB().WithContext(ctx).Where("blake3_hash = ?", hash).Order("chunk_number ASC").Find(&locations).Error; err != nil {
		return nil, err
	}

	return locations, nil
}

// GetFileLocationsByBlake3Hashes 批量按 blake3 哈希列表获取文件位置
// 用于避免逐文件查询的 N+1（如批量移动/删除时检查各 provider 副本存在性）
func GetFileLocationsByBlake3Hashes(hashes []string) ([]*native.FileLocationORM, error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	var locations []*native.FileLocationORM
	if err := GetDB().Where("blake3_hash IN ?", hashes).
		Order("blake3_hash ASC, chunk_number ASC").Find(&locations).Error; err != nil {
		return nil, err
	}
	return locations, nil
}

// GetFileLocationsByProviderAndHash 根据提供者ID和blake3哈希获取文件位置
func GetFileLocationsByProviderAndHash(providerIDs []uint32, blake3Hash string) ([]*native.FileLocationORM, error) {
	ctx := context.Background()
	var locations []*native.FileLocationORM

	if err := GetDB().WithContext(ctx).Where("provider_id IN ? AND blake3_hash = ?", providerIDs, blake3Hash).Order("chunk_number ASC").Find(&locations).Error; err != nil {
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
	location, err := gorm.G[*native.FileLocationORM](db).Where("blake3_hash = ? AND provider_id = ?", fileHash, providerID).Order("chunk_number ASC").First(ctx)
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
