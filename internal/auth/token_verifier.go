package auth

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/llog"
)

const (
	// introspectCacheTTL introspect 结果缓存有效期
	introspectCacheTTL = 30 * time.Second
)

var (
	ErrInvalidToken   = errors.New("invalid token")
	ErrTokenInactive  = errors.New("token is inactive or user is disabled")
)

// tokenCacheEntry 缓存条目
type tokenCacheEntry struct {
	claims    *casdoorsdk.Claims
	active    bool
	expiresAt time.Time
}

// TokenVerifier token 验证器
// 先本地解析 JWT，再定期向 Casdoor introspect 确认 token 是否仍然有效
type TokenVerifier struct {
	cache  sync.Map // map[string]*tokenCacheEntry
	client *casdoorsdk.Client
}

// NewTokenVerifier 创建 token 验证器
func NewTokenVerifier() *TokenVerifier {
	cd := config.GetConfig().Casdoor
	client := casdoorsdk.NewClient(
		cd.Endpoint, cd.ClientID, cd.ClientSecret,
		cd.Certificate, cd.OrganizationName, cd.ApplicationName,
	)
	return &TokenVerifier{client: client}
}

// Verify 验证 token
// 返回 claims 和 error。error 不为 nil 表示 token 无效或用户已被禁用
func (v *TokenVerifier) Verify(token string) (*casdoorsdk.Claims, error) {
	// 1. 先本地解析 JWT（验证签名和格式）
	claims, err := casdoorsdk.ParseJwtToken(token)
	if err != nil {
		return nil, err
	}
	if claims.User.Name == "" {
		return nil, ErrInvalidToken
	}

	// 2. 查缓存
	if cached, ok := v.cache.Load(token); ok {
		entry := cached.(*tokenCacheEntry)
		if time.Now().Before(entry.expiresAt) {
			if !entry.active {
				return nil, ErrTokenInactive
			}
			// 缓存命中时仍需补充角色信息（旧缓存可能没有 Roles）
			v.fillUserRoles(entry.claims)
			return entry.claims, nil
		}
		v.cache.Delete(token)
	}

	// 3. 缓存未命中或已过期，向 Casdoor introspect
	result, err := v.client.IntrospectToken(token, "access_token")
	if err != nil {
		// Casdoor 不可用时降级为仅本地验证，避免因 Casdoor 临时故障导致所有请求失败
		llog.L().Warnf("token introspect failed, fallback to local verify: %v", err)
		// 降级时仍尝试补充用户角色信息
		v.fillUserRoles(claims)
		return claims, nil
	}

	// 4. 补充完整用户信息（JWT 中 Roles 是 []string，User.Roles 是 []*Role，反序列化会丢失）
	v.fillUserRoles(claims)

	// 5. 缓存结果
	entry := &tokenCacheEntry{
		claims:    claims,
		active:    result.Active,
		expiresAt: time.Now().Add(introspectCacheTTL),
	}
	v.cache.Store(token, entry)

	if !result.Active {
		return nil, ErrTokenInactive
	}

	return claims, nil
}

// fillUserRoles 从 Casdoor API 获取完整用户信息，补充 Roles 字段
// JWT 中 roles 是 []string，但 casdoorsdk.User.Roles 是 []*Role，JSON 反序列化会静默失败
// GetUser API 也不返回 roles，所以需要通过 GetRole 反查
func (v *TokenVerifier) fillUserRoles(claims *casdoorsdk.Claims) {
	if claims.User.Name == "" {
		return
	}
	// 如果 Roles 已经有值，跳过
	if len(claims.User.Roles) > 0 {
		return
	}
	// 方案：获取 pfm_admin 角色，检查用户是否在角色的 users 列表中
	role, err := v.client.GetRole("pfm_admin")
	if err != nil {
		llog.L().Warnf("fillUserRoles: failed to get pfm_admin role: %v", err)
		return
	}
	if role == nil {
		llog.L().Warnf("fillUserRoles: pfm_admin role not found")
		return
	}
	// 检查用户是否在角色的 users 列表中
	for _, u := range role.Users {
		if u == claims.User.Name || u == fmt.Sprintf("%s/%s", v.client.OrganizationName, claims.User.Name) {
			claims.User.Roles = []*casdoorsdk.Role{role}
			llog.L().Infof("fillUserRoles: user %s has pfm_admin role", claims.User.Name)
			return
		}
	}
	llog.L().Infof("fillUserRoles: user %s does not have pfm_admin role", claims.User.Name)
}
