package service

import (
	"context"
	"fmt"

	casdoorsdk "github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// 权限位掩码常量
const (
	PermRead   uint32 = 1 << 0 // 0001 - 查看/下载
	PermUpload uint32 = 1 << 1 // 0010 - 上传
	PermMkdir  uint32 = 1 << 2 // 0100 - 创建子目录
	PermDelete uint32 = 1 << 3 // 1000 - 删除
	PermMove   uint32 = 1 << 4 // 10000 - 移动
	PermRename uint32 = 1 << 5 // 100000 - 重命名

	PermAll     = PermRead | PermUpload | PermMkdir | PermDelete | PermMove | PermRename
	PermDefault = PermRead | PermUpload | PermMkdir
)

// CheckPermission 检查用户对指定文件/目录的权限
// 覆盖语义：找到最近的祖先权限记录即返回，不再继续向上合并
//
// Deprecated: 已弃用，改用 CheckOwnership / CheckOwnershipOrAdmin 基于 owner_id 的权限模型。
// 保留此函数用于向后兼容。
func CheckPermission(userID, fileID string, requiredPerm uint32) bool {
	currentID := fileID
	for currentID != "" && currentID != "0" {
		perm, err := db.GetDirectoryPermission(userID, currentID)
		if err == nil && perm != nil {
			// 找到权限记录，直接以它为准（无论是当前目录还是祖先）
			allowed := *perm.Permissions&requiredPerm == requiredPerm
			llog.L().Infof("CheckPermission: user=%s, file=%s, perm=%d, required=%d, allowed=%v", userID, currentID, *perm.Permissions, requiredPerm, allowed)
			return allowed
		}

		// 没有记录，继续向上找最近的祖先权限
		file, err := db.GetFileByID(currentID)
		if err != nil || file == nil {
			llog.L().Infof("CheckPermission: user=%s, file=%s, no file found", userID, currentID)
			return false
		}

		parentID := *file.ParentId
		if parentID == "" || parentID == "0" {
			llog.L().Infof("CheckPermission: user=%s, file=%s, reached root, no permission", userID, currentID)
			return false
		}

		currentID = parentID
	}
	llog.L().Infof("CheckPermission: user=%s, file=%s, empty fileID", userID, fileID)
	return false
}

// CheckOwnership 检查当前用户是否为文件所有者（或管理员）
// 从 context 获取当前用户 ID，检查 file.owner_id == userID
func CheckOwnership(ctx context.Context, fileID string) bool {
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return false
	}
	// 管理员跳过
	if db.IsAdmin(userID) {
		return true
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

// CheckOwnershipOrAdmin 检查当前用户是否为文件所有者或管理员
// 与 CheckOwnership 行为完全一致，保留语义别名
func CheckOwnershipOrAdmin(ctx context.Context, fileID string) bool {
	return CheckOwnership(ctx, fileID)
}

// CheckPermissionWithDetails 检查权限并返回详细信息（用于调试和日志）
func CheckPermissionWithDetails(userID, fileID string, requiredPerm uint32) (allowed bool, source string) {
	currentID := fileID
	for currentID != "" && currentID != "0" {
		perm, err := db.GetDirectoryPermission(userID, currentID)
		if err == nil && perm != nil {
			allowed := *perm.Permissions&requiredPerm == requiredPerm
			return allowed, fmt.Sprintf("matched:%s", currentID)
		}

		file, err := db.GetFileByID(currentID)
		if err != nil || file == nil {
			return false, "no_file"
		}

		parentID := *file.ParentId
		if parentID == "" || parentID == "0" {
			return false, "no_permission"
		}

		currentID = parentID
	}
	return false, "no_permission"
}

// GetUserAccessibleDirectories 获取用户可访问的目录列表
func GetUserAccessibleDirectories(userID string) (map[string]uint32, error) {
	perms, err := db.GetUserDirectoryPermissions(userID)
	if err != nil {
		return nil, err
	}

	result := make(map[string]uint32)
	for _, p := range perms {
		result[*p.FileId] = *p.Permissions
	}
	return result, nil
}

// GrantDirectoryPermission 授予目录权限
func GrantDirectoryPermission(ctx context.Context, req *native.GrantDirectoryPermissionReq, operator string) *models.Err {
	userID := *req.UserId
	fileID := *req.FileId
	permissions := *req.Permissions
	inherit := true
	if req.Inherit != nil {
		inherit = *req.Inherit
	}
	alias := ""
	if req.Alias != nil {
		alias = *req.Alias
	}

	file, err := db.GetFileByID(fileID)
	if err != nil || file == nil {
		return &models.Err{E: fmt.Errorf("file not found: %s", fileID), C: native.RespCode_NOT_FOUND}
	}

	if file.Type == nil || *file.Type != int32(native.FileType_FOLDER) {
		return &models.Err{E: fmt.Errorf("can only grant permissions on directories"), C: native.RespCode_PARAMS_ERROR}
	}

	// 校验同名冲突：获取用户所有权限，检查展示名是否重复
	if err := validateDisplayName(userID, fileID, *file.Name, alias); err != nil {
		return err
	}

	existing, _ := db.GetDirectoryPermission(userID, fileID)
	if existing != nil {
		oldPerms := *existing.Permissions
		existing.Permissions = &permissions
		existing.Inherit = &inherit
		existing.GrantedBy = &operator
		existing.Alias = &alias
		if err := db.UpdateDirectoryPermission(existing); err != nil {
			return &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}
		db.CreateAuditLog(&native.PermissionAuditLogORM{
			Operator:       &operator,
			TargetUser:     &userID,
			FileId:         &fileID,
			Action:         strPtr("UPDATE"),
			OldPermissions: &oldPerms,
			NewPermissions: &permissions,
		})
		return nil
	}

	perm := &native.DirectoryPermissionORM{
		UserId:      &userID,
		FileId:      &fileID,
		Permissions: &permissions,
		Inherit:     &inherit,
		GrantedBy:   &operator,
		Alias:       &alias,
	}
	if err := db.CreateDirectoryPermission(perm); err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	db.CreateAuditLog(&native.PermissionAuditLogORM{
		Operator:       &operator,
		TargetUser:     &userID,
		FileId:         &fileID,
		Action:         strPtr("GRANT"),
		NewPermissions: &permissions,
	})

	llog.L().Infof("granted permission: user=%s, file=%s, perms=%d, alias=%s, by=%s", userID, fileID, permissions, alias, operator)
	return nil
}

// validateDisplayName 校验授权目录的展示名是否与已有权限冲突
// 展示名 = alias（非空时）或 目录名
func validateDisplayName(userID, fileID, fileName, alias string) *models.Err {
	displayName := fileName
	if alias != "" {
		displayName = alias
	}

	perms, err := db.GetUserDirectoryPermissions(userID)
	if err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	for _, p := range perms {
		// 跳过自身（更新场景）
		if p.FileId != nil && *p.FileId == fileID {
			continue
		}

		// 获取已有权限的展示名
		var existingName string
		if p.Alias != nil && *p.Alias != "" {
			existingName = *p.Alias
		} else if p.FileId != nil {
			f, _ := db.GetFileByID(*p.FileId)
			if f != nil && f.Name != nil {
				existingName = *f.Name
			}
		}

		if existingName == displayName {
			return &models.Err{
				E: fmt.Errorf("展示名 \"%s\" 与已有授权目录冲突，请设置别名", displayName),
				C: native.RespCode_PARAMS_ERROR,
			}
		}
	}

	return nil
}

// RevokeDirectoryPermission 撤销目录权限
func RevokeDirectoryPermission(ctx context.Context, req *native.RevokeDirectoryPermissionReq, operator string) *models.Err {
	userID := *req.UserId
	fileID := *req.FileId

	existing, err := db.GetDirectoryPermission(userID, fileID)
	if err != nil || existing == nil {
		return &models.Err{E: fmt.Errorf("permission not found"), C: native.RespCode_NOT_FOUND}
	}

	oldPerms := *existing.Permissions
	if err := db.DeleteDirectoryPermission(userID, fileID); err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	db.CreateAuditLog(&native.PermissionAuditLogORM{
		Operator:       &operator,
		TargetUser:     &userID,
		FileId:         &fileID,
		Action:         strPtr("REVOKE"),
		OldPermissions: &oldPerms,
	})

	llog.L().Infof("revoked permission: user=%s, file=%s, by=%s", userID, fileID, operator)
	return nil
}

// UpdateDirectoryPermission 更新目录权限
func UpdateDirectoryPermission(ctx context.Context, req *native.UpdateDirectoryPermissionReq, operator string) *models.Err {
	userID := *req.UserId
	fileID := *req.FileId

	existing, err := db.GetDirectoryPermission(userID, fileID)
	if err != nil || existing == nil {
		return &models.Err{E: fmt.Errorf("permission not found"), C: native.RespCode_NOT_FOUND}
	}

	oldPerms := *existing.Permissions

	if req.Permissions != nil {
		existing.Permissions = req.Permissions
	}
	if req.Inherit != nil {
		existing.Inherit = req.Inherit
	}
	existing.GrantedBy = &operator

	if err := db.UpdateDirectoryPermission(existing); err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	db.CreateAuditLog(&native.PermissionAuditLogORM{
		Operator:       &operator,
		TargetUser:     &userID,
		FileId:         &fileID,
		Action:         strPtr("UPDATE"),
		OldPermissions: &oldPerms,
		NewPermissions: existing.Permissions,
	})

	llog.L().Infof("updated permission: user=%s, file=%s, by=%s", userID, fileID, operator)
	return nil
}

// GetUserPermissions 获取用户的目录权限列表
func GetUserPermissions(ctx context.Context, req *native.GetUserPermissionsReq) (*native.GetUserPermissionsResp, *models.Err) {
	userID := *req.UserId

	perms, err := db.GetUserDirectoryPermissions(userID)
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	result := &native.GetUserPermissionsResp{
		Permissions: make([]*native.DirectoryPermissionInfo, 0, len(perms)),
	}

	for _, p := range perms {
		var createdAt *timestamppb.Timestamp
		if p.CreatedAt != nil {
			createdAt = timestamppb.New(*p.CreatedAt)
		}
		info := &native.DirectoryPermissionInfo{
			Id:          p.Id,
			UserId:      p.UserId,
			FileId:      p.FileId,
			Permissions: p.Permissions,
			Inherit:     p.Inherit,
			GrantedBy:   p.GrantedBy,
			CreatedAt:   createdAt,
			Alias:       p.Alias,
		}

		if file, err := db.GetFileByID(*p.FileId); err == nil && file != nil {
			info.FileName = file.Name
		}

		result.Permissions = append(result.Permissions, info)
	}

	return result, nil
}

// GetDirectoryPermissions 获取目录的权限列表
func GetDirectoryPermissions(ctx context.Context, req *native.GetDirectoryPermissionsReq) (*native.GetDirectoryPermissionsResp, *models.Err) {
	fileID := *req.FileId

	perms, err := db.GetDirectoryPermissionsByFileID(fileID)
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	result := &native.GetDirectoryPermissionsResp{
		Permissions: make([]*native.DirectoryPermissionInfo, 0, len(perms)),
	}

	fileName := ""
	if file, err := db.GetFileByID(fileID); err == nil && file != nil && file.Name != nil {
		fileName = *file.Name
	}

	for _, p := range perms {
		var createdAt *timestamppb.Timestamp
		if p.CreatedAt != nil {
			createdAt = timestamppb.New(*p.CreatedAt)
		}
		info := &native.DirectoryPermissionInfo{
			Id:          p.Id,
			UserId:      p.UserId,
			FileId:      p.FileId,
			FileName:    &fileName,
			Permissions: p.Permissions,
			Inherit:     p.Inherit,
			GrantedBy:   p.GrantedBy,
			CreatedAt:   createdAt,
			Alias:       p.Alias,
		}
		result.Permissions = append(result.Permissions, info)
	}

	// 递归获取子目录权限
	if req.Recursive != nil && *req.Recursive {
		appendChildrenPermissions(fileID, result, "")
	}

	return result, nil
}

// appendChildrenPermissions 递归添加子目录权限
func appendChildrenPermissions(parentID string, result *native.GetDirectoryPermissionsResp, ownerID string) {
	children, err := db.GetFileChildren(parentID, ownerID)
	if err != nil {
		return
	}

	for _, child := range children {
		if child.Type == nil || *child.Type != int32(native.FileType_FOLDER) || child.Id == nil {
			continue
		}

		childPerms, err := db.GetDirectoryPermissionsByFileID(*child.Id)
		if err != nil {
			continue
		}

		childName := ""
		if child.Name != nil {
			childName = *child.Name
		}

		for _, p := range childPerms {
			var createdAt *timestamppb.Timestamp
			if p.CreatedAt != nil {
				createdAt = timestamppb.New(*p.CreatedAt)
			}
			info := &native.DirectoryPermissionInfo{
				Id:          p.Id,
				UserId:      p.UserId,
				FileId:      p.FileId,
				FileName:    &childName,
				Permissions: p.Permissions,
				Inherit:     p.Inherit,
				GrantedBy:   p.GrantedBy,
				CreatedAt:   createdAt,
				Alias:       p.Alias,
			}
			result.Permissions = append(result.Permissions, info)
		}

		// 递归处理子目录
		appendChildrenPermissions(*child.Id, result, ownerID)
	}
}

// BatchGrantPermission 批量授予权限
func BatchGrantPermission(ctx context.Context, req *native.BatchGrantPermissionReq, operator string) *models.Err {
	userID := *req.UserId
	permissions := *req.Permissions
	inherit := true
	if req.Inherit != nil {
		inherit = *req.Inherit
	}

	for _, fileID := range req.FileIds {
		grantReq := &native.GrantDirectoryPermissionReq{
			UserId:      &userID,
			FileId:      &fileID,
			Permissions: &permissions,
			Inherit:     &inherit,
		}
		if err := GrantDirectoryPermission(ctx, grantReq, operator); err != nil {
			llog.L().Errorf("batch grant failed for file %s: %v", fileID, err.E)
		}
	}

	if req.IncludeChildren != nil && *req.IncludeChildren {
		for _, fileID := range req.FileIds {
			grantChildrenRecursive(ctx, userID, fileID, permissions, inherit, operator)
		}
	}

	llog.L().Infof("batch granted permission: user=%s, files=%d, by=%s", userID, len(req.FileIds), operator)
	return nil
}

// grantChildrenRecursive 递归授予子目录权限
func grantChildrenRecursive(ctx context.Context, userID, parentID string, permissions uint32, inherit bool, operator string) {
	children, err := db.GetFileChildren(parentID, userID)
	if err != nil {
		return
	}

	for _, child := range children {
		if child.Type != nil && *child.Type == int32(native.FileType_FOLDER) && child.Id != nil {
			childID := *child.Id
			grantReq := &native.GrantDirectoryPermissionReq{
				UserId:      &userID,
				FileId:      &childID,
				Permissions: &permissions,
				Inherit:     &inherit,
			}
			if err := GrantDirectoryPermission(ctx, grantReq, operator); err != nil {
				llog.L().Errorf("batch grant children failed for %s: %v", childID, err.E)
			}
			grantChildrenRecursive(ctx, userID, childID, permissions, inherit, operator)
		}
	}
}

// GetAccessibleDirectories 获取用户可访问的目录列表
func GetAccessibleDirectories(ctx context.Context, req *native.GetAccessibleDirectoriesReq) (*native.GetAccessibleDirectoriesResp, *models.Err) {
	userID := ""
	if req.UserId != nil && *req.UserId != "" {
		userID = *req.UserId
	} else {
		return nil, &models.Err{E: fmt.Errorf("user_id is required"), C: native.RespCode_PARAMS_ERROR}
	}

	perms, err := db.GetUserDirectoryPermissions(userID)
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	result := &native.GetAccessibleDirectoriesResp{
		Directories: make([]*native.AccessibleDirectory, 0, len(perms)),
	}

	for _, p := range perms {
		dir := &native.AccessibleDirectory{
			FileId:      p.FileId,
			Permissions: p.Permissions,
		}

		if file, err := db.GetFileByID(*p.FileId); err == nil && file != nil {
			dir.FileName = file.Name
			dir.ParentId = file.ParentId
		}

		result.Directories = append(result.Directories, dir)
	}

	return result, nil
}

func strPtr(s string) *string { return &s }

// ListUsers 获取用户列表（通过 Casdoor API）
func ListUsers(ctx context.Context) (*native.ListUsersResp, *models.Err) {
	// 获取所有用户
	users, err := casdoorsdk.GetUsers()
	if err != nil {
		llog.L().Errorf("get casdoor users failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}

	// 获取所有角色（用于关联用户和角色）
	roles, err := casdoorsdk.GetRoles()
	if err != nil {
		llog.L().Warnf("get casdoor roles failed (non-fatal): %v", err)
		roles = nil
	}

	// 构建 user -> roles 映射
	userRoles := make(map[string][]string)
	if roles != nil {
		for _, role := range roles {
			if role.Users != nil {
				for _, userName := range role.Users {
					userRoles[userName] = append(userRoles[userName], role.Name)
				}
			}
		}
	}

	// 组装响应
	result := &native.ListUsersResp{}
	for _, u := range users {
		info := &native.CasdoorUserInfo{
			UserId:      &u.Name,
			DisplayName: strPtr(u.DisplayName),
			Email:       strPtr(u.Email),
			Avatar:      strPtr(u.Avatar),
			CreatedTime: strPtr(u.CreatedTime),
		}
		if r, ok := userRoles[u.Name]; ok {
			info.Roles = r
		}
		result.Users = append(result.Users, info)
	}

	return result, nil
}

// GetMyRoles 获取当前用户角色
func GetMyRoles(ctx context.Context) (*native.GetMyRolesResp, *models.Err) {
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return nil, &models.Err{E: fmt.Errorf("unauthorized"), C: native.RespCode_PARAMS_ERROR}
	}

	// 检查是否为管理员
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
