package auth

import (
	"errors"

	"github.com/golang-jwt/jwt/v4"
)

// 认证层自定义 claims 类型。
// JSON 结构与历史 token 保持兼容：
//   - User 内嵌展开（id/name/email 位于 JWT payload 顶层），旧版本签发的
//     token（HS256、同 secret）无需重新登录即可继续通过 VerifyToken 校验；
//   - jwt.RegisteredClaims 提供标准 iss/sub/iat/exp。
//
// ctx 中存放 *Claims（globals.ContextUserKey），middleware/service/api/webdav
// 层均通过该类型读取当前用户。
type Claims struct {
	User
	jwt.RegisteredClaims
}

// User 登录用户核心信息（仅保留业务实际使用字段）
type User struct {
	Id    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// ErrInvalidToken token 无效或格式错误
var ErrInvalidToken = errors.New("invalid token")
