package service

import (
	"context"
	"fmt"

	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// getCurrentUserIDFromCtx 从 context 获取当前用户 ID（UUID，稳定不变）
// 如果未登录返回空字符串
func getCurrentUserIDFromCtx(ctx context.Context) string {
	val := ctx.Value(globals.ContextUserKey)
	if val == nil {
		return ""
	}
	claims, ok := val.(*auth.Claims)
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

// getUserContext 从 context 获取完整用户空间信息
// 如果用户未配置 UserSpace，返回 nil
func getUserContext(ctx context.Context) *models.UserContext {
	val := ctx.Value(globals.ContextUserSpaceKey)
	if val == nil {
		return nil
	}
	uc, ok := val.(*models.UserContext)
	if !ok {
		return nil
	}
	return uc
}

// getBoundProviderIdsFromCtx 从 context 获取绑定的存储提供者
// 返回错误表示用户根目录未配置
func getBoundProviderIdsFromCtx(ctx context.Context) ([]uint32, *models.Err) {
	uc := getUserContext(ctx)
	if uc == nil || len(uc.BoundProviderIds) == 0 {
		return nil, &models.Err{
			E: fmt.Errorf("根目录未配置，请联系管理员"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}
	return uc.BoundProviderIds, nil
}

// getRootFileIDFromCtx 从 context 获取用户根目录 ID
// 返回错误表示用户根目录未配置
func getRootFileIDFromCtx(ctx context.Context) (string, *models.Err) {
	uc := getUserContext(ctx)
	if uc == nil || uc.RootFileID == "" {
		return "", &models.Err{
			E: fmt.Errorf("根目录未配置，请联系管理员"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}
	return uc.RootFileID, nil
}

// GetBoundProviderIdsByOwnerID 通过 owner_id 查询 UserSpace 获取绑定的存储提供者
// 用于后台协程（无 context 的场景，如上传调度）及其他包调用
func GetBoundProviderIdsByOwnerID(ownerID string) ([]uint32, error) {
	return db.GetBoundProviderIdsByOwnerID(ownerID)
}
