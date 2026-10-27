package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// jwtSecretFile 自动生成的 JWT 密钥持久化路径（相对数据目录）
const jwtSecretFile = "jwt_secret"

// localAuth 全局本地认证器（懒初始化，线程安全）
var (
	localAuthOnce sync.Once
	localAuthInst *LocalAuthenticator
	localAuthErr  error
)

// LocalAuthenticator 本地认证器
//
// 用户密码以 bcrypt 哈希存于 users 表，登录成功后签发 HS256 JWT。
// JWT claims 采用顶层扁平 user 字段（{"id":..., "name":..., "email":...}），
// 由 middleware / service / webdav 层经 ctx 中的 *Claims 读取当前用户。
type LocalAuthenticator struct {
	jwtSecret []byte
	ttl       time.Duration
}

// InitLocalAuth 初始化本地认证器（读取或生成 JWT 密钥）
// 密钥来源：配置 auth.local.jwt_secret；为空时自动生成 32 字节随机密钥，
// 持久化到 <DataDir>/jwt_secret（0600 权限），保证重启后已签发 token 仍有效。
// 幂等：多次调用只初始化一次。
func InitLocalAuth() error {
	localAuthOnce.Do(func() {
		cfg := config.GetConfig().Auth.Local
		secret := strings.TrimSpace(cfg.JWTSecret)
		if secret == "" {
			secret, localAuthErr = loadOrGenerateSecret()
			if localAuthErr != nil {
				return
			}
		}

		ttl := time.Duration(cfg.TokenTTLHours) * time.Hour
		if ttl <= 0 {
			ttl = 720 * time.Hour
		}

		localAuthInst = &LocalAuthenticator{
			jwtSecret: []byte(secret),
			ttl:       ttl,
		}
		llog.L().Info("local auth initialized")
	})
	return localAuthErr
}

// GetLocalAuth 获取本地认证器（未初始化时自动初始化）
func GetLocalAuth() *LocalAuthenticator {
	if localAuthInst == nil {
		if err := InitLocalAuth(); err != nil {
			panic(fmt.Sprintf("init local auth failed: %v", err))
		}
	}
	return localAuthInst
}

// loadOrGenerateSecret 读取已持久化的密钥，不存在则生成新密钥并落盘
func loadOrGenerateSecret() (string, error) {
	path := filepath.Join(globals.DataDir, jwtSecretFile)
	if data, err := os.ReadFile(path); err == nil {
		secret := strings.TrimSpace(string(data))
		if secret != "" {
			return secret, nil
		}
	}

	// 生成 32 字节随机密钥
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate jwt secret failed: %w", err)
	}
	secret := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(secret+"\n"), 0600); err != nil {
		return "", fmt.Errorf("persist jwt secret to %s failed: %w", path, err)
	}
	llog.L().Infof("generated new local JWT secret, saved to %s", path)
	return secret, nil
}

// HashPassword 使用 bcrypt 哈希密码
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword 校验密码是否匹配 bcrypt 哈希
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// Authenticate 校验本地用户名密码，成功返回用户 ID（UUID，与 owner_id / user_id 一致）
// 返回 ("", error) 表示认证失败
func (a *LocalAuthenticator) Authenticate(username, password string) (string, error) {
	user, err := db.GetUserByUsername(username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", fmt.Errorf("user not found: %s", username)
		}
		return "", fmt.Errorf("query user %s failed: %w", username, err)
	}

	if user.PasswordHash == nil || !VerifyPassword(*user.PasswordHash, password) {
		return "", fmt.Errorf("invalid password for user: %s", username)
	}

	if user.UserId == nil {
		return "", fmt.Errorf("user %s has no user_id", username)
	}
	llog.L().Infof("local auth success: user=%s, user_id=%s", username, *user.UserId)
	return *user.UserId, nil
}

// IssueToken 为本地用户签发 JWT（claims 顶层扁平：id/name/email）
func (a *LocalAuthenticator) IssueToken(user *native.UserORM) (string, error) {
	if user.UserId == nil || user.Username == nil {
		return "", fmt.Errorf("issue token failed: user id/username is nil")
	}
	now := time.Now()
	claims := &Claims{
		User: User{
			Id:    *user.UserId,
			Name:  *user.Username,
			Email: derefStr(user.Email),
		},
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "pfm-local",
			Subject:   *user.UserId,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(a.ttl)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.jwtSecret)
}

// VerifyToken 校验本地 JWT，返回用户 claims
// 只接受 HS256 签名（防止 alg 混淆攻击）
func (a *LocalAuthenticator) VerifyToken(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return a.jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, ErrInvalidToken
	}
	if claims.User.Name == "" || claims.User.Id == "" {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// derefStr 安全解引用 string 指针
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
