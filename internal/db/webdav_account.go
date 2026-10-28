package db

import (
	"context"
	"time"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"gorm.io/gorm"
)

// ===== WebDAV 独立账号 CRUD =====
//
// 账号与登录账号（users）完全解耦：username 是 /dav 专用命名空间（唯一索引 idx_wa_un），
// password_hash 为 bcrypt。认证热路径 GetWebdavAccountByUsername 必须走唯一索引。

// CreateWebdavAccount 创建 WebDAV 账号
// 用户名重复由 idx_wa_un 唯一索引兜底（并发下「先查后插」不可靠）
func CreateWebdavAccount(a *entity.WebdavAccountORM) error {
	return db.WithContext(context.Background()).Create(a).Error
}

// GetWebdavAccountByUsername 按 WebDAV 用户名查询（/dav 认证热路径）
// 返回 gorm.ErrRecordNotFound 表示账号不存在
func GetWebdavAccountByUsername(username string) (*entity.WebdavAccountORM, error) {
	return gorm.G[*entity.WebdavAccountORM](db).
		Where("username = ?", username).
		First(context.Background())
}

// GetWebdavAccountByID 按主键查询 WebDAV 账号
func GetWebdavAccountByID(id uint32) (*entity.WebdavAccountORM, error) {
	return gorm.G[*entity.WebdavAccountORM](db).
		Where("id = ?", id).
		First(context.Background())
}

// ListWebdavAccountsByUser 列出某用户的全部 WebDAV 账号（新建的排前面）
func ListWebdavAccountsByUser(userID string) ([]*entity.WebdavAccountORM, error) {
	return gorm.G[*entity.WebdavAccountORM](db).
		Where("user_id = ?", userID).
		Order("id DESC").
		Find(context.Background())
}

// UpdateWebdavAccountFields 按 id 更新指定列。
//
// 必须用 map 而不是结构体：GORM 结构体 Updates 会跳过零值，enabled=false /
// read_only=false / 空串等「合法的新值」会被静默丢弃（UserSpace 的更新就踩过这个坑）。
func UpdateWebdavAccountFields(id uint32, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return db.WithContext(context.Background()).
		Model(&entity.WebdavAccountORM{}).
		Where("id = ?", id).
		Updates(fields).Error
}

// UpdateWebdavAccountPassword 更新密码哈希
func UpdateWebdavAccountPassword(id uint32, passwordHash string) error {
	return UpdateWebdavAccountFields(id, map[string]any{"password_hash": passwordHash})
}

// UpdateWebdavAccountLastUsed 记录账号最后使用时间
func UpdateWebdavAccountLastUsed(id uint32) error {
	return UpdateWebdavAccountFields(id, map[string]any{"last_used_at": time.Now()})
}

// SetWebdavAccountEnabled 启用/禁用账号；禁用原因写入 disabled_reason
// （启用时传空串即清空原因）
func SetWebdavAccountEnabled(id uint32, enabled bool, reason string) error {
	return UpdateWebdavAccountFields(id, map[string]any{
		"enabled":         enabled,
		"disabled_reason": reason,
	})
}

// DeleteWebdavAccount 删除账号（硬删除：纯配置数据，无保留价值）
// 返回 gorm.ErrRecordNotFound 表示账号不存在
func DeleteWebdavAccount(id uint32) error {
	result := db.WithContext(context.Background()).
		Where("id = ?", id).
		Delete(&entity.WebdavAccountORM{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DisableWebdavAccountsByRootIDs 把绑定目录落在 rootIDs 中的「仍启用」账号批量自动禁用，
// 返回被禁用的账号 ID（调用方据此清理认证缓存）。
//
// 两次固定查询（先取 ID 再批量 UPDATE），不随账号数增长；更新用 map 以避开
// GORM 结构体 Updates 的零值跳过问题（enabled=false 会被静默丢弃）。
func DisableWebdavAccountsByRootIDs(rootIDs []string, reason string) ([]uint32, error) {
	if len(rootIDs) == 0 {
		return nil, nil
	}
	ctx := context.Background()

	var accounts []*entity.WebdavAccountORM
	if err := db.WithContext(ctx).
		Model(&entity.WebdavAccountORM{}).
		Where("root_file_id IN ?", rootIDs).
		Find(&accounts).Error; err != nil {
		return nil, err
	}

	ids := make([]uint32, 0, len(accounts))
	for _, a := range accounts {
		if a.Id == nil || (a.Enabled != nil && !*a.Enabled) {
			continue
		}
		ids = append(ids, *a.Id)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	if err := db.WithContext(ctx).
		Model(&entity.WebdavAccountORM{}).
		Where("id IN ?", ids).
		Updates(map[string]any{"enabled": false, "disabled_reason": reason}).Error; err != nil {
		return nil, err
	}
	return ids, nil
}
