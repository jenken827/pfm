package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/llog"
)

const (
	// basicAuthCacheTTL Basic Auth 认证结果缓存有效期
	basicAuthCacheTTL = 5 * time.Minute
	// basicAuthFailCacheTTL 认证失败缓存有效期（防暴力破解）
	basicAuthFailCacheTTL = 1 * time.Minute
)

// basicAuthCacheEntry Basic Auth 缓存条目
type basicAuthCacheEntry struct {
	valid     bool
	userName  string
	expiresAt time.Time
}

// BasicAuthenticator Basic Auth 认证器
// 通过 Casdoor OAuth2 ROPC grant 验证用户名密码
type BasicAuthenticator struct {
	cache      sync.Map // map[string]*basicAuthCacheEntry, key=username
	casdoorURL string
	clientID   string
	orgName    string
	appName    string
}

// NewBasicAuthenticator 创建 Basic Auth 认证器
func NewBasicAuthenticator() *BasicAuthenticator {
	cd := config.GetConfig().Casdoor
	return &BasicAuthenticator{
		casdoorURL: cd.Endpoint,
		clientID:   cd.ClientID,
		orgName:    cd.OrganizationName,
		appName:    cd.ApplicationName,
	}
}

// Authenticate 验证用户名密码
// 返回 (userName, nil) 表示认证成功
// 返回 ("", error) 表示认证失败
func (a *BasicAuthenticator) Authenticate(username, password string) (string, error) {
	// 1. 查缓存
	cacheKey := username + ":" + password
	if cached, ok := a.cache.Load(cacheKey); ok {
		entry := cached.(*basicAuthCacheEntry)
		if time.Now().Before(entry.expiresAt) {
			if !entry.valid {
				return "", fmt.Errorf("authentication failed (cached)")
			}
			return entry.userName, nil
		}
		a.cache.Delete(cacheKey)
	}

	// 2. 调 Casdoor OAuth2 ROPC grant 验证
	token, err := a.loginWithCasdoor(username, password)
	if err != nil {
		// 缓存失败结果
		a.cache.Store(cacheKey, &basicAuthCacheEntry{
			valid:     false,
			expiresAt: time.Now().Add(basicAuthFailCacheTTL),
		})
		return "", fmt.Errorf("casdoor authentication failed: %w", err)
	}

	// 3. 验证成功，解析 token 获取用户名
	// ROPC grant 返回的 token 中包含用户信息
	_ = token // token 已获取，认证成功

	// 4. 缓存成功结果
	a.cache.Store(cacheKey, &basicAuthCacheEntry{
		valid:     true,
		userName:  username,
		expiresAt: time.Now().Add(basicAuthCacheTTL),
	})

	llog.L().Infof("webdav basic auth success: user=%s", username)
	return username, nil
}

// loginWithCasdoor 调用 Casdoor OAuth2 ROPC grant 获取 token
func (a *BasicAuthenticator) loginWithCasdoor(username, password string) (string, error) {
	tokenURL := fmt.Sprintf("%s/api/login/oauth/access_token", a.casdoorURL)

	data := url.Values{
		"grant_type":    {"password"},
		"client_id":     {a.clientID},
		"username":      {username},
		"password":      {password},
		"organization":  {a.orgName},
		"application":   {a.appName},
	}

	resp, err := http.PostForm(tokenURL, data)
	if err != nil {
		return "", fmt.Errorf("request casdoor failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("casdoor returned status %d", resp.StatusCode)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode casdoor response failed: %w", err)
	}

	if result.Error != "" {
		return "", fmt.Errorf("casdoor error: %s - %s", result.Error, result.ErrorDesc)
	}

	if result.AccessToken == "" {
		return "", fmt.Errorf("casdoor returned empty access_token")
	}

	return result.AccessToken, nil
}
