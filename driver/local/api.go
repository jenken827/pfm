package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// Upload 上传文件（本地存储始终不加密）
func (l *LocalDriver) Upload(ctx context.Context, filePath string, fileName string, encryptor types.Encryptor) ([]*types.StorageProviderFileInfo, error) {
	rootDir := *l.Config.RootPath

	// 本地存储始终不加密，忽略encryptor参数
	_ = encryptor

	// 1. 按年/月创建目录结构
	now := time.Now()
	yearMonthDir := path.Join(rootDir, now.Format("2006"), now.Format("01"))
	if err := os.MkdirAll(yearMonthDir, 0755); err != nil {
		llog.L().Errorf("create directory failed: %v", err)
		return nil, fmt.Errorf("create directory failed: %w", err)
	}

	// 2. 使用公共方法生成唯一文件名
	finalFileName := types.GetUniqueFileName(fileName)

	// 目标文件路径
	targetPath := path.Join(yearMonthDir, finalFileName)

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
