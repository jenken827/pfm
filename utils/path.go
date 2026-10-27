package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/pkg/llog"
)

// 暂存文件名约定（写入方与读取方必须一致，故集中定义，避免两侧字符串漂移）
const (
	// chunkFileNamePrefix 分片文件名前缀（chunk_0、chunk_1 …）
	chunkFileNamePrefix = "chunk_"
	// mergedFileNamePrefix 分片合并产物前缀（merged_<原始文件名>）
	mergedFileNamePrefix = "merged_"
)

// MergedFileName 分片合并产物的文件名
// 写入方：service.processCompleteUpload；读取方：service.tryServeFromLocalTemp
func MergedFileName(fileName string) string {
	return mergedFileNamePrefix + fileName
}

// ============================================
// 文件临时目录结构说明
// ============================================
// 临时目录分两层，用途不同：
//
// 1) 内容寻址层（跨用户共享，hash 即内容标识，秒传依赖）：
//    - globals.TempDir/filehash/原始文件名           (staging 明文，上传窗口期可读)
//    - globals.TempDir/filehash/decrypted_原始文件名 (解密后的文件)
//    - globals.TempDir/filehash/merged_file          (分片合并临时文件)
//
// 2) 上传暂存层（按 owner 隔离，防止多用户同 hash 并发上传互相踩踏）：
//    - globals.TempDir/users/<ownerID>/<filehash>/chunks/  (上传分片)
//
// 分片必须按 owner 隔离：upload_record.file_hash 的唯一约束是复合
// (file_hash, owner_id)，但分片目录若共用 <hash>/chunks/，两个用户同时
// 上传同一 hash 时 B 的分片会覆盖 A 的，导致合并出错误内容。

// RemoveChunkTempFiles 删除 owner 隔离目录下的分片文件（chunk_*），保留其余文件
//
// 为什么不能整目录删：分片合并产出的明文 merged_<name> 与分片同处
// TempDir/users/<ownerID>/<filehash>/chunks/，而异步分发正是从这份合并明文读源
// （upload_record.TempPath）。合并完成后直接 RemoveAll 整个 chunks/ 会把分发要读的
// 源一起删掉，Dispatch 的 os.Stat 与兜底查找双双失败 → 报
// "temp file not found" → 上传编排标失败，且文件记录已落库 → 该 hash 永远没有
// file_location 行（读取端随后 500，内容也随分片一起丢失）。
//
// 整目录清理由 dispatch 终态的 cleanupUploadTemp 负责：仅在**全部目标 provider 都成功**
// 时（completeUpload）立即清理；只要有 provider 失败就保留 staging 供重试，由 janitor
// 超龄回收。分片合并阶段失败时由 cleanupChunks 处理，崩溃残留由 janitor 兜底。
func RemoveChunkTempFiles(fileHash, ownerID string) error {
	dir := GetChunkTempDir(fileHash, ownerID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// 目录已不在（终态已清理）：无需处理
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), chunkFileNamePrefix) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// CreateFileTempDir 创建分片临时目录（按 owner 隔离）
// 目录：globals.TempDir/users/<ownerID>/<filehash>/chunks/
func CreateFileTempDir(fileHash, ownerID string, withChunks bool) error {
	tempDir := ""
	if withChunks {
		tempDir = GetChunkTempDir(fileHash, ownerID)
	} else {
		tempDir = GetFileTempDir(fileHash)
	}
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		llog.L().Errorf("create file temp dir failed: %v", err)
		return err
	}
	return nil
}

// GetFileTempDir 获取内容寻址临时目录（跨用户共享）
func GetFileTempDir(fileHash string) string {
	return filepath.Join(globals.TempDir, fileHash)
}

// GetFileTempPath 获取文件临时路径（内容寻址层）
func GetFileTempPath(fileHash string, fileName string) string {
	return filepath.Join(globals.TempDir, fileHash, fileName)
}

// GetChunkTempDir 获取分片临时目录（按 owner 隔离）
// 目录：globals.TempDir/users/<ownerID>/<filehash>/chunks/
func GetChunkTempDir(fileHash, ownerID string) string {
	return filepath.Join(globals.TempDir, globals.UsersTempDirName, ownerID, fileHash, globals.ChunksDirName)
}

// GetChunkTempPath 获取指定分片的临时路径（按 owner 隔离）
func GetChunkTempPath(fileHash, ownerID string, chunkIndex int) string {
	return filepath.Join(GetChunkTempDir(fileHash, ownerID), fmt.Sprintf("%s%d", chunkFileNamePrefix, chunkIndex))
}

// FindTempFile 在 hash 临时目录中查找任意一个文件（fallback）
// 当精确路径（hash+filename）不存在时，扫描目录找到任意可用文件
// 同一 hash 下文件内容相同，所以任意文件名都可以
func FindTempFile(fileHash string) (string, bool) {
	return FindTempFileInDir(GetFileTempDir(fileHash))
}

// FindTempFileInDir 在指定目录中查找任意一个非目录条目（fallback）
// 用于 owner 隔离目录下的 staging 文件查找（merged_<name> / chunk 场景）
func FindTempFileInDir(dir string) (string, bool) {
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
	return removeAllWithRetry(tempDir)
}

// CleanupOwnerHashTempDir 清理指定 owner 的分片/暂存目录
// 目录：globals.TempDir/users/<ownerID>/<filehash>/
// 上传完成或取消后调用，避免隔离目录残留占用磁盘。
func CleanupOwnerHashTempDir(fileHash, ownerID string) error {
	if ownerID == "" {
		// owner 未知时退化为清理内容寻址层（旧布局兼容）
		return CleanupFileTempDir(fileHash)
	}
	// 删到 users/<ownerID>/<hash> 这一层，不误删同 owner 其他 hash 的目录
	dir := filepath.Join(globals.TempDir, globals.UsersTempDirName, ownerID, fileHash)
	return removeAllWithRetry(dir)
}

// removeAllWithRetry 带指数退避重试的目录删除
// Windows 上文件可能仍被占用，删除会短暂失败
func removeAllWithRetry(dir string) error {
	// 重试机制：Windows 上文件可能仍被占用
	maxRetries := 3
	retryDelay := 100 * time.Millisecond

	for i := range maxRetries {
		err := os.RemoveAll(dir)
		if err == nil {
			llog.L().Infof("cleaned up temp dir: %s", dir)
			return nil
		}

		// 如果是文件被占用的错误，等待后重试
		if i < maxRetries-1 {
			llog.L().Warnf("cleanup temp dir failed (attempt %d/%d): %v, retrying in %v...", i+1, maxRetries, err, retryDelay)
			time.Sleep(retryDelay)
			retryDelay *= 2 // 指数退避：200ms, 400ms, 800ms, 1600ms
		} else {
			llog.L().Warnf("cleanup temp dir failed after %d attempts: %v", maxRetries, err)
			return err
		}
	}

	return nil
}
