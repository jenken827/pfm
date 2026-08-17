package types

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"time"
)

// MaxFilesPerDir 每个目录最大文件数
// 当年月目录下的文件数达到此值时，创建数字子文件夹
const MaxFilesPerDir = 500

// DirEntry 目录条目
type DirEntry struct {
	Name  string
	IsDir bool
}

// DirBackend 目录后端（List/Mkdir 由各驱动适配：local 用 os，云端用 API）
type DirBackend interface {
	// List 列出目录下所有条目
	List(ctx context.Context, dir string) ([]DirEntry, error)
	// Mkdir 创建目录（应容忍"已存在"）
	Mkdir(ctx context.Context, dir string) error
}

// AllocateUploadDir 年/月/数字子目录 sharding 的单一实现
// 当年月目录文件数达 MaxFilesPerDir 时创建/递增数字子目录
// 路径统一使用正斜杠；local backend 由 os 自动转换
func AllocateUploadDir(ctx context.Context, root string, backend DirBackend) (string, error) {
	now := time.Now()
	yearMonthDir := path.Join(root, now.Format("2006"), now.Format("01"))
	if err := backend.Mkdir(ctx, yearMonthDir); err != nil {
		return "", fmt.Errorf("create year-month directory failed: %w", err)
	}

	entries, err := backend.List(ctx, yearMonthDir)
	if err != nil {
		return "", fmt.Errorf("list year-month directory failed: %w", err)
	}

	directFileCount := 0
	maxSubDirNum := 0
	for _, entry := range entries {
		if entry.IsDir {
			if n, err := strconv.Atoi(entry.Name); err == nil && n > maxSubDirNum {
				maxSubDirNum = n
			}
		} else {
			directFileCount++
		}
	}

	// 情况1：没有数字子目录，且直接文件数未满 → 直接用年月目录
	if maxSubDirNum == 0 && directFileCount < MaxFilesPerDir {
		return yearMonthDir, nil
	}
	// 情况2：没有数字子目录但文件已满 → 创建 1/
	if maxSubDirNum == 0 {
		subDir := path.Join(yearMonthDir, "1")
		if err := backend.Mkdir(ctx, subDir); err != nil {
			return "", fmt.Errorf("create subdirectory 1 failed: %w", err)
		}
		return subDir, nil
	}
	// 情况3：有数字子目录 → 检查最大子目录的文件数
	maxSubDir := path.Join(yearMonthDir, strconv.Itoa(maxSubDirNum))
	subEntries, err := backend.List(ctx, maxSubDir)
	if err != nil {
		return "", fmt.Errorf("list subdirectory %d failed: %w", maxSubDirNum, err)
	}
	subFileCount := 0
	for _, entry := range subEntries {
		if !entry.IsDir {
			subFileCount++
		}
	}
	if subFileCount < MaxFilesPerDir {
		return maxSubDir, nil
	}
	newSubDir := path.Join(yearMonthDir, strconv.Itoa(maxSubDirNum+1))
	if err := backend.Mkdir(ctx, newSubDir); err != nil {
		return "", fmt.Errorf("create subdirectory %d failed: %w", maxSubDirNum+1, err)
	}
	return newSubDir, nil
}
