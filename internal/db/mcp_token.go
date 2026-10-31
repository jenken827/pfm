package db

import (
	"context"
	"time"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"gorm.io/gorm"
)

// ===== MCP 令牌 CRUD =====
//
// 令牌是长期凭据：库里只存明文的 sha256（唯一索引 idx_mt_hash），鉴权热路径
// GetMcpTokenByHash 必须走这个索引。撤销 = 物理删除——令牌不承载审计价值，
// 调用流水在日志里按 token_id 记。

// CreateMcpToken 创建 MCP 令牌
//
// token_hash 重复由 idx_mt_hash 唯一索引兜底（并发下「先查后插」不可靠）。
func CreateMcpToken(token *entity.McpTokenORM) error {
	return db.WithContext(context.Background()).Create(token).Error
}

// GetMcpTokenByHash 按令牌哈希查询（/mcp 鉴权的热路径）
// 返回 gorm.ErrRecordNotFound 表示令牌不存在或已被撤销。
func GetMcpTokenByHash(hash string) (*entity.McpTokenORM, error) {
	return gorm.G[*entity.McpTokenORM](db).
		Where("token_hash = ?", hash).
		First(context.Background())
}

// GetMcpTokenByID 按主键查询 MCP 令牌
func GetMcpTokenByID(id uint32) (*entity.McpTokenORM, error) {
	return gorm.G[*entity.McpTokenORM](db).
		Where("id = ?", id).
		First(context.Background())
}

// ListMcpTokens 列出令牌（ownerID 为空时列出全部，供管理员排查）
func ListMcpTokens(ownerID string) ([]*entity.McpTokenORM, error) {
	q := gorm.G[*entity.McpTokenORM](db)
	if ownerID != "" {
		return q.Where("owner_id = ?", ownerID).
			Order("id DESC").
			Find(context.Background())
	}
	return q.Order("id DESC").Find(context.Background())
}

// DeleteMcpToken 撤销令牌（返回受影响行数，0 表示本来就不存在）
func DeleteMcpToken(id uint32) (int64, error) {
	result := db.WithContext(context.Background()).
		Where("id = ?", id).
		Delete(&entity.McpTokenORM{})
	return result.RowsAffected, result.Error
}

// UpdateMcpTokenLastUsed 记录令牌最后使用时间
//
// 调用方负责节流（同一令牌每分钟最多写一次），这里只做单点更新。
func UpdateMcpTokenLastUsed(id uint32, at time.Time) error {
	return db.WithContext(context.Background()).
		Model(&entity.McpTokenORM{}).
		Where("id = ?", id).
		Update("last_used_at", at).Error
}
