package webdav

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	gowebdav "github.com/emersion/go-webdav"
	"github.com/jenken827/pfm/pkg/llog"
)

// Backend 把 go-webdav 的 FileSystem 接口翻译到 FileTree。
//
// 分层：这一层只做「协议 ↔ 文件树」的翻译——路径形状（尾斜杠、mountPrefix、
// href）、条件请求（If-Match / If-None-Match）与状态码映射。文件从哪来、存到哪、
// 怎么入库与分发，全部由 FileTree 负责（见 tree.go）。
//
// 这样切的好处：协议语义（go-webdav 的坑）与业务语义（归属、去重、版本、分发）
// 可以分别测；将来若把文件树搬到进程外，替换的只是 FileTree 实现。
type Backend struct {
	tree FileTree
	// mountPrefix 是 HTTP 路由 StripPrefix 剥掉的前缀（见 server/router.go 的
	// "/dav/"）。PROPFIND/MKCOL 等响应里的 href 必须与请求 URI 的路径一致
	// （RFC 4918 §8.3、§9.1），否则客户端按路径匹配子项时会把条目全部丢掉。
	mountPrefix string
}

// NewBackend 创建 WebDAV 后端（宿主侧文件树）
//
// rootID 为账号绑定的根目录 file ID（客户端可见的 "/"），ownerID 为当前账号归属
// 用户 ID（与 File.owner_id 一致），mountPrefix 为路由上被 StripPrefix 剥掉的前缀
// （客户端可见路径 = mountPrefix + 路径）。
func NewBackend(rootID, ownerID, mountPrefix string) *Backend {
	return NewBackendWithTree(NewHostFileTree(rootID, ownerID), mountPrefix)
}

// NewBackendWithTree 用任意 FileTree 实现构造后端（测试替身 / 将来的进程外实现）
func NewBackendWithTree(tree FileTree, mountPrefix string) *Backend {
	return &Backend{tree: tree, mountPrefix: mountPrefix}
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
func (b *Backend) toFileInfo(entry *Entry, webdavPath string) gowebdav.FileInfo {
	fi := gowebdav.FileInfo{
		Path:    b.mountPrefix + webdavPath,
		Size:    entry.Size,
		ModTime: entry.ModTime,
		IsDir:   entry.IsDir,
		ETag:    entry.ETag,
	}
	if entry.Mime != "" {
		fi.MIMEType = entry.Mime
	} else if !entry.IsDir && entry.Name != "" {
		fi.MIMEType = mime.TypeByExtension(filepath.Ext(entry.Name))
	}
	return fi
}

// httpError 把文件树的域错误翻译成 WebDAV 状态码。
//
// overrides 用于少数「同一分类在不同方法上状态码不同」的场景
// （如 Mkdir 的已存在是 405，MOVE 的 NoOverwrite 冲突是 412）。
// 未知错误按 500 处理，消息原样带给客户端（排障靠它定位到具体一步）。
func httpError(err error, overrides map[ErrorKind]int) error {
	code, ok := overrides[kindOf(err)]
	if !ok {
		switch kindOf(err) {
		case KindNotFound:
			code = http.StatusNotFound
		case KindInvalid, KindNotDir, KindIsDir, KindExists:
			code = http.StatusMethodNotAllowed
		default:
			code = http.StatusInternalServerError
		}
	}
	msg := err.Error()
	if msg == "" {
		msg = http.StatusText(code)
	}
	return gowebdav.NewHTTPError(code, errors.New(msg))
}

// Open 打开文件
func (b *Backend) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	rc, err := b.tree.Open(ctx, name)
	if err != nil {
		return nil, httpError(err, nil)
	}
	return rc, nil
}

// Stat 获取文件信息
func (b *Backend) Stat(ctx context.Context, name string) (*gowebdav.FileInfo, error) {
	entry, err := b.tree.Stat(ctx, name)
	if err != nil {
		return nil, httpError(err, nil)
	}
	fi := b.toFileInfo(entry, name)
	return &fi, nil
}

// ReadDir 读取目录
func (b *Backend) ReadDir(ctx context.Context, name string, recursive bool) ([]gowebdav.FileInfo, error) {
	entries, err := b.tree.List(ctx, name)
	if err != nil {
		return nil, httpError(err, nil)
	}

	var result []gowebdav.FileInfo
	for _, entry := range entries {
		childPath := joinWebdavPath(name, entry.Name)
		result = append(result, b.toFileInfo(entry, childPath))

		if recursive && entry.IsDir {
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

// joinWebdavPath 拼出子项路径：父路径（可能带/不带尾斜杠、可能不以 "/" 开头）+
// 末级名字，并保证结果以 "/" 开头（href 需要绝对路径）。
func joinWebdavPath(parent, name string) string {
	p := parent
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	p += name
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// checkPrecondition 检查 If-Match / If-None-Match 条件请求头
// 返回 412 错误表示条件不满足
func checkPrecondition(existing *Entry, ifMatch, ifNoneMatch gowebdav.ConditionalMatch) error {
	// 计算当前 ETag
	currentETag := ""
	if existing != nil {
		currentETag = existing.ETag
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
	if _, _, ok := splitWebdavName(name); !ok {
		return nil, false, gowebdav.NewHTTPError(405, fmt.Errorf("invalid PUT target: %s", name))
	}

	// 取现状用于条件请求与 created 判定。"不存在" 是正常分支（新建），
	// 父目录缺失等情况留给 Create 自己报错（消息更准确）。
	existing, err := b.tree.Stat(ctx, name)
	if err != nil {
		if !isNotFound(err) {
			return nil, false, httpError(err, nil)
		}
		existing = nil
	}
	created := existing == nil

	// 处理 If-Match / If-None-Match 条件请求头（必须在消费 body 之前）
	if opts != nil {
		if pErr := checkPrecondition(existing, opts.IfMatch, opts.IfNoneMatch); pErr != nil {
			return nil, false, pErr
		}
	}

	entry, err := b.tree.Create(ctx, name, body, !created)
	if err != nil {
		return nil, false, httpError(err, nil)
	}

	return &gowebdav.FileInfo{
		Path:    name,
		Size:    entry.Size,
		ModTime: entry.ModTime,
		IsDir:   false,
		ETag:    entry.ETag,
	}, created, nil
}

// RemoveAll 删除文件
func (b *Backend) RemoveAll(ctx context.Context, name string, opts *gowebdav.RemoveAllOptions) error {
	if name == "/" || name == "" {
		return gowebdav.NewHTTPError(403, fmt.Errorf("cannot delete root"))
	}
	existing, err := b.tree.Stat(ctx, name)
	if err != nil {
		return httpError(err, nil)
	}

	// 处理 If-Match / If-None-Match 条件请求头
	if opts != nil {
		if pErr := checkPrecondition(existing, opts.IfMatch, opts.IfNoneMatch); pErr != nil {
			return pErr
		}
	}

	if err := b.tree.Remove(ctx, name); err != nil {
		return httpError(err, nil)
	}
	return nil
}

// Mkdir 创建目录
func (b *Backend) Mkdir(ctx context.Context, name string) error {
	// MKCOL 的 URI 允许带尾斜杠，必须先剥再 Dir/Base（见 splitWebdavName）
	if _, _, ok := splitWebdavName(name); !ok {
		return gowebdav.NewHTTPError(405, fmt.Errorf("cannot create collection at root: %s", name))
	}
	if err := b.tree.MakeDir(ctx, name); err != nil {
		return httpError(err, nil)
	}
	return nil
}

// Copy 复制（当前服务端未提供 Copy RPC，暂不支持）
func (b *Backend) Copy(ctx context.Context, name, dest string, options *gowebdav.CopyOptions) (bool, error) {
	return false, gowebdav.NewHTTPError(501, fmt.Errorf("copy not supported"))
}

// Move 移动文件
func (b *Backend) Move(ctx context.Context, name, dest string, options *gowebdav.MoveOptions) (bool, error) {
	// 先确认源存在（缺源时 404 优先于「目标非法」的 409，与历史行为一致）
	if _, err := b.tree.Stat(ctx, name); err != nil {
		return false, httpError(err, nil)
	}

	// Destination 是客户端可见路径（带 mountPrefix），go-webdav 不会剥；
	// 且集合的 Destination 常以 "/" 结尾。两者都在 splitWebdavName 之前处理。
	stripped := b.stripMountPrefix(dest)
	if _, _, ok := splitWebdavName(stripped); !ok {
		return false, gowebdav.NewHTTPError(409, fmt.Errorf("invalid move destination: %s", dest))
	}

	noOverwrite := options != nil && options.NoOverwrite
	created, err := b.tree.Move(ctx, name, stripped, noOverwrite)
	if err != nil {
		// MOVE 的域错误码与其它方法不同：非法目标是 409，NoOverwrite 冲突是 412
		return false, httpError(err, map[ErrorKind]int{
			KindInvalid: http.StatusConflict,
			KindExists:  http.StatusPreconditionFailed,
		})
	}
	return created, nil
}
