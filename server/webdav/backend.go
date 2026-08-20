package webdav

import (
	"context"
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
	"lukechampine.com/blake3"
)

// Backend WebDAV 文件系统后端
type Backend struct {
	rootID  string
	ownerID string
}

// NewBackend 创建 WebDAV 后端
// rootID 为用户根目录文件 ID，ownerID 为当前用户 ID（与 File.owner_id 一致）
func NewBackend(rootID, ownerID string) *Backend {
	return &Backend{rootID: rootID, ownerID: ownerID}
}

// resolvePath 解析 WebDAV 路径到 PFM 文件
func (b *Backend) resolvePath(ctx context.Context, name string) (*native.FileORM, error) {
	name = strings.TrimPrefix(name, "/")
	name = strings.TrimSuffix(name, "/")

	parentID := b.rootID

	// 根目录
	if name == "" {
		if parentID == "" {
			return nil, gowebdav.NewHTTPError(404, fmt.Errorf("root not configured"))
		}
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
		if parentID == "" {
			return nil, gowebdav.NewHTTPError(404, fmt.Errorf("not found: %s", name))
		}
		f, err := db.GetFileByParentAndName(parentID, part, b.ownerID)
		if err != nil {
			return nil, gowebdav.NewHTTPError(404, fmt.Errorf("not found: %s", name))
		}
		current = f
		parentID = *f.Id
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

	files, _, _, err := db.GetFileList(*file.Id, 0, 10000, b.ownerID)
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

// checkPrecondition 检查 If-Match / If-None-Match 条件请求头
// 返回 412 错误表示条件不满足
func checkPrecondition(existing *native.FileORM, ifMatch, ifNoneMatch gowebdav.ConditionalMatch) error {
	// 计算当前 ETag
	currentETag := ""
	if existing != nil && existing.Hash != nil {
		currentETag = *existing.Hash
	}

	// If-None-Match：条件为真时（资源不存在 或 ETag 不匹配）才允许操作
	if ifNoneMatch.IsSet() {
		matched, err := ifNoneMatch.MatchETag(currentETag)
		if err != nil {
			return gowebdav.NewHTTPError(412, fmt.Errorf("invalid If-None-Match: %w", err))
		}
		if matched {
			return gowebdav.NewHTTPError(412, fmt.Errorf("precondition failed: If-None-Match"))
		}
	}

	// If-Match：条件为真时（资源存在 且 ETag 匹配）才允许操作
	if ifMatch.IsSet() {
		matched, err := ifMatch.MatchETag(currentETag)
		if err != nil {
			return gowebdav.NewHTTPError(412, fmt.Errorf("invalid If-Match: %w", err))
		}
		if !matched {
			return gowebdav.NewHTTPError(412, fmt.Errorf("precondition failed: If-Match"))
		}
	}

	return nil
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
	existing, err := db.GetFileByParentAndName(*parentFile.Id, fileName, b.ownerID)
	var existingFile *native.FileORM
	if err == nil {
		existingFile = existing
	}
	created := existingFile == nil

	// 处理 If-Match / If-None-Match 条件请求头
	if opts != nil {
		if pErr := checkPrecondition(existingFile, opts.IfMatch, opts.IfNoneMatch); pErr != nil {
			return nil, false, pErr
		}
	}

	// 读取内容到临时文件并计算 blake3 hash（与系统文件 hash 算法一致）
	tmpFile, err := os.CreateTemp("", "webdav-upload-*")
	if err != nil {
		return nil, false, fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	hasher := blake3.New(32, nil)
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
		Name:      &fileName,
		ParentId:  parentFile.Id,
		Size:      uint64Ptr(uint64(written)),
		Hash:      &hash,
		Content:   content,
		Overwrite: boolPtr(existingFile != nil),
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
		ETag:    hash,
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

	// 处理 If-Match / If-None-Match 条件请求头
	if opts != nil {
		if pErr := checkPrecondition(file, opts.IfMatch, opts.IfNoneMatch); pErr != nil {
			return pErr
		}
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

	// 已存在同名目录/文件则返回 405（与 OpenList 行为一致）
	if _, err := db.GetFileByParentAndName(*parentFile.Id, dirName, b.ownerID); err == nil {
		return gowebdav.NewHTTPError(405, fmt.Errorf("already exists: %s", dirName))
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

// Copy 复制（当前服务端未提供 Copy RPC，暂不支持）
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
	_, err = db.GetFileByParentAndName(*destFile.Id, destName, b.ownerID)
	created := err != nil
	if err == nil && options != nil && options.NoOverwrite {
		return false, gowebdav.NewHTTPError(412, fmt.Errorf("exists"))
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
