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
func (t *LocalDriver) Upload(ctx context.Context, filePath string, fileName string, encryptor types.Encryptor) (*types.StorageProviderFileInfo, error) {
	rootDir := *t.Config.RootPath

	// 本地存储始终不加密，忽略encryptor参数
	_ = encryptor

	// 1. 按年/月创建目录结构
	now := time.Now()
	yearMonthDir := path.Join(rootDir, now.Format("2006"), now.Format("01"))
	if err := os.MkdirAll(yearMonthDir, 0755); err != nil {
		llog.L().Errorf("create directory failed: %v", err)
		return nil, fmt.Errorf("create directory failed: %w", err)
	}

	// 2. 生成带时间戳的文件名，避免同名覆盖
	timestamp := now.Format("20060102150405")
	finalFileName := fmt.Sprintf("%s_%s", fileName, timestamp)

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

	// 6. 返回文件信息
	return &types.StorageProviderFileInfo{
		ID:       finalFileName, // 本地存储使用文件名作为ID
		Name:     finalFileName,
		Path:     targetPath,
		HashType: native.HashType_HASH_TYPE_UNSPECIFIED,
		Hash:     "", // 本地存储不计算hash
	}, nil
}

// Link 获取文件链接
func (t *LocalDriver) Link(ctx context.Context, fileLocation *native.FileLocationORM) (string, error) {
	return *fileLocation.Path, nil
}

// Delete 删除文件
func (t *LocalDriver) Delete(ctx context.Context, fileLocation *native.FileLocationORM) error {
	return os.Remove(*fileLocation.Path)
}
