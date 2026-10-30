package webdav

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/globals"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/service"
	"github.com/jenken827/pfm/pkg/llog"
	"github.com/jenken827/pfm/utils"
	"lukechampine.com/blake3"
)

// Entry 文件树里的一项（协议层看到的文件）。
//
// 刻意只放标量、不出现 gorm 实体：这一层是 WebDAV 协议与宿主业务之间的缝。
// 现在 FileTree 是同进程实现，将来若要搬到进程外（插件），Entry 就是线上 DTO，
// 而 entity.FileORM 属于宿主内部表结构，永远不该跨过去。
type Entry struct {
	ID   string // 宿主侧文件 id（协议层不用，排障与后续 ABI 用）
	Name string
	// IsDir 是目录（对应 files.type = FOLDER）
	IsDir bool
	// Size 是**明文**大小（files.size）；加密文件与存储上的密文长度不同
	Size int64
	// ModTime 是内容最后修改时间：优先 UpdatedAt，缺失时退回 CreatedAt
	ModTime time.Time
	Mime    string
	// ETag 是内容哈希（blake3），空表示未知；If-Match / If-None-Match 用它比对
	ETag string
}

// ErrorKind 文件树域错误分类：由协议层决定映射成哪个 WebDAV 状态码，
// 因此数据层不出现 HTTP 概念（将来跨进程时它就是错误码枚举）。
type ErrorKind uint8

const (
	// KindNotFound 路径不存在（含上溯越界、绑定根缺失）
	KindNotFound ErrorKind = iota + 1
	// KindIsDir 该操作不接受目录（如 Open）
	KindIsDir
	// KindNotDir 该操作要求目录（如 List）
	KindNotDir
	// KindExists 目标已存在（MakeDir / 覆盖保护）
	KindExists
	// KindInvalid 路径形状不合法（指向根、集合 URI 等）
	KindInvalid
	// KindInternal 宿主内部失败（IO / 入库 / 分发）
	KindInternal
)

// TreeError 带分类的域错误。Msg 会原样进入 WebDAV 响应体，Err 只用于包装底层原因。
type TreeError struct {
	Kind ErrorKind
	Msg  string
	Err  error
}

func (e *TreeError) Error() string { return e.Msg }
func (e *TreeError) Unwrap() error { return e.Err }

func treeError(kind ErrorKind, msg string, err error) *TreeError {
	return &TreeError{Kind: kind, Msg: msg, Err: err}
}

// kindOf 取出域错误分类；非域错误按内部错误处理
func kindOf(err error) ErrorKind {
	var te *TreeError
	if errors.As(err, &te) {
		return te.Kind
	}
	return KindInternal
}

func isNotFound(err error) bool { return err != nil && kindOf(err) == KindNotFound }

// FileTree WebDAV 协议层唯一需要的宿主能力面。
//
// 方法一律吃「客户端可见路径的等价内部路径」：以账号绑定目录为根、已剥掉
// mountPrefix 的绝对路径（如 "/readany/sync/index.json"）。路径解析（逐级查库、
// 越界即 404、不做 ".." 归一化）属于宿主实现，协议层不认识 file_id。
//
// 契约（换成别的实现也必须成立）：
//   - 路径不存在 → KindNotFound；根目录由实现自己记住，协议层不传
//   - Open 返回的流必须是**明文**，且 `Size` 与它可读出的字节数一致
//   - Create 的 body 由实现消费（可流式落盘），overwrite=false 时不允许覆盖已有内容
//   - Move 的 from/to 都已完成 mountPrefix 剥离；created 表示目标是新建的
type FileTree interface {
	Stat(ctx context.Context, p string) (*Entry, error)
	List(ctx context.Context, p string) ([]*Entry, error)
	Open(ctx context.Context, p string) (io.ReadCloser, error)
	Create(ctx context.Context, p string, body io.Reader, overwrite bool) (*Entry, error)
	MakeDir(ctx context.Context, p string) error
	Remove(ctx context.Context, p string) error
	Move(ctx context.Context, from, to string, noOverwrite bool) (created bool, err error)
}

// hostFileTree FileTree 的同进程实现：账号绑定目录 + 宿主 service/db。
//
// rootID 是「WebDAV 账号绑定的目录」(files.id)，不是用户网盘根；客户端可见的 "/"
// 就是它，所以账号天然被限制在这棵子树内。
type hostFileTree struct {
	rootID  string
	ownerID string
}

// NewHostFileTree 构造宿主侧文件树（rootID 为账号绑定目录，ownerID 为归属用户）
func NewHostFileTree(rootID, ownerID string) FileTree {
	return &hostFileTree{rootID: rootID, ownerID: ownerID}
}

// resolve 把内部路径解析成文件行：逐级 GetFileByParentAndName。
//
// 注意 ".." 是**普通名字**：查不到就 404，不做路径归一化——归一化会把
// "/../xyz" 变成兄弟目录，绕过账号根隔离。
func (t *hostFileTree) resolve(ctx context.Context, p string) (*entity.FileORM, error) {
	// 去掉全部尾斜杠（而不是单个）：MKCOL/MOVE 的 URI 允许以 "/" 结尾，
	// "/a/b//" 也要能解析成同一个目录
	name := strings.TrimRight(strings.TrimPrefix(p, "/"), "/")

	parentID := t.rootID

	// 根目录
	if name == "" {
		if parentID == "" {
			return nil, treeError(KindNotFound, "root not configured", nil)
		}
		file, err := db.GetFileByID(parentID)
		if err != nil {
			return nil, treeError(KindNotFound, "root not found", err)
		}
		return file, nil
	}

	// 逐级解析
	var current *entity.FileORM
	for _, part := range strings.Split(name, "/") {
		if part == "" {
			// 容忍 "/a//b" 这类多余分隔符
			continue
		}
		if parentID == "" {
			return nil, treeError(KindNotFound, "not found: "+p, nil)
		}
		f, err := db.GetFileByParentAndName(parentID, part, t.ownerID)
		if err != nil {
			return nil, treeError(KindNotFound, "not found: "+p, err)
		}
		current = f
		parentID = *f.Id
	}
	return current, nil
}

// Stat 取路径对应的文件信息
func (t *hostFileTree) Stat(ctx context.Context, p string) (*Entry, error) {
	file, err := t.resolve(ctx, p)
	if err != nil {
		return nil, err
	}
	if file == nil {
		return nil, treeError(KindNotFound, "not found: "+p, nil)
	}
	return entryOf(file), nil
}

// List 列出目录的直接子项（不做递归；PROPFIND 的 Depth 由协议层展开）
func (t *hostFileTree) List(ctx context.Context, p string) ([]*Entry, error) {
	dir, err := t.resolve(ctx, p)
	if err != nil {
		return nil, err
	}
	if !isDir(dir) {
		return nil, treeError(KindNotDir, "not a directory", nil)
	}

	files, _, _, err := db.GetFileList(*dir.Id, 0, 10000, t.ownerID)
	if err != nil {
		return nil, treeError(KindInternal, err.Error(), err)
	}

	entries := make([]*Entry, 0, len(files))
	for _, f := range files {
		entries = append(entries, entryOf(f))
	}
	return entries, nil
}

// Open 打开文件内容（明文流）
func (t *hostFileTree) Open(ctx context.Context, p string) (io.ReadCloser, error) {
	file, err := t.resolve(ctx, p)
	if err != nil {
		return nil, err
	}
	if isDir(file) {
		return nil, treeError(KindIsDir, "cannot open directory", nil)
	}
	result, err := service.GetFileStream(ctx, *file.Id, nil)
	if err != nil {
		return nil, treeError(KindInternal, err.Error(), err)
	}
	return result.Reader, nil
}

// Create 收流 → 落临时文件 → 入库 → 异步分发
//
// 大文件不整读进内存：body 边收边写 staging（TempDir 根目录），hash 确定后再
// 原子迁入规范临时目录 TempDir/<hash>/<fileName>（与 UploadSmall 的临时文件布局一致，
// storage.Dispatch 的兜底查找依赖该目录）。staging 与规范目录同盘，os.Rename 零拷贝。
func (t *hostFileTree) Create(ctx context.Context, p string, body io.Reader, overwrite bool) (*Entry, error) {
	dirPath, fileName, ok := splitWebdavName(p)
	if !ok {
		return nil, treeError(KindInvalid, "invalid PUT target: "+p, nil)
	}
	parent, err := t.resolve(ctx, dirPath)
	if err != nil {
		return nil, treeError(KindNotFound, "parent not found: "+err.Error(), err)
	}

	// 并发上传仲裁：请求头到达即发号（body 读取前）。同一文件 key
	// (owner+parent+name) 下后到达的 PUT 拿到更大的版本号，后续落库 CAS
	// 保证"后到者赢"，防止两个 PUT 交错时旧请求覆盖新内容。
	// 注：发号在父目录解析之后、流式读取 body 之前——PUT 传输大文件时
	// 第二个 PUT 可能已到达并接管，本请求 body 收完后 CAS 会发现版本落后。
	myVersion := service.AllocateFileVersion(t.ownerID, *parent.Id, fileName)
	ctx = service.WithUploadVersion(ctx, myVersion)

	staging, err := os.CreateTemp(globals.TempDir, "webdav-stage-*")
	if err != nil {
		return nil, treeError(KindInternal, "create staging file: "+err.Error(), err)
	}
	stagingName := staging.Name()
	defer os.Remove(stagingName) // rename 成功后为 no-op；copy 兜底场景清理 staging

	hasher := blake3.New(32, nil)
	written, err := io.Copy(io.MultiWriter(staging, hasher), body)
	if err != nil {
		staging.Close()
		return nil, treeError(KindInternal, "write staging: "+err.Error(), err)
	}
	staging.Close()

	hash := hex.EncodeToString(hasher.Sum(nil))

	if err := utils.CreateFileTempDir(hash, t.ownerID, false); err != nil {
		return nil, treeError(KindInternal, "create temp dir: "+err.Error(), err)
	}
	tempPath := utils.GetFileTempPath(hash, fileName)
	if err := os.Rename(stagingName, tempPath); err != nil {
		if copyErr := copyFile(stagingName, tempPath); copyErr != nil {
			return nil, treeError(KindInternal, "move to temp dir: "+err.Error(), err)
		}
	}

	// 调用 WebDAV 专用上传（无 10MB 限制，直接从临时文件入库 + 异步分发）
	size := uint64(written)
	uploadReq := &native.UploadSmallReq{
		Name:      &fileName,
		ParentId:  parent.Id,
		Size:      &size,
		Hash:      &hash,
		Overwrite: &overwrite,
	}
	if _, mErr := service.UploadWebdavFromTemp(ctx, uploadReq, tempPath); mErr != nil {
		return nil, treeError(KindInternal, "upload failed: "+mErr.E.Error(), nil)
	}

	return &Entry{
		Name:    fileName,
		Size:    int64(written),
		ModTime: time.Now(),
		ETag:    hash,
	}, nil
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

// MakeDir 建目录；同名已存在时返回 KindExists（协议层映射 405）
func (t *hostFileTree) MakeDir(ctx context.Context, p string) error {
	dirPath, dirName, ok := splitWebdavName(p)
	if !ok {
		return treeError(KindInvalid, "cannot create collection at root: "+p, nil)
	}
	parent, err := t.resolve(ctx, dirPath)
	if err != nil {
		return treeError(KindNotFound, "parent not found: "+err.Error(), err)
	}

	// 已存在同名目录/文件则拒绝（与 OpenList 行为一致）
	if _, err := db.GetFileByParentAndName(*parent.Id, dirName, t.ownerID); err == nil {
		return treeError(KindExists, "already exists: "+dirName, nil)
	}

	if _, mErr := service.CreateDir(ctx, &native.CreateDirReq{
		Name:     &dirName,
		ParentId: parent.Id,
	}); mErr != nil {
		return treeError(KindInternal, "mkdir failed: "+mErr.E.Error(), nil)
	}
	return nil
}

// Remove 删除文件/目录（软删 + 异步清理存储副本）
func (t *hostFileTree) Remove(ctx context.Context, p string) error {
	if strings.TrimRight(p, "/") == "" {
		// 账号根不可删（协议层也会拦，这里兜底，避免误把绑定目录整棵删掉）
		return treeError(KindInvalid, "cannot delete root", nil)
	}
	file, err := t.resolve(ctx, p)
	if err != nil {
		return err
	}
	if mErr := service.DeleteFile(ctx, &native.DeleteFileReq{Ids: []string{*file.Id}}); mErr != nil {
		return treeError(KindInternal, "delete failed: "+mErr.E.Error(), nil)
	}
	return nil
}

// Move 移动（可同时改名）；noOverwrite 时目标已存在返回 KindExists
func (t *hostFileTree) Move(ctx context.Context, from, to string, noOverwrite bool) (bool, error) {
	file, err := t.resolve(ctx, from)
	if err != nil {
		return false, err
	}

	destDir, destName, ok := splitWebdavName(to)
	if !ok {
		return false, treeError(KindInvalid, "invalid move destination: "+to, nil)
	}
	destFile, err := t.resolve(ctx, destDir)
	if err != nil {
		return false, treeError(KindNotFound, "dest not found: "+err.Error(), err)
	}

	// 目标已存在时：默认覆盖（与 go-webdav 的 Overwrite 语义一致），NoOverwrite 则 412
	_, err = db.GetFileByParentAndName(*destFile.Id, destName, t.ownerID)
	created := err != nil
	if err == nil && noOverwrite {
		return false, treeError(KindExists, "exists", nil)
	}

	_, _, _, mErr := service.MoveFile(ctx, &native.MoveFileReq{
		FileIds:        []string{*file.Id},
		TargetParentId: destFile.Id,
	})
	if mErr != nil {
		return false, treeError(KindInternal, "move failed: "+mErr.E.Error(), nil)
	}

	// 改名（移动与改名是两次独立操作，改名失败不回滚移动：旧行为如此）
	if file.Name != nil && destName != *file.Name {
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

// entryOf 把文件行投影成协议层的 Entry
func entryOf(f *entity.FileORM) *Entry {
	e := &Entry{}
	if f.Id != nil {
		e.ID = *f.Id
	}
	if f.Name != nil {
		e.Name = *f.Name
	}
	if f.Size != nil {
		e.Size = int64(*f.Size)
	}
	if f.Type != nil {
		e.IsDir = *f.Type == int32(native.FileType_FOLDER)
	}
	// Last-Modified 必须是「内容最后修改时间」。用 CreatedAt 时，覆盖上传
	// 不会让这个值前进，客户端拿 Last-Modified 做 If-Modified-Since 或计算
	// 启发式新鲜度时会把旧副本当成最新的（ReadAny 曾在 Android 上因此读到
	// 几小时前的书文件，把对端的注解覆盖掉）。
	if f.UpdatedAt != nil {
		e.ModTime = *f.UpdatedAt
	} else if f.CreatedAt != nil {
		e.ModTime = *f.CreatedAt
	}
	if f.Mime != nil {
		e.Mime = *f.Mime
	}
	if f.Hash != nil {
		e.ETag = *f.Hash
	}
	return e
}

// isDir 安全判断目录类型（Type 可能为 nil）
func isDir(f *entity.FileORM) bool {
	return f != nil && f.Type != nil && *f.Type == int32(native.FileType_FOLDER)
}
