package server

import (
	"context"
	"net/http"

	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/server/middleware"
)

// restAuthMiddleware 为 REST 业务路由提供鉴权，等价于原先的 connect AuthInterceptor：
//   - Debug 模式：注入模拟管理员并放行（与 AuthInterceptor 行为一致）；
//   - 否则校验 Authorization: Bearer <JWT>，并把 claims 与 UserSpace 注入 ctx；
//   - 失败时以 BaseResp(TOKEN_INVALID) 返回，引导前端清理登录态。
//
// 注意：仅包裹业务接口。公开接口（登录、分享下载、健康检查等）不得经过本中间件。
func restAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if globals.Debug {
			claims := &auth.Claims{
				User: auth.User{
					Id:    "debug-admin-id",
					Name:  "debug-admin",
					Email: "debug@localhost",
				},
			}
			ctx := context.WithValue(r.Context(), globals.ContextUserKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		if _, err := middleware.AuthHTTPMiddleware(w, r); err != nil {
			writeProtoError(w, http.StatusUnauthorized, native.RespCode_TOKEN_INVALID, err.Error())
			return
		}
		next.ServeHTTP(w, r)
	})
}
