package db

import (
	"context"

	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// CreateDownloadRecord 创建下载记录
func CreateDownloadRecord(dr *native.DownloadRecordORM) error {
	ctx := context.Background()
	if err := GetDB().WithContext(ctx).Create(dr).Error; err != nil {
		llog.L().Errorf("create download record failed: %v", err)
		return err
	}
	return nil
}

// GetDownloadRecordByGid 根据gid获取下载记录
func GetDownloadRecordByGid(gid string) (*native.DownloadRecordORM, error) {
	ctx := context.Background()
	var dr native.DownloadRecordORM
	if err := GetDB().WithContext(ctx).Where("gid = ?", gid).First(&dr).Error; err != nil {
		llog.L().Errorf("get download record by gid failed, gid: %s, err: %v", gid, err)
		return nil, err
	}
	return &dr, nil
}

// GetDownloadRecordByUploadID 根据uploadId获取下载记录
func GetDownloadRecordByUploadID(uploadID uint32) (*native.DownloadRecordORM, error) {

	ctx := context.Background()

	var dr native.DownloadRecordORM

	if err := GetDB().WithContext(ctx).Where("upload_record_id = ?", uploadID).First(&dr).Error; err != nil {

		llog.L().Errorf("get download record by upload id failed, upload id: %d, err: %v", uploadID, err)

		return nil, err

	}

	return &dr, nil

}

// UpdateDownloadRecord 更新下载记录
func UpdateDownloadRecord(dr *native.DownloadRecordORM, updateFields []string) error {
	ctx := context.Background()

	// 使用 map 进行更新，避免 GORM 的零值检查问题
	updateMap := make(map[string]interface{})
	for _, field := range updateFields {
		switch field {
		case "state":
			updateMap["state"] = *dr.State
		case "error_message":
			updateMap["error_message"] = *dr.ErrorMessage
		case "file_path":
			updateMap["file_path"] = *dr.FilePath
		case "file_size":
			updateMap["file_size"] = *dr.FileSize
		case "gid":
			updateMap["gid"] = *dr.Gid
		case "blake3_hash":
			updateMap["blake3_hash"] = *dr.Blake3Hash
		case "source_location_id":
			updateMap["source_location_id"] = *dr.SourceLocationId
		case "upload_record_id":
			updateMap["upload_record_id"] = *dr.UploadRecordId
		case "callback_info":
			updateMap["callback_info"] = *dr.CallbackInfo
		}
	}

	if err := GetDB().WithContext(ctx).Model(&native.DownloadRecordORM{}).Where("id = ?", *dr.Id).Updates(updateMap).Error; err != nil {
		llog.L().Errorf("update download record failed: %v", err)
		return err
	}
	return nil
}

// GetPendingDownloadRecords 获取等待下载的记录
// func GetPendingDownloadRecords() ([]*native.DownloadRecordORM, error) {
// 	ctx := context.Background()
// 	var drs []*native.DownloadRecordORM
// 	if err := GetDB().WithContext(ctx).Where("state = ?", native.DownloadRecordState_DOWNLOAD_PENDING).Find(&drs).Error; err != nil {
// 		llog.L().Errorf("get pending download records failed: %v", err)
// 		return nil, err
// 	}
// 	return drs, nil
// }

// GetDownloadingDownloadRecords 获取正在下载的记录
// func GetDownloadingDownloadRecords() ([]*native.DownloadRecordORM, error) {
// 	ctx := context.Background()
// 	var drs []*native.DownloadRecordORM
// 	if err := GetDB().WithContext(ctx).Where("state = ?", native.DownloadRecordState_DOWNLOADING).Find(&drs).Error; err != nil {
// 		llog.L().Errorf("get downloading download records failed: %v", err)
// 		return nil, err
// 	}
// 	return drs, nil
// }

// DeleteDownloadRecord 删除下载记录
// func DeleteDownloadRecord(id uint32) error {
// 	ctx := context.Background()
// 	if err := GetDB().WithContext(ctx).Delete(&native.DownloadRecordORM{}, id).Error; err != nil {
// 		llog.L().Errorf("delete download record failed, id: %d, err: %v", id, err)
// 		return err
// 	}
// 	return nil
// }
