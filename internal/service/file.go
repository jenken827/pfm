package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/internal/thumbnail"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// checkOwnership 检查当前用户是否拥有指定文件（或为管理员）
func checkOwnership(ctx context.Context, fileID string) bool {
	if isAdminUser(ctx) {
		return true
	}
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

// GetFileList 获取文件列表
func GetFileList(ctx context.Context, req *native.FileListReq) ([]*native.FileORM, int64, bool, *models.Err) {
	userID := getCurrentUserIDFromCtx(ctx)
	parentID := req.GetParentId()
	llog.L().Infof("GetFileList: user=%s, parentId=%s", userID, parentID)

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
		llog.L().Infof("GetFileList: user=%s, resolved root to %s", userID, parentID)
	}

	// 非根目录：检查目录所有权
	if !checkOwnership(ctx, parentID) {
		llog.L().Infof("GetFileList: user=%s, ownership check failed for parentId=%s", userID, parentID)
		return nil, 0, false, &models.Err{
			E: fmt.Errorf("permission denied: not owner of directory %s", parentID),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	offset := req.GetOffset()
	pageSize := req.GetPageSize()

	files, total, hasMore, err := db.GetFileList(parentID, offset, pageSize, userID)
	if err != nil {
		llog.L().Errorf("GetFileList failed, err: %v", err)
		return nil, 0, false, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	llog.L().Infof("GetFileList: user=%s, parentId=%s, returned %d files", userID, parentID, len(files))
	return files, total, hasMore, nil
}
// GetFolderList 获取文件夹列表（用于移动文件选择目标文件夹）
func GetFolderList(ctx context.Context, req *native.FolderListReq) ([]*native.FileORM, *models.Err) {
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
		llog.L().Errorf("GetFolderList failed, err: %v", err)
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

	// 所有权检查：检查对父目录的所有权
	if !checkOwnership(ctx, req.GetParentId()) {
		return "", &models.Err{
			E: fmt.Errorf("permission denied: not owner of directory %s", req.GetParentId()),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 处理加密设置：如果前端未传递，则从父文件夹继承
	enableEncryption := req.EnableEncryption
	if enableEncryption == nil {
		inherited, mErr := getInheritedEncryption(req.ParentId)
		if mErr != nil {
			return "", mErr
		}
		enableEncryption = &inherited
	}

	f := &native.FileORM{
		Name:             req.Name,
		ParentId:         req.ParentId,
		Type:             utils.EnumPtr(native.FileType_FOLDER),
		EnableEncryption: enableEncryption,
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

// UpdateDir 更新文件夹
func UpdateDir(ctx context.Context, req *native.UpdateDirReq) *models.Err {
	orm := native.FileORM{
		Id:               req.Id,
		Name:             req.Name,
		EnableEncryption: req.EnableEncryption,
	}
	llog.L().Infof("update dir req:%+v", req)

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

	// 所有权检查：检查当前用户是否拥有每个文件
	for _, id := range ids {
		if !checkOwnership(ctx, id) {
			return &models.Err{
				E: fmt.Errorf("permission denied: not owner of file %s", id),
				C: native.RespCode_PARAMS_ERROR,
			}
		}
	}

	// 递归获取所有子文件和子文件夹的ID
	allChildrenIDs, err := db.GetAllChildrenIDs(ids)
	if err != nil {
		llog.L().Errorf("get all children IDs failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 合并指定的文件ID和所有子文件ID
	allIDs := append(ids, allChildrenIDs...)

	if err := db.DeleteFiles(allIDs); err != nil {
		llog.L().Errorf("DeleteFiles failed, err: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	llog.L().Infof("deleted %d files (including %d children)", len(allIDs), len(allChildrenIDs))
	return nil
}

// RenameFile 重命名文件
func RenameFile(ctx context.Context, req *native.RenameFileReq) *models.Err {
	// 所有权检查：检查当前用户是否拥有每个文件
	for _, obj := range req.RenameObjs {
		if !checkOwnership(ctx, obj.GetId()) {
			return &models.Err{
				E: fmt.Errorf("permission denied: not owner of file %s", obj.GetId()),
				C: native.RespCode_PARAMS_ERROR,
			}
		}
	}

	if err := db.RenameFiles(req.RenameObjs); err != nil {
		llog.L().Errorf("RenameFiles failed, err: %v", err)
		return &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	return nil
}

// GetAvailableProvidersForSync 获取可用于同步的存储提供者
// GetAvailableProvidersForSync 获取可用于同步的存储提供者
// 返回：已启用的提供者，排除文件已存在且完成的提供者
func GetAvailableProvidersForSync(ctx context.Context, fileHash string) ([]*native.StorageProvider, *models.Err) {
	// 1. Validate file hash
	if fileHash == "" {
		return nil, &models.Err{
			E: errors.New("file hash is required"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 2. Query available providers from database
	providers, err := db.GetAvailableProvidersForSync(fileHash)
	if err != nil {
		llog.L().Errorf("get available providers failed: %v", err)
		return nil, &models.Err{
			E: err,
			C: native.RespCode_DB_ERROR,
		}
	}

	// 3. Convert ORM to PB
	items := make([]*native.StorageProvider, 0, len(providers))
	for _, p := range providers {
		pb, err := p.ToPB(ctx)
		if err != nil {
			llog.L().Errorf("convert provider to PB failed: %v", err)
			continue
		}
		items = append(items, &pb)
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

	// 收集所有需要恢复的文件ID
	// selectedRestore: 用户选中的文件（可能改名/改父目录）
	// childRestore: 文件夹下的子文件（只清 deleted_at，保持原 parent_id）
	type restoreItem struct {
		file        *native.FileORM
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
			if err := tx.WithContext(ctx).Unscoped().Model(&native.FileORM{}).
				Where("id = ?", *item.file.Id).
				Updates(updates).Error; err != nil {
				return fmt.Errorf("restore file %s failed: %w", *item.file.Id, err)
			}
		}

		// 2. 恢复子文件（只清 deleted_at，保持原 parent_id 和 name）
		if len(childIDs) > 0 {
			if err := tx.WithContext(ctx).Unscoped().Model(&native.FileORM{}).
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
	return nil
}

// resolveNameConflict 检查目标目录中是否有同名文件，有则自动重命名
// 返回新名称（冲突时），无冲突返回空字符串
func resolveNameConflict(parentID, name, ownerID string) string {
	existing, err := db.GetFileByParentAndName(parentID, name, ownerID)
	if err != nil || existing == nil {
		return "" // 无冲突
	}

	// 解析文件名和扩展名
	ext := filepath.Ext(name)
	baseName := strings.TrimSuffix(name, ext)

	// 尝试 "name (1).ext", "name (2).ext", ...
	for i := 1; i <= 999; i++ {
		newName := fmt.Sprintf("%s (%d)%s", baseName, i, ext)
		conflict, err := db.GetFileByParentAndName(parentID, newName, ownerID)
		if err != nil || conflict == nil {
			return newName
		}
	}

	// 极端情况：999 都冲突，用 UUID 后缀
	return fmt.Sprintf("%s_%s%s", baseName, uuid.New().String()[:8], ext)
}

// batchDeleteFromProvider 从单个存储提供者批量删除文件位置
// 返回：成功删除的 location ID 列表、删除失败的 location ID 列表、删除的总大小
func batchDeleteFromProvider(
	ctx context.Context,
	providerID uint32,
	locations []*native.FileLocationORM,
) (deletedIDs []uint64, failedIDs []uint64, deletedSize uint64) {
	st := storage.GetStorage(providerID)
	if st == nil {
		llog.L().Warnf("storage provider not found, id: %d", providerID)
		for _, loc := range locations {
			if loc.Id != nil {
				failedIDs = append(failedIDs, *loc.Id)
			}
		}
		return
	}

	batchDeleter, ok := st.(types.BatchDeleter)
	if !ok {
		llog.L().Warnf("provider %d does not support BatchDeleter, skipping", providerID)
		for _, loc := range locations {
			if loc.Id != nil {
				failedIDs = append(failedIDs, *loc.Id)
			}
		}
		return
	}

	results, err := batchDeleter.BatchDelete(ctx, types.FileRefsFromLocations(locations))

	// 全部成功
	if results == nil && err == nil {
		llog.L().Infof("batch deleted all %d files from provider %d", len(locations), providerID)
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
		llog.L().Errorf("batch delete all failed: %v, provider_id: %d", err, providerID)
		for _, loc := range locations {
			if loc.Id != nil {
				failedIDs = append(failedIDs, *loc.Id)
			}
		}
		return
	}

	// 部分成功
	llog.L().Infof("batch delete partial success: %d/%d from provider %d",
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
			llog.L().Errorf("delete location %d failed: %v", *loc.Id, result.Error)
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
	llog.L().Infof("delete files from providers, id length: %d", len(fileIDs))

	// 1. 批量获取文件信息（包含软删除的记录，因为可能是回收站文件）
	files, err := db.GetFilesByIDsUnscoped(fileIDs)
	if err != nil {
		llog.L().Errorf("batch get files failed: %v", err)
		return
	}

	// 2. 收集文件hash，用于查询位置记录
	fileHashMap := make(map[string]string) // fileID -> fileHash
	hashSet := make(map[string]bool)       // 用于去重查询
	for _, file := range files {
		if file.Id != nil && file.Hash != nil && *file.Hash != "" {
			fileHashMap[*file.Id] = *file.Hash
			hashSet[*file.Hash] = true
			llog.L().Debugf("get file hash success %s", *file.Hash)
		}
	}

	// 3. 检查每个 hash 是否还有其他非删除文件引用
	// 被其他文件引用的 hash 不应删除其 file_location 记录
	hashesWithRefs := make(map[string]bool) // 仍有其他文件引用的 hash
	for fileHash := range hashSet {
		refCount, err := db.CountActiveFilesByHashExcludingIDs(fileHash, fileIDs)
		if err != nil {
			llog.L().Errorf("count active files by hash failed: %v, hash: %s", err, fileHash)
			// 查询失败时保守处理，不删除该 hash 的 file_location
			hashesWithRefs[fileHash] = true
			continue
		}
		if refCount > 0 {
			llog.L().Infof("hash %s still referenced by %d other files, skip file_location deletion", fileHash, refCount)
			hashesWithRefs[fileHash] = true
		}
	}

	// 4. 批量获取所有文件位置记录，按 provider_id 分组
	providerLocations := make(map[uint32][]*native.FileLocationORM) // providerID -> locations
	for fileHash := range hashSet {
		locations, err := db.GetFileLocationsByBlake3Hash(fileHash)
		if err != nil {
			llog.L().Errorf("get file locations failed: %v, hash: %s", err, fileHash)
			continue
		}
		llog.L().Debugf("success get file location by hash: %s, locations len: %d", fileHash, len(locations))

		for _, loc := range locations {
			if loc.ProviderId == nil {
				llog.L().Debugf("location provider id is nil, location id: %d", loc.Id)
				continue
			}
			providerID := *loc.ProviderId
			providerLocations[providerID] = append(providerLocations[providerID], loc)
		}
	}
	llog.L().Infof("delete files from providers, provider len: %d", len(providerLocations))

	// 5. 按 provider_id 批量删除文件（跳过仍有其他文件引用的 hash）
	providerDeletedSizes := make(map[uint32]uint64)
	var allDeletedIDs, allFailedIDs []uint64

	for providerID, locations := range providerLocations {
		// 过滤掉仍有其他文件引用的 location
		var locationsToDelete []*native.FileLocationORM
		for _, loc := range locations {
			if loc.Blake3Hash != nil && hashesWithRefs[*loc.Blake3Hash] {
				llog.L().Debugf("skip deleting location %d for hash %s (still referenced by other files)", loc.Id, *loc.Blake3Hash)
				continue
			}
			locationsToDelete = append(locationsToDelete, loc)
		}

		if len(locationsToDelete) == 0 {
			continue
		}

		llog.L().Infof("delete files loop providerlocations, providerID: %d, locations: %d", providerID, len(locationsToDelete))
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
			llog.L().Errorf("decrement used capacity failed: %v, provider_id: %d", err, providerID)
		}
	}

	// 7. 批量删除成功删除的文件位置记录
	if len(allDeletedIDs) > 0 {
		if err := db.DeleteFileLocations(allDeletedIDs); err != nil {
			llog.L().Errorf("batch delete file locations failed: %v", err)
		}
	}

	// 8. 更新删除失败的文件位置记录状态
	if len(allFailedIDs) > 0 {
		if err := db.UpdateFileLocationStatus(allFailedIDs, int32(native.LocationStatus_PDELETED_FAILED)); err != nil {
			llog.L().Errorf("update file location status to DELETED_FAILED failed: %v", err)
		} else {
			llog.L().Warnf("marked %d file locations as DELETED_FAILED", len(allFailedIDs))
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

	// 复制 IDs 避免异步访问问题
	fileIDs := make([]string, len(req.Ids))
	copy(fileIDs, req.Ids)

	// 获取所有子文件和子文件夹的ID（包括软删除的）
	allChildrenIDs, err := db.GetAllChildrenIDs(fileIDs)
	if err != nil {
		llog.L().Errorf("get all children IDs failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 合并指定的文件ID和所有子文件ID
	allFileIDs := append(fileIDs, allChildrenIDs...)
	llog.L().Infof("permanently deleting %d files (including %d children)", len(allFileIDs), len(allChildrenIDs))

	permanentlyDeleteFiles(allFileIDs, "permanently deleted")
	return nil
}

// deleteFileHistories 删除文件的历史记录和对应的存储提供者上的文件
func deleteFileHistories(ctx context.Context, fileIDs []string) {
	if len(fileIDs) == 0 {
		return
	}
	llog.L().Infof("delete file history ids length: %d", len(fileIDs))
	// 批量获取所有文件的历史记录
	allHistories := make([]*native.FileHistoryORM, 0)
	for _, fileID := range fileIDs {
		histories, err := db.GetFileHistoryByFileID(fileID)
		if err != nil {
			llog.L().Errorf("get file history failed, file_id: %s, err: %v", fileID, err)
			continue
		}
		allHistories = append(allHistories, histories...)
	}

	if len(allHistories) == 0 {
		llog.L().Infof("no file history need delete")
		return
	}

	// 从存储提供者删除历史文件并更新容量
	deleteHistoryFromProviders(ctx, allHistories)

	// 删除数据库中的历史记录
	if err := db.DeleteFileHistoryByFileIDs(fileIDs); err != nil {
		llog.L().Errorf("delete file histories failed: %v", err)
	} else {
		llog.L().Infof("deleted %d file history records", len(allHistories))
	}
}

// DeleteFileHistory 删除文件历史版本
func DeleteFileHistory(ctx context.Context, ids []uint64) *models.Err {
	if len(ids) == 0 {
		return nil
	}

	// 获取所有历史记录
	var histories []*native.FileHistoryORM
	for _, id := range ids {
		history, err := db.GetFileHistoryByID(id)
		if err != nil {
			llog.L().Errorf("get file history failed, id: %d, err: %v", id, err)
			continue
		}
		histories = append(histories, history)
	}

	if len(histories) == 0 {
		return nil
	}

	// 从存储提供者删除历史文件并更新容量
	deleteHistoryFromProviders(ctx, histories)

	// 删除数据库中的历史记录
	if err := db.DeleteFileHistoryByIDs(ids); err != nil {
		llog.L().Errorf("delete file history failed, err: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	llog.L().Infof("deleted %d file history records", len(histories))
	return nil
}

// deleteHistoryFromProviders 从存储提供者删除历史文件（公共逻辑）
// 收集 hash -> 查询 FileLocation -> 按 provider 分组 -> 批量删除 -> 更新容量 -> 删除 FileLocation 记录
func deleteHistoryFromProviders(ctx context.Context, histories []*native.FileHistoryORM) {
	// 收集所有历史记录的 hash
	hashSet := make(map[string]bool)
	for _, history := range histories {
		if history.Hash != nil && *history.Hash != "" {
			hashSet[*history.Hash] = true
		}
	}

	// 根据 hash 批量获取文件位置记录，按 provider 分组
	providerLocations := make(map[uint32][]*native.FileLocationORM)
	for hash := range hashSet {
		locations, err := db.GetFileLocationsByBlake3Hash(hash)
		if err != nil {
			llog.L().Errorf("get file locations by hash failed, hash: %s, err: %v", hash, err)
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
			llog.L().Errorf("decrement used capacity failed: %v, provider_id: %d", err, providerID)
		}
	}

	// 删除文件位置记录
	for hash := range hashSet {
		if err := db.DeleteFileLocationsByBlake3Hash(hash); err != nil {
			llog.L().Errorf("delete file locations failed, hash: %s, err: %v", hash, err)
		}
	}
}

// permanentlyDeleteFiles 同步永久删除文件（历史记录 + 存储提供者文件 + 数据库记录）
func permanentlyDeleteFiles(allFileIDs []string, logLabel string) {
	ctx := context.Background()

	// 删除缩略图缓存
	deleteThumbnailsForFiles(allFileIDs)

	deleteFileHistories(ctx, allFileIDs)
	deleteFilesFromProviders(ctx, allFileIDs)

	if err := db.DeletePermanently(allFileIDs); err != nil {
		llog.L().Errorf("delete permanently failed: %v", err)
		return
	}

	llog.L().Infof("%s %d files", logLabel, len(allFileIDs))
}

// EmptyTrash 清空回收站
func EmptyTrash(ctx context.Context, req *native.EmptyTrashReq) *models.Err {
	// 1. 获取所有回收站文件
	files, _, err := db.GetTrashList(0, 10000, getCurrentUserIDFromCtx(ctx)) // 获取所有文件
	if err != nil {
		llog.L().Errorf("get trash list failed: %v", err)
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
		llog.L().Errorf("get all children IDs failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 合并指定的文件ID和所有子文件ID
	allFileIDs := append(fileIDs, allChildrenIDs...)
	llog.L().Infof("emptying trash: %d files (including %d children)", len(allFileIDs), len(allChildrenIDs))

	permanentlyDeleteFiles(allFileIDs, "emptied trash, deleted")
	return nil
}

// GetTrashList 获取回收站文件列表
func GetTrashList(ctx context.Context) (*native.TrashListRespData, *models.Err) {
	files, total, err := db.GetTrashList(0, 500, getCurrentUserIDFromCtx(ctx))
	if err != nil {
		llog.L().Errorf("get trash list failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 转换为 PB
	items := make([]*native.File, 0, len(files))
	for _, f := range files {
		pb, err := f.ToPB(ctx)
		if err != nil {
			llog.L().Errorf("convert file to PB failed: %v", err)
			continue
		}
		items = append(items, &pb)
	}

	totalUint32 := uint32(total)
	return &native.TrashListRespData{
		Files: items,
		Total: &totalUint32,
	}, nil
}

// SearchFiles 按文件名或标签搜索文件
func SearchFiles(ctx context.Context, req *native.SearchFilesReq) ([]*native.FileORM, int64, bool, *models.Err) {
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
	var files []*native.FileORM
	var total int64
	var hasMore bool
	var err error

	if searchType == native.SearchType_TAG {
		files, total, hasMore, err = db.SearchFilesByTag(keyword, offset, pageSize)
	} else {
		files, total, hasMore, err = db.SearchFiles(keyword, offset, pageSize, getCurrentUserIDFromCtx(ctx))
	}

	if err != nil {
		llog.L().Errorf("SearchFiles failed, keyword: %s, type: %v, err: %v", keyword, searchType, err)
		return nil, 0, false, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	return files, total, hasMore, nil
}

// SyncFileToProviders 同步文件到指定存储提供者
func SyncFileToProviders(ctx context.Context, req *native.SyncFileToProvidersReq) *models.Err {
	// 1. Validate file exists
	file, err := db.GetFileByID(*req.FileId)
	if err != nil {
		llog.L().Errorf("get file failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 2. Check file has at least one completed location
	locations, err := db.GetFileLocationsByFileHash(*file.Hash)
	if err != nil {
		llog.L().Errorf("get file locations failed: %v", err)
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
		llog.L().Errorf("get providers failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	if len(providers) != len(providerIDs) {
		return &models.Err{
			E: errors.New("some providers not found or disabled"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 4. Filter out providers where file already exists and is completed
	var validProviderIDs []uint32
	for _, provider := range providers {
		existingLoc, err := db.GetFileLocationByFileHashAndProvider(*file.Hash, *provider.Id)
		if err == nil && existingLoc != nil {
			// File already exists on this provider
			if existingLoc.Status != nil && *existingLoc.Status == int32(native.LocationStatus_PCOMPLETED) {
				llog.L().Warnf("file already exists on provider %d, skipping", *provider.Id)
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
		llog.L().Errorf("marshal provider ids failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}

	// 6. Create ONE sync upload record with all target providers
	uploadRecord := &native.UploadRecordORM{
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
	}

	if err := db.CreateUploadRecord(uploadRecord); err != nil {
		llog.L().Errorf("create upload record failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 7. Trigger storage dispatch (will handle multi-provider distribution)
	// 使用独立的 context，避免 HTTP context 在请求结束后被取消
	go storage.Dispatch(context.Background(), uploadRecord)

	llog.L().Infof("created sync task for file %s to %d providers", *file.Id, len(validProviderIDs))
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
func GetRecentFiles(ctx context.Context, req *native.GetRecentFilesReq) ([]*native.FileORM, int64, bool, *models.Err) {
	offset := req.GetOffset()
	pageSize := req.GetPageSize()
	if pageSize == 0 {
		pageSize = 20
	}

	files, total, hasMore, err := db.GetRecentFiles(offset, pageSize, getCurrentUserIDFromCtx(ctx))
	if err != nil {
		llog.L().Errorf("GetRecentFiles failed, err: %v", err)
		return nil, 0, false, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 管理员直接返回
	if isAdminUser(ctx) {
		return files, total, hasMore, nil
	}

	// 普通用户按权限过滤
	var filtered []*native.FileORM
	for _, f := range files {
		if f.ParentId != nil && checkFilePermission(ctx, *f.ParentId, PermRead) {
			filtered = append(filtered, f)
		}
	}

	return filtered, int64(len(filtered)), false, nil
}

// GetFilesByType 按文件类型获取文件列表（带权限过滤）
func GetFilesByType(ctx context.Context, req *native.GetFilesByTypeReq) ([]*native.FileORM, int64, bool, *models.Err) {
	fileType := req.GetFileType()
	offset := req.GetOffset()
	pageSize := req.GetPageSize()
	if pageSize == 0 {
		pageSize = 50
	}

	// 管理员不过滤
	if isAdminUser(ctx) {
		files, total, hasMore, err := db.GetFilesByType(fileType, offset, pageSize, getCurrentUserIDFromCtx(ctx))
		if err != nil {
			llog.L().Errorf("GetFilesByType failed, type: %v, err: %v", fileType, err)
			return nil, 0, false, &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}
		return files, total, hasMore, nil
	}

	// 普通用户：获取更多数据后按权限过滤（避免分页不足）
	fetchLimit := pageSize * 3
	if fetchLimit > 200 {
		fetchLimit = 200
	}
	allFiles, _, _, err := db.GetFilesByType(fileType, 0, fetchLimit, getCurrentUserIDFromCtx(ctx))
	if err != nil {
		llog.L().Errorf("GetFilesByType failed, type: %v, err: %v", fileType, err)
		return nil, 0, false, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 按目录权限过滤
	var filtered []*native.FileORM
	userID := getCurrentUserIDFromCtx(ctx)
	llog.L().Infof("GetFilesByType: user=%s, totalFiles=%d", userID, len(allFiles))
	for _, f := range allFiles {
		if f.ParentId == nil {
			llog.L().Infof("GetFilesByType: skip file %s (no parent)", *f.Name)
			continue
		}
		allowed := checkFilePermission(ctx, *f.ParentId, PermRead)
		llog.L().Infof("GetFilesByType: file=%s, parent=%s, allowed=%v", *f.Name, *f.ParentId, allowed)
		if allowed {
			filtered = append(filtered, f)
		}
	}
	llog.L().Infof("GetFilesByType: user=%s, filtered=%d", userID, len(filtered))

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
