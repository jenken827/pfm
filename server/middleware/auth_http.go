package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/auth"
)

const tokenHeader = "Authorization"

// AuthHTTPMiddleware HTTP认证中间件（用于非RPC的HTTP handler）
func AuthHTTPMiddleware(w http.ResponseWriter, r *http.Request) (*auth.Claims, error) {
	accessToken := r.Header.Get(tokenHeader)
	if accessToken == "" {
		return nil, errors.New("no token provided")
	}

	at, _ := strings.CutPrefix(accessToken, "Bearer ")

	claims, err := auth.GetLocalAuth().VerifyToken(at)
	if err != nil {
		return nil, err
	}

	// 设置用户信息到 context
	ctx := r.Context()
	ctx = context.WithValue(ctx, globals.ContextUserKey, claims)
	ctx = injectUserSpace(ctx, claims.User.Id)
	*r = *r.WithContext(ctx)

	return claims, nil
}

// AuthPreviewTicketHTTP 单文件票据鉴权（用于 ?ticket= 的预览 / 下载端点）
//
// 票据是单文件、短期的签名凭证（见 internal/auth/preview_ticket.go），给浏览器 /
// WebView / 浏览器下载器这类"带不了 Authorization 头"的场景替代 access token：
// 票据写进 URL 也只泄露"这段时间内能读这一个文件"，而不是账户级 30 天的全部权限。
//
// 这里合成的 claims 只填 user_id（name 标成 preview-ticket，仅用于日志），并把
// 票据绑定的 file_id 放进 claims.PreviewFileID；后续 GetFileStream 仍按该 user_id
// 做归属校验，因此票据不能越权读别人的文件。
//
// 请求带了 file_id 时要求与票据一致（否则 A 文件的票能读 B 文件）；历史版本这类
// "资源 id 不是 file_id"的端点不带 file_id，由 handler 拿 PreviewFileID 做宿主校验。
func AuthPreviewTicketHTTP(w http.ResponseWriter, r *http.Request) (*auth.Claims, error) {
	raw := r.URL.Query().Get("ticket")
	if raw == "" {
		return nil, errors.New("no ticket provided")
	}

	ticket, err := auth.GetLocalAuth().VerifyPreviewTicket(raw)
	if err != nil {
		return nil, err
	}

	if fileID := r.URL.Query().Get("file_id"); fileID != "" && fileID != ticket.FileID {
		return nil, errors.New("ticket does not match file_id")
	}

	claims := &auth.Claims{
		User:          auth.User{Id: ticket.UserID, Name: "preview-ticket"},
		PreviewFileID: ticket.FileID,
	}
	ctx := r.Context()
	ctx = context.WithValue(ctx, globals.ContextUserKey, claims)
	ctx = injectUserSpace(ctx, claims.User.Id)
	*r = *r.WithContext(ctx)

	return claims, nil
}
