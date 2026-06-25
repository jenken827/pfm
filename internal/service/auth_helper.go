package service

import (
	"context"
	"fmt"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// getCurrentUserIDFromCtx 从 context 获取当前用户 ID（Casdoor UUID，稳定不变）
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
	return claims.User.Id
}

// isAdminUser 检查当前用户是否为管理员（查询本地 admin_users 表）
func isAdminUser(ctx context.Context) bool {
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return false
	}
	return db.IsAdmin(userID)
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
