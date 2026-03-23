package db

import (
	"context"

	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"gorm.io/gorm"
)

// GetUploadRecord 获取上传记录
func GetUploadRecord(expression string, withChunks bool, args ...any) (*native.UploadRecordORM, []*native.UploadChunkRecordORM, error) {
	record, err := gorm.G[*native.UploadRecordORM](db).Where(expression, args...).
		First(context.Background())
	if err != nil {
		llog.GetLogger().Errorf("query existing upload records failed: %v", err)
		return nil, nil, err
	}
	if withChunks {
		chunks, err := gorm.G[*native.UploadChunkRecordORM](db).Where("upload_id = ?", *record.Id).Find(context.Background())
		if err != nil {
			llog.GetLogger().Warnf("get uploading chunks failed, uploadID: %s, err: %v", *record.Id, err)
			return record, nil, err
		}
		return record, chunks, nil
	}
	return record, nil, nil
}

// DelUploadRecord 删除上传记录
func DelUploadRecord(id string) error {
	ctx := context.Background()
	err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := gorm.G[*native.UploadRecordORM](tx).Where("id = ?", id).Delete(ctx); err != nil {
			llog.GetLogger().Errorf("delete upload record failed, id: %s, err: %v", id, err)
			return err
		}
		if _, err := gorm.G[*native.UploadChunkRecordORM](tx).Where("upload_id = ?", id).Delete(ctx); err != nil {
			llog.GetLogger().Errorf("delete upload chunk record failed, id: %s, err: %v", id, err)
			return err
		}
		return nil
	})
	return err
}
