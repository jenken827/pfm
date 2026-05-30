package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// getUploadDir 获取上传目录（年/月/数字子目录结构）
// 逻辑：优先使用年月目录，文件数达到 MaxFilesPerDir 时创建数字子文件夹
func (l *LocalDriver) getUploadDir(rootDir string) (string, error) {
	now := time.Now()
	yearMonthDir := filepath.Join(rootDir, now.Format("2006"), now.Format("01"))

	// 确保年月目录存在
	if err := os.MkdirAll(yearMonthDir, 0755); err != nil {
		return "", fmt.Errorf("create year-month directory failed: %w", err)
	}

	// 读取年月目录下所有条目
	entries, err := os.ReadDir(yearMonthDir)
	if err != nil {
		return "", fmt.Errorf("read year-month directory failed: %w", err)
	}

	// 统计直接文件数和最大数字子目录名
	directFileCount := 0
	maxSubDirNum := 0
	for _, entry := range entries {
		if entry.IsDir() {
			// 检查是否为数字命名的子目录
			if n, err := strconv.Atoi(entry.Name()); err == nil && n > maxSubDirNum {
				maxSubDirNum = n
			}
		} else {
			directFileCount++
		}
	}

	// 情况1：没有数字子目录，且直接文件数未满 → 直接用年月目录
	if maxSubDirNum == 0 && directFileCount < types.MaxFilesPerDir {
		return yearMonthDir, nil
	}

	// 情况2：没有数字子目录但文件已满 → 创建 1/
	if maxSubDirNum == 0 {
		subDir := filepath.Join(yearMonthDir, "1")
		if err := os.MkdirAll(subDir, 0755); err != nil {
			return "", fmt.Errorf("create subdirectory 1 failed: %w", err)
		}
		return subDir, nil
	}

	// 情况3：有数字子目录 → 检查最大子目录的文件数
	maxSubDir := filepath.Join(yearMonthDir, strconv.Itoa(maxSubDirNum))
	subEntries, err := os.ReadDir(maxSubDir)
	if err != nil {
		return "", fmt.Errorf("read subdirectory %d failed: %w", maxSubDirNum, err)
	}

	subFileCount := 0
	for _, entry := range subEntries {
		if !entry.IsDir() {
			subFileCount++
		}
	}

	if subFileCount < types.MaxFilesPerDir {
		// 最大子目录未满，继续使用
		return maxSubDir, nil
	}

	// 最大子目录已满，创建新的
	newSubDir := filepath.Join(yearMonthDir, strconv.Itoa(maxSubDirNum+1))
	if err := os.MkdirAll(newSubDir, 0755); err != nil {
		return "", fmt.Errorf("create subdirectory %d failed: %w", maxSubDirNum+1, err)
	}
	return newSubDir, nil
}

// Upload 上传文件（本地存储始终不加密）
func (l *LocalDriver) Upload(ctx context.Context, record *native.UploadRecordORM, encryptor types.Encryptor) ([]*types.StorageProviderFileInfo, error) {
	rootDir := *l.Config.RootPath
	filePath := *record.TempPath
	fileName := *record.FileName

	// 本地存储始终不加密，忽略encryptor参数
	_ = encryptor

	// 1. 获取上传目录（年/月/数字子目录结构）
	uploadDir, err := l.getUploadDir(rootDir)
	if err != nil {
		llog.L().Errorf("get upload directory failed: %v", err)
		return nil, fmt.Errorf("get upload directory failed: %w", err)
	}

	// 2. 使用公共方法生成唯一文件名
	finalFileName := types.GetUniqueFileName(fileName)

	// 目标文件路径
	targetPath := filepath.Join(uploadDir, finalFileName)

	// 3. 打开源文件
	srcFile, err := os.Open(filePath)
	if err != nil {
		llog.L().Errorf("open source file failed: %v", err)
		return nil, fmt.Errorf("open source file failed: %w", err)
	}
	defer srcFile.Close()

	// 4. 创建目标文件
	dstFile, err := os.Create(targetPath)
	if err != nil {
		llog.L().Errorf("create target file failed: %v", err)
		return nil, fmt.Errorf("create target file failed: %w", err)
	}
	defer dstFile.Close()

	// 5. 直接复制数据（不加密）
	written, err := io.Copy(dstFile, srcFile)
	if err != nil {
		llog.L().Errorf("copy file failed: %v", err)
		return nil, fmt.Errorf("copy file failed: %w", err)
	}

	llog.L().Infof("upload completed, file: %s, size: %d bytes", targetPath, written)

	// 6. 返回文件信息（本地存储只有一个文件，不涉及分片）
	info := &types.StorageProviderFileInfo{
		ID:        finalFileName, // 本地存储使用文件名作为ID
		Name:      finalFileName,
		Path:      targetPath,
		Size:      uint64(written), // 文件大小
		ChunkSize: uint64(written), // 单文件，分片大小等于文件大小
		Hash:      "",              // 本地存储不计算hash
	}

	return []*types.StorageProviderFileInfo{info}, nil
}

// Open 打开文件（本地存储直接打开文件，不加密不分片）
func (l *LocalDriver) Open(ctx context.Context, fileLocations []*native.FileLocationORM, encryptor types.Encryptor) (types.ReadSeekCloser, error) {
	if len(fileLocations) == 0 {
		return nil, fmt.Errorf("no file locations provided")
	}

	// 本地存储不分片，直接打开第一个文件
	firstLoc := fileLocations[0]
	if firstLoc.Path == nil || *firstLoc.Path == "" {
		return nil, fmt.Errorf("file path is empty")
	}

	// 直接打开文件（本地存储不加密）
	file, err := os.Open(*firstLoc.Path)
	if err != nil {
		return nil, fmt.Errorf("open file failed: %w", err)
	}

	return file, nil
}

// BatchDelete 批量删除文件
func (l *LocalDriver) BatchDelete(ctx context.Context, fileLocations []*native.FileLocationORM) ([]types.DeleteResult, error) {
	results := make([]types.DeleteResult, len(fileLocations))
	var hasFailure bool

	for i, loc := range fileLocations {
		results[i].ID = *loc.Id
		if loc.Path == nil {
			results[i].Success = false
			results[i].Error = fmt.Errorf("file path is nil")
			hasFailure = true
			continue
		}
		if err := os.Remove(*loc.Path); err != nil {
			results[i].Success = false
			results[i].Error = fmt.Errorf("delete file failed: %w, path: %s", err, *loc.Path)
			hasFailure = true
		} else {
			results[i].Success = true
		}
	}

	// 如果有失败，返回详细结果；否则返回 nil, nil 表示全部成功
	if hasFailure {
		return results, nil
	}
	return nil, nil
}
