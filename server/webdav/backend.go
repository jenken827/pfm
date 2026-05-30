package webdav

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	gowebdav "github.com/emersion/go-webdav"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// Backend WebDAV 文件系统后端
type Backend struct {
	rootID string
}

// NewBackend 创建 WebDAV 后端
func NewBackend(rootID string) *Backend {
	return &Backend{rootID: rootID}
}

// resolvePath 解析 WebDAV 路径到 PFM 文件
func (b *Backend) resolvePath(ctx context.Context, name string) (*native.FileORM, error) {
	name = strings.TrimPrefix(name, "/")
	name = strings.TrimSuffix(name, "/")

	parentID := b.rootID
	if parentID == "" {
		parentID = "0"
	}

	// 根目录
	if name == "" {
		file, err := db.GetFileByID(parentID)
		if err != nil {
			return nil, gowebdav.NewHTTPError(404, fmt.Errorf("root not found"))
		}
		return file, nil
	}

	// 逐级解析
	parts := strings.Split(name, "/")
	var current *native.FileORM
	for _, part := range parts {
		files, _, _, err := db.GetFileList(parentID, 0, 10000)
		if err != nil {
			return nil, fmt.Errorf("list files failed: %w", err)
		}
		found := false
		for _, f := range files {
			if f.Name != nil && *f.Name == part {
				current = f
				parentID = *f.Id
				found = true
				break
			}
		}
		if !found {
			return nil, gowebdav.NewHTTPError(404, fmt.Errorf("not found: %s", name))
		}
	}
	return current, nil
}

// toFileInfo 转换为 WebDAV FileInfo
func (b *Backend) toFileInfo(file *native.FileORM, webdavPath string) gowebdav.FileInfo {
	fi := gowebdav.FileInfo{Path: webdavPath}
	if file.Size != nil {
		fi.Size = int64(*file.Size)
	}
	if file.CreatedAt != nil {
		fi.ModTime = *file.CreatedAt
	}
	if file.Type != nil {
		fi.IsDir = *file.Type == int32(native.FileType_FOLDER)
	}
	if file.Mime != nil && *file.Mime != "" {
		fi.MIMEType = *file.Mime
	} else if !fi.IsDir && file.Name != nil {
		fi.MIMEType = mime.TypeByExtension(filepath.Ext(*file.Name))
	}
	if file.Hash != nil {
		fi.ETag = *file.Hash
	}
	return fi
}

// Open 打开文件
func (b *Backend) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	file, err := b.resolvePath(ctx, name)
	if err != nil {
		return nil, err
	}
	if file.Type != nil && *file.Type == int32(native.FileType_FOLDER) {
		return nil, gowebdav.NewHTTPError(405, fmt.Errorf("cannot open directory"))
	}
	result, err := service.GetFileStream(ctx, *file.Id, nil)
	if err != nil {
		return nil, gowebdav.NewHTTPError(500, err)
	}
	return result.Reader, nil
}

// Stat 获取文件信息
func (b *Backend) Stat(ctx context.Context, name string) (*gowebdav.FileInfo, error) {
	file, err := b.resolvePath(ctx, name)
	if err != nil {
		return nil, err
	}
	fi := b.toFileInfo(file, name)
	return &fi, nil
}

// ReadDir 读取目录
func (b *Backend) ReadDir(ctx context.Context, name string, recursive bool) ([]gowebdav.FileInfo, error) {
	file, err := b.resolvePath(ctx, name)
	if err != nil {
		return nil, err
	}
	if file.Type == nil || *file.Type != int32(native.FileType_FOLDER) {
		return nil, gowebdav.NewHTTPError(405, fmt.Errorf("not a directory"))
	}

	files, _, _, err := db.GetFileList(*file.Id, 0, 10000)
	if err != nil {
		return nil, err
	}

	var result []gowebdav.FileInfo
	for _, f := range files {
		childPath := name
		if !strings.HasSuffix(childPath, "/") {
			childPath += "/"
		}
		childPath += *f.Name
		if !strings.HasPrefix(childPath, "/") {
			childPath = "/" + childPath
		}
		fi := b.toFileInfo(f, childPath)
		result = append(result, fi)

		if recursive && f.Type != nil && *f.Type == int32(native.FileType_FOLDER) {
			children, err := b.ReadDir(ctx, childPath, true)
			if err != nil {
				llog.L().Warnf("recursive readdir failed: %v", err)
				continue
			}
			result = append(result, children...)
		}
	}
	return result, nil
}

// Create 创建/更新文件
func (b *Backend) Create(ctx context.Context, name string, body io.ReadCloser, opts *gowebdav.CreateOptions) (*gowebdav.FileInfo, bool, error) {
	dirPath := path.Dir(name)
	fileName := path.Base(name)

	parentFile, err := b.resolvePath(ctx, dirPath)
	if err != nil {
		return nil, false, gowebdav.NewHTTPError(404, fmt.Errorf("parent not found: %w", err))
	}

	// 检查是否已存在
	existingFiles, _, _, _ := db.GetFileList(*parentFile.Id, 0, 10000)
	var existing *native.FileORM
	for _, f := range existingFiles {
		if f.Name != nil && *f.Name == fileName {
			existing = f
			break
		}
	}
	created := existing == nil

	// 读取内容到临时文件并计算 hash
	tmpFile, err := os.CreateTemp("", "webdav-upload-*")
	if err != nil {
		return nil, false, fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	hasher := sha256.New()
	writer := io.MultiWriter(tmpFile, hasher)
	written, err := io.Copy(writer, body)
	if err != nil {
		tmpFile.Close()
		return nil, false, fmt.Errorf("write temp: %w", err)
	}
	tmpFile.Close()

	hash := hex.EncodeToString(hasher.Sum(nil))

	// 读取文件内容
	content, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		return nil, false, fmt.Errorf("read temp: %w", err)
	}

	// 调用 UploadSmall
	uploadReq := &native.UploadSmallReq{
		Name:     &fileName,
		ParentId: parentFile.Id,
		Size:     uint64Ptr(uint64(written)),
		Hash:     &hash,
		Content:  content,
		Overwrite: boolPtr(existing != nil),
	}

	_, mErr := service.UploadSmall(ctx, uploadReq)
	if mErr != nil {
		return nil, false, fmt.Errorf("upload failed: %s", mErr.E.Error())
	}

	return &gowebdav.FileInfo{
		Path:    name,
		Size:    written,
		ModTime: time.Now(),
		IsDir:   false,
	}, created, nil
}

// RemoveAll 删除文件
func (b *Backend) RemoveAll(ctx context.Context, name string, opts *gowebdav.RemoveAllOptions) error {
	if name == "/" || name == "" {
		return gowebdav.NewHTTPError(403, fmt.Errorf("cannot delete root"))
	}
	file, err := b.resolvePath(ctx, name)
	if err != nil {
		return err
	}
	mErr := service.DeleteFile(ctx, &native.DeleteFileReq{
		Ids: []string{*file.Id},
	})
	if mErr != nil {
		return fmt.Errorf("delete failed: %s", mErr.E.Error())
	}
	return nil
}

// Mkdir 创建目录
func (b *Backend) Mkdir(ctx context.Context, name string) error {
	dirPath := path.Dir(name)
	dirName := path.Base(name)

	parentFile, err := b.resolvePath(ctx, dirPath)
	if err != nil {
		return gowebdav.NewHTTPError(404, fmt.Errorf("parent not found: %w", err))
	}

	_, mErr := service.CreateDir(ctx, &native.CreateDirReq{
		Name:     &dirName,
		ParentId: parentFile.Id,
	})
	if mErr != nil {
		return fmt.Errorf("mkdir failed: %s", mErr.E.Error())
	}
	return nil
}

// Copy 复制（暂不支持）
func (b *Backend) Copy(ctx context.Context, name, dest string, options *gowebdav.CopyOptions) (bool, error) {
	return false, gowebdav.NewHTTPError(501, fmt.Errorf("copy not supported"))
}

// Move 移动文件
func (b *Backend) Move(ctx context.Context, name, dest string, options *gowebdav.MoveOptions) (bool, error) {
	file, err := b.resolvePath(ctx, name)
	if err != nil {
		return false, err
	}

	destDir := path.Dir(dest)
	destName := path.Base(dest)
	destFile, err := b.resolvePath(ctx, destDir)
	if err != nil {
		return false, gowebdav.NewHTTPError(404, fmt.Errorf("dest not found: %w", err))
	}

	// 检查目标是否已存在
	destFiles, _, _, _ := db.GetFileList(*destFile.Id, 0, 10000)
	created := true
	for _, f := range destFiles {
		if f.Name != nil && *f.Name == destName {
			if options != nil && options.NoOverwrite {
				return false, gowebdav.NewHTTPError(412, fmt.Errorf("exists"))
			}
			created = false
			break
		}
	}

	// 移动
	_, _, _, mErr := service.MoveFile(ctx, &native.MoveFileReq{
		FileIds:        []string{*file.Id},
		TargetParentId: destFile.Id,
	})
	if mErr != nil {
		return false, fmt.Errorf("move failed: %s", mErr.E.Error())
	}

	// 重命名
	if destName != *file.Name {
		renameErr := service.RenameFile(ctx, &native.RenameFileReq{
			RenameObjs: []*native.RenameObj{
				{Id: file.Id, NewName: &destName},
			},
		})
		if renameErr != nil {
			llog.L().Warnf("rename after move failed: %v", renameErr)
		}
	}

	return created, nil
}

func uint64Ptr(v uint64) *uint64 { return &v }
func boolPtr(v bool) *bool       { return &v }
