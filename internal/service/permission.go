package service

import (
	"context"
	"fmt"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/pkg/llog"
)

// CheckOwnership 检查当前用户是否为文件所有者。
//
// 权限模型：owner_id 隔离。管理员在此**不**享有特权（普通文件操作不能被管理员越权）。
// 管理功能请用 CheckOwnershipOrAdmin。
func CheckOwnership(ctx context.Context, fileID string) bool {
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return false
	}
	file, err := db.GetFileByID(fileID)
	if err != nil || file == nil {
		return false
	}
	if file.OwnerId != nil && *file.OwnerId == userID {
		return true
	}
	return false
}

// CheckOwnershipOrAdmin 检查当前用户是否为文件所有者或管理员（管理功能专用）
// 仅用于明确的管理场景：分享审查、备份任务跨用户读取等。
func CheckOwnershipOrAdmin(ctx context.Context, fileID string) bool {
	if isAdminUser(ctx) {
		return true
	}
	return CheckOwnership(ctx, fileID)
}

// ListUsers 获取用户列表（管理员用）
// 数据源：本地用户表（users），角色按 users.is_admin 判定（pfm_admin / pfm_user）
func ListUsers(ctx context.Context) (*native.ListUsersResp, *models.Err) {
	return listLocalUsers()
}

// listLocalUsers 从本地用户表列出用户
// display_name 回退为 username；角色按 users.is_admin 判定（pfm_admin / pfm_user）。
// 注：is_admin / avatar 已在 users 行上，直接读取，无需额外查询（避免 N+1）。
func listLocalUsers() (*native.ListUsersResp, *models.Err) {
	users, err := db.ListUsers()
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	result := &native.ListUsersResp{}
	for _, u := range users {
		info := &native.UserInfo{
			UserId:      u.UserId,
			DisplayName: u.Username,
		}
		if u.Email != nil && *u.Email != "" {
			info.Email = u.Email
		}
		if u.Avatar != nil && *u.Avatar != "" {
			info.Avatar = u.Avatar
		}
		if u.IsAdmin != nil && *u.IsAdmin {
			info.Roles = []string{"pfm_admin"}
		} else {
			info.Roles = []string{"pfm_user"}
		}
		if u.CreatedAt != nil {
			info.CreatedTime = strPtr(u.CreatedAt.Format("2006-01-02 15:04:05"))
		}
		result.Users = append(result.Users, info)
	}
	return result, nil
}

// strPtr 返回 string 的指针（proto optional 字段需要）。
func strPtr(s string) *string { return &s }

// GetMyRoles 获取当前用户角色
func GetMyRoles(ctx context.Context) (*native.GetMyRolesResp, *models.Err) {
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return nil, &models.Err{E: fmt.Errorf("unauthorized"), C: native.RespCode_PARAMS_ERROR}
	}

	isAdmin := isAdminUser(ctx)

	var roles []string
	if isAdmin {
		roles = []string{"pfm_admin"}
	} else {
		roles = []string{"pfm_user"}
	}

	return &native.GetMyRolesResp{
		Roles:   roles,
		IsAdmin: &isAdmin,
	}, nil
}

// GetMyBoundProviders 返回管理员分配给当前用户的存储提供者（仅启用的）。
//
// 用途：用户创建/设置文件夹时，只能在这些 provider 里选择绑定（至少 1 个）。
// 顺序沿用分配顺序（UserSpace.bound_provider_ids），已被禁用/删除的分配项不下发。
func GetMyBoundProviders(ctx context.Context) (*native.GetMyBoundProvidersResp, *models.Err) {
	assigned, mErr := getBoundProviderIdsFromCtx(ctx)
	if mErr != nil {
		return nil, mErr
	}

	providers, err := db.GetEnabledProvidersByIds(assigned)
	if err != nil {
		llog.C(ctx).Errorf("get assigned providers failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	byID := make(map[uint32]*entity.StorageProviderORM, len(providers))
	for _, p := range providers {
		if p.Id != nil {
			byID[*p.Id] = p
		}
	}

	items := make([]*native.StorageProvider, 0, len(assigned))
	for _, id := range assigned {
		p, ok := byID[id]
		if !ok {
			continue
		}
		pb, convErr := p.ToPB(ctx)
		if convErr != nil {
			llog.C(ctx).Warnf("convert provider %d to PB failed: %v", id, convErr)
			continue
		}
		items = append(items, pb)
	}

	return &native.GetMyBoundProvidersResp{Providers: items}, nil
}
