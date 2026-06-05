package api

import (
	"context"

	"connectrpc.com/connect"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/proto/gen/native/nativeconnect"
	"jenken827.com/jenken827/pfm/server/response"
)

// PermissionServer 权限服务
type PermissionServer struct{}

var _ = (nativeconnect.PermissionServiceHandler)((*PermissionServer)(nil))

// getOperatorFromCtx 从 context 获取当前用户（操作者）
func getOperatorFromCtx(ctx context.Context) string {
	val := ctx.Value(globals.ContextUserKey)
	if val == nil {
		return ""
	}
	claims, ok := val.(*casdoorsdk.Claims)
	if !ok || claims == nil {
		return ""
	}
	return claims.User.Name
}

// GrantDirectoryPermission 授予目录权限
func (s *PermissionServer) GrantDirectoryPermission(ctx context.Context, req *connect.Request[native.GrantDirectoryPermissionReq]) (*connect.Response[native.BaseResp], error) {
	operator := getOperatorFromCtx(ctx)
	if err := service.GrantDirectoryPermission(ctx, req.Msg, operator); err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// RevokeDirectoryPermission 撤销目录权限
func (s *PermissionServer) RevokeDirectoryPermission(ctx context.Context, req *connect.Request[native.RevokeDirectoryPermissionReq]) (*connect.Response[native.BaseResp], error) {
	operator := getOperatorFromCtx(ctx)
	if err := service.RevokeDirectoryPermission(ctx, req.Msg, operator); err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// UpdateDirectoryPermission 更新目录权限
func (s *PermissionServer) UpdateDirectoryPermission(ctx context.Context, req *connect.Request[native.UpdateDirectoryPermissionReq]) (*connect.Response[native.BaseResp], error) {
	operator := getOperatorFromCtx(ctx)
	if err := service.UpdateDirectoryPermission(ctx, req.Msg, operator); err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// GetUserPermissions 获取用户的目录权限列表
func (s *PermissionServer) GetUserPermissions(ctx context.Context, req *connect.Request[native.GetUserPermissionsReq]) (*connect.Response[native.BaseResp], error) {
	result, err := service.GetUserPermissions(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(result)
}

// GetDirectoryPermissions 获取目录的权限列表
func (s *PermissionServer) GetDirectoryPermissions(ctx context.Context, req *connect.Request[native.GetDirectoryPermissionsReq]) (*connect.Response[native.BaseResp], error) {
	result, err := service.GetDirectoryPermissions(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(result)
}

// BatchGrantPermission 批量授予权限
func (s *PermissionServer) BatchGrantPermission(ctx context.Context, req *connect.Request[native.BatchGrantPermissionReq]) (*connect.Response[native.BaseResp], error) {
	operator := getOperatorFromCtx(ctx)
	if err := service.BatchGrantPermission(ctx, req.Msg, operator); err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// GetAccessibleDirectories 获取用户可访问的目录列表
func (s *PermissionServer) GetAccessibleDirectories(ctx context.Context, req *connect.Request[native.GetAccessibleDirectoriesReq]) (*connect.Response[native.BaseResp], error) {
	result, err := service.GetAccessibleDirectories(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(result)
}

// ListUsers 获取用户列表（管理员用）
func (s *PermissionServer) ListUsers(ctx context.Context, req *connect.Request[native.ListUsersReq]) (*connect.Response[native.BaseResp], error) {
	result, err := service.ListUsers(ctx)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(result)
}
