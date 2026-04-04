package utils

import (
	"fmt"
	"os"
	"path/filepath"

	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/llog"
)

// ============================================
// 文件临时目录结构说明
// ============================================
// 所有临时文件统一在 globals.TempDir/filehash/ 目录下管理
// 目录结构：
// - globals.TempDir/filehash/原始文件名          (下载的文件，可能是加密的)
// - globals.TempDir/filehash/decrypted_原始文件名 (解密后的文件)
// - globals.TempDir/filehash/chunks/             (上传分片目录)

// CreateFileTempDir 创建文件临时目录
// 每个文件都有独立的临时目录：globals.TempDir/filehash/
func CreateFileTempDir(fileHash string) error {
	tempDir := filepath.Join(globals.TempDir, fileHash)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		llog.L().Errorf("create file temp dir failed: %v", err)
		return err
	}
	return nil
}

// GetFileTempDir 获取文件临时目录
func GetFileTempDir(fileHash string) string {
	return filepath.Join(globals.TempDir, fileHash)
}

// GetFileTempPath 获取文件临时路径
func GetFileTempPath(fileHash string, fileName string) string {
	return filepath.Join(globals.TempDir, fileHash, fileName)
}

// GetChunkTempDir 获取分片临时目录
func GetChunkTempDir(fileHash string) string {
	return filepath.Join(globals.TempDir, fileHash, globals.ChunksDirName)
}

// GetChunkTempPath 获取指定分片的临时路径
func GetChunkTempPath(fileHash string, chunkIndex int) string {
	return filepath.Join(globals.TempDir, fileHash, globals.ChunksDirName, fmt.Sprintf("chunk_%d", chunkIndex))
}

// CleanupFileTempDir 清理文件临时目录
// 删除整个 filehash 目录及其所有内容
func CleanupFileTempDir(fileHash string) error {
	tempDir := filepath.Join(globals.TempDir, fileHash)
	if err := os.RemoveAll(tempDir); err != nil {
		llog.L().Warnf("cleanup file temp dir failed: %v", err)
		return err
	}
	llog.L().Infof("cleaned up temp dir for file: %s", fileHash)
	return nil
}
