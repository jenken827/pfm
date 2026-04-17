package service

import (
	"context"
	"encoding/json"
	"errors"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// MoveFile 移动文件到目标文件夹
// 检查：
// 1. 目标文件夹中是否存在同名文件
// 2. 源和目标文件夹的存储提供者绑定是否一致，如果不一致触发同步
func MoveFile(ctx context.Context, req *native.MoveFileReq) (uint32, []string, map[string]string, *models.Err) {
	successCount := uint32(0)
	var failedIDs []string
	failedReasons := make(map[string]string)

	targetParentID := req.GetTargetParentId()

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

	// 解析目标文件夹绑定的存储提供者
	targetProviderIDs, mErr := parseProviderIDs(targetFolder.BoundProviderIds)
	if mErr != nil {
		return 0, nil, nil, mErr
	}

	// 2. 遍历要移动的文件
	for _, fileID := range req.FileIds {
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
		existingFile, err := db.GetFileByParentAndName(targetParentID, *file.Name)
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

		// 4. 检查源和目标文件夹的存储提供者绑定
		sourceParentID := *file.ParentId
		if sourceParentID != "0" && sourceParentID != targetParentID {
			// 获取源文件夹
			sourceParent, err := db.GetFileByID(sourceParentID)
			if err != nil {
				failedIDs = append(failedIDs, fileID)
				failedReasons[fileID] = "source folder not found"
				continue
			}

			// 比较存储提供者绑定
			sourceProviderIDs, mErr := parseProviderIDs(sourceParent.BoundProviderIds)
			if mErr != nil {
				failedIDs = append(failedIDs, fileID)
				failedReasons[fileID] = mErr.E.Error()
				continue
			}

			// 如果存储提供者不一致，触发同步
			if !providerIDsEqual(sourceProviderIDs, targetProviderIDs) {
				// 获取文件所有位置信息
				locations, err := db.GetFileLocationsByBlake3Hash(*file.Hash)
				if err != nil {
					failedIDs = append(failedIDs, fileID)
					failedReasons[fileID] = "get file locations failed"
					continue
				}

				// 计算需要同步的目标提供者
				var needSyncProviderIDs []uint32
				for _, targetPID := range targetProviderIDs {
					found := false
					for _, loc := range locations {
						if loc.ProviderId != nil && *loc.ProviderId == targetPID {
							found = true
							break
						}
					}
					if !found {
						needSyncProviderIDs = append(needSyncProviderIDs, targetPID)
					}
				}

				// 如果需要同步，创建同步任务
				if len(needSyncProviderIDs) > 0 {
					llog.L().Infof("move file %s needs sync to providers: %v", fileID, needSyncProviderIDs)
					// 同步任务会在后台执行，不阻塞移动操作
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

// parseProviderIDs 解析存储提供者ID列表
func parseProviderIDs(boundProviderIDs *string) ([]uint32, *models.Err) {
	if boundProviderIDs == nil || *boundProviderIDs == "" {
		return nil, &models.Err{
			E: errors.New("folder has no bound storage providers"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	var providerIDs []uint32
	if err := json.Unmarshal([]byte(*boundProviderIDs), &providerIDs); err != nil {
		llog.L().Errorf("parse provider ids failed: %v", err)
		return nil, &models.Err{
			E: errors.New("invalid provider_ids format"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	return providerIDs, nil
}

// providerIDsEqual 比较两个存储提供者ID列表是否相等
func providerIDsEqual(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}

	aMap := make(map[uint32]bool)
	for _, id := range a {
		aMap[id] = true
	}

	for _, id := range b {
		if !aMap[id] {
			return false
		}
	}

	return true
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
