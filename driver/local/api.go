package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/pkg/llog"
)

// localDirBackend 基于本地文件系统的目录后端
type localDirBackend struct{}

func (b *localDirBackend) List(ctx context.Context, dir string) ([]types.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	result := make([]types.DirEntry, 0, len(entries))
	for _, e := range entries {
		result = append(result, types.DirEntry{Name: e.Name(), IsDir: e.IsDir()})
	}
	return result, nil
}

func (b *localDirBackend) Mkdir(ctx context.Context, dir string) error {
	return os.MkdirAll(dir, 0755)
}

// Upload 上传文件（本地存储始终不加密）
func (l *LocalDriver) Upload(ctx context.Context, src types.UploadSource, opt types.UploadOptions) ([]types.FileRef, error) {
	rootDir := *l.Config.RootPath
	filePath := src.TempPath
	fileName := src.FileName

	// 本地存储始终不加密，忽略 encryptor 选项
	_ = opt

	// 1. 获取上传目录（年/月/数字子目录结构）
	uploadDir, err := types.AllocateUploadDir(ctx, rootDir, &localDirBackend{})
	if err != nil {
		llog.C(ctx).Errorf("get upload directory failed: %v", err)
		return nil, fmt.Errorf("get upload directory failed: %w", err)
	}

	// 2. 使用公共方法生成唯一文件名
	finalFileName := types.GetUniqueFileName(fileName)

	// 目标文件路径
	targetPath := filepath.Join(uploadDir, finalFileName)

	// 3. 打开源文件
	srcFile, err := os.Open(filePath)
	if err != nil {
		llog.C(ctx).Errorf("open source file failed: %v", err)
		return nil, fmt.Errorf("open source file failed: %w", err)
	}
	defer srcFile.Close()

	// 4. 创建目标文件
	dstFile, err := os.Create(targetPath)
	if err != nil {
		llog.C(ctx).Errorf("create target file failed: %v", err)
		return nil, fmt.Errorf("create target file failed: %w", err)
	}
	defer dstFile.Close()

	// 5. 直接复制数据（不加密）
	written, err := io.Copy(dstFile, srcFile)
	if err != nil {
		llog.C(ctx).Errorf("copy file failed: %v", err)
		return nil, fmt.Errorf("copy file failed: %w", err)
	}

	llog.C(ctx).Infof("upload completed, file: %s, size: %d bytes", targetPath, written)

	// 6. 返回文件引用（本地存储只有一个文件，不涉及分片）
	ref := types.FileRef{
		ProviderFileID: finalFileName, // 本地存储使用文件名作为ID
		Name:           finalFileName,
		Path:           targetPath,
		Size:           uint64(written), // 文件大小
		ChunkSize:      uint64(written), // 单文件，分片大小等于文件大小
		Hash:           "",              // 本地存储不计算hash
	}

	return []types.FileRef{ref}, nil
}

// Open 打开文件（本地存储直接打开文件，不加密不分片）
func (l *LocalDriver) Open(ctx context.Context, refs []types.FileRef, opt types.OpenOptions) (types.ReadSeekCloser, error) {
	if len(refs) == 0 {
		return nil, fmt.Errorf("no file refs provided")
	}

	// 本地存储不分片，直接打开第一个文件
	firstRef := refs[0]
	if firstRef.Path == "" {
		return nil, fmt.Errorf("file path is empty")
	}

	// 直接打开文件（本地存储不加密）
	file, err := os.Open(firstRef.Path)
	if err != nil {
		return nil, fmt.Errorf("open file failed: %w", err)
	}

	return file, nil
}

// BatchDelete 批量删除文件
func (l *LocalDriver) BatchDelete(ctx context.Context, refs []types.FileRef) ([]types.DeleteResult, error) {
	results := make([]types.DeleteResult, len(refs))
	var hasFailure bool

	for i, ref := range refs {
		results[i].ID = ref.LocationID
		if ref.Path == "" {
			results[i].Success = false
			results[i].Error = fmt.Errorf("file path is empty")
			hasFailure = true
			continue
		}
		if err := os.Remove(ref.Path); err != nil {
			results[i].Success = false
			results[i].Error = fmt.Errorf("delete file failed: %w, path: %s", err, ref.Path)
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
