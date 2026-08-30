package auth

import (
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"jenken827.com/jenken827/pfm/internal/config"
)

// Verifier token 验证器统一接口（casdoor / local 双实现）
// 两个实现都返回 *casdoorsdk.Claims，中间件与 service 层无需感知认证模式
type Verifier interface {
	Verify(token string) (*casdoorsdk.Claims, error)
}

// IsLocalMode 当前是否为本地认证模式（auth.mode = local）
func IsLocalMode() bool {
	return config.GetConfig().Auth.Mode == "local"
}

// AuthenticateBasic 按当前认证模式验证用户名密码，返回用户 ID
// mode=local 时使用本地用户表（bcrypt），否则使用 Casdoor ROPC
func AuthenticateBasic(username, password string) (string, error) {
	if IsLocalMode() {
		return GetLocalAuth().Authenticate(username, password)
	}
	return NewBasicAuthenticator().Authenticate(username, password)
}

// LocalTokenVerifier 本地 JWT 验证器（实现 Verifier 接口）
type LocalTokenVerifier struct {
	auth *LocalAuthenticator
}

// NewLocalTokenVerifier 创建本地 JWT 验证器
func NewLocalTokenVerifier() *LocalTokenVerifier {
	return &LocalTokenVerifier{auth: GetLocalAuth()}
}

// Verify 校验本地 JWT
func (v *LocalTokenVerifier) Verify(token string) (*casdoorsdk.Claims, error) {
	return v.auth.VerifyToken(token)
}
