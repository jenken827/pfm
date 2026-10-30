package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/jenken827/pfm/globals"
	"github.com/jenken827/pfm/internal/auth"
	"github.com/jenken827/pfm/internal/config"
	"github.com/jenken827/pfm/internal/mcp"
	"github.com/jenken827/pfm/pkg/llog"
	"github.com/jenken827/pfm/server/middleware"
)

// mcpRoute 注册 MCP 端点。
//
// 默认关闭（mcp.enabled=false）：关闭时连路由都不注册，请求直接落到 mux 的 404，
// 不留下"探测得到但一律拒绝"的额外指纹。
func mcpRoute(mux *http.ServeMux) {
	if !config.GetConfig().Mcp.Enabled {
		return
	}
	path := mcp.EndpointPath()
	server := mcp.NewServer(mcp.LoadSettings())
	mux.Handle(path, mcpAuthMiddleware(server))
	llog.L().Infof("MCP 服务已启用: %s", path)
}

// mcpAuthMiddleware MCP 入口鉴权。
//
// 两种凭据并列，互相替代：
//   - MCP 令牌（pfm_mcp_ 前缀）：长期凭据，带读 / 写 / 可见根目录三件套；
//   - 登录 JWT：方便本机调试，权限按"整账户 + 写开关"。
//
// 鉴权通过后把 claims（service 层归属校验要用）与 UserSpace（落盘 provider 解析
// 要用）都注入 ctx，语义与 REST 的 restAuthMiddleware 完全一致——MCP 不允许绕开
// 这两层直接读库。
func mcpAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawToken := bearerToken(r)
		if rawToken == "" {
			mcp.WriteHTTPError(w, http.StatusUnauthorized, "缺少 Authorization: Bearer <令牌>")
			return
		}

		var (
			principal *auth.McpPrincipal
			claims    *auth.Claims
		)
		if strings.HasPrefix(rawToken, auth.McpTokenPrefix) {
			parsed, err := auth.VerifyMcpToken(rawToken)
			if err != nil {
				// 失败原因照实回给客户端（无效 / 已撤销 / 已过期），但不回显令牌本身
				mcp.WriteHTTPError(w, http.StatusUnauthorized, err.Error())
				return
			}
			principal = parsed
			claims = &auth.Claims{User: auth.User{Id: parsed.UserID, Name: parsed.TokenName}}
		} else {
			verified, err := auth.GetLocalAuth().VerifyToken(rawToken)
			if err != nil {
				mcp.WriteHTTPError(w, http.StatusUnauthorized, "令牌无效或已过期")
				return
			}
			claims = verified
			// JWT 代表"本人整账户"：读全开，写交给配置开关（effectiveWrite 仍会再与一次）
			principal = &auth.McpPrincipal{
				UserID:     verified.User.Id,
				TokenName:  "jwt",
				AllowRead:  true,
				AllowWrite: true,
			}
		}

		if claims.User.Id == "" {
			mcp.WriteHTTPError(w, http.StatusUnauthorized, "凭据里没有用户身份")
			return
		}

		ctx := context.WithValue(r.Context(), globals.ContextUserKey, claims)
		ctx = middleware.InjectUserSpace(ctx, claims.User.Id)
		ctx = mcp.WithPrincipal(ctx, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// bearerToken 取 Authorization: Bearer <token> 里的令牌
func bearerToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
