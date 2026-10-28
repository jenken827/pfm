package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	ormconv "github.com/jenken827/pfm/driver/orm"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/models"
	"github.com/jenken827/pfm/internal/storage"
	"github.com/jenken827/pfm/internal/thumbnail"
	"github.com/jenken827/pfm/pkg/llog"
	"github.com/jenken827/pfm/utils"
	"gorm.io/gorm"
)

// checkOwnership 检查当前用户是否拥有指定文件
//
// 注意：管理员在此**不**享有特权。管理员身份只在管理功能（目录权限授予、
// 用户管理、存储提供者管理、备份任务管理、分享审查）中生效，普通文件操作
// 必须是自己拥有的资源，不能借管理员身份操作他人文件。
func checkOwnership(ctx context.Context, fileID string) bool {
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return false
	}
	file, err := db.GetFileByID(fileID)
	if err != nil || file == nil {
		return false
	}
	return file.OwnerId != nil && *file.OwnerId == userID
}

// checkOwnershipBatch 批量校验归属，返回第一个不属于当前用户的 fileID。
//
// 用于替代循环调用 checkOwnership 造成的 N+1 查询：单条 SQL（id IN (...)）
// 取回全部记录后在内存比对。返回空字符串表示全部通过。
//
// 语义与 checkOwnership 保持一致：文件不存在 / 已被软删（GetFilesByIDs 默认
// 过滤软删）/ owner 不匹配 / 未登录，均视为校验失败（返回该 id）。
func checkOwnershipBatch(ctx context.Context, fileIDs []string) (string, *models.Err) {
	if len(fileIDs) == 0 {
		return "", nil
	}
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return fileIDs[0], &models.Err{
			E: errors.New("unauthorized"),
			C: native.RespCode_TOKEN_INVALID,
		}
	}

	// 去重，避免重复 id 带来的无谓比对
	uniq := make([]string, 0, len(fileIDs))
	seen := make(map[string]struct{}, len(fileIDs))
	for _, id := range fileIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}

	files, err := db.GetFilesByIDs(uniq)
	if err != nil {
		llog.C(ctx).Errorf("batch ownership query failed: %v", err)
		return "", &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	owned := make(map[string]struct{}, len(files))
	for _, f := range files {
		if f.Id != nil && f.OwnerId != nil && *f.OwnerId == userID {
			owned[*f.Id] = struct{}{}
		}
	}

	// 按传入顺序返回第一个无权限的 id（保证报错稳定可预期）
	for _, id := range fileIDs {
		if _, ok := owned[id]; !ok {
			return id, nil
		}
	}
	return "", nil
}

// GetFileList 获取文件列表
func GetFileList(ctx context.Context, req *native.FileListReq) ([]*entity.FileORM, int64, bool, *models.Err) {
	userID := getCurrentUserIDFromCtx(ctx)
	parentID := req.GetParentId()
	llog.C(ctx).Infof("GetFileList: user=%s, parentId=%s", userID, parentID)

	if userID == "" {
		return nil, 0, false, &models.Err{E: fmt.Errorf("unauthorized"), C: native.RespCode_PARAMS_ERROR}
	}

	// 查询根目录时，从 context 获取用户根目录 ID
	if parentID == "" || parentID == "0" {
		rootID, mErr := getRootFileIDFromCtx(ctx)
		if mErr != nil {
			return nil, 0, false, mErr
		}
		parentID = rootID
		llog.C(ctx).Infof("GetFileList: user=%s, resolved root to %s", userID, parentID)
	}

	// 非根目录：检查目录所有权
	if !checkOwnership(ctx, parentID) {
		llog.C(ctx).Infof("GetFileList: user=%s, ownership check failed for parentId=%s", userID, parentID)
		return nil, 0, false, &models.Err{
			E: fmt.Errorf("permission denied: not owner of directory %s", parentID),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	offset := req.GetOffset()
	pageSize := req.GetPageSize()

	files, total, hasMore, err := db.GetFileList(parentID, offset, pageSize, userID)
	if err != nil {
		llog.C(ctx).Errorf("GetFileList failed, err: %v", err)
		return nil, 0, false, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 文件夹大小展示已取消（子树遍历统计不划算，与主流网盘一致）；
	// 前端将文件夹行 size 显示为 "-"。用户总占用由容量异步更新器维护。

	llog.C(ctx).Infof("GetFileList: user=%s, parentId=%s, returned %d files", userID, parentID, len(files))
	return files, total, hasMore, nil
}

// GetFolderList 获取文件夹列表（用于移动文件选择目标文件夹）
func GetFolderList(ctx context.Context, req *native.FolderListReq) ([]*entity.FileORM, *models.Err) {
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return nil, &models.Err{E: fmt.Errorf("unauthorized"), C: native.RespCode_PARAMS_ERROR}
	}

	parentID := req.GetParentId()

	// 查询根目录时，从 context 获取用户根目录 ID
	if parentID == "" || parentID == "0" {
		rootID, mErr := getRootFileIDFromCtx(ctx)
		if mErr != nil {
			return nil, mErr
		}
		parentID = rootID
	}

	// 非根目录：检查目录所有权
	if !checkOwnership(ctx, parentID) {
		return nil, &models.Err{
			E: fmt.Errorf("permission denied: not owner of directory %s", parentID),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	folders, err := db.GetFolderList(parentID, userID)
	if err != nil {
		llog.C(ctx).Errorf("GetFolderList failed, err: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	return folders, nil
}

// CreateDir 创建文件夹
func CreateDir(ctx context.Context, req *native.CreateDirReq) (string, *models.Err) {
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return "", &models.Err{
			E: fmt.Errorf("unauthorized"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 查询根目录时，从 context 获取用户根目录 ID（与 GetFileList/GetFolderList 保持一致）
	parentID := req.GetParentId()
	if parentID == "" || parentID == "0" {
		rootID, mErr := getRootFileIDFromCtx(ctx)
		if mErr != nil {
			return "", mErr
		}
		parentID = rootID
	}

	// 所有权检查：检查对父目录的所有权
	if !checkOwnership(ctx, parentID) {
		return "", &models.Err{
			E: fmt.Errorf("permission denied: not owner of directory %s", parentID),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 处理加密设置：如果前端未传递，则从父文件夹继承
	enableEncryption := req.EnableEncryption
	if enableEncryption == nil {
		inherited, mErr := getInheritedEncryption(&parentID)
		if mErr != nil {
			return "", mErr
		}
		enableEncryption = &inherited
	}

	// 处理历史版本数：如果前端未传递，则从父文件夹继承（0 = 不保留历史）
	maxHistories := req.MaxHistories
	if maxHistories == nil {
		inherited := getInheritedMaxHistories(&parentID)
		maxHistories = &inherited
	}

	// 处理存储提供者绑定：显式选择（≥1 且 ⊆ 用户分配集）或继承父目录
	boundProviderIds, mErr := resolveCreateDirProviderIds(ctx, parentID, req.ProviderIds)
	if mErr != nil {
		return "", mErr
	}

	f := &entity.FileORM{
		Name:             req.Name,
		ParentId:         &parentID,
		Type:             utils.EnumPtr(native.FileType_FOLDER),
		IsReady:          utils.BoolPtr(true), // 目录没有内容要上传，永远是就绪的
		EnableEncryption: enableEncryption,
		MaxHistories:     maxHistories,
		BoundProviderIds: &boundProviderIds,
		OwnerId:          &userID,
	}
	e := db.CreateFile(f)
	if e != nil {
		return "", &models.Err{E: e, C: native.RespCode_DB_ERROR}
	}

	// 返回新创建的目录 ID
	if f.Id == nil {
		return "", &models.Err{E: errors.New("failed to get created directory ID"), C: native.RespCode_INTERNAL_ERROR}
	}
	return *f.Id, nil
}

// resolveCreateDirProviderIds 计算新建目录绑定的存储提供者（落库 JSON 数组字符串）：
//   - 显式传参（≥1 个且 ⊆ 用户分配集）→ 校验后原样使用；
//   - 未传 → 继承父目录绑定（父目录未显式绑定则回退用户分配集）。
func resolveCreateDirProviderIds(ctx context.Context, parentID string, requested []uint32) (string, *models.Err) {
	if len(requested) > 0 {
		if mErr := validateProviderIdsAssigned(ctx, requested); mErr != nil {
			return "", mErr
		}
		if mErr := validateProvidersUsable(ctx, requested); mErr != nil {
			return "", mErr
		}
		return storage.EncodeProviderIds(requested), nil
	}

	ids, mErr := resolveProviderIdsForFolder(ctx, parentID)
	if mErr != nil {
		return "", mErr
	}
	return storage.EncodeProviderIds(ids), nil
}

// getInheritedEncryption 获取继承的加密设置
// 从父文件夹继承加密属性
func getInheritedEncryption(parentID *string) (bool, *models.Err) {
	// 如果没有父文件夹，默认不加密
	if parentID == nil || *parentID == "" {
		return false, nil
	}

	// 查询父文件夹
	parentFile, err := db.GetFileByID(*parentID)
	if err != nil {
		llog.L().Warnf("get parent folder failed: %v, defaulting to no encryption", err)
		return false, nil
	}

	// 如果父文件夹没有设置加密属性，默认不加密
	if parentFile.EnableEncryption == nil {
		return false, nil
	}

	llog.L().Infof("inherited encryption from parent: %v, parent_id: %s", *parentFile.EnableEncryption, *parentID)
	return *parentFile.EnableEncryption, nil
}

// getInheritedMaxHistories 获取继承的历史版本数配置
// 从父文件夹继承；父目录记录缺失/查询失败/未配置时返回 0（不保留历史）
func getInheritedMaxHistories(parentID *string) uint32 {
	if parentID == nil || *parentID == "" {
		return 0
	}

	parent, err := db.GetFileByID(*parentID)
	if err != nil {
		llog.L().Warnf("get parent folder failed: %v, defaulting max_histories to 0", err)
		return 0
	}
	if parent.MaxHistories == nil {
		return 0
	}

	llog.L().Infof("inherited max_histories from parent: %d, parent_id: %s", *parent.MaxHistories, *parentID)
	return *parent.MaxHistories
}

// UpdateDir 更新文件夹
func UpdateDir(ctx context.Context, req *native.UpdateDirReq) *models.Err {
	// 归属校验：仅目录所有者（或管理员）可更新目录设置
	if req.Id == nil || !CheckOwnership(ctx, *req.Id) {
		return &models.Err{
			E: fmt.Errorf("permission denied: not owner of directory"),
			C: native.RespCode_PERMISSION_DENIED,
		}
	}

	orm := entity.FileORM{
		Id:               req.Id,
		Name:             req.Name,
		EnableEncryption: req.EnableEncryption,
		MaxHistories:     req.MaxHistories,
	}
	// 存储提供者绑定：显式更新时校验「≥1 且 ⊆ 用户分配集」；不传则保持原绑定
	if len(req.ProviderIds) > 0 {
		if mErr := validateProviderIdsAssigned(ctx, req.ProviderIds); mErr != nil {
			return mErr
		}
		if mErr := validateProvidersUsable(ctx, req.ProviderIds); mErr != nil {
			return mErr
		}
		encoded := storage.EncodeProviderIds(req.ProviderIds)
		orm.BoundProviderIds = &encoded
	}
	llog.C(ctx).Infof("update dir req:%+v", req)

	if err := db.UpdateDir(&orm); err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	return nil
}

// DeleteFile 删除文件（软删除，移至回收站）
// 删除文件夹时递归软删除所有子文件和子目录
func DeleteFile(ctx context.Context, req *native.DeleteFileReq) *models.Err {
	ids := req.Ids
	if len(ids) == 0 {
		return nil
	}

	// 所有权检查：批量校验（单条 SQL，避免逐文件查询的 N+1）
	if badID, mErr := checkOwnershipBatch(ctx, ids); mErr != nil {
		return mErr
	} else if badID != "" {
		return &models.Err{
			E: fmt.Errorf("permission denied: not owner of file %s", badID),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 递归获取所有子文件和子文件夹的ID
	allChildrenIDs, err := db.GetAllChildrenIDs(ids)
	if err != nil {
		llog.C(ctx).Errorf("get all children IDs failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 合并指定的文件ID和所有子文件ID
	allIDs := append(ids, allChildrenIDs...)

	if err := db.DeleteFiles(allIDs); err != nil {
		llog.C(ctx).Errorf("DeleteFiles failed, err: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// WebDAV 账号绑定目录被删除（进回收站）→ 自动禁用对应账号
	DisableWebdavAccountsForDeletedRoots(ctx, allIDs)

	llog.C(ctx).Infof("deleted %d files (including %d children)", len(allIDs), len(allChildrenIDs))
	// 容量统计说明：软删后文件进入回收站，used_bytes 口径含回收站（物理仍占用），
	// 故无需重算用户占用；文件夹大小已取消统计，无需标记目录。
	return nil
}

// RenameFile 重命名文件
func RenameFile(ctx context.Context, req *native.RenameFileReq) *models.Err {
	// 所有权检查：批量校验（单条 SQL，避免逐文件查询的 N+1）
	ids := make([]string, 0, len(req.RenameObjs))
	for _, obj := range req.RenameObjs {
		ids = append(ids, obj.GetId())
	}
	if badID, mErr := checkOwnershipBatch(ctx, ids); mErr != nil {
		return mErr
	} else if badID != "" {
		return &models.Err{
			E: fmt.Errorf("permission denied: not owner of file %s", badID),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	if err := db.RenameFiles(req.RenameObjs); err != nil {
		llog.C(ctx).Errorf("RenameFiles failed, err: %v", err)
		return &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	return nil
}

// GetAvailableProvidersForSync 获取可用于同步的存储提供者
//
// 返回：已启用的提供者，排除文件已存在且完成的提供者。
// 范围限定为「文件所在目录的有效绑定」（目录绑定 → 祖先继承 → 用户分配集），
// 与 SyncFileToProviders 的校验口径一致，避免前端列出用户无权同步的 provider。
func GetAvailableProvidersForSync(ctx context.Context, fileHash string) ([]*native.StorageProvider, *models.Err) {
	// 1. Validate file hash
	if fileHash == "" {
		return nil, &models.Err{
			E: errors.New("file hash is required"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 2. 定位当前用户名下该 hash 的文件，确定其所在目录
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return nil, &models.Err{E: errors.New("unauthorized"), C: native.RespCode_TOKEN_INVALID}
	}
	file, err := db.GetFileByHashAndOwner(fileHash, userID)
	if err != nil || file == nil {
		return nil, &models.Err{
			E: errors.New("file not found"),
			C: native.RespCode_NOT_FOUND,
		}
	}
	allowed, mErr := resolveProviderIdsForFolder(ctx, strPtrVal(file.ParentId))
	if mErr != nil {
		return nil, mErr
	}
	allowedSet := make(map[uint32]struct{}, len(allowed))
	for _, id := range allowed {
		allowedSet[id] = struct{}{}
	}

	// 3. Query available providers from database（已是「该 hash 尚无完整副本」的启用 provider）
	providers, err := db.GetAvailableProvidersForSync(fileHash)
	if err != nil {
		llog.C(ctx).Errorf("get available providers failed: %v", err)
		return nil, &models.Err{
			E: err,
			C: native.RespCode_DB_ERROR,
		}
	}

	// 4. Convert ORM to PB（按目录绑定过滤）
	items := make([]*native.StorageProvider, 0, len(providers))
	for _, p := range providers {
		if p.Id == nil {
			continue
		}
		if _, ok := allowedSet[*p.Id]; !ok {
			continue
		}
		pb, err := p.ToPB(ctx)
		if err != nil {
			llog.C(ctx).Errorf("convert provider to PB failed: %v", err)
			continue
		}
		stripProviderSecrets(pb)
		items = append(items, pb)
	}

	return items, nil
}

// RestoreFromTrash 从回收站恢复文件
// 支持指定目标目录，自动处理同名冲突，文件夹递归还原子文件
func RestoreFromTrash(ctx context.Context, req *native.RestoreFromTrashReq) *models.Err {
	if len(req.Ids) == 0 {
		return nil
	}

	targetParentID := ""
	if req.TargetParentId != nil && *req.TargetParentId != "" {
		targetParentID = *req.TargetParentId
	}

	// 获取要恢复的文件（包含软删除的）
	selectedFiles, err := db.GetFilesByIDsUnscoped(req.Ids)
	if err != nil {
		llog.L().Errorf("get files by IDs failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	if len(selectedFiles) == 0 {
		return &models.Err{E: fmt.Errorf("no files found"), C: native.RespCode_PARAMS_ERROR}
	}

	// 所有权检查：仅文件所有者可从回收站恢复（管理员不可越权操作他人回收站）
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return &models.Err{E: fmt.Errorf("unauthorized"), C: native.RespCode_PERMISSION_DENIED}
	}
	for _, file := range selectedFiles {
		if strPtrVal(file.OwnerId) != userID {
			return &models.Err{
				E: fmt.Errorf("permission denied: not owner of file %s", strPtrVal(file.Id)),
				C: native.RespCode_PERMISSION_DENIED,
			}
		}
	}

	// 收集所有需要恢复的文件ID
	// selectedRestore: 用户选中的文件（可能改名/改父目录）
	// childRestore: 文件夹下的子文件（只清 deleted_at，保持原 parent_id）
	type restoreItem struct {
		file        *entity.FileORM
		newParentID string // 空表示保持原 parent_id
		newName     string // 空表示保持原名
	}
	selectedRestore := make([]restoreItem, 0, len(selectedFiles))
	childIDs := make([]string, 0)

	for _, file := range selectedFiles {
		if file.Id == nil || file.DeletedAt == nil {
			continue // 跳过未删除的文件
		}

		// 确定目标父目录
		parentID := targetParentID
		if parentID == "" {
			// 未指定目标，使用原 parent_id
			if file.ParentId != nil {
				parentID = *file.ParentId
			}
		}

		// 检查目标父目录是否存在且未被删除
		if parentID != "" && parentID != "0" {
			parentFile, err := db.GetFileByID(parentID)
			if err != nil || parentFile == nil || parentFile.DeletedAt != nil {
				// 父目录不存在或仍在回收站，无法还原
				return &models.Err{
					E: fmt.Errorf("target directory (id: %s) does not exist or is in trash, please specify a valid target", parentID),
					C: native.RespCode_PARAMS_ERROR,
				}
			}
		}

		// 检查同名冲突并自动重命名
		newName := ""
		if file.Name != nil {
			fileOwnerID := ""
			if file.OwnerId != nil {
				fileOwnerID = *file.OwnerId
			}
			newName = resolveNameConflict(parentID, *file.Name, fileOwnerID)
		}

		selectedRestore = append(selectedRestore, restoreItem{
			file:        file,
			newParentID: parentID,
			newName:     newName,
		})

		// 如果是文件夹，递归收集所有子文件
		if file.Type != nil && *file.Type == int32(native.FileType_FOLDER) {
			children, err := db.GetAllChildrenIDs([]string{*file.Id})
			if err != nil {
				llog.L().Errorf("get children IDs failed: %v", err)
				return &models.Err{E: err, C: native.RespCode_DB_ERROR}
			}
			childIDs = append(childIDs, children...)
		}
	}

	// 在事务中执行恢复
	err = db.Transaction(func(tx *gorm.DB) error {
		ctx := context.Background()

		// 1. 恢复用户选中的文件（可能改名/改父目录）
		for _, item := range selectedRestore {
			updates := map[string]interface{}{
				"deleted_at": nil,
			}
			if item.newParentID != "" {
				updates["parent_id"] = item.newParentID
			}
			if item.newName != "" {
				updates["name"] = item.newName
			}
			if err := tx.WithContext(ctx).Unscoped().Model(&entity.FileORM{}).
				Where("id = ?", *item.file.Id).
				Updates(updates).Error; err != nil {
				return fmt.Errorf("restore file %s failed: %w", *item.file.Id, err)
			}
		}

		// 2. 恢复子文件（只清 deleted_at，保持原 parent_id 和 name）
		if len(childIDs) > 0 {
			if err := tx.WithContext(ctx).Unscoped().Model(&entity.FileORM{}).
				Where("id IN ?", childIDs).
				Update("deleted_at", nil).Error; err != nil {
				return fmt.Errorf("restore children failed: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		llog.L().Errorf("restore from trash failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	totalRestored := len(selectedRestore) + len(childIDs)
	llog.L().Infof("restored %d files from trash (selected: %d, children: %d)", totalRestored, len(selectedRestore), len(childIDs))
	// 容量统计说明：恢复只是清 deleted_at，used_bytes 口径含回收站（物理一直占用），
	// 无需重算用户占用；文件夹大小已取消统计。
	return nil
}

// resolveNameConflict 检查目标目录中是否有同名文件，有则自动重命名
// 返回新名称（冲突时），无冲突返回空字符串。实现收敛在 db.ResolveNameConflict，
// 离线下载导入等后台路径共用同一套命名规则。
func resolveNameConflict(parentID, name, ownerID string) string {
	return db.ResolveNameConflict(parentID, name, ownerID)
}

// batchDeleteFromProvider 从单个存储提供者批量删除文件位置
// 返回：成功删除的 location ID 列表、删除失败的 location ID 列表、删除的总大小
func batchDeleteFromProvider(
	ctx context.Context,
	providerID uint32,
	locations []*entity.FileLocationORM,
) (deletedIDs []uint64, failedIDs []uint64, deletedSize uint64) {
	st := storage.GetStorage(providerID)
	if st == nil {
		llog.C(ctx).Warnf("storage provider not found, id: %d", providerID)
		for _, loc := range locations {
			if loc.Id != nil {
				failedIDs = append(failedIDs, *loc.Id)
			}
		}
		return
	}

	batchDeleter, ok := st.(types.BatchDeleter)
	if !ok {
		llog.C(ctx).Warnf("provider %d does not support BatchDeleter, skipping", providerID)
		for _, loc := range locations {
			if loc.Id != nil {
				failedIDs = append(failedIDs, *loc.Id)
			}
		}
		return
	}

	results, err := batchDeleter.BatchDelete(ctx, ormconv.FileRefsFromLocations(locations))

	// 全部成功
	if results == nil && err == nil {
		llog.C(ctx).Infof("batch deleted all %d files from provider %d", len(locations), providerID)
		for _, loc := range locations {
			if loc.Id != nil {
				deletedIDs = append(deletedIDs, *loc.Id)
			}
			if loc.ChunkSize != nil && *loc.Status == int32(native.LocationStatus_PCOMPLETED) {
				deletedSize += *loc.ChunkSize
			}
		}
		return
	}

	// 全部失败
	if results == nil && err != nil {
		llog.C(ctx).Errorf("batch delete all failed: %v, provider_id: %d", err, providerID)
		for _, loc := range locations {
			if loc.Id != nil {
				failedIDs = append(failedIDs, *loc.Id)
			}
		}
		return
	}

	// 部分成功
	llog.C(ctx).Infof("batch delete partial success: %d/%d from provider %d",
		countSuccess(results), len(results), providerID)
	for i, result := range results {
		if i >= len(locations) {
			break
		}
		loc := locations[i]
		if result.Success {
			if loc.Id != nil {
				deletedIDs = append(deletedIDs, *loc.Id)
			}
			if loc.ChunkSize != nil && *loc.Status == int32(native.LocationStatus_PCOMPLETED) {
				deletedSize += *loc.ChunkSize
			}
		} else {
			if loc.Id != nil {
				failedIDs = append(failedIDs, *loc.Id)
			}
			// 驱动标记为 Permanent 的失败（远端硬限制 / 参数错误）降为 warning，
			// 其余保持 error 让监控感知；判断完全由驱动给出，宿主不认识具体平台。
			if result.Permanent || types.IsPermanent(result.Error) {
				llog.C(ctx).Warnf("delete location %d failed (will keep record): %v", *loc.Id, result.Error)
			} else {
				llog.C(ctx).Errorf("delete location %d failed: %v", *loc.Id, result.Error)
			}
		}
	}
	return
}

// deleteFilesFromProviders 从存储提供者批量删除文件（内部函数）
// 按 provider_id 分组后批量删除，提高效率
// fileIDs: 当前正在删除的文件 ID 列表（用于引用检查，避免误删被其他文件共享的 file_location）
func deleteFilesFromProviders(ctx context.Context, fileIDs []string) {
	if len(fileIDs) == 0 {
		return
	}
	llog.C(ctx).Infof("delete files from providers, id length: %d", len(fileIDs))

	// 1. 批量获取文件信息（包含软删除的记录，因为可能是回收站文件）
	files, err := db.GetFilesByIDsUnscoped(fileIDs)
	if err != nil {
		llog.C(ctx).Errorf("batch get files failed: %v", err)
		return
	}

	// 2. 收集文件hash，用于查询位置记录
	fileHashMap := make(map[string]string) // fileID -> fileHash
	hashSet := make(map[string]bool)       // 用于去重查询
	for _, file := range files {
		if file.Id != nil && file.Hash != nil && *file.Hash != "" {
			fileHashMap[*file.Id] = *file.Hash
			hashSet[*file.Hash] = true
			llog.C(ctx).Debugf("get file hash success %s", *file.Hash)
		}
	}

	// 3. 检查每个 hash 是否还有其他非删除文件引用
	// 被其他文件引用的 hash 不应删除其 file_location 记录
	hashesWithRefs := make(map[string]bool) // 仍有其他文件引用的 hash
	for fileHash := range hashSet {
		refCount, err := db.CountActiveFilesByHashExcludingIDs(fileHash, fileIDs)
		if err != nil {
			llog.C(ctx).Errorf("count active files by hash failed: %v, hash: %s", err, fileHash)
			// 查询失败时保守处理，不删除该 hash 的 file_location
			hashesWithRefs[fileHash] = true
			continue
		}
		if refCount > 0 {
			llog.C(ctx).Infof("hash %s still referenced by %d other files, skip file_location deletion", fileHash, refCount)
			hashesWithRefs[fileHash] = true
		}
	}

	// 4. 批量获取所有文件位置记录，按 provider_id 分组
	providerLocations := make(map[uint32][]*entity.FileLocationORM) // providerID -> locations
	for fileHash := range hashSet {
		locations, err := db.GetFileLocationsByBlake3Hash(fileHash)
		if err != nil {
			llog.C(ctx).Errorf("get file locations failed: %v, hash: %s", err, fileHash)
			continue
		}
		llog.C(ctx).Debugf("success get file location by hash: %s, locations len: %d", fileHash, len(locations))

		for _, loc := range locations {
			if loc.ProviderId == nil {
				llog.C(ctx).Debugf("location provider id is nil, location id: %d", loc.Id)
				continue
			}
			providerID := *loc.ProviderId
			providerLocations[providerID] = append(providerLocations[providerID], loc)
		}
	}
	llog.C(ctx).Infof("delete files from providers, provider len: %d", len(providerLocations))

	// 5. 按 provider_id 批量删除文件（跳过仍有其他文件引用的 hash）
	providerDeletedSizes := make(map[uint32]uint64)
	var allDeletedIDs, allFailedIDs []uint64

	for providerID, locations := range providerLocations {
		// 过滤掉仍有其他文件引用的 location
		var locationsToDelete []*entity.FileLocationORM
		for _, loc := range locations {
			if loc.Blake3Hash != nil && hashesWithRefs[*loc.Blake3Hash] {
				llog.C(ctx).Debugf("skip deleting location %d for hash %s (still referenced by other files)", loc.Id, *loc.Blake3Hash)
				continue
			}
			locationsToDelete = append(locationsToDelete, loc)
		}

		if len(locationsToDelete) == 0 {
			continue
		}

		llog.C(ctx).Infof("delete files loop providerlocations, providerID: %d, locations: %d", providerID, len(locationsToDelete))
		deletedIDs, failedIDs, deletedSize := batchDeleteFromProvider(ctx, providerID, locationsToDelete)
		allDeletedIDs = append(allDeletedIDs, deletedIDs...)
		allFailedIDs = append(allFailedIDs, failedIDs...)
		if deletedSize > 0 {
			providerDeletedSizes[providerID] = deletedSize
		}
	}

	// 6. 更新存储提供者的已使用容量
	for providerID, size := range providerDeletedSizes {
		if err := db.DecrementUsedCapacity(providerID, size); err != nil {
			llog.C(ctx).Errorf("decrement used capacity failed: %v, provider_id: %d", err, providerID)
		}
	}

	// 7. 批量删除成功删除的文件位置记录
	if len(allDeletedIDs) > 0 {
		if err := db.DeleteFileLocations(allDeletedIDs); err != nil {
			llog.C(ctx).Errorf("batch delete file locations failed: %v", err)
		}
	}

	// 8. 更新删除失败的文件位置记录状态
	if len(allFailedIDs) > 0 {
		if err := db.UpdateFileLocationStatus(allFailedIDs, int32(native.LocationStatus_PDELETED_FAILED)); err != nil {
			llog.C(ctx).Errorf("update file location status to DELETED_FAILED failed: %v", err)
		} else {
			llog.C(ctx).Warnf("marked %d file locations as DELETED_FAILED", len(allFailedIDs))
		}
	}
}

// countSuccess 计算删除结果中成功的数量
func countSuccess(results []types.DeleteResult) int {
	count := 0
	for _, r := range results {
		if r.Success {
			count++
		}
	}
	return count
}

// DeletePermanently 彻底删除文件（从回收站永久删除）
func DeletePermanently(ctx context.Context, req *native.DeletePermanentlyReq) *models.Err {
	if len(req.Ids) == 0 {
		return nil
	}

	// 所有权检查：物理删除不可逆，逐个校验（与 DeleteFile 同风格）。
	// 目标是回收站软删文件，checkOwnership 默认作用域查不到会误判，
	// 因此用 Unscoped 查询后比对 owner_id。管理员不可越权删除他人文件。
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return &models.Err{E: fmt.Errorf("unauthorized"), C: native.RespCode_PERMISSION_DENIED}
	}
	files, err := db.GetFilesByIDsUnscoped(req.Ids)
	if err != nil {
		llog.C(ctx).Errorf("get files by IDs failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	ownerByFileID := make(map[string]string, len(files))
	for _, f := range files {
		if f.Id != nil {
			ownerByFileID[*f.Id] = strPtrVal(f.OwnerId)
		}
	}
	for _, id := range req.Ids {
		if ownerByFileID[id] != userID {
			return &models.Err{
				E: fmt.Errorf("permission denied: not owner of file %s", id),
				C: native.RespCode_PERMISSION_DENIED,
			}
		}
	}

	// 复制 IDs 避免异步访问问题
	fileIDs := make([]string, len(req.Ids))
	copy(fileIDs, req.Ids)

	// 获取所有子文件和子文件夹的ID（包括软删除的）
	allChildrenIDs, err := db.GetAllChildrenIDs(fileIDs)
	if err != nil {
		llog.C(ctx).Errorf("get all children IDs failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 合并指定的文件ID和所有子文件ID
	allFileIDs := append(fileIDs, allChildrenIDs...)
	llog.C(ctx).Infof("permanently deleting %d files (including %d children)", len(allFileIDs), len(allChildrenIDs))

	permanentlyDeleteFiles(ctx, allFileIDs, "permanently deleted")

	// 容量统计：行真正删除后用户占用减少
	MarkFileCapacityChanged(getCurrentUserIDFromCtx(ctx))
	return nil
}

// RestoreFileHistory 恢复文件到某个历史版本
// 行为：
//   - req.NewName 为空：覆盖当前文件（当前内容自动写入历史，不丢失；CAS 防并发覆盖冲突）
//   - req.NewName 非空：以新名称在文件所在目录创建新文件（秒传，物理数据按 hash 共享），不覆盖当前文件
//
// 历史版本的 hash 若已无可用存储位置（曾被清理），返回错误提示
func RestoreFileHistory(ctx context.Context, req *native.RestoreFileHistoryReq) *models.Err {
	if req.HistoryId == nil || *req.HistoryId == 0 {
		return &models.Err{E: errors.New("history_id is required"), C: native.RespCode_PARAMS_ERROR}
	}

	// 1. 查历史记录
	history, err := db.GetFileHistoryByID(*req.HistoryId)
	if err != nil {
		llog.C(ctx).Errorf("get file history failed, id: %d, err: %v", *req.HistoryId, err)
		return &models.Err{E: errors.New("history not found"), C: native.RespCode_NOT_FOUND}
	}
	if history.FileId == nil || history.Hash == nil || *history.Hash == "" {
		return &models.Err{E: errors.New("history record incomplete"), C: native.RespCode_PARAMS_ERROR}
	}
	fileID := *history.FileId
	targetHash := *history.Hash
	targetSize := uint64(0)
	if history.Size != nil {
		targetSize = *history.Size
	}

	// 2. 归属校验：仅文件所有者（或管理员）可恢复
	if !checkOwnership(ctx, fileID) {
		return &models.Err{E: errors.New("permission denied"), C: native.RespCode_PERMISSION_DENIED}
	}

	// 3. 读取当前文件记录
	file, err := db.GetFileByID(fileID)
	if err != nil || file == nil {
		llog.C(ctx).Errorf("get file failed, id: %s, err: %v", fileID, err)
		return &models.Err{E: errors.New("file not found"), C: native.RespCode_NOT_FOUND}
	}
	if file.Type != nil && *file.Type == int32(native.FileType_FOLDER) {
		return &models.Err{E: errors.New("cannot restore history to a folder"), C: native.RespCode_PARAMS_ERROR}
	}

	// 4. 校验历史 hash 在存储中仍有可用位置（content-address 共享存储：
	// 历史版本的物理数据可能已被去重清理/覆盖回收，此时无法恢复）
	locations, err := db.GetFileLocationsByBlake3Hash(targetHash)
	if err != nil {
		llog.C(ctx).Errorf("get locations for history hash failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	if !hasUsableLocation(locations) {
		llog.C(ctx).Warnf("history hash has no usable location, hash: %s, history_id: %d", targetHash, *req.HistoryId)
		return &models.Err{
			E: errors.New("历史版本内容已被清理，无法恢复（请尝试先同步/重新上传）"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	newName := ""
	if req.NewName != nil {
		newName = strings.TrimSpace(*req.NewName)
	}

	// 5a. 覆盖当前文件：CAS 版本仲裁（后到者赢），当前内容写入历史
	if newName == "" {
		if file.Hash != nil && *file.Hash == targetHash {
			return nil // 已经是该版本，无需操作
		}
		myVersion := AllocateFileVersion(strPtrVal(file.OwnerId), strPtrVal(file.ParentId), strPtrVal(file.Name))
		ctx = withUploadVersion(ctx, myVersion)
		f := &entity.FileORM{
			Id:      file.Id,
			Hash:    &targetHash,
			Size:    &targetSize,
			IsReady: utils.BoolPtr(true),
		}
		taken, updateErr := db.UpdateFileWithHistoryCAS(ctx, f, file, myVersion)
		if updateErr != nil {
			llog.C(ctx).Errorf("restore history (overwrite) failed: %v", updateErr)
			return &models.Err{E: updateErr, C: native.RespCode_DB_ERROR}
		}
		if !taken {
			return &models.Err{
				E: errors.New("文件已被其他操作更新，请刷新后重试"),
				C: native.RespCode_ALREADY_EXISTS,
			}
		}
		llog.C(ctx).Infof("file restored to history version, file_id: %s, history_id: %d, hash: %s",
			fileID, *req.HistoryId, targetHash)
		return nil
	}

	// 5b. 以新名称创建副本（秒传：数据已存在，仅新增目录条目），不覆盖当前文件
	if file.ParentId == nil || file.Name == nil {
		return &models.Err{E: errors.New("file record incomplete"), C: native.RespCode_PARAMS_ERROR}
	}
	ownerID := strPtrVal(file.OwnerId)
	if ownerID == "" {
		ownerID = getCurrentUserIDFromCtx(ctx)
	}
	parentID := *file.ParentId

	// 目标目录存在性/归属校验（创建记录前必须确认）
	if parentID != "" {
		parentDir, err := db.GetFileByID(parentID)
		if err != nil || parentDir == nil {
			return &models.Err{E: errors.New("target directory not found"), C: native.RespCode_NOT_FOUND}
		}
		if parentDir.OwnerId == nil || *parentDir.OwnerId != ownerID {
			return &models.Err{E: errors.New("permission denied: target directory not owned by you"), C: native.RespCode_PERMISSION_DENIED}
		}
	}

	// 重名自动加后缀（与相册归档 / 移动冲突处理一致）
	finalName := resolveNameConflict(parentID, newName, ownerID)
	if finalName == "" {
		finalName = newName
	}

	// 类型/扩展名按新文件名判定（同名恢复时与原文件类型一致）
	ftype, mime := GetFileType("", finalName)
	myVersion := AllocateFileVersion(ownerID, parentID, finalName)
	newFile := &entity.FileORM{
		Name:     &finalName,
		ParentId: &parentID,
		Size:     &targetSize,
		Hash:     &targetHash,
		Type:     &ftype,
		Mime:     &mime,
		IsReady:  utils.BoolPtr(true), // 秒传：内容已在存储
		OwnerId:  &ownerID,
		Version:  &myVersion,
	}
	if err := db.CreateFile(newFile); err != nil {
		llog.C(ctx).Errorf("restore history (new name) create file failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	llog.C(ctx).Infof("file history restored as new file, history_id: %d, new_file_id: %s, name: %s",
		*req.HistoryId, strPtrVal(newFile.Id), finalName)
	return nil
}

// hasUsableLocation 判断历史 hash 是否至少有一个可用的（已上传完成/已缓存）存储位置
func hasUsableLocation(locations []*entity.FileLocationORM) bool {
	for _, loc := range locations {
		if loc == nil || loc.Status == nil {
			continue
		}
		st := native.LocationStatus(*loc.Status)
		if st == native.LocationStatus_PCOMPLETED {
			return true
		}
	}
	return false
}

// deleteFileHistories 删除文件的历史记录和对应的存储提供者上的文件
func deleteFileHistories(ctx context.Context, fileIDs []string) {
	if len(fileIDs) == 0 {
		return
	}
	llog.C(ctx).Infof("delete file history ids length: %d", len(fileIDs))
	// 批量获取所有文件的历史记录
	allHistories := make([]*entity.FileHistoryORM, 0)
	for _, fileID := range fileIDs {
		histories, err := db.GetFileHistoryByFileID(fileID)
		if err != nil {
			llog.C(ctx).Errorf("get file history failed, file_id: %s, err: %v", fileID, err)
			continue
		}
		allHistories = append(allHistories, histories...)
	}

	if len(allHistories) == 0 {
		llog.C(ctx).Infof("no file history need delete")
		return
	}

	// 从存储提供者删除历史文件并更新容量
	deleteHistoryFromProviders(ctx, allHistories)

	// 删除数据库中的历史记录
	if err := db.DeleteFileHistoryByFileIDs(fileIDs); err != nil {
		llog.C(ctx).Errorf("delete file histories failed: %v", err)
	} else {
		llog.C(ctx).Infof("deleted %d file history records", len(allHistories))
	}
}

// DeleteFileHistory 删除文件历史版本
func DeleteFileHistory(ctx context.Context, ids []uint64) *models.Err {
	if len(ids) == 0 {
		return nil
	}

	// 批量获取历史记录（单条 SQL，避免逐条查询的 N+1）
	histories, err := db.GetFileHistoriesByIDs(ids)
	if err != nil {
		llog.C(ctx).Errorf("get file histories failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	if len(histories) == 0 {
		return nil
	}

	// 归属校验：批量取出涉及的 file_id 后一次性校验（同样避免 N+1）
	fileIDs := make([]string, 0, len(histories))
	for _, h := range histories {
		if h.FileId != nil {
			fileIDs = append(fileIDs, *h.FileId)
		}
	}
	if badID, mErr := checkOwnershipBatch(ctx, fileIDs); mErr != nil {
		return mErr
	} else if badID != "" {
		return &models.Err{
			E: fmt.Errorf("permission denied: not owner of file %s", badID),
			C: native.RespCode_PERMISSION_DENIED,
		}
	}

	// 从存储提供者删除历史文件并更新容量
	deleteHistoryFromProviders(ctx, histories)

	// 删除数据库中的历史记录
	if err := db.DeleteFileHistoryByIDs(ids); err != nil {
		llog.C(ctx).Errorf("delete file history failed, err: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	llog.C(ctx).Infof("deleted %d file history records", len(histories))
	return nil
}

// deleteHistoryFromProviders 从存储提供者删除历史文件（公共逻辑）
// 收集 hash -> 查询 FileLocation -> 按 provider 分组 -> 批量删除 -> 更新容量 -> 删除 FileLocation 记录
func deleteHistoryFromProviders(ctx context.Context, histories []*entity.FileHistoryORM) {
	// 收集所有历史记录的 hash
	hashSet := make(map[string]bool)
	for _, history := range histories {
		if history.Hash != nil && *history.Hash != "" {
			hashSet[*history.Hash] = true
		}
	}

	// 根据 hash 批量获取文件位置记录，按 provider 分组
	providerLocations := make(map[uint32][]*entity.FileLocationORM)
	for hash := range hashSet {
		locations, err := db.GetFileLocationsByBlake3Hash(hash)
		if err != nil {
			llog.C(ctx).Errorf("get file locations by hash failed, hash: %s, err: %v", hash, err)
			continue
		}

		for _, loc := range locations {
			if loc.ProviderId == nil {
				continue
			}
			providerLocations[*loc.ProviderId] = append(providerLocations[*loc.ProviderId], loc)
		}
	}

	// 按 provider 批量删除并记录容量变化
	providerDeletedSizes := make(map[uint32]uint64)
	for providerID, locations := range providerLocations {
		_, _, deletedSize := batchDeleteFromProvider(ctx, providerID, locations)
		if deletedSize > 0 {
			providerDeletedSizes[providerID] = deletedSize
		}
	}

	// 更新存储提供者的已使用容量
	for providerID, size := range providerDeletedSizes {
		if err := db.DecrementUsedCapacity(providerID, size); err != nil {
			llog.C(ctx).Errorf("decrement used capacity failed: %v, provider_id: %d", err, providerID)
		}
	}

	// 删除文件位置记录
	for hash := range hashSet {
		if err := db.DeleteFileLocationsByBlake3Hash(hash); err != nil {
			llog.C(ctx).Errorf("delete file locations failed, hash: %s, err: %v", hash, err)
		}
	}
}

// permanentlyDeleteFiles 同步永久删除文件（历史记录 + 存储提供者文件 + 数据库记录）
func permanentlyDeleteFiles(ctx context.Context, allFileIDs []string, logLabel string) {
	if ctx == nil {
		ctx = context.Background()
	}

	// 删除缩略图缓存
	deleteThumbnailsForFiles(allFileIDs)

	deleteFileHistories(ctx, allFileIDs)
	deleteFilesFromProviders(ctx, allFileIDs)

	if err := db.DeletePermanently(allFileIDs); err != nil {
		llog.L().Errorf("delete permanently failed: %v", err)
		return
	}

	// WebDAV 账号绑定目录被永久删除 → 自动禁用对应账号
	DisableWebdavAccountsForDeletedRoots(ctx, allFileIDs)

	llog.L().Infof("%s %d files", logLabel, len(allFileIDs))
}

// EmptyTrash 清空回收站
func EmptyTrash(ctx context.Context, req *native.EmptyTrashReq) *models.Err {
	// 1. 获取所有回收站文件
	files, _, err := db.GetTrashList(0, 10000, getCurrentUserIDFromCtx(ctx)) // 获取所有文件
	if err != nil {
		llog.C(ctx).Errorf("get trash list failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	if len(files) == 0 {
		return nil
	}

	// 2. 提取文件ID
	fileIDs := make([]string, len(files))
	for i, file := range files {
		fileIDs[i] = *file.Id
	}

	// 3. 获取所有子文件和子文件夹的ID（包括软删除的）
	allChildrenIDs, err := db.GetAllChildrenIDs(fileIDs)
	if err != nil {
		llog.C(ctx).Errorf("get all children IDs failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 合并指定的文件ID和所有子文件ID
	allFileIDs := append(fileIDs, allChildrenIDs...)
	llog.C(ctx).Infof("emptying trash: %d files (including %d children)", len(allFileIDs), len(allChildrenIDs))

	permanentlyDeleteFiles(ctx, allFileIDs, "emptied trash, deleted")

	// 容量统计：行真正删除后用户占用减少
	MarkFileCapacityChanged(getCurrentUserIDFromCtx(ctx))
	return nil
}

// GetTrashList 获取回收站文件列表
func GetTrashList(ctx context.Context) (*native.TrashListRespData, *models.Err) {
	files, total, err := db.GetTrashList(0, 500, getCurrentUserIDFromCtx(ctx))
	if err != nil {
		llog.C(ctx).Errorf("get trash list failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 转换为 PB
	items := make([]*native.File, 0, len(files))
	for _, f := range files {
		pb, err := f.ToPB(ctx)
		if err != nil {
			llog.C(ctx).Errorf("convert file to PB failed: %v", err)
			continue
		}
		items = append(items, pb)
	}

	totalUint32 := uint32(total)
	return &native.TrashListRespData{
		Files: items,
		Total: &totalUint32,
	}, nil
}

// SearchFiles 按文件名或标签搜索文件
func SearchFiles(ctx context.Context, req *native.SearchFilesReq) ([]*entity.FileORM, int64, bool, *models.Err) {
	keyword := req.GetKeyword()
	if keyword == "" {
		return nil, 0, false, &models.Err{
			E: errors.New("keyword is required"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	offset := req.GetOffset()
	pageSize := req.GetPageSize()
	if pageSize == 0 {
		pageSize = 50
	}

	// 根据搜索类型选择查询方式
	searchType := req.GetSearchType()
	var files []*entity.FileORM
	var total int64
	var hasMore bool
	var err error

	if searchType == native.SearchType_TAG {
		files, total, hasMore, err = db.SearchFilesByTag(keyword, offset, pageSize)
	} else {
		files, total, hasMore, err = db.SearchFiles(keyword, offset, pageSize, getCurrentUserIDFromCtx(ctx))
	}

	if err != nil {
		llog.C(ctx).Errorf("SearchFiles failed, keyword: %s, type: %v, err: %v", keyword, searchType, err)
		return nil, 0, false, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	return files, total, hasMore, nil
}

// SyncFileToProviders 同步文件到指定存储提供者
func SyncFileToProviders(ctx context.Context, req *native.SyncFileToProvidersReq) *models.Err {
	// 1. Validate file exists
	file, err := db.GetFileByID(*req.FileId)
	if err != nil {
		llog.C(ctx).Errorf("get file failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 2. Check file has at least one completed location
	locations, err := db.GetFileLocationsByFileHash(*file.Hash)
	if err != nil {
		llog.C(ctx).Errorf("get file locations failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	hasCompleted := false
	for _, loc := range locations {
		if loc.Status != nil && *loc.Status == int32(native.LocationStatus_PCOMPLETED) {
			hasCompleted = true
			break
		}
	}

	if !hasCompleted {
		return &models.Err{
			E: errors.New("file has no completed locations"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 3. Validate target providers
	providerIDs := make([]uint32, len(req.ProviderIds))
	copy(providerIDs, req.ProviderIds)

	providers, err := db.GetStorageProvidersByIDs(providerIDs)
	if err != nil {
		llog.C(ctx).Errorf("get providers failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	if len(providers) != len(providerIDs) {
		return &models.Err{
			E: errors.New("some providers not found or disabled"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 归属校验：目标 provider 必须在「文件所在目录的有效绑定」内，
	// 且文件必须属于当前用户——防止把他人文件同步到任意 provider
	if !checkOwnership(ctx, *req.FileId) {
		return &models.Err{
			E: errors.New("permission denied: not owner of file"),
			C: native.RespCode_PERMISSION_DENIED,
		}
	}
	boundIDs, mErr := resolveProviderIdsForFolder(ctx, strPtrVal(file.ParentId))
	if mErr != nil {
		return mErr
	}
	boundSet := make(map[uint32]struct{}, len(boundIDs))
	for _, id := range boundIDs {
		boundSet[id] = struct{}{}
	}
	for _, pid := range providerIDs {
		if _, ok := boundSet[pid]; !ok {
			return &models.Err{
				E: fmt.Errorf("provider %d is not bound to you", pid),
				C: native.RespCode_PERMISSION_DENIED,
			}
		}
	}

	// 4. Filter out providers where file already exists and is completed
	var validProviderIDs []uint32
	for _, provider := range providers {
		existingLoc, err := db.GetFileLocationByFileHashAndProvider(*file.Hash, *provider.Id)
		if err == nil && existingLoc != nil {
			// File already exists on this provider
			if existingLoc.Status != nil && *existingLoc.Status == int32(native.LocationStatus_PCOMPLETED) {
				llog.C(ctx).Warnf("file already exists on provider %d, skipping", *provider.Id)
				continue // Skip this provider
			}
		}
		validProviderIDs = append(validProviderIDs, *provider.Id)
	}

	if len(validProviderIDs) == 0 {
		return &models.Err{
			E: errors.New("all selected providers already have this file"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 5. Convert provider IDs to JSON array string
	providerIDsJSON, err := json.Marshal(validProviderIDs)
	if err != nil {
		llog.C(ctx).Errorf("marshal provider ids failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}

	// 6. Create ONE sync upload record with all target providers
	uploadRecord := &entity.UploadRecordORM{
		FileHash:          file.Hash,
		FileName:          file.Name,
		FileSize:          file.Size,
		ParentId:          file.ParentId,
		ChunkTotal:        utils.Uint32Ptr(1),
		ChunkSize:         utils.Uint64Ptr(0),
		TempPath:          utils.StringPtr(""), // Will be filled by dispatcher
		Status:            utils.Int32Ptr(int32(native.LocationStatus_PPENDING)),
		TaskStatus:        utils.EnumPtr(native.UploadStatus_PENDING),
		TargetProviderIds: utils.StringPtr(string(providerIDsJSON)), // JSON array like [1,3,5]
		TaskType:          utils.Int32Ptr(int32(native.UploadTaskType_SYNC)),
		OwnerId:           utils.StringPtr(getCurrentUserIDFromCtx(ctx)),
	}

	if err := db.CreateUploadRecord(uploadRecord); err != nil {
		llog.C(ctx).Errorf("create upload record failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 7. Trigger storage dispatch (will handle multi-provider distribution)
	// 使用独立的 context，避免 HTTP context 在请求结束后被取消
	go storage.Dispatch(context.Background(), uploadRecord)

	llog.C(ctx).Infof("created sync task for file %s to %d providers", *file.Id, len(validProviderIDs))
	return nil
}

// UpdateFileTags 更新文件标签
func UpdateFileTags(ctx context.Context, req *native.UpdateFileTagsReq) *models.Err {
	return UpdateFileTagsImpl(ctx, req)
}

// deleteThumbnailsForFiles 删除文件的缩略图缓存
func deleteThumbnailsForFiles(fileIDs []string) {
	cacheDir := "data/thumbnails"
	generator := thumbnail.NewGenerator(cacheDir)

	// 用 Unscoped 查询：永久删除的文件已在回收站（软删），
	// 默认作用域的 GetFileByID 查不到软删记录，会导致缩略图永远无法清理
	files, err := db.GetFilesByIDsUnscoped(fileIDs)
	if err != nil {
		llog.L().Warnf("get files for thumbnail cleanup failed: %v", err)
		return
	}

	for _, file := range files {
		if file.Hash != nil && *file.Hash != "" {
			// 删除所有尺寸的缩略图
			if err := generator.DeleteAllThumbnails(*file.Hash); err != nil {
				llog.L().Warnf("delete thumbnails failed: hash=%s, err=%v", *file.Hash, err)
			}
		}
	}
}

// GetRecentFiles 获取最近使用的文件（已废弃：前端改为本地存储，此接口保留用于兼容）
// Deprecated: 前端已改用本地 RecentFilesStorage，此接口将在后续版本移除
func GetRecentFiles(ctx context.Context, req *native.GetRecentFilesReq) ([]*entity.FileORM, int64, bool, *models.Err) {
	offset := req.GetOffset()
	pageSize := req.GetPageSize()
	if pageSize == 0 {
		pageSize = 20
	}

	files, _, _, err := db.GetRecentFiles(offset, pageSize, getCurrentUserIDFromCtx(ctx))
	if err != nil {
		llog.C(ctx).Errorf("GetRecentFiles failed, err: %v", err)
		return nil, 0, false, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 归属过滤：直接比对文件自身的 owner_id。
	// 注：db.GetRecentFiles 已按 owner_id 过滤，此处为防御性二次校验。
	// 不能用 CheckOwnership(*f.ParentId)：父目录可能已进回收站或被永久删除，
	// 查不到父目录会导致文件被误过滤。
	userID := getCurrentUserIDFromCtx(ctx)
	var filtered []*entity.FileORM
	for _, f := range files {
		if f.OwnerId != nil && *f.OwnerId == userID {
			filtered = append(filtered, f)
		}
	}

	return filtered, int64(len(filtered)), false, nil
}

// GetFilesByType 按文件类型获取文件列表（带权限过滤）
func GetFilesByType(ctx context.Context, req *native.GetFilesByTypeReq) ([]*entity.FileORM, int64, bool, *models.Err) {
	fileType := req.GetFileType()
	offset := req.GetOffset()
	pageSize := req.GetPageSize()
	if pageSize == 0 {
		pageSize = 50
	}

	// 统一按权限过滤（管理员也只看自己的文件）。db 层已按 userID 过滤，
	// 这里再做一次目录读权限过滤以覆盖分享/授权目录场景。

	// 获取更多数据后按权限过滤（避免分页不足）
	fetchLimit := pageSize * 3
	if fetchLimit > 200 {
		fetchLimit = 200
	}
	allFiles, _, _, err := db.GetFilesByType(fileType, 0, fetchLimit, getCurrentUserIDFromCtx(ctx))
	if err != nil {
		llog.C(ctx).Errorf("GetFilesByType failed, type: %v, err: %v", fileType, err)
		return nil, 0, false, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 归属过滤：直接比对文件自身的 owner_id。
	// 注：db.GetFilesByType 已按 owner_id 过滤，此处为防御性二次校验。
	// 不能用 CheckOwnership(*f.ParentId)：父目录可能已进回收站或被永久删除，
	// 查不到父目录会导致文件被误过滤。
	var filtered []*entity.FileORM
	userID := getCurrentUserIDFromCtx(ctx)
	llog.C(ctx).Infof("GetFilesByType: user=%s, totalFiles=%d", userID, len(allFiles))
	for _, f := range allFiles {
		if f.OwnerId != nil && *f.OwnerId == userID {
			filtered = append(filtered, f)
		}
	}
	llog.C(ctx).Infof("GetFilesByType: user=%s, filtered=%d", userID, len(filtered))

	// 应用 offset/limit
	total := int64(len(filtered))
	start := int(offset)
	if start >= len(filtered) {
		return nil, total, false, nil
	}
	end := start + int(pageSize)
	if end > len(filtered) {
		end = len(filtered)
	}
	hasMore := int64(end) < total

	return filtered[start:end], total, hasMore, nil
}

// UpdateFileAccess 更新文件的最后访问时间
func UpdateFileAccess(fileID string) {
	if err := db.UpdateLastAccessedAt(fileID); err != nil {
		llog.L().Warnf("UpdateLastAccessedAt failed, fileID: %s, err: %v", fileID, err)
	}
}
