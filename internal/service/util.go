package service

import (
	"path"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
)

// getParentDir 获取父目录路径
func getParentDir(parentID string) string {
	dir := "/"
	if parentID == "" {
		return dir
	}
	parentDir, err := db.GetFile("id=?", parentID)
	if err != nil {
		llog.GetLogger().Warnf("get parent dir failed, err: %v", err)
		return dir
	}
	dir = path.Join(*parentDir.Dir, *parentDir.Name)
	return dir
}
