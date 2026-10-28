package service

import (
	"context"
	"fmt"

	"github.com/jenken827/pfm-proto/proto/gen/native"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/pkg/llog"
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

// isAdminUser 检查当前用户是否为管理员（users.is_admin 字段，原 admin_users 表已移除）
func isAdminUser(ctx context.Context) bool {
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return false
	}
	return db.IsAdmin(userID)
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

// getBoundProviderIdsFromCtx 从 context 获取「管理员分配给当前用户的存储提供者」
// （UserSpace 级别，由 middleware/user_space.go 注入）。返回错误表示用户根目录未配置。
//
// 注意：这是「可用范围」（上限），不是「落盘目标」。落盘目标必须用
// resolveProviderIdsForFolder 按目录绑定解析。
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

// resolveProviderIdsForFolder 解析「当前用户在某目录下应落盘到哪些存储提供者」。
//
// 规则（storage.ResolveProviderIds）：目录自身绑定 → 沿祖先链向上继承 →
// 回退到用户分配集，最后与分配集取交集（越界绑定自动失效）。
func resolveProviderIdsForFolder(ctx context.Context, folderID string) ([]uint32, *models.Err) {
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return nil, &models.Err{
			E: fmt.Errorf("unauthorized"),
			C: native.RespCode_TOKEN_INVALID,
		}
	}
	ids, err := storage.ResolveProviderIds(userID, folderID)
	if err != nil {
		return nil, &models.Err{
			E: fmt.Errorf("根目录未配置，请联系管理员: %w", err),
			C: native.RespCode_PARAMS_ERROR,
		}
	}
	return ids, nil
}

// validateProviderIdsAssigned 校验目录绑定请求：必须至少 1 个，且全部落在
// 管理员分配给当前用户的范围内（这是原「文件夹绑定」被下线的直接原因——
// 旧实现只校验 provider 存在/启用，用户可绑定任意 provider）。
func validateProviderIdsAssigned(ctx context.Context, ids []uint32) *models.Err {
	if len(ids) == 0 {
		return &models.Err{
			E: fmt.Errorf("请至少选择一个存储提供者"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}
	assigned, mErr := getBoundProviderIdsFromCtx(ctx)
	if mErr != nil {
		return mErr
	}
	if out := storage.OutOfAssigned(ids, assigned); len(out) > 0 {
		return &models.Err{
			E: fmt.Errorf("存储提供者 %v 不在你的可用范围内", out),
			C: native.RespCode_PERMISSION_DENIED,
		}
	}
	return nil
}

// validateProvidersUsable 校验 provider 存在且启用（与「分配范围」校验配套，
// 防止管理员禁用/删除某个 provider 后，用户目录仍绑着它并上传失败）
func validateProvidersUsable(ctx context.Context, ids []uint32) *models.Err {
	providers, err := db.GetEnabledProvidersByIds(ids)
	if err != nil {
		llog.C(ctx).Errorf("query providers failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	if len(providers) != uniqueCount(ids) {
		return &models.Err{
			E: fmt.Errorf("部分存储提供者不存在或已被禁用"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}
	return nil
}

// uniqueCount 去重计数（用于 provider 校验时的数量比对）
func uniqueCount(ids []uint32) int {
	seen := make(map[uint32]struct{}, len(ids))
	for _, id := range ids {
		seen[id] = struct{}{}
	}
	return len(seen)
}

// inheritBoundProviderIds 计算「程序化新建的子目录」应继承的绑定
// （父目录绑定 → 沿祖先链向上 → 用户分配集），返回落库用 JSON 字符串。
// 用于后台/无界面路径建目录：相册编号子目录、mkdir -p、保存分享。
// 解析失败返回空串（= 未显式绑定，落库后由 upload 侧解析规则兜底）。
func inheritBoundProviderIds(ownerID, parentID string) string {
	ids, err := storage.ResolveProviderIds(ownerID, parentID)
	if err != nil {
		llog.L().Warnf("inherit bound provider ids failed (owner=%s, dir=%s): %v", ownerID, parentID, err)
		return ""
	}
	return storage.EncodeProviderIds(ids)
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
