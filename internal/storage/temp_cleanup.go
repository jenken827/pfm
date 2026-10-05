package storage

import (
	"os"
	"path/filepath"
	"time"

	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// StartTempCleanup 启动定期清理过期 temp 文件的 goroutine
// 每小时检查一次，删除超过 7 天且没有进行中（PENDING/UPLOADING）upload_record 的 temp 目录。
// 终态（COMPLETED/FAILED）record 不再保护 temp：COMPLETED 由 dispatch 成功即删；
// FAILED（任一 provider 上传失败，temp 是重试唯一可读源）保留 7 天供排查/重试后回收。
func StartTempCleanup() {
	go func() {
		// 启动后延迟 5 分钟再执行第一次清理，避免与 resumeDispatch 竞争
		time.Sleep(5 * time.Minute)
		cleanupOrphanedTempFiles()

		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			cleanupOrphanedTempFiles()
		}
	}()
}

// cleanupOrphanedTempFiles 清理无进行中上传的过期 temp 目录
//
// 目录结构（见 utils/path.go）：
//   - TempDir/<hash>/                  内容寻址层（旧布局 + 合并残留）
//   - TempDir/users/<ownerID>/<hash>/  上传暂存层（按 owner 隔离）
//
// 两层都要按「是否有 PENDING/UPLOADING 记录 + 是否超龄」判定，不能直接删
// TempDir/users 顶层目录（会被误当作 hash 目录，导致整层用户分片被清空）。
func cleanupOrphanedTempFiles() {
	tempDir := globals.TempDir
	if tempDir == "" {
		return
	}

	entries, err := os.ReadDir(tempDir)
	if err != nil {
		llog.L().Warnf("read temp dir failed: %v", err)
		return
	}

	const maxAge = 7 * 24 * time.Hour
	cleanedCount := 0
	skippedCount := 0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()

		// owner 隔离层：进入 users/<ownerID>/ 逐个 hash 判定
		if name == globals.UsersTempDirName {
			c, s := cleanupOwnerTempDirs(filepath.Join(tempDir, name), maxAge)
			cleanedCount += c
			skippedCount += s
			continue
		}

		// 内容寻址层：目录名即 hash
		hash := name

		// 检查是否有进行中的上传（PENDING/UPLOADING）——只有进行中才需要保护 staging
		if hasActiveUploadRecord(hash) {
			skippedCount++
			continue // 有对应记录，保留
		}

		// 检查目录修改时间
		info, err := entry.Info()
		if err != nil {
			continue
		}

		if time.Since(info.ModTime()) > maxAge {
			dirPath := filepath.Join(tempDir, hash)
			if err := os.RemoveAll(dirPath); err != nil {
				llog.L().Warnf("cleanup orphaned temp dir failed: %s, err: %v", hash, err)
			} else {
				cleanedCount++
			}
		}
	}

	if cleanedCount > 0 {
		llog.L().Infof("temp cleanup: removed %d orphaned dirs, skipped %d with active records", cleanedCount, skippedCount)
	}
}

// cleanupOwnerTempDirs 清理 users/<ownerID>/ 下超龄且无活跃上传的 hash 目录
// 返回 (清理数, 跳过数)。owner 目录本身若清空则一并删除。
func cleanupOwnerTempDirs(usersDir string, maxAge time.Duration) (int, int) {
	cleaned, skipped := 0, 0

	owners, err := os.ReadDir(usersDir)
	if err != nil {
		llog.L().Warnf("read users temp dir failed: %v", err)
		return 0, 0
	}

	for _, ownerEntry := range owners {
		if !ownerEntry.IsDir() {
			continue
		}
		ownerDir := filepath.Join(usersDir, ownerEntry.Name())
		hashes, err := os.ReadDir(ownerDir)
		if err != nil {
			continue
		}
		for _, hashEntry := range hashes {
			if !hashEntry.IsDir() {
				continue
			}
			hash := hashEntry.Name()
			if hasActiveUploadRecord(hash) {
				skipped++
				continue
			}
			info, err := hashEntry.Info()
			if err != nil {
				continue
			}
			if time.Since(info.ModTime()) > maxAge {
				if rmErr := os.RemoveAll(filepath.Join(ownerDir, hash)); rmErr != nil {
					llog.L().Warnf("cleanup orphaned owner temp dir failed: %s/%s, err: %v", ownerEntry.Name(), hash, rmErr)
				} else {
					cleaned++
				}
			}
		}
		// owner 目录已空则移除，避免 users/ 下堆积空目录
		if remaining, _ := os.ReadDir(ownerDir); len(remaining) == 0 {
			_ = os.Remove(ownerDir)
		}
	}

	return cleaned, skipped
}

// hasActiveUploadRecord 判断该 hash 是否存在进行中（PENDING/UPLOADING）的上传记录
func hasActiveUploadRecord(hash string) bool {
	var count int64
	if err := db.GetDB().Model(&native.UploadRecordORM{}).
		Where("file_hash = ? AND task_status IN ?", hash,
			[]int32{int32(native.UploadStatus_PENDING), int32(native.UploadStatus_UPLOADING)}).
		Count(&count).Error; err != nil {
		llog.L().Warnf("check upload record for temp cleanup failed: %v", err)
		return true // 查询失败时保守保留，避免误删
	}
	return count > 0
}
