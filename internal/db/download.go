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

// UpdateDownloadRecord 更新下载记录
func UpdateDownloadRecord(dr *native.DownloadRecordORM, updateFields []string) error {
	ctx := context.Background()
	if err := GetDB().WithContext(ctx).Model(dr).Select(updateFields).Updates(dr).Error; err != nil {
		llog.L().Errorf("update download record failed: %v", err)
		return err
	}
	return nil
}

// GetPendingDownloadRecords 获取等待下载的记录
func GetPendingDownloadRecords() ([]*native.DownloadRecordORM, error) {
	ctx := context.Background()
	var drs []*native.DownloadRecordORM
	if err := GetDB().WithContext(ctx).Where("state = ?", native.DownloadRecordState_DOWNLOAD_PENDING).Find(&drs).Error; err != nil {
		llog.L().Errorf("get pending download records failed: %v", err)
		return nil, err
	}
	return drs, nil
}

// GetDownloadingDownloadRecords 获取正在下载的记录
func GetDownloadingDownloadRecords() ([]*native.DownloadRecordORM, error) {
	ctx := context.Background()
	var drs []*native.DownloadRecordORM
	if err := GetDB().WithContext(ctx).Where("state = ?", native.DownloadRecordState_DOWNLOADING).Find(&drs).Error; err != nil {
		llog.L().Errorf("get downloading download records failed: %v", err)
		return nil, err
	}
	return drs, nil
}

// DeleteDownloadRecord 删除下载记录
func DeleteDownloadRecord(id uint32) error {
	ctx := context.Background()
	if err := GetDB().WithContext(ctx).Delete(&native.DownloadRecordORM{}, id).Error; err != nil {
		llog.L().Errorf("delete download record failed, id: %d, err: %v", id, err)
		return err
	}
	return nil
}
