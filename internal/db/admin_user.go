package db

import (
	"context"
	"errors"

	"jenken827.com/jenken827/pfm/proto/gen/native"
	"gorm.io/gorm"
)

// IsAdmin 检查用户是否为管理员
func IsAdmin(userID string) bool {
	_, err := gorm.G[*native.AdminUserORM](db).
		Where("user_id = ?", userID).
		First(context.Background())
	return err == nil
}

// GetAdminUser 获取管理员记录
func GetAdminUser(userID string) (*native.AdminUserORM, error) {
	admin, err := gorm.G[*native.AdminUserORM](db).
		Where("user_id = ?", userID).
		First(context.Background())
	if err != nil {
		return nil, err
	}
	return admin, nil
}

// ListAdminUsers 获取所有管理员
func ListAdminUsers() ([]*native.AdminUserORM, error) {
	admins, err := gorm.G[*native.AdminUserORM](db).
		Find(context.Background())
	if err != nil {
		return nil, err
	}
	return admins, nil
}

// AddAdminUser 添加管理员
func AddAdminUser(userID string) error {
	// 检查是否已存在
	_, err := GetAdminUser(userID)
	if err == nil {
		return errors.New("user is already admin")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	admin := &native.AdminUserORM{
		UserId: &userID,
	}
	return db.Create(admin).Error
}

// RemoveAdminUser 移除管理员
func RemoveAdminUser(userID string) error {
	result := db.Where("user_id = ?", userID).Delete(&native.AdminUserORM{})
	if result.RowsAffected == 0 {
		return errors.New("user is not admin")
	}
	return result.Error
}
