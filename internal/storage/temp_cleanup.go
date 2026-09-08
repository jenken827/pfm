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
// FAILED（全部 provider 上传失败，temp 为内容唯一副本）保留 7 天供排查/重试后回收。
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

		hash := entry.Name()

		// 检查是否有进行中的上传（PENDING/UPLOADING）——只有进行中才需要保护 staging
		// （上传窗口内 GET 可能降级读 TempDir/<hash>/<fileName>，见 service.file_stream）
		var count int64
		if err := db.GetDB().Model(&native.UploadRecordORM{}).
			Where("file_hash = ? AND task_status IN ?", hash,
				[]int32{int32(native.UploadStatus_PENDING), int32(native.UploadStatus_UPLOADING)}).
			Count(&count).Error; err != nil {
			llog.L().Warnf("check upload record for temp cleanup failed: %v", err)
			continue
		}

		if count > 0 {
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
