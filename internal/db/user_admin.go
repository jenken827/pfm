package db

import (
	"context"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"gorm.io/gorm"
)

// IsAdmin 检查用户是否为管理员。
//
// 数据源：users.is_admin（原 admin_users 表已移除，管理员标记收敛到用户表）。
// 用户不存在时返回 false。
func IsAdmin(userID string) bool {
	user, err := gorm.G[*entity.UserORM](db).
		Where("user_id = ?", userID).
		First(context.Background())
	if err != nil {
		return false
	}
	return user.IsAdmin != nil && *user.IsAdmin
}

// SetAdmin 设置/取消用户的管理员标记
// 返回 gorm.ErrRecordNotFound 表示用户不存在
func SetAdmin(userID string, isAdmin bool) error {
	result := db.Model(&entity.UserORM{}).
		Where("user_id = ?", userID).
		Update("is_admin", isAdmin)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// UpdateUserProfile 更新用户资料（用户名 / 邮箱）
// 调用方需自行保证用户名唯一性（冲突时由唯一索引报错）。
// 返回 gorm.ErrRecordNotFound 表示用户不存在
func UpdateUserProfile(userID, username, email string) error {
	result := db.Model(&entity.UserORM{}).
		Where("user_id = ?", userID).
		Updates(map[string]any{
			"username": username,
			"email":    email,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// UpdateUserAvatar 更新用户头像 URL
func UpdateUserAvatar(userID, avatarURL string) error {
	result := db.Model(&entity.UserORM{}).
		Where("user_id = ?", userID).
		Update("avatar", avatarURL)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// IsUsernameTaken 检查用户名是否已被其他用户占用
// excludeUserID 用于更新场景（排除自己）
func IsUsernameTaken(username, excludeUserID string) (bool, error) {
	var count int64
	err := db.Model(&entity.UserORM{}).
		Where("username = ? AND user_id != ?", username, excludeUserID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
