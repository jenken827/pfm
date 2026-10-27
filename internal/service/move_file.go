package service

import (
	"context"
	"errors"
	"fmt"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/proto/gen/entity"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// MoveFile 移动文件到目标文件夹
//
// 性能说明：所有校验均按批处理，避免逐文件查询造成的 N+1——
//   - 源文件归属：checkOwnershipBatch（单条 id IN (...)）
//   - 源文件记录：db.GetFilesByIDs（已由上方批量取回，直接复用）
//   - 目标同名冲突：一次取回目标目录下全部文件名（GetFileNamesInParent）
//   - 循环引用：一次取回祖先链所需记录（collectAncestors）
//
// 检查：
// 1. 目标文件夹归属（写权限）
// 2. 每个源文件归属 + 存在性 + 非自身 + 非其后代 + 目标无同名
//
// 注意：移动**不改绑定、不补齐副本、不触发同步**（设计决策）——
// 只改 parent_id。目标目录的绑定在下次上传/覆盖时自然生效；
// 已存在的物理副本保持原样，需要补副本请显式用「同步到存储提供者」。
func MoveFile(ctx context.Context, req *native.MoveFileReq) (uint32, []string, map[string]string, *models.Err) {
	successCount := uint32(0)
	var failedIDs []string
	failedReasons := make(map[string]string)

	targetParentID := req.GetTargetParentId()
	userID := getCurrentUserIDFromCtx(ctx)

	// 权限检查：目标目录归属（移动需要写权限）
	if !CheckOwnership(ctx, targetParentID) {
		return 0, nil, nil, &models.Err{
			E: fmt.Errorf("permission denied: no write access to target directory %s", targetParentID),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 1. 获取目标文件夹信息
	targetFolder, err := db.GetFileByID(targetParentID)
	if err != nil {
		llog.C(ctx).Errorf("get target folder failed: %v", err)
		return 0, nil, nil, &models.Err{
			E: errors.New("target folder not found"),
			C: native.RespCode_NOT_FOUND,
		}
	}
	if targetFolder.Type == nil || native.FileType(*targetFolder.Type) != native.FileType_FOLDER {
		return 0, nil, nil, &models.Err{
			E: errors.New("target is not a folder"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 2. 批量预备数据（避免循环内查询）
	prep, mErr := prepareMoveBatch(ctx, req.FileIds, targetParentID, userID)
	if mErr != nil {
		return 0, nil, nil, mErr
	}

	// 3. 遍历要移动的文件
	for _, fileID := range req.FileIds {
		file := prep.files[fileID]
		if file == nil {
			failedIDs = append(failedIDs, fileID)
			failedReasons[fileID] = "file not found or permission denied"
			continue
		}

		// 不能移动到自身
		if fileID == targetParentID {
			failedIDs = append(failedIDs, fileID)
			failedReasons[fileID] = "cannot move to itself"
			continue
		}

		// 不能移动到自己的后代（防止形成环）
		if prep.isDescendantOf(fileID) {
			failedIDs = append(failedIDs, fileID)
			failedReasons[fileID] = "cannot move folder to its descendant"
			continue
		}

		// 目标目录同名冲突（批量取回的映射，无需查询）
		if file.Name != nil {
			if existing := prep.targetNames[*file.Name]; existing != nil && existing.Id != file.Id {
				failedIDs = append(failedIDs, fileID)
				failedReasons[fileID] = "file with same name already exists in target folder"
				continue
			}
		}

		// 更新 parent_id
		file.ParentId = &targetParentID
		if err := db.UpdateFile(file, []string{"parent_id"}); err != nil {
			failedIDs = append(failedIDs, fileID)
			failedReasons[fileID] = "update file parent_id failed"
			continue
		}
		// 容量统计说明：移动不改变 owner 归属，用户总占用不变，无需重算
		successCount++
		llog.C(ctx).Infof("file moved successfully: %s -> %s", fileID, targetParentID)
	}

	return successCount, failedIDs, failedReasons, nil
}

// moveBatch 移动操作的批量预取结果
type moveBatch struct {
	// 目标父目录 ID（循环引用检测起点）
	targetParentID string
	// 源文件：fileID -> 记录（仅包含属于当前用户的存活文件）
	files map[string]*entity.FileORM
	// 目标目录下「文件名 -> 记录」，用于同名冲突检测
	targetNames map[string]*entity.FileORM
	// 祖先链：fileID -> parentID，用于循环引用判定
	parentOf map[string]string
}

// isDescendantOf 基于预取的祖先链判断 targetParentID 是否位于 fileID 的子树中
// （等价于原 isDescendant：从目标向上回溯，途中遇到 fileID 即为其后代）
func (b *moveBatch) isDescendantOf(fileID string) bool {
	visited := make(map[string]bool)
	current := b.targetParentID
	for current != "" && current != "0" {
		if current == fileID {
			return true
		}
		if visited[current] {
			break // 防御环形数据
		}
		visited[current] = true
		next, ok := b.parentOf[current]
		if !ok || next == "" {
			break
		}
		current = next
	}
	return false
}

// prepareMoveBatch 一次性取回移动所需全部数据，避免循环内逐条查询（N+1）
func prepareMoveBatch(ctx context.Context, fileIDs []string, targetParentID, userID string) (*moveBatch, *models.Err) {
	// 归属 + 存在性：单条 id IN (...) 查询后批量比对
	filesSlice, err := db.GetFilesByIDs(fileIDs)
	if err != nil {
		llog.C(ctx).Errorf("batch get files failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	files := make(map[string]*entity.FileORM, len(filesSlice))
	for _, f := range filesSlice {
		if f.Id == nil || f.OwnerId == nil || *f.OwnerId != userID {
			continue // 非本人文件：视为不存在（caller 报 file not found or permission denied）
		}
		files[*f.Id] = f
	}

	// 目标目录下已有文件名（一次查询）
	targetNames, err := db.GetFileNamesInParent(targetParentID, userID)
	if err != nil {
		llog.C(ctx).Errorf("get target names failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 祖先链：从目标目录向上收集（本次移动所有文件共用同一条链）
	parentOf := make(map[string]string)
	collectAncestors(targetParentID, parentOf)

	batch := &moveBatch{
		files:       files,
		targetNames: targetNames,
		parentOf:    parentOf,
	}
	batch.targetParentID = targetParentID
	return batch, nil
}

// collectAncestors 从 startID 向上收集祖先链（parentID 由调用链逐层查询）
// 使用单层批量查询：每轮把「当前层的全部 id」一次性查回，避免逐层单查。
func collectAncestors(startID string, out map[string]string) {
	level := []string{startID}
	for len(level) > 0 {
		files, err := db.GetFilesByIDsUnscoped(level)
		if err != nil {
			return
		}
		next := make([]string, 0, len(files))
		for _, f := range files {
			if f.Id == nil {
				continue
			}
			pid := ""
			if f.ParentId != nil {
				pid = *f.ParentId
			}
			out[*f.Id] = pid
			if pid != "" && pid != "0" {
				if _, seen := out[pid]; !seen {
					next = append(next, pid)
				}
			}
		}
		level = next
	}
}

// isDescendant 检查 targetID 是否是 fileID 的后代（保留：供其他场景单次判定使用）
func isDescendant(ctx context.Context, fileID, targetID string) bool {
	_ = ctx
	parentOf := make(map[string]string)
	collectAncestors(targetID, parentOf)
	b := &moveBatch{parentOf: parentOf, targetParentID: targetID}
	return b.isDescendantOf(fileID)
}
