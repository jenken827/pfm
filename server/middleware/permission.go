package middleware

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/server/response"
)

// PermInterceptor 权限检查拦截器
// 基于 owner_id 的权限模型：文件所有者或管理员可访问
func PermInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(
			ctx context.Context,
			req connect.AnyRequest,
		) (connect.AnyResponse, error) {
			// 从请求中提取 file_id（根据不同的 RPC 方法）
			procedure := req.Spec().Procedure
			fileID := extractFileIDFromRequest(req, procedure)

			if fileID != "" {
				// 基于 owner_id 的权限检查：所有者或管理员可访问
				if !service.CheckOwnershipOrAdmin(ctx, fileID) {
					return response.ErrResp(&models.Err{
						E: errors.New("permission denied: not file owner"),
						C: native.RespCode_PERMISSION_DENIED,
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
// 基于 owner_id 的权限模型：文件所有者或管理员可访问
func RequirePermission(perm uint32) connect.UnaryInterceptorFunc {
	_ = perm // owner_id 模型下不再使用具体权限位
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(
			ctx context.Context,
			req connect.AnyRequest,
		) (connect.AnyResponse, error) {
			// 从请求中提取 file_id
			procedure := req.Spec().Procedure
			fileID := extractFileIDFromRequest(req, procedure)

			if fileID != "" {
				// 基于 owner_id 的权限检查：所有者或管理员可访问
				if !service.CheckOwnershipOrAdmin(ctx, fileID) {
					return response.ErrResp(&models.Err{
						E: errors.New("permission denied: not file owner"),
						C: native.RespCode_PERMISSION_DENIED,
					})
				}
			}

			return next(ctx, req)
		}
	}
}
