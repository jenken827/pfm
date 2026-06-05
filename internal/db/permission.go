package db

import (
	"context"

	"jenken827.com/jenken827/pfm/proto/gen/native"
	"gorm.io/gorm"
)

// GetDirectoryPermission 获取用户对指定目录的权限
func GetDirectoryPermission(userID, fileID string) (*native.DirectoryPermissionORM, error) {
	perm, err := gorm.G[*native.DirectoryPermissionORM](db).
		Where("user_id = ? AND file_id = ?", userID, fileID).
		First(context.Background())
	if err != nil {
		return nil, err
	}
	return perm, nil
}

// GetUserDirectoryPermissions 获取用户的所有目录权限
func GetUserDirectoryPermissions(userID string) ([]*native.DirectoryPermissionORM, error) {
	perms, err := gorm.G[*native.DirectoryPermissionORM](db).
		Where("user_id = ?", userID).
		Find(context.Background())
	if err != nil {
		return nil, err
	}
	return perms, nil
}

// GetDirectoryPermissionsByFileID 获取目录的所有用户权限
func GetDirectoryPermissionsByFileID(fileID string) ([]*native.DirectoryPermissionORM, error) {
	perms, err := gorm.G[*native.DirectoryPermissionORM](db).
		Where("file_id = ?", fileID).
		Find(context.Background())
	if err != nil {
		return nil, err
	}
	return perms, nil
}

// CreateDirectoryPermission 创建目录权限
func CreateDirectoryPermission(perm *native.DirectoryPermissionORM) error {
	return db.Create(perm).Error
}

// UpdateDirectoryPermission 更新目录权限
func UpdateDirectoryPermission(perm *native.DirectoryPermissionORM) error {
	return db.Save(perm).Error
}

// DeleteDirectoryPermission 删除目录权限
func DeleteDirectoryPermission(userID, fileID string) error {
	result := db.Where("user_id = ? AND file_id = ?", userID, fileID).
		Delete(&native.DirectoryPermissionORM{})
	return result.Error
}

// GetAllDirectoryPermissions 获取所有目录权限（用于管理员查看）
func GetAllDirectoryPermissions() ([]*native.DirectoryPermissionORM, error) {
	perms, err := gorm.G[*native.DirectoryPermissionORM](db).
		Find(context.Background())
	if err != nil {
		return nil, err
	}
	return perms, nil
}

// CreateAuditLog 创建审计日志
func CreateAuditLog(log *native.PermissionAuditLogORM) error {
	return db.Create(log).Error
}
