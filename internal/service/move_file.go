package service

import (
	"context"
	"errors"
	"fmt"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// MoveFile 移动文件到目标文件夹
// 检查：
// 1. 目标文件夹中是否存在同名文件
// 2. 文件是否已在所有绑定的存储提供者上，如果缺失则触发同步
func MoveFile(ctx context.Context, req *native.MoveFileReq) (uint32, []string, map[string]string, *models.Err) {
	successCount := uint32(0)
	var failedIDs []string
	failedReasons := make(map[string]string)

	targetParentID := req.GetTargetParentId()

	// 权限检查：检查对目标目录的上传权限（移动需要写权限）
	if !checkFilePermission(ctx, targetParentID, PermUpload) {
		return 0, nil, nil, &models.Err{
			E: fmt.Errorf("permission denied: no write access to target directory %s", targetParentID),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 1. 获取目标文件夹信息
	targetFolder, err := db.GetFileByID(targetParentID)
	if err != nil {
		llog.L().Errorf("get target folder failed: %v", err)
		return 0, nil, nil, &models.Err{
			E: errors.New("target folder not found"),
			C: native.RespCode_NOT_FOUND,
		}
	}

	// 检查目标文件夹是否是文件夹类型
	if targetFolder.Type == nil || native.FileType(*targetFolder.Type) != native.FileType_FOLDER {
		return 0, nil, nil, &models.Err{
			E: errors.New("target is not a folder"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 从 context 获取用户绑定的存储提供者
	providerIDs, mErr := getBoundProviderIdsFromCtx(ctx)
	if mErr != nil {
		return 0, nil, nil, mErr
	}

	// 2. 遍历要移动的文件
	for _, fileID := range req.FileIds {
		// 权限检查：检查对源文件的移动权限
		if !checkFilePermission(ctx, fileID, PermMove) {
			failedIDs = append(failedIDs, fileID)
			failedReasons[fileID] = "permission denied: no move access"
			continue
		}

		// 获取文件记录
		file, err := db.GetFileByID(fileID)
		if err != nil {
			failedIDs = append(failedIDs, fileID)
			failedReasons[fileID] = "file not found"
			continue
		}

		// 不能移动到自身或子文件夹中
		if fileID == targetParentID {
			failedIDs = append(failedIDs, fileID)
			failedReasons[fileID] = "cannot move to itself"
			continue
		}

		// 检查是否移动到子文件夹中（防止循环）
		if isDescendant(ctx, fileID, targetParentID) {
			failedIDs = append(failedIDs, fileID)
			failedReasons[fileID] = "cannot move folder to its descendant"
			continue
		}

		// 3. 检查目标文件夹中是否存在同名文件
		existingFile, err := db.GetFileByParentAndName(targetParentID, *file.Name, getCurrentUserIDFromCtx(ctx))
		if err != nil && err.Error() != "record not found" {
			failedIDs = append(failedIDs, fileID)
			failedReasons[fileID] = "check existing file failed"
			continue
		}
		if existingFile != nil {
			failedIDs = append(failedIDs, fileID)
			failedReasons[fileID] = "file with same name already exists in target folder"
			continue
		}

		// 4. 检查文件是否缺少存储副本，如果缺失则触发同步
		if file.Hash != nil && *file.Hash != "" {
			locations, err := db.GetFileLocationsByBlake3Hash(*file.Hash)
			if err == nil {
				var needSyncProviderIDs []uint32
				for _, pid := range providerIDs {
					found := false
					for _, loc := range locations {
						if loc.ProviderId != nil && *loc.ProviderId == pid &&
							loc.Status != nil && *loc.Status == int32(native.LocationStatus_PCOMPLETED) {
							found = true
							break
						}
					}
					if !found {
						needSyncProviderIDs = append(needSyncProviderIDs, pid)
					}
				}

				if len(needSyncProviderIDs) > 0 {
					llog.L().Infof("move file %s needs sync to providers: %v", fileID, needSyncProviderIDs)
					createSyncTask(file.Name, file.Size, &targetParentID, *file.Hash, needSyncProviderIDs)
				}
			}
		}

		// 5. 更新文件的 parent_id
		file.ParentId = &targetParentID
		if err := db.UpdateFile(file, []string{"parent_id"}); err != nil {
			failedIDs = append(failedIDs, fileID)
			failedReasons[fileID] = "update file parent_id failed"
			continue
		}

		successCount++
		llog.L().Infof("file moved successfully: %s -> %s", fileID, targetParentID)
	}

	return successCount, failedIDs, failedReasons, nil
}

// isDescendant 检查 targetID 是否是 fileID 的后代
func isDescendant(ctx context.Context, fileID, targetID string) bool {
	_ = ctx
	// 获取目标文件夹的父文件夹
	visited := make(map[string]bool)
	currentID := targetID

	for currentID != "0" && currentID != "" {
		if currentID == fileID {
			return true
		}
		if visited[currentID] {
			// 防止循环引用
			break
		}
		visited[currentID] = true

		// 获取当前文件夹
		file, err := db.GetFileByID(currentID)
		if err != nil {
			break
		}
		if file.ParentId == nil {
			break
		}
		currentID = *file.ParentId
	}

	return false
}
