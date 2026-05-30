package auth

import (
	"errors"
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
			return entry.claims, nil
		}
		v.cache.Delete(token)
	}

	// 3. 缓存未命中或已过期，向 Casdoor introspect
	result, err := v.client.IntrospectToken(token, "access_token")
	if err != nil {
		// Casdoor 不可用时降级为仅本地验证，避免因 Casdoor 临时故障导致所有请求失败
		llog.L().Warnf("token introspect failed, fallback to local verify: %v", err)
		return claims, nil
	}

	// 4. 缓存结果
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
