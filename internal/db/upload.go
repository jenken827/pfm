package db

import (
	"context"

	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"gorm.io/gorm"
)

// GetUploadRecords 获取上传记录
// func GetUploadRecords(expression string, withChunks bool, args ...any) (*native.UploadRecordORM, []*native.UploadChunkRecordORM, error) {
// 	record, err := gorm.G[*native.UploadRecordORM](db).Where(expression, args...).Order("created_at asc").
// 		First(context.Background())
// 	if err != nil {
// 		llog.L().Errorf("query existing upload records failed: %v", err)
// 		return nil, nil, err
// 	}
// 	if withChunks {
// 		chunks, err := gorm.G[*native.UploadChunkRecordORM](db).Where("upload_id = ?", *record.Id).Find(context.Background())
// 		if err != nil {
// 			llog.L().Warnf("get uploading chunks failed, uploadID: %d, err: %v", *record.Id, err)
// 			return record, nil, err
// 		}
// 		return record, chunks, nil
// 	}
// 	return record, nil, nil
// }

// GetUploadRecordByID 获取上传记录ByID
func GetUploadRecordByID(id uint32, withChunk bool) (*native.UploadRecordORM, []*native.UploadChunkRecordORM, error) {
	return getUploadRecord("id = ?", withChunk, id)
}

// GetUploadRecordByHash 获取上传记录
func GetUploadRecordByHash(hash string, withChunk bool) (*native.UploadRecordORM, []*native.UploadChunkRecordORM, error) {
	return getUploadRecord("hash = ?", withChunk, hash)
}

// GetUploadRecordsByStatus 获取上传记录
func GetUploadRecordsByStatus(status []native.UploadProviderStatus) ([]*native.UploadRecordORM, error) {
	ints := []int32{}
	for _, s := range status {

		ints = append(ints, int32(s))

	}

	return gorm.G[*native.UploadRecordORM](db).Where("status in ?", ints).Find(context.Background())

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
