package db

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"jenken827.com/jenken827/pfm/proto/gen/entity"
)

// GetUserByUsername 按用户名查询本地认证用户
// 返回 gorm.ErrRecordNotFound 表示用户不存在
func GetUserByUsername(username string) (*entity.UserORM, error) {
	user, err := gorm.G[*entity.UserORM](db).
		Where("username = ?", username).
		First(context.Background())
	if err != nil {
		return nil, err
	}
	return user, nil
}

// GetUserByID 按用户 ID 查询本地认证用户
func GetUserByID(userID string) (*entity.UserORM, error) {
	user, err := gorm.G[*entity.UserORM](db).
		Where("user_id = ?", userID).
		First(context.Background())
	if err != nil {
		return nil, err
	}
	return user, nil
}

// CreateUser 创建本地认证用户
func CreateUser(user *entity.UserORM) error {
	return db.Create(user).Error
}

// ListUsers 列出所有本地认证用户
func ListUsers() ([]*entity.UserORM, error) {
	var users []*entity.UserORM
	if err := db.Order("created_at ASC").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

// DeleteUser 删除本地认证用户（硬删除）
// 返回 gorm.ErrRecordNotFound 表示用户不存在
func DeleteUser(userID string) error {
	result := db.Where("user_id = ?", userID).Delete(&entity.UserORM{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// UpdateUserPassword 更新本地认证用户密码哈希
func UpdateUserPassword(userID, passwordHash string) error {
	result := db.Model(&entity.UserORM{}).
		Where("user_id = ?", userID).
		Update("password_hash", passwordHash)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("user not found")
	}
	return nil
}

// UpdateUserUsername 修改本地认证用户用户名
// user_id 不变，UserSpace / admin_users 等关联不受影响
// 返回 gorm.ErrRecordNotFound 表示用户不存在；用户名冲突返回唯一键错误
func UpdateUserUsername(userID, newUsername string) error {
	result := db.Model(&entity.UserORM{}).
		Where("user_id = ?", userID).
		Update("username", newUsername)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
