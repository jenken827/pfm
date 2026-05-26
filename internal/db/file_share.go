package db

import (
	"context"

	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// CreateFileShare 创建分享记录
func CreateFileShare(fs *native.FileShareORM) error {
	ctx := context.Background()
	if err := GetDB().WithContext(ctx).Create(fs).Error; err != nil {
		llog.L().Errorf("create file share failed: %v", err)
		return err
	}
	return nil
}

// GetFileShareByCode 根据分享码获取分享记录
func GetFileShareByCode(shareCode string) (*native.FileShareORM, error) {
	ctx := context.Background()
	var fs native.FileShareORM
	if err := GetDB().WithContext(ctx).Where("share_code = ?", shareCode).First(&fs).Error; err != nil {
		return nil, err
	}
	return &fs, nil
}

// GetFileShareByID 根据ID获取分享记录
func GetFileShareByID(id uint64) (*native.FileShareORM, error) {
	ctx := context.Background()
	var fs native.FileShareORM
	if err := GetDB().WithContext(ctx).Where("id = ?", id).First(&fs).Error; err != nil {
		return nil, err
	}
	return &fs, nil
}

// GetFileSharesByFileID 根据文件ID获取所有分享记录
func GetFileSharesByFileID(fileID string) ([]*native.FileShareORM, error) {
	ctx := context.Background()
	var shares []*native.FileShareORM
	if err := GetDB().WithContext(ctx).Where("file_id = ?", fileID).Find(&shares).Error; err != nil {
		return nil, err
	}
	return shares, nil
}

// UpdateFileShare 更新分享记录
func UpdateFileShare(fs *native.FileShareORM, updateFields []string) error {
	ctx := context.Background()

	// 使用 map 进行更新，避免 GORM 的零值检查问题
	updateMap := make(map[string]interface{})
	for _, field := range updateFields {
		switch field {
		case "status":
			updateMap["status"] = *fs.Status
		case "expire_at":
			updateMap["expire_at"] = *fs.ExpireAt
		case "max_access_count":
			updateMap["max_access_count"] = *fs.MaxAccessCount
		case "never_expire":
			updateMap["never_expire"] = *fs.NeverExpire
		case "remark":
			updateMap["remark"] = *fs.Remark
		case "access_count":
			updateMap["access_count"] = *fs.AccessCount
		case "file_id":
			updateMap["file_id"] = *fs.FileId
		case "share_code":
			updateMap["share_code"] = *fs.ShareCode
		case "sign":
			updateMap["sign"] = *fs.Sign
		}
	}

	if err := GetDB().WithContext(ctx).Model(&native.FileShareORM{}).Where("id = ?", *fs.Id).Updates(updateMap).Error; err != nil {
		llog.L().Errorf("update file share failed: %v", err)
		return err
	}
	return nil
}

// IncrementShareAccessCount 增加分享访问次数
func IncrementShareAccessCount(id uint64) error {
	ctx := context.Background()
	if err := GetDB().WithContext(ctx).Model(&native.FileShareORM{}).
		Where("id = ?", id).
		UpdateColumn("access_count", GetDB().Raw("access_count + 1")).Error; err != nil {
		llog.L().Errorf("increment share access count failed: %v", err)
		return err
	}
	return nil
}

// CancelFileShare 取消分享
func CancelFileShare(id uint64) error {
	ctx := context.Background()
	status := int32(native.ShareStatus_CANCELLED)
	if err := GetDB().WithContext(ctx).Model(&native.FileShareORM{}).
		Where("id = ?", id).
		Update("status", status).Error; err != nil {
		llog.L().Errorf("cancel file share failed: %v", err)
		return err
	}
	return nil
}

// ExpireActiveShares 将所有已过期的活跃分享标记为过期
func ExpireActiveShares() error {
	ctx := context.Background()
	statusExpired := int32(native.ShareStatus_EXPIRED)
	statusActive := int32(native.ShareStatus_ACTIVE)
	if err := GetDB().WithContext(ctx).Model(&native.FileShareORM{}).
		Where("status = ? AND never_expire = ? AND expire_at < NOW()", statusActive, false).
		Update("status", statusExpired).Error; err != nil {
		llog.L().Errorf("expire active shares failed: %v", err)
		return err
	}
	return nil
}
