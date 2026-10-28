package db

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/pkg/llog"
)

// CreateDownloadRecord 创建下载记录
func CreateDownloadRecord(dr *entity.DownloadRecordORM) error {
	ctx := context.Background()
	if err := GetDB().WithContext(ctx).Create(dr).Error; err != nil {
		llog.L().Errorf("create download record failed: %v", err)
		return err
	}
	return nil
}

// GetDownloadRecordByGid 根据gid获取下载记录
func GetDownloadRecordByGid(gid string) (*entity.DownloadRecordORM, error) {
	ctx := context.Background()
	var dr entity.DownloadRecordORM
	if err := GetDB().WithContext(ctx).Where("gid = ?", gid).First(&dr).Error; err != nil {
		llog.L().Errorf("get download record by gid failed, gid: %s, err: %v", gid, err)
		return nil, err
	}
	return &dr, nil
}

// FindDownloadRecordByGid 按 gid 查记录；查不到返回 (nil, nil) 且不打错误日志
//
// 仅供「顺手置状态」这类可忽略场景使用（如 下载器通知了非 pfm 发起的任务）；
// 正常业务流程仍用 GetDownloadRecordByGid（查不到要当异常处理）。
func FindDownloadRecordByGid(gid string) (*entity.DownloadRecordORM, error) {
	ctx := context.Background()
	var dr entity.DownloadRecordORM
	if err := GetDB().WithContext(ctx).Where("gid = ?", gid).First(&dr).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		llog.L().Errorf("find download record by gid failed, gid: %s, err: %v", gid, err)
		return nil, err
	}
	return &dr, nil
}

// GetDownloadRecordByID 根据 ID 获取下载记录
func GetDownloadRecordByID(id uint32) (*entity.DownloadRecordORM, error) {
	ctx := context.Background()
	var dr entity.DownloadRecordORM
	if err := GetDB().WithContext(ctx).Where("id = ?", id).First(&dr).Error; err != nil {
		llog.L().Errorf("get download record by id failed, id: %d, err: %v", id, err)
		return nil, err
	}
	return &dr, nil
}

// GetDownloadRecordByUploadID 根据uploadId获取下载记录
func GetDownloadRecordByUploadID(uploadID uint32) (*entity.DownloadRecordORM, error) {
	ctx := context.Background()
	var dr entity.DownloadRecordORM
	if err := GetDB().WithContext(ctx).Where("upload_record_id = ?", uploadID).First(&dr).Error; err != nil {
		llog.L().Errorf("get download record by upload id failed, upload id: %d, err: %v", uploadID, err)
		return nil, err
	}
	return &dr, nil
}

// ListOfflineDownloadsByOwner 分页列出指定用户的 URL 离线下载任务（按 id 倒序）
func ListOfflineDownloadsByOwner(ownerID string, offset, limit int) ([]*entity.DownloadRecordORM, int64, error) {
	ctx := context.Background()
	var drs []*entity.DownloadRecordORM
	q := GetDB().WithContext(ctx).Where("owner_id = ?", ownerID).Order("id DESC")

	var total int64
	if err := q.Model(&entity.DownloadRecordORM{}).Count(&total).Error; err != nil {
		llog.L().Errorf("count download records failed, owner: %s, err: %v", ownerID, err)
		return nil, 0, err
	}
	if err := q.Offset(offset).Limit(limit).Find(&drs).Error; err != nil {
		llog.L().Errorf("list download records failed, owner: %s, err: %v", ownerID, err)
		return nil, 0, err
	}
	return drs, total, nil
}

// ListUnfinishedDownloadRecords 列出未终态的下载记录（PENDING / DOWNLOADING）
//
// 用于启动时与下载器真实状态对齐（见 internal/download/reconcile.go）：
// 完成通知只推给当时连着的客户端，停机期间完成或 下载器重启换 gid 都会漏。
func ListUnfinishedDownloadRecords() ([]*entity.DownloadRecordORM, error) {
	var drs []*entity.DownloadRecordORM
	if err := GetDB().
		Where("state IN (?, ?)",
			int32(native.DownloadRecordState_DOWNLOAD_PENDING),
			int32(native.DownloadRecordState_DOWNLOADING)).
		Find(&drs).Error; err != nil {
		llog.L().Errorf("list unfinished download records failed: %v", err)
		return nil, err
	}
	return drs, nil
}

// UpdateDownloadRecord 更新下载记录
func UpdateDownloadRecord(dr *entity.DownloadRecordORM, updateFields []string) error {
	ctx := context.Background()

	// 使用 map 进行更新，避免 GORM 的零值检查问题
	updateMap := make(map[string]interface{})
	for _, field := range updateFields {
		switch field {
		case "state":
			if dr.State != nil {
				updateMap["state"] = *dr.State
			}
		case "error_message":
			if dr.ErrorMessage != nil {
				updateMap["error_message"] = *dr.ErrorMessage
			}
		case "file_path":
			if dr.FilePath != nil {
				updateMap["file_path"] = *dr.FilePath
			}
		case "file_size":
			if dr.FileSize != nil {
				updateMap["file_size"] = *dr.FileSize
			}
		case "gid":
			if dr.Gid != nil {
				updateMap["gid"] = *dr.Gid
			}
		case "blake3_hash":
			if dr.Blake3Hash != nil {
				updateMap["blake3_hash"] = *dr.Blake3Hash
			}
		case "source_location_id":
			if dr.SourceLocationId != nil {
				updateMap["source_location_id"] = *dr.SourceLocationId
			}
		case "upload_record_id":
			if dr.UploadRecordId != nil {
				updateMap["upload_record_id"] = *dr.UploadRecordId
			}
		case "callback_info":
			if dr.CallbackInfo != nil {
				updateMap["callback_info"] = *dr.CallbackInfo
			}
		}
	}

	if len(updateMap) == 0 {
		return nil
	}
	if dr.Id == nil {
		return nil
	}
	if err := GetDB().WithContext(ctx).Model(&entity.DownloadRecordORM{}).Where("id = ?", *dr.Id).Updates(updateMap).Error; err != nil {
		llog.L().Errorf("update download record failed: %v", err)
		return err
	}
	return nil
}

// DeleteDownloadRecord 删除下载记录
func DeleteDownloadRecord(id uint32) error {
	ctx := context.Background()
	if err := GetDB().WithContext(ctx).Delete(&entity.DownloadRecordORM{}, id).Error; err != nil {
		llog.L().Errorf("delete download record failed, id: %d, err: %v", id, err)
		return err
	}
	return nil
}
