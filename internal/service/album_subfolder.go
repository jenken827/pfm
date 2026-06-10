package service

import (
	"context"
	"fmt"
	"strconv"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
	"github.com/google/uuid"
)

const maxFilesPerSubfolder = 500

// GetOrCreateSubfolder 获取有空位的子目录，如果都满了则创建新目录
// 查找 parentID 下以 prefix 开头的子目录，返回第一个文件数 < maxFiles 的目录 ID
// 如果没有合适的目录，创建一个新的（编号递增）
func GetOrCreateSubfolder(ctx context.Context, parentID string, prefix string) (string, *models.Err) {
	// 获取所有子文件夹
	folders, err := db.GetSubfoldersByParentID(parentID)
	if err != nil {
		llog.L().Errorf("get subfolders failed: %v", err)
		return "", &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 查找第一个有空位的目录（按名称排序，匹配 prefix 开头的）
	for _, folder := range folders {
		if folder.Name == nil || folder.Id == nil {
			continue
		}
		name := *folder.Name
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			count, err := db.CountChildrenByParentID(*folder.Id)
			if err != nil {
				llog.L().Errorf("count children failed: %v", err)
				continue
			}
			if count < maxFilesPerSubfolder {
				return *folder.Id, nil
			}
		}
	}

	// 没有合适目录，创建新的
	// 找到最大的编号
	maxNum := 0
	for _, folder := range folders {
		if folder.Name == nil {
			continue
		}
		name := *folder.Name
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			numStr := name[len(prefix):]
			if num, e := strconv.Atoi(numStr); e == nil && num > maxNum {
				maxNum = num
			}
		}
	}
	newName := fmt.Sprintf("%s%03d", prefix, maxNum+1)

	// 从父目录继承属性
	parent, err := db.GetFileByID(parentID)
	if err != nil {
		llog.L().Errorf("get parent folder failed: %v", err)
		return "", &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	newID := uuid.New().String()
	newFolder := &native.FileORM{
		Id:               &newID,
		Name:             &newName,
		ParentId:         &parentID,
		Type:             utils.EnumPtr(native.FileType_FOLDER),
		IsReady:          utils.BoolPtr(true),
		EnableEncryption: parent.EnableEncryption,
		BoundProviderIds: parent.BoundProviderIds,
		MaxHistories:     parent.MaxHistories,
	}

	if err := db.CreateFile(newFolder); err != nil {
		llog.L().Errorf("create subfolder failed: %v", err)
		return "", &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	llog.L().Infof("created subfolder %s (id=%s) under parent %s", newName, newID, parentID)
	return newID, nil
}
