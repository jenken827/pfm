package utils

import (
	"os"
	"path"
	"path/filepath"

	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/llog"
)

// CreateUploadTempDir 创建上传临时目录
func CreateUploadTempDir(fileHash string, withChunk bool) (string, error) {
	tempDir := filepath.Join(globals.TempDir, fileHash)
	if withChunk {
		tempDir = filepath.Join(tempDir, globals.ChunksDirName)
	}
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		llog.L().Errorf("create temp dir failed: %v", err)
		return "", err
	}
	return tempDir, nil
}

// GetUploadTempDir 获取上传临时目录
func GetUploadTempDir(fileHash string) string {
	return path.Join(globals.TempDir, fileHash)
}

// GetUploadTempPath 获取上传文件临时路径
func GetUploadTempPath(fileHash string, filename string) string {
	return path.Join(globals.TempDir, fileHash, filename)
}

// GetUploadChunkTempDir 获取上传分片临时目录
func GetUploadChunkTempDir(fileHash string) string {
	return path.Join(globals.TempDir, fileHash, globals.ChunksDirName)
}

// CreateDownloadTempDir 创建下载临时目录
func CreateDownloadTempDir(fileHash string) (string, error) {
	tempDir := filepath.Join(globals.TempDir, "downloads", fileHash)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		llog.L().Errorf("create download temp dir failed: %v", err)
		return "", err
	}
	return tempDir, nil
}

// GetDownloadTempDir 获取下载临时目录
func GetDownloadTempDir(fileHash string) string {
	return path.Join(globals.TempDir, "downloads", fileHash)
}
