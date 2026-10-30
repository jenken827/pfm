package webdav

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	gowebdav "github.com/emersion/go-webdav"
)

// fakeTree 内存版 FileTree：证明协议层（Backend）不需要 DB / service / 存储提供者。
//
// 它同时也是「FileTree 契约」的可执行说明：任何实现（宿主版、将来的进程外版）
// 只要满足这里的语义，WebDAV 行为就一致。
type fakeTree struct {
	entries map[string]*Entry // 规范化路径（"/" 开头、无尾斜杠；根为 "/"）
	blobs   map[string][]byte
}

func newFakeTree() *fakeTree {
	return &fakeTree{
		entries: map[string]*Entry{"/": {ID: "root", IsDir: true, ModTime: time.Unix(0, 0)}},
		blobs:   map[string][]byte{},
	}
}

func normPath(p string) string {
	p = strings.TrimRight(p, "/")
	if p == "" {
		return "/"
	}
	return p
}

func parentOf(p string) string {
	idx := strings.LastIndex(p, "/")
	if idx <= 0 {
		return "/"
	}
	return p[:idx]
}

func baseOf(p string) string {
	return p[strings.LastIndex(p, "/")+1:]
}

func (f *fakeTree) Stat(_ context.Context, p string) (*Entry, error) {
	entry, ok := f.entries[normPath(p)]
	if !ok {
		return nil, treeError(KindNotFound, "not found: "+p, nil)
	}
	clone := *entry
	return &clone, nil
}

func (f *fakeTree) List(_ context.Context, p string) ([]*Entry, error) {
	dir, err := f.Stat(context.Background(), p)
	if err != nil {
		return nil, err
	}
	if !dir.IsDir {
		return nil, treeError(KindNotDir, "not a directory", nil)
	}
	prefix := normPath(p)
	if prefix != "/" {
		prefix += "/"
	}
	var out []*Entry
	for path, entry := range f.entries {
		if path == "/" || !strings.HasPrefix(path, prefix) {
			continue
		}
		if strings.Contains(strings.TrimPrefix(path, prefix), "/") {
			continue // 深层项由协议层递归展开
		}
		clone := *entry
		out = append(out, &clone)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeTree) Open(_ context.Context, p string) (io.ReadCloser, error) {
	entry, err := f.Stat(context.Background(), p)
	if err != nil {
		return nil, err
	}
	if entry.IsDir {
		return nil, treeError(KindIsDir, "cannot open directory", nil)
	}
	return io.NopCloser(bytes.NewReader(f.blobs[normPath(p)])), nil
}

func (f *fakeTree) Create(_ context.Context, p string, body io.Reader, overwrite bool) (*Entry, error) {
	path := normPath(p)
	if parent := f.entries[parentOf(path)]; parent == nil || !parent.IsDir {
		return nil, treeError(KindNotFound, "parent not found: "+parentOf(path), nil)
	}
	existing, exists := f.entries[path]
	if exists && existing.IsDir {
		return nil, treeError(KindExists, "is a directory: "+path, nil)
	}
	if exists && !overwrite {
		return nil, treeError(KindExists, "exists", nil)
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, treeError(KindInternal, "read body: "+err.Error(), err)
	}
	f.blobs[path] = data
	entry := &Entry{ID: path, Name: baseOf(path), Size: int64(len(data)), ModTime: time.Now()}
	if exists {
		entry.ID = existing.ID
	}
	entry.ETag = "etag:" + entry.Name
	f.entries[path] = entry
	clone := *entry
	return &clone, nil
}

func (f *fakeTree) MakeDir(_ context.Context, p string) error {
	path := normPath(p)
	if _, ok := f.entries[path]; ok {
		return treeError(KindExists, "already exists: "+baseOf(path), nil)
	}
	if parent := f.entries[parentOf(path)]; parent == nil || !parent.IsDir {
		return treeError(KindNotFound, "parent not found: "+parentOf(path), nil)
	}
	f.entries[path] = &Entry{ID: path, Name: baseOf(path), IsDir: true, ModTime: time.Now()}
	return nil
}

func (f *fakeTree) Remove(_ context.Context, p string) error {
	path := normPath(p)
	if path == "/" {
		return treeError(KindInvalid, "cannot delete root", nil)
	}
	if _, ok := f.entries[path]; !ok {
		return treeError(KindNotFound, "not found: "+p, nil)
	}
	for key := range f.entries {
		if key == path || strings.HasPrefix(key, path+"/") {
			delete(f.entries, key)
			delete(f.blobs, key)
		}
	}
	return nil
}

func (f *fakeTree) Move(_ context.Context, from, to string, noOverwrite bool) (bool, error) {
	fromPath, toPath := normPath(from), normPath(to)
	if _, ok := f.entries[fromPath]; !ok {
		return false, treeError(KindNotFound, "not found: "+from, nil)
	}
	if parent := f.entries[parentOf(toPath)]; parent == nil || !parent.IsDir {
		return false, treeError(KindNotFound, "dest not found: "+parentOf(toPath), nil)
	}
	if _, exists := f.entries[toPath]; exists && noOverwrite {
		return false, treeError(KindExists, "exists", nil)
	}
	_, existed := f.entries[toPath]

	// 先收集整棵子树再搬，避免边遍历边写 map
	type kv struct {
		key   string
		entry *Entry
	}
	var moving []kv
	for key, entry := range f.entries {
		if key == fromPath || strings.HasPrefix(key, fromPath+"/") {
			moving = append(moving, kv{key: key, entry: entry})
		}
	}
	for _, item := range moving {
		delete(f.entries, item.key)
		delete(f.blobs, item.key)
		newKey := toPath + strings.TrimPrefix(item.key, fromPath)
		f.entries[newKey] = item.entry
	}
	f.entries[toPath].Name = baseOf(toPath)
	return !existed, nil
}

// ===== 用内存树驱动真实 go-webdav handler（无 DB、无插件、无认证）=====

func newFakeDavHandler() (*fakeTree, http.Handler) {
	tree := newFakeTree()
	return tree, &gowebdav.Handler{FileSystem: NewBackendWithTree(tree, "/dav")}
}

func davRequest(t *testing.T, handler http.Handler, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// 一条完整的读写生命周期：MKCOL → PUT → GET → PROPFIND → MOVE → DELETE
func TestBackendAgainstFakeTreeLifecycle(t *testing.T) {
	_, handler := newFakeDavHandler()

	if rec := davRequest(t, handler, "MKCOL", "/books", "", nil); rec.Code != http.StatusCreated {
		t.Fatalf("MKCOL /books = %d, want 201 (body=%q)", rec.Code, rec.Body.String())
	}
	if rec := davRequest(t, handler, http.MethodPut, "/books/a.txt", "hello", nil); rec.Code != http.StatusCreated {
		t.Fatalf("PUT /books/a.txt = %d, want 201 (body=%q)", rec.Code, rec.Body.String())
	}

	// GET：Content-Length 与可读字节数必须一致（FileTree 契约里的 Size=明文长度）
	rec := davRequest(t, handler, http.MethodGet, "/books/a.txt", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /books/a.txt = %d, want 200 (body=%q)", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body != "hello" {
		t.Fatalf("GET body = %q, want %q", body, "hello")
	}

	// PROPFIND 的 href 必须带 mountPrefix（客户端按路径匹配子项）
	rec = davRequest(t, handler, "PROPFIND", "/books", "", map[string]string{"Depth": "1"})
	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND /books = %d, want 207", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/dav/books/a.txt") {
		t.Fatalf("PROPFIND href 应带 /dav 前缀，实际：%s", rec.Body.String())
	}

	// MOVE：Destination 带 mountPrefix，必须被剥掉再解析
	rec = davRequest(t, handler, "MOVE", "/books/a.txt", "", map[string]string{
		"Destination": "/dav/books/b.txt",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("MOVE = %d, want 201 (body=%q)", rec.Code, rec.Body.String())
	}
	if rec := davRequest(t, handler, http.MethodGet, "/books/b.txt", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET moved file = %d, want 200", rec.Code)
	}

	// DELETE
	if rec := davRequest(t, handler, http.MethodDelete, "/books/b.txt", "", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204", rec.Code)
	}
	if rec := davRequest(t, handler, http.MethodGet, "/books/b.txt", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("GET after DELETE = %d, want 404", rec.Code)
	}
}

// 域错误 → WebDAV 状态码的映射，全部走真实 handler 验证
func TestBackendMapsTreeErrorsToStatus(t *testing.T) {
	_, handler := newFakeDavHandler()

	if rec := davRequest(t, handler, http.MethodGet, "/missing.txt", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("GET missing = %d, want 404", rec.Code)
	}
	if rec := davRequest(t, handler, http.MethodPut, "/books/a.txt/", "x", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT to collection URI = %d, want 405", rec.Code)
	}
	if rec := davRequest(t, handler, http.MethodPut, "/nope/a.txt", "x", nil); rec.Code != http.StatusNotFound {
		t.Errorf("PUT under missing parent = %d, want 404", rec.Code)
	}

	if rec := davRequest(t, handler, "MKCOL", "/books", "", nil); rec.Code != http.StatusCreated {
		t.Fatalf("MKCOL /books = %d, want 201", rec.Code)
	}
	// 已存在：宿主实现返回 KindExists，协议层映射 405（与旧行为一致）
	if rec := davRequest(t, handler, "MKCOL", "/books", "", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("MKCOL existing = %d, want 405", rec.Code)
	}
	if rec := davRequest(t, handler, http.MethodGet, "/books", "", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET directory = %d, want 405", rec.Code)
	}

	// If-None-Match: * 在已存在资源上必须失败（412），且不能消费 body 后写入
	if rec := davRequest(t, handler, http.MethodPut, "/books", "", nil); rec.Code == http.StatusCreated {
		t.Errorf("PUT over a directory must not succeed")
	}
	davRequest(t, handler, http.MethodPut, "/books/a.txt", "v1", nil)
	rec := davRequest(t, handler, http.MethodPut, "/books/a.txt", "v2", map[string]string{"If-None-Match": "*"})
	if rec.Code != http.StatusPreconditionFailed {
		t.Errorf("PUT with If-None-Match:* on existing = %d, want 412", rec.Code)
	}
	if body := davRequest(t, handler, http.MethodGet, "/books/a.txt", "", nil).Body.String(); body != "v1" {
		t.Errorf("content must be unchanged after 412, got %q", body)
	}

	// MOVE：NoOverwrite（Overwrite: F）撞已有目标 → 412
	davRequest(t, handler, http.MethodPut, "/books/b.txt", "keep", nil)
	rec = davRequest(t, handler, "MOVE", "/books/a.txt", "", map[string]string{
		"Destination": "/dav/books/b.txt",
		"Overwrite":   "F",
	})
	if rec.Code != http.StatusPreconditionFailed {
		t.Errorf("MOVE over existing with Overwrite:F = %d, want 412", rec.Code)
	}

	// MOVE：非法目标（指向根）→ 409
	rec = davRequest(t, handler, "MOVE", "/books/a.txt", "", map[string]string{
		"Destination": "/dav/",
	})
	if rec.Code != http.StatusConflict {
		t.Errorf("MOVE to root = %d, want 409", rec.Code)
	}

	// COPY 未实现 → 501
	rec = davRequest(t, handler, "COPY", "/books/a.txt", "", map[string]string{
		"Destination": "/dav/books/c.txt",
	})
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("COPY = %d, want 501", rec.Code)
	}
}
