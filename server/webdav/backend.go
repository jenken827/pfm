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
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/globals"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/service"
	"github.com/jenken827/pfm/pkg/llog"
	"github.com/jenken827/pfm/utils"
	"lukechampine.com/blake3"
)

// Backend WebDAV 文件系统后端
type Backend struct {
	// rootID 是「WebDAV 账号绑定的目录」file_id（不是用户网盘根）。
	// 客户端可见的 "/" 即该目录，所有路径都从这里逐级向下解析，
	// 因此账号天然被限制在该子树内（".." 被当作普通子目录名查库 → 查不到 → 404）。
	rootID  string
	ownerID string
	// mountPrefix 是 HTTP 路由 StripPrefix 剥掉的前缀（见 server/router.go 的
	// "/dav/"）。PROPFIND/MKCOL 等响应里的 href 必须与请求 URI 的路径一致
	// （RFC 4918 §8.3、§9.1），否则客户端按路径匹配子项时会把条目全部丢掉。
	mountPrefix string
}

// NewBackend 创建 WebDAV 后端
// rootID 为账号绑定的根目录 file ID，ownerID 为当前账号归属用户 ID（与 File.owner_id 一致）
// mountPrefix 为路由上被 StripPrefix 剥掉的前缀（客户端可见路径 = mountPrefix + 路径）
func NewBackend(rootID, ownerID, mountPrefix string) *Backend {
	return &Backend{rootID: rootID, ownerID: ownerID, mountPrefix: mountPrefix}
}

// resolvePath 解析 WebDAV 路径到 PFM 文件
func (b *Backend) resolvePath(ctx context.Context, name string) (*entity.FileORM, error) {
	// 去掉全部尾斜杠（而不是单个）：MKCOL/MOVE 的 URI 允许以 "/" 结尾，
	// "/a/b//" 也要能解析成同一个目录
	name = strings.TrimRight(strings.TrimPrefix(name, "/"), "/")

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
	var current *entity.FileORM
	for _, part := range parts {
		if part == "" {
			// 容忍 "/a//b" 这类多余分隔符。注意 ".." 仍按普通名字去库里查
			// （查不到即 404），所以这里不需要也不允许做路径归一化——
			// 那会把 "/../xyz" 变成兄弟目录，绕过账号根隔离。
			continue
		}
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

// splitWebdavName 把 WebDAV 请求路径拆成「父路径 + 末级名称」。
//
// MKCOL / PUT 的 request-URI 与 MOVE 的 Destination 都允许以 "/" 结尾
// （RFC 4918 §9.3 只要求 MKCOL 不带请求体，并未禁止尾斜杠；Windows 资源管理器、
// macOS Finder、rclone 在创建或移动集合时都会带）。而 path.Dir("/a/b/") = "/a/b"、
// path.Base("/a/b/") = "b"，直接用会变成「在 /a/b 下建 b」——比客户端请求的层级
// **多一层**：父目录不存在时报 404→409，父目录已存在时则静默建深一层。
//
// 判据是「层级」，不是「服务端出现同名嵌套」：绑定目录与目标同名时（用户在网盘里
// 自建 photos/ 并把 WebDAV 账号绑到它，客户端看不到这一层），客户端建 photos 的
// 正确服务端路径本来就是 photos/photos，属正常结果。
//
// 返回 ok=false 表示该路径指向根目录（"" 或 "/"），不能作为创建/移动目标。
func splitWebdavName(name string) (dirPath, baseName string, ok bool) {
	name = strings.TrimRight(name, "/")
	if name == "" {
		return "", "", false
	}
	dirPath = path.Dir(name)
	baseName = path.Base(name)
	if baseName == "" || baseName == "/" || baseName == "." || baseName == ".." {
		return "", "", false
	}
	return dirPath, baseName, true
}

// stripMountPrefix 去掉 Destination 里客户端可见的挂载前缀（如 "/dav"）。
//
// http.StripPrefix 只改 r.URL.Path；MOVE/COPY 的 Destination 头由
// emersion/go-webdav 的 parseDestination 直接 url.Parse（Href 就是 url.URL），
// 前缀原样保留。不剥掉的话 dest 会按 "/dav/sub/x" 去库里找名为 "dav" 的子目录，
// 一律 404 dest not found——即所有正常客户端的 MOVE 都失败。
func (b *Backend) stripMountPrefix(p string) string {
	if b.mountPrefix == "" {
		return p
	}
	if p == b.mountPrefix {
		return "/"
	}
	if strings.HasPrefix(p, b.mountPrefix+"/") {
		return strings.TrimPrefix(p, b.mountPrefix)
	}
	return p
}

// toFileInfo 转换为 WebDAV FileInfo
//
// Path 必须是「客户端可见路径」：本 handler 挂在 /dav/ 上并由 StripPrefix("/dav")
// 处理，后端看到的路径是剥掉前缀后的 "/webvav/…"，而 PROPFIND 的 href 必须与请求
// URI 的路径（"/dav/webvav/…"）一致，否则按路径匹配子项的客户端会把所有条目丢掉
// （ReadAny 因此判定「远端没有任何书」，每轮重传整个本地书库）。
func (b *Backend) toFileInfo(file *entity.FileORM, webdavPath string) gowebdav.FileInfo {
	fi := gowebdav.FileInfo{Path: b.mountPrefix + webdavPath}
	if file.Size != nil {
		fi.Size = int64(*file.Size)
	}
	if file.UpdatedAt != nil {
		// Last-Modified 必须是「内容最后修改时间」。用 CreatedAt 时，覆盖上传
		// 不会让这个值前进，客户端拿 Last-Modified 做 If-Modified-Since 或计算
		// 启发式新鲜度时会把旧副本当成最新的（ReadAny 曾在 Android 上因此读到
		// 几小时前的书文件，把对端的注解覆盖掉）。
		fi.ModTime = *file.UpdatedAt
	} else if file.CreatedAt != nil {
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
				llog.C(ctx).Warnf("recursive readdir failed: %v", err)
				continue
			}
			result = append(result, children...)
		}
	}
	return result, nil
}

// checkPrecondition 检查 If-Match / If-None-Match 条件请求头
// 返回 412 错误表示条件不满足
func checkPrecondition(existing *entity.FileORM, ifMatch, ifNoneMatch gowebdav.ConditionalMatch) error {
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
	// PUT 到以 "/" 结尾的 URI 表示「向集合写内容」，RFC 4918 §9.7.2 明确不允许。
	// 不拦的话 path.Dir/path.Base 会把 "/foo/" 当「在 /foo 下建文件 foo」，
	// 父目录不存在时报 404、存在时静默建出 /foo/foo 这种错层文件。
	if strings.HasSuffix(name, "/") {
		return nil, false, gowebdav.NewHTTPError(405, fmt.Errorf("cannot PUT to a collection: %s", name))
	}
	dirPath, fileName, ok := splitWebdavName(name)
	if !ok {
		return nil, false, gowebdav.NewHTTPError(405, fmt.Errorf("invalid PUT target: %s", name))
	}

	parentFile, err := b.resolvePath(ctx, dirPath)
	if err != nil {
		return nil, false, gowebdav.NewHTTPError(404, fmt.Errorf("parent not found: %w", err))
	}

	// 检查是否已存在
	existing, err := db.GetFileByParentAndName(*parentFile.Id, fileName, b.ownerID)
	var existingFile *entity.FileORM
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

	// 并发上传仲裁：请求头到达即发号（body 读取前）。同一文件 key
	// (owner+parent+name) 下后到达的 PUT 拿到更大的版本号，后续落库 CAS
	// 保证"后到者赢"，防止两个 PUT 交错时旧请求覆盖新内容。
	// 注：发号在 resolvePath 之后、流式读取 body 之前——PUT 传输大文件时
	// 第二个 PUT 可能已到达并接管，本请求 body 收完后 CAS 会发现版本落后。
	myVersion := service.AllocateFileVersion(b.ownerID, *parentFile.Id, fileName)
	ctx = service.WithUploadVersion(ctx, myVersion)

	// 流式写入临时文件并计算 blake3 hash（与系统文件 hash 算法一致）。
	// 大文件不整读进内存：body 边收边写 staging（TempDir 根目录），
	// hash 确定后再原子迁入规范临时目录 TempDir/<hash>/<fileName>（与
	// UploadSmall 的临时文件布局一致，storage.Dispatch 的兜底查找依赖该目录）。
	// staging 与规范目录同盘，os.Rename 零拷贝；跨设备极端场景走 copy 兜底。
	staging, err := os.CreateTemp(globals.TempDir, "webdav-stage-*")
	if err != nil {
		return nil, false, gowebdav.NewHTTPError(500, fmt.Errorf("create staging file: %w", err))
	}
	stagingName := staging.Name()
	defer os.Remove(stagingName) // rename 成功后为 no-op；copy 兜底场景清理 staging

	hasher := blake3.New(32, nil)
	written, err := io.Copy(io.MultiWriter(staging, hasher), body)
	if err != nil {
		staging.Close()
		return nil, false, gowebdav.NewHTTPError(500, fmt.Errorf("write staging: %w", err))
	}
	staging.Close()

	hash := hex.EncodeToString(hasher.Sum(nil))

	if err := utils.CreateFileTempDir(hash, b.ownerID, false); err != nil {
		return nil, false, gowebdav.NewHTTPError(500, fmt.Errorf("create temp dir: %w", err))
	}
	tempPath := utils.GetFileTempPath(hash, fileName)
	if err := os.Rename(stagingName, tempPath); err != nil {
		if copyErr := copyFile(stagingName, tempPath); copyErr != nil {
			return nil, false, gowebdav.NewHTTPError(500, fmt.Errorf("move to temp dir: %w", err))
		}
	}

	// 调用 WebDAV 专用上传（无 10MB 限制，直接从临时文件入库 + 异步分发）
	size := uint64(written)
	uploadReq := &native.UploadSmallReq{
		Name:      &fileName,
		ParentId:  parentFile.Id,
		Size:      &size,
		Hash:      &hash,
		Overwrite: boolPtr(existingFile != nil),
	}

	_, mErr := service.UploadWebdavFromTemp(ctx, uploadReq, tempPath)
	if mErr != nil {
		return nil, false, gowebdav.NewHTTPError(500, fmt.Errorf("upload failed: %s", mErr.E.Error()))
	}

	return &gowebdav.FileInfo{
		Path:    name,
		Size:    int64(written),
		ModTime: time.Now(),
		IsDir:   false,
		ETag:    hash,
	}, created, nil
}

// copyFile 跨设备移动临时文件的兜底（正常同盘 os.Rename 不触发）
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
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
	// MKCOL 的 URI 允许带尾斜杠，必须先剥再 Dir/Base（见 splitWebdavName）
	dirPath, dirName, ok := splitWebdavName(name)
	if !ok {
		return gowebdav.NewHTTPError(405, fmt.Errorf("cannot create collection at root: %s", name))
	}

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

	// Destination 是客户端可见路径（带 mountPrefix），go-webdav 不会剥；
	// 且集合的 Destination 常以 "/" 结尾。两者都在 splitWebdavName 之前处理。
	destDir, destName, ok := splitWebdavName(b.stripMountPrefix(dest))
	if !ok {
		return false, gowebdav.NewHTTPError(409, fmt.Errorf("invalid move destination: %s", dest))
	}
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
			llog.C(ctx).Warnf("rename after move failed: %v", renameErr)
		}
	}

	return created, nil
}

func boolPtr(v bool) *bool { return &v }
