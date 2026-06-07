package service

import (
	"context"
	"fmt"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// getCurrentUserIDFromCtx 从 context 获取当前用户 ID
// 如果未登录返回空字符串
func getCurrentUserIDFromCtx(ctx context.Context) string {
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

// isAdminUser 检查当前用户是否为管理员
func isAdminUser(ctx context.Context) bool {
	val := ctx.Value(globals.ContextUserKey)
	if val == nil {
		return false
	}
	claims, ok := val.(*casdoorsdk.Claims)
	if !ok || claims == nil {
		return false
	}

	for _, role := range claims.User.Roles {
		if role.Name == "pfm_admin" {
			return true
		}
	}
	return false
}

// checkFilePermission 检查当前用户对文件的权限
// 管理员跳过检查，普通用户需要检查权限
// 返回 true 表示有权限，false 表示无权限
func checkFilePermission(ctx context.Context, fileID string, perm uint32) bool {
	// 管理员跳过权限检查
	if isAdminUser(ctx) {
		return true
	}

	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return false
	}

	return CheckPermission(userID, fileID, perm)
}

// RequireAdmin 检查当前用户是否为管理员，非管理员返回 PERMISSION_DENIED 错误
func RequireAdmin(ctx context.Context) *models.Err {
	if isAdminUser(ctx) {
		return nil
	}
	return &models.Err{
		E: fmt.Errorf("permission denied: admin access required"),
		C: native.RespCode_PERMISSION_DENIED,
	}
}
