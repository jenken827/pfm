package db

import (
	"context"

	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"gorm.io/gorm"
)

// GetUploadRecords 获取上传记录
func GetUploadRecord(expression string, withChunks bool, args ...any) (*native.UploadRecordORM, []*native.UploadChunkRecordORM, error) {
	record, err := gorm.G[*native.UploadRecordORM](db).Where(expression, args...).Order("created_at asc").
		First(context.Background())
	if err != nil {
		llog.L().Errorf("query existing upload records failed: %v", err)
		return nil, nil, err
	}
	if withChunks {
		chunks, err := gorm.G[*native.UploadChunkRecordORM](db).Where("upload_id = ?", *record.Id).Find(context.Background())
		if err != nil {
			llog.L().Warnf("get uploading chunks failed, uploadID: %s, err: %v", *record.Id, err)
			return record, nil, err
		}
		return record, chunks, nil
	}
	return record, nil, nil
}

// GetUploadRecords 获取上传记录列表
func GetUploadRecords(expression string, args ...any) ([]*native.UploadRecordORM, error) {
	gdb := gorm.G[*native.UploadRecordORM](db)
	ctx := context.Background()
	if expression == "" {
		return gdb.Find(ctx)
	}
	return gdb.Where(expression, args...).Find(ctx)
}

// DelUploadRecord 删除上传记录
func DelUploadRecord(id uint32) error {
	ctx := context.Background()
	err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := gorm.G[*native.UploadRecordORM](tx).Where("id = ?", id).Delete(ctx); err != nil {
			llog.L().Errorf("delete upload record failed, id: %s, err: %v", id, err)
			return err
		}
		if _, err := gorm.G[*native.UploadChunkRecordORM](tx).Where("upload_id = ?", id).Delete(ctx); err != nil {
			llog.L().Errorf("delete upload chunk record failed, id: %s, err: %v", id, err)
			return err
		}
		return nil
	})
	return err
}

// CreateUploadRecord 创建上传记录
func CreateUploadRecord(r *native.UploadRecordORM) error {
	ctx := context.Background()
	if err := gorm.G[native.UploadRecordORM](db).Create(ctx, r); err != nil {
		llog.L().Errorf("create upload record failed: %v", err)
		return err
	}
	return nil
}

// ClearUploadRecords 清理上传记录
func ClearUploadRecords(ctx context.Context, id uint32) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if _, err := gorm.G[*native.UploadRecordORM](tx).Where("id = ?", id).Delete(ctx); err != nil {
			llog.L().Errorf("delete upload record failed, id: %s, err: %v", id, err)
			return err
		}
		if _, err := gorm.G[*native.UploadChunkRecordORM](tx).Where("upload_id = ?", id).Delete(ctx); err != nil {
			llog.L().Errorf("delete upload chunk record failed, id: %s, err: %v", id, err)
			return err
		}
		return nil
	})
}
