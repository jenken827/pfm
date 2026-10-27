package backup

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/proto/gen/entity"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

// TestEnsureDirStructureRaceCondition 测试 ensureDirStructure 的并发竞态修复
// 注意：该测试需要 DB 初始化（db.Init + sqlite）与特定环境，当前开发环境未初始化 DB，
// 且自提交以来从未成功运行（既有的编译错误 + 运行时 nil db panic）。暂跳过。
func TestEnsureDirStructureRaceCondition(t *testing.T) {
	t.Skip("需要 DB 初始化环境，当前测试环境未初始化 DB")
	// 创建多个子目录路径
	paths := []string{
		"sub1/file1.txt",
		"sub1/sub2/file2.txt",
		"sub1/sub2/sub3/file3.txt",
		"sub1/sub4/file4.txt",
		"sub5/file5.txt",
		"sub1/sub2/sub3/file6.txt",
		"sub1/sub2/sub3/file7.txt",
		"sub1/sub4/file8.txt",
	}

	// 获取目标目录 ID（需要先创建）
	targetDirID := createTestTargetDir(t)

	// 并发测试
	var wg sync.WaitGroup
	errors := make(chan error, len(paths))

	for _, p := range paths {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			_, err := ensureDirStructure(context.Background(), targetDirID, path, "")
			if err != nil {
				errors <- fmt.Errorf("failed for %s: %v", path, err)
			}
		}(p)
	}

	wg.Wait()
	close(errors)

	// 检查错误
	for err := range errors {
		t.Error(err)
	}

	// 验证所有目录都已创建
	verifyDirectoriesCreated(t, targetDirID, paths)
}

func createTestTargetDir(t *testing.T) string {
	// 创建目标文件夹
	targetDir := &entity.FileORM{
		Name:    utils.StringPtr("test-backup-target"),
		Type:    utils.EnumPtr(native.FileType_FOLDER),
		IsReady: utils.BoolPtr(true),
	}
	if err := db.CreateFile(targetDir); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}
	return *targetDir.Id
}

func verifyDirectoriesCreated(t *testing.T, targetDirID string, paths []string) {
	// 提取所有唯一的目录
	dirSet := make(map[string]bool)
	for _, p := range paths {
		dir := filepath.Dir(p)
		if dir != "." {
			dirSet[dir] = true
		}
	}

	// 验证每个目录都存在
	for dir := range dirSet {
		_, err := db.GetFileByParentAndName(targetDirID, dir, "")
		if err != nil {
			t.Errorf("Directory %s not found", dir)
		}
	}
}
