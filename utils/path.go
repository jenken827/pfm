package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

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
func CreateFileTempDir(fileHash string, withChunks bool) error {
	tempDir := ""
	if withChunks {
		tempDir = filepath.Join(globals.TempDir, fileHash, globals.ChunksDirName)
	} else {
		tempDir = filepath.Join(globals.TempDir, fileHash)
	}
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

// FindTempFile 在 hash 临时目录中查找任意一个文件（fallback）
// 当精确路径（hash+filename）不存在时，扫描目录找到任意可用文件
// 同一 hash 下文件内容相同，所以任意文件名都可以
func FindTempFile(fileHash string) (string, bool) {
	dir := GetFileTempDir(fileHash)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue // 跳过 chunks/ 等子目录
		}
		return filepath.Join(dir, entry.Name()), true
	}
	return "", false
}

// CleanupFileTempDir 清理文件临时目录
// 删除整个 filehash 目录及其所有内容
// 包含重试机制，处理文件仍被占用的情况
func CleanupFileTempDir(fileHash string) error {
	tempDir := filepath.Join(globals.TempDir, fileHash)

	// 重试机制：Windows 上文件可能仍被占用
	maxRetries := 3
	retryDelay := 100 * time.Millisecond

	for i := range maxRetries {
		err := os.RemoveAll(tempDir)
		if err == nil {
			llog.L().Infof("cleaned up temp dir for file: %s", fileHash)
			return nil
		}

		// 如果是文件被占用的错误，等待后重试
		if i < maxRetries-1 {
			llog.L().Warnf("cleanup file temp dir failed (attempt %d/%d): %v, retrying in %v...", i+1, maxRetries, err, retryDelay)
			time.Sleep(retryDelay)
			retryDelay *= 2 // 指数退避：200ms, 400ms, 800ms, 1600ms
		} else {
			llog.L().Warnf("cleanup file temp dir failed after %d attempts: %v", maxRetries, err)
			return err
		}
	}

	return nil
}
