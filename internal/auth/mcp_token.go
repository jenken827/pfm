package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/pkg/llog"
	"github.com/jenken827/pfm/utils"
	"gorm.io/gorm"
)

// MCP 令牌（长期凭据）
//
// 与登录 JWT 并列、互不替代：JWT 给人和 App（30 天、换 secret 即全失效），
// 令牌给长期运行的 MCP 客户端（可撤销、可限定读/写与可见根目录、可设过期）。
// 库里只存明文的 sha256，明文**只在创建时返回一次**；令牌只对 /mcp 生效。
const (
	// McpTokenPrefix 令牌明文前缀（便于在配置 / 日志里一眼认出，不参与校验语义）
	McpTokenPrefix = "pfm_mcp_"
	// mcpTokenRandomBytes 令牌随机部分长度（32 字节 → base64url 43 字符）
	mcpTokenRandomBytes = 32
	// mcpTokenLastUsedInterval last_used_at 的节流间隔（同一令牌每分钟最多写一次库）
	mcpTokenLastUsedInterval = time.Minute
)

// MCP 令牌相关错误。调用方按 errors.Is 判断，不要匹配文案。
var (
	// ErrMcpTokenInvalid 格式不对 / 前缀不符
	ErrMcpTokenInvalid = errors.New("mcp token invalid")
	// ErrMcpTokenNotFound 令牌不存在或已被撤销
	ErrMcpTokenNotFound = errors.New("mcp token not found or revoked")
	// ErrMcpTokenExpired 令牌已过期
	ErrMcpTokenExpired = errors.New("mcp token expired")
)

// McpPrincipal 一次 MCP 调用的身份与权限（令牌校验通过后填充）
type McpPrincipal struct {
	// TokenID 令牌主键（审计日志只记 id，不记明文）
	TokenID uint32
	// UserID 归属用户（UUID，与 users.user_id / owner_id 同口径）
	UserID string
	// TokenName 令牌标签（仅展示 / 日志）
	TokenName string
	// AllowRead 是否允许读工具
	AllowRead bool
	// AllowWrite 是否允许写工具（还需宿主配置 mcp.allow_write 也打开）
	AllowWrite bool
	// RootFolderID 可见根目录（空串 = 用户根目录，即整棵树）
	RootFolderID string
	// ExpiresAt 过期时间（nil 表示不过期）
	ExpiresAt *time.Time
}

// GenerateMcpToken 生成令牌明文与其哈希。
//
// 明文形如 pfm_mcp_<43 字符 base64url>，只应出现在创建命令的输出里；
// 返回的 hash 是完整明文（含前缀）的 sha256 小写十六进制。
func GenerateMcpToken() (plain string, hash string, err error) {
	buf := make([]byte, mcpTokenRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("生成令牌随机数失败: %w", err)
	}
	plain = McpTokenPrefix + base64.RawURLEncoding.EncodeToString(buf)
	return plain, HashMcpToken(plain), nil
}

// HashMcpToken 计算令牌明文的 sha256（与签发时同一口径）
func HashMcpToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// VerifyMcpToken 校验令牌并返回调用者身份。
//
// 校验顺序：格式 → 查库（唯一索引）→ 过期。撤销即删除，因此"查不到"与
// "已撤销"是同一个结果。明文比较不做逐字节比对——查的是哈希索引，
// 数据库侧不存在时序侧信道。
func VerifyMcpToken(raw string) (*McpPrincipal, error) {
	token := strings.TrimSpace(raw)
	if token == "" || !strings.HasPrefix(token, McpTokenPrefix) {
		return nil, ErrMcpTokenInvalid
	}
	orm, err := db.GetMcpTokenByHash(HashMcpToken(token))
	if err != nil {
		// 只有"查不到"才是令牌失效；连接类故障要说清楚，否则会把 DB 故障
		// 伪装成"令牌无效"，排查时被带偏。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMcpTokenNotFound
		}
		return nil, fmt.Errorf("查询 MCP 令牌失败: %w", err)
	}
	if orm.ExpiresAt != nil && !orm.ExpiresAt.After(time.Now()) {
		return nil, ErrMcpTokenExpired
	}
	principal := &McpPrincipal{
		TokenID:      utils.DerefUint32(orm.Id),
		UserID:       utils.DerefString(orm.OwnerId),
		TokenName:    utils.DerefString(orm.Name),
		AllowRead:    utils.DerefBool(orm.AllowRead),
		AllowWrite:   utils.DerefBool(orm.AllowWrite),
		RootFolderID: utils.DerefString(orm.RootFolderId),
		ExpiresAt:    orm.ExpiresAt,
	}
	if principal.UserID == "" {
		return nil, ErrMcpTokenInvalid
	}
	TouchMcpTokenLastUsed(principal.TokenID)
	return principal, nil
}

// lastUsedMu 保护 lastUsedAt：令牌 id → 上次写库时间
var (
	lastUsedMu sync.Mutex
	lastUsedAt = map[uint32]time.Time{}
)

// TouchMcpTokenLastUsed 节流更新 last_used_at。
//
// 鉴权是热路径（每次工具调用都过一次），每分钟每个令牌最多写一次库；
// 写失败只 warn，不影响本次调用。
func TouchMcpTokenLastUsed(id uint32) {
	if id == 0 {
		return
	}
	now := time.Now()
	lastUsedMu.Lock()
	last, ok := lastUsedAt[id]
	if ok && now.Sub(last) < mcpTokenLastUsedInterval {
		lastUsedMu.Unlock()
		return
	}
	lastUsedAt[id] = now
	lastUsedMu.Unlock()

	if err := db.UpdateMcpTokenLastUsed(id, now); err != nil {
		llog.L().Warnf("更新 MCP 令牌 %d 的最后使用时间失败: %v", id, err)
	}
}
