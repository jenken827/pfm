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

	// PreviewFileID 非空表示本次请求用的是**单文件票据**（预览 / 下载），
	// 值为票据绑定的 file_id。给"资源 id 不是 file_id"的端点（历史版本）做宿主校验用：
	// 票据的文件必须与历史记录所属文件一致，否则一张 A 文件的票能读该用户所有历史版本。
	//
	// 不参与 JWT 序列化（json:"-"），由 AuthPreviewTicketHTTP 填充。
	PreviewFileID string `json:"-"`
}

// User 登录用户核心信息（仅保留业务实际使用字段）
type User struct {
	Id    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// ErrInvalidToken token 无效或格式错误
var ErrInvalidToken = errors.New("invalid token")
