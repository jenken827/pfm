package db

import (
	"context"

	"gorm.io/gorm"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
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
func GetUploadRecordsByStatus(status []native.UploadStatus) ([]*native.UploadRecordORM, error) {
	ints := []int32{}
	for _, s := range status {

		ints = append(ints, int32(s))

	}

	return gorm.G[*native.UploadRecordORM](db).Where("task_status in ?", ints).Find(context.Background())

}

// GetChunkRecord 获取分片记录
func GetChunkRecord(uploadID uint32, chunkIndex uint32) (*native.UploadChunkRecordORM, error) {
	ctx := context.Background()
	chunk, err := gorm.G[*native.UploadChunkRecordORM](db).
		Where(`upload_id = ? AND "index" = ?`, uploadID, chunkIndex).
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
	if len(updateFields) == 0 {
		if _, err := gorm.G[*native.UploadRecordORM](db).Where("id = ?", *r.Id).Updates(ctx, r); err != nil {
			llog.L().Errorf("update upload record failed: %v", err)
			return err
		}
		return nil
	}

	// 使用 map 进行更新，避免 GORM 的零值检查问题
	updateMap := make(map[string]interface{})
	for _, field := range updateFields {
		switch field {
		case "status":
			updateMap["status"] = *r.Status
		case "task_status":
			updateMap["task_status"] = *r.TaskStatus
		case "message":
			updateMap["message"] = *r.Message
		case "file_hash":
			updateMap["file_hash"] = *r.FileHash
		case "file_name":
			updateMap["file_name"] = *r.FileName
		case "file_size":
			updateMap["file_size"] = *r.FileSize
		case "parent_id":
			updateMap["parent_id"] = *r.ParentId
		case "temp_path":
			updateMap["temp_path"] = *r.TempPath
		case "overwrite_id":
			updateMap["overwrite_id"] = *r.OverwriteId
		case "tags":
			updateMap["tags"] = *r.Tags
		case "target_provider_ids":
			updateMap["target_provider_ids"] = *r.TargetProviderIds
		case "task_type":
			updateMap["task_type"] = *r.TaskType
		case "chunk_size":
			updateMap["chunk_size"] = *r.ChunkSize
		case "chunk_total":
			updateMap["chunk_total"] = *r.ChunkTotal
		}
	}

	result := db.WithContext(ctx).Model(&native.UploadRecordORM{}).Where("id = ?", *r.Id).Updates(updateMap)
	if result.Error != nil {
		llog.L().Errorf("update upload record failed: %v", result.Error)
		return result.Error
	}
	return nil
}

// TransitionUploadRecordTaskStatus 任务状态迁移（带乐观锁，操作 task_status 列）
// 仅当记录当前 task_status 在 prevStatuses 中时才更新，防止并发覆盖已完成状态
func TransitionUploadRecordTaskStatus(id uint32, prevStatuses []native.UploadStatus, newStatus native.UploadStatus, message *string) error {
	ctx := context.Background()
	updateMap := map[string]interface{}{"task_status": int32(newStatus)}
	if message != nil {
		updateMap["message"] = *message
	}
	prevInts := make([]int32, 0, len(prevStatuses))
	for _, s := range prevStatuses {
		prevInts = append(prevInts, int32(s))
	}
	if err := db.WithContext(ctx).Model(&native.UploadRecordORM{}).
		Where("id = ? AND task_status IN ?", id, prevInts).
		Updates(updateMap).Error; err != nil {
		llog.L().Errorf("transition upload record task status failed: %v, id: %d", err, id)
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

// CountUploadRecordsByStatus 按状态统计上传记录数（可按 owner 过滤，ownerID 空则全局）
func CountUploadRecordsByStatus(status int32, ownerID string) (int64, error) {
	var count int64
	query := db.Model(&native.UploadRecordORM{}).Where("task_status = ?", status)
	if ownerID != "" {
		query = query.Where("owner_id = ?", ownerID)
	}
	if err := query.Count(&count).Error; err != nil {
		llog.L().Errorf("count upload records by status failed: %v", err)
		return 0, err
	}
	return count, nil
}

// ListUploadRecords 分页查询上传记录（按 ownerID 过滤，ownerID 空则全局）
func ListUploadRecords(page, pageSize int32, status *int32, ownerID string) ([]*native.UploadRecordORM, int64, error) {
	var total int64

	// 构建基础查询
	query := db.Model(&native.UploadRecordORM{})
	if status != nil {
		query = query.Where("task_status = ?", *status)
	}
	if ownerID != "" {
		query = query.Where("owner_id = ?", ownerID)
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
				d = d.Where("task_status = ?", *status)
			}
			if ownerID != "" {
				d = d.Where("owner_id = ?", ownerID)
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
