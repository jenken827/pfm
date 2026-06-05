package middleware

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/server/response"
)

// adminRoleName 管理员角色名
const adminRoleName = "pfm_admin"

// getUserFromCtx 从 context 获取当前用户
func getUserFromCtx(ctx context.Context) *casdoorsdk.User {
	val := ctx.Value(globals.ContextUserKey)
	if val == nil {
		return nil
	}
	claims, ok := val.(*casdoorsdk.Claims)
	if !ok || claims == nil {
		return nil
	}
	return &claims.User
}

// isAdmin 检查是否为管理员
func isAdmin(ctx context.Context) bool {
	user := getUserFromCtx(ctx)
	if user == nil {
		return false
	}
	for _, role := range user.Roles {
		if role.Name == adminRoleName {
			return true
		}
	}
	return false
}

// PermInterceptor 权限检查拦截器
// 管理员跳过权限检查，普通用户需要检查目录权限
func PermInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(
			ctx context.Context,
			req connect.AnyRequest,
		) (connect.AnyResponse, error) {
			// 管理员跳过权限检查
			if isAdmin(ctx) {
				return next(ctx, req)
			}

			// 获取当前用户名
			user := getUserFromCtx(ctx)
			if user == nil {
				return response.ErrResp(&models.Err{
					E: errors.New("no user in context"),
					C: native.RespCode_TOKEN_INVALID,
				})
			}
			userID := user.Name

			// 从请求中提取 file_id（根据不同的 RPC 方法）
			procedure := req.Spec().Procedure
			fileID := extractFileIDFromRequest(req, procedure)

			if fileID != "" {
				allowed := service.CheckPermission(userID, fileID, service.PermRead)
				if !allowed {
					return response.ErrResp(&models.Err{
						E: errors.New("permission denied"),
						C: native.RespCode_PARAMS_ERROR,
					})
				}
			}

			return next(ctx, req)
		}
	}
}

// extractFileIDFromRequest 从请求中提取 file_id
func extractFileIDFromRequest(req connect.AnyRequest, procedure string) string {
	_ = procedure
	_ = req
	// TODO: 实现从不同 RPC 请求中提取 file_id
	return ""
}

// RequirePermission 要求特定权限的拦截器
func RequirePermission(perm uint32) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(
			ctx context.Context,
			req connect.AnyRequest,
		) (connect.AnyResponse, error) {
			// 管理员跳过权限检查
			if isAdmin(ctx) {
				return next(ctx, req)
			}

			// 获取当前用户名
			user := getUserFromCtx(ctx)
			if user == nil {
				return response.ErrResp(&models.Err{
					E: errors.New("no user in context"),
					C: native.RespCode_TOKEN_INVALID,
				})
			}
			userID := user.Name

			// 从请求中提取 file_id
			procedure := req.Spec().Procedure
			fileID := extractFileIDFromRequest(req, procedure)

			if fileID != "" {
				allowed := service.CheckPermission(userID, fileID, perm)
				if !allowed {
					return response.ErrResp(&models.Err{
						E: errors.New("permission denied"),
						C: native.RespCode_PARAMS_ERROR,
					})
				}
			}

			return next(ctx, req)
		}
	}
}
