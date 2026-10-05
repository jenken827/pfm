package webdav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
	"github.com/google/uuid"
)

// WebDAV 的尾斜杠与 mountPrefix 处理
//
// 背景（emersion/go-webdav v0.7.0）：
//   - MKCOL 走 server.go 的 backend.Mkcol → FileSystem.Mkdir(r.Context(), r.URL.Path)，
//     r.URL.Path 是 StripPrefix 之后、**未做任何 clean/trim** 的原始路径。
//   - MOVE 的 Destination 头由 parseDestination 直接 url.Parse（Href 就是 url.URL），
//     Path 原样带客户端可见的 mountPrefix（"/dav"），库不会剥。
//   - path.Dir("/a/b/") = "/a/b"、path.Base("/a/b/") = "b"，即尾斜杠会让 Dir/Base
//     变成「在 /a/b 下建 b」——比客户端请求的层级**多了一层**。
//     注意判据是「层级」，不是「服务端出现了同名嵌套」：绑定目录与目标同名时
//     （用户自建 photos/ 并把账号绑到它，客户端看不到这一层），客户端建 photos
//     的正确服务端路径本来就是 photos/photos，见 TestMkcolTrailingSlashWithSameNamedBoundRoot。
//
// 所以这两件事必须在 Backend 内部处理。RFC 4918 §9.3 只要求 MKCOL 不带请求体，
// 并未禁止 URI 带尾斜杠；Windows 资源管理器 / macOS Finder / rclone 等在创建或
// 移动集合时都会带。

// doWebdav 通过真实 handler（含 Basic Auth + StripPrefix）发一个请求
func doWebdav(t *testing.T, handler http.Handler, method, path, dest, user, pwd string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if dest != "" {
		req.Header.Set("Destination", dest)
	}
	req.SetBasicAuth(user, pwd)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// MKCOL 带结尾斜杠必须能在正确层级建目录，而不是「在 /newdir 下再建 newdir」
func TestMkcolWithTrailingSlash(t *testing.T) {
	env := newWebdavTestEnv(t)
	handler := http.StripPrefix("/dav", NewHandler("/dav"))
	user, pwd, _ := makeTestAccount(t, env, false)
	ctx := context.Background()

	// 基线：不带尾斜杠
	if rec := doWebdav(t, handler, "MKCOL", "/dav/plaindir", "", user, pwd); rec.Code != http.StatusCreated {
		t.Fatalf("MKCOL /dav/plaindir = %d, want 201", rec.Code)
	}

	// 回归点 1：新建目录带尾斜杠 → 201，且建在账号根下
	rec := doWebdav(t, handler, "MKCOL", "/dav/slashdir/", "", user, pwd)
	if rec.Code != http.StatusCreated {
		t.Errorf("MKCOL /dav/slashdir/ = %d, want 201 (body=%q)", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	if _, err := env.backend.Stat(ctx, "/slashdir"); err != nil {
		t.Errorf("expect /slashdir created at account root, Stat failed: %v", err)
	}

	// 回归点 2：已存在目录带尾斜杠 → 405，且不得建出 /plaindir/plaindir
	rec = doWebdav(t, handler, "MKCOL", "/dav/plaindir/", "", user, pwd)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("MKCOL /dav/plaindir/ = %d, want 405", rec.Code)
	}
	if _, err := env.backend.Stat(ctx, "/plaindir/plaindir"); err == nil {
		t.Errorf("must not create nested /plaindir/plaindir")
	}

	// 回归点 3：多级路径带尾斜杠 → 建在正确父目录下
	if rec := doWebdav(t, handler, "MKCOL", "/dav/sub/deep/", "", user, pwd); rec.Code != http.StatusCreated {
		t.Errorf("MKCOL /dav/sub/deep/ = %d, want 201", rec.Code)
	}
	if _, err := env.backend.Stat(ctx, "/sub/deep"); err != nil {
		t.Errorf("expect /sub/deep created under /sub, Stat failed: %v", err)
	}
}

// 绑定目录与客户端要建的目录**同名**时，「服务端出现 X/X」是正常结果，不是 bug。
//
// 场景：用户在网盘里自建 photos/，分配 WebDAV 账号时绑定到它 —— 它就是客户端看不见的根 R。
// 第三方应用同步本地名为 photos 的文件夹，发的是 MKCOL /photos/，正确结果正是 R/photos，
// 其服务端路径本来就是 photos/photos（外层那个 photos 就是绑定目录本身，应用感知不到）。
// 所以尾斜杠 bug 的判据是「比客户端请求的层级**多了一层**」（R/photos/photos），
// 而不是「服务端出现了同名嵌套」。
func TestMkcolTrailingSlashWithSameNamedBoundRoot(t *testing.T) {
	env := newWebdavTestEnv(t)
	ctx := context.Background()

	// 用户自建的 photos/ → 账号绑定到它，成为客户端看不见的根 R
	photosID := makeTestFolder(t, env.userID, "photos", env.abcID)

	username := "dav-" + uuid.New().String()[:8]
	password := "correct-horse-battery"
	if _, mErr := service.CreateWebdavAccount(webdavUserCtx(env.userID), &native.WebdavAccountCreateReq{
		RootFileId: utils.StringPtr(photosID),
		Username:   utils.StringPtr(username),
		Password:   utils.StringPtr(password),
		ReadOnly:   utils.BoolPtr(false),
	}); mErr != nil {
		t.Fatalf("create webdav account bound to photos/ failed: %v", mErr)
	}

	handler := http.StripPrefix("/dav", NewHandler("/dav"))

	// 客户端（它的根就是 R）建一个名为 photos 的目录
	rec := doWebdav(t, handler, "MKCOL", "/dav/photos/", "", username, password)
	if rec.Code != http.StatusCreated {
		t.Fatalf("MKCOL /dav/photos/ = %d, want 201 (body=%q)", rec.Code, strings.TrimSpace(rec.Body.String()))
	}

	// 客户端视角：应看到 /photos（= R/photos），且不能再多一层 /photos/photos
	clientView := NewBackend(photosID, env.userID, "/dav")
	if _, err := clientView.Stat(ctx, "/photos"); err != nil {
		t.Errorf("expect client-visible /photos under the bound root, Stat failed: %v", err)
	}
	if _, err := clientView.Stat(ctx, "/photos/photos"); err == nil {
		t.Errorf("must not create an extra level: /photos/photos")
	}
}

// PUT 到以 "/" 结尾的 URI = 向集合写内容，RFC 4918 §9.7.2 明确不允许
func TestPutWithTrailingSlashRejected(t *testing.T) {
	env := newWebdavTestEnv(t)
	handler := http.StripPrefix("/dav", NewHandler("/dav"))
	user, pwd, _ := makeTestAccount(t, env, false)

	rec := doWebdav(t, handler, http.MethodPut, "/dav/newfile.txt/", "", user, pwd)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT /dav/newfile.txt/ = %d, want 405", rec.Code)
	}
	if _, err := env.backend.Stat(context.Background(), "/newfile.txt"); err == nil {
		t.Errorf("must not create /newfile.txt from a trailing-slash PUT")
	}
}

// MOVE 的 Destination 带客户端可见的 mountPrefix，Backend 必须先剥掉再解析
func TestMoveDestinationCarriesMountPrefix(t *testing.T) {
	env := newWebdavTestEnv(t)
	handler := http.StripPrefix("/dav", NewHandler("/dav"))
	user, pwd, _ := makeTestAccount(t, env, false)
	ctx := context.Background()

	// 带前缀、不带尾斜杠：文件应移动到 /sub/moved.epub
	rec := doWebdav(t, handler, "MOVE", "/dav/book.epub", "http://example.com/dav/sub/moved.epub", user, pwd)
	if rec.Code != http.StatusCreated {
		t.Errorf("MOVE with prefixed Destination = %d, want 201 (body=%q)", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	if _, err := env.backend.Stat(ctx, "/sub/moved.epub"); err != nil {
		t.Errorf("expect /sub/moved.epub after MOVE, Stat failed: %v", err)
	}

	// 带前缀 + 尾斜杠：重命名集合（/sub → /sub2/）也必须成功
	if rec := doWebdav(t, handler, "MKCOL", "/dav/plaindir", "", user, pwd); rec.Code != http.StatusCreated {
		t.Fatalf("setup MKCOL failed: %d", rec.Code)
	}
	rec = doWebdav(t, handler, "MOVE", "/dav/plaindir", "http://example.com/dav/plaindir2/", user, pwd)
	if rec.Code != http.StatusCreated {
		t.Errorf("MOVE dir with trailing-slash Destination = %d, want 201 (body=%q)", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	if _, err := env.backend.Stat(ctx, "/plaindir2"); err != nil {
		t.Errorf("expect /plaindir2 after MOVE, Stat failed: %v", err)
	}
}
