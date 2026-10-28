package webdav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/globals"
	"github.com/jenken827/pfm/internal/auth"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/dbtest"
	"github.com/jenken827/pfm/internal/service"
	"github.com/jenken827/pfm/utils"
)

// ===== 测试基建 =====

// webdavTestEnv 一个用户的空间目录树：
//
//	<root>/                 （用户网盘根，name=uuid）
//	├── webdav/
//	│   ├── abc/            ← 账号 A 的绑定根（客户端可见的 "/"）
//	│   │   ├── book.epub
//	│   │   └── sub/
//	│   └── xyz/            ← 兄弟目录，账号 A 绝不能触达
//	│       └── secret.epub
type webdavTestEnv struct {
	userID  string
	rootID  string
	abcID   string
	xyzID   string
	backend *Backend
}

func newWebdavTestEnv(t *testing.T) *webdavTestEnv {
	t.Helper()
	dbtest.Setup(t)

	userID := uuid.New().String()
	rootID := makeTestFolder(t, userID, "root-"+userID, "")
	space := &entity.UserSpaceORM{
		UserId:           utils.StringPtr(userID),
		RootFileId:       utils.StringPtr(rootID),
		BoundProviderIds: utils.StringPtr("[1]"),
		QuotaBytes:       utils.Uint64Ptr(0),
		UsedBytes:        utils.Uint64Ptr(0),
	}
	if err := db.CreateUserSpace(space); err != nil {
		t.Fatalf("create test user space failed: %v", err)
	}

	webdavDir := makeTestFolder(t, userID, "webdav", rootID)
	abcID := makeTestFolder(t, userID, "abc", webdavDir)
	xyzID := makeTestFolder(t, userID, "xyz", webdavDir)
	makeTestFolder(t, userID, "sub", abcID)
	makeTestFile(t, userID, "book.epub", abcID)
	makeTestFile(t, userID, "secret.epub", xyzID)

	return &webdavTestEnv{
		userID:  userID,
		rootID:  rootID,
		abcID:   abcID,
		xyzID:   xyzID,
		backend: NewBackend(abcID, userID, "/dav"),
	}
}

func makeTestFolder(t *testing.T, ownerID, name, parentID string) string {
	t.Helper()
	id := uuid.New().String()
	f := &entity.FileORM{
		Id:       utils.StringPtr(id),
		Name:     utils.StringPtr(name),
		ParentId: utils.StringPtr(parentID),
		OwnerId:  utils.StringPtr(ownerID),
		Type:     utils.EnumPtr(native.FileType_FOLDER),
		IsReady:  utils.BoolPtr(true),
	}
	if err := db.CreateFile(f); err != nil {
		t.Fatalf("create test folder %s failed: %v", name, err)
	}
	return id
}

func makeTestFile(t *testing.T, ownerID, name, parentID string) string {
	t.Helper()
	id := uuid.New().String()
	f := &entity.FileORM{
		Id:       utils.StringPtr(id),
		Name:     utils.StringPtr(name),
		ParentId: utils.StringPtr(parentID),
		OwnerId:  utils.StringPtr(ownerID),
		Type:     utils.EnumPtr(native.FileType_DOCUMENT),
		IsReady:  utils.BoolPtr(true),
	}
	if err := db.CreateFile(f); err != nil {
		t.Fatalf("create test file %s failed: %v", name, err)
	}
	return id
}

// makeTestAccount 创建 WebDAV 账号（readOnly 决定权限），返回 (username, password)
func makeTestAccount(t *testing.T, env *webdavTestEnv, readOnly bool) (string, string, uint32) {
	t.Helper()
	username := "dav-" + uuid.New().String()[:8]
	password := "correct-horse-battery"
	resp, mErr := service.CreateWebdavAccount(webdavUserCtx(env.userID), &native.WebdavAccountCreateReq{
		RootFileId: utils.StringPtr(env.abcID),
		Username:   utils.StringPtr(username),
		Password:   utils.StringPtr(password),
		ReadOnly:   utils.BoolPtr(readOnly),
	})
	if mErr != nil {
		t.Fatalf("create webdav account failed: %v", mErr)
	}
	return username, password, resp.GetAccount().GetId()
}

// webdavUserCtx 构造带登录用户身份的 ctx（service 层从 globals.ContextUserKey 取当前用户）
func webdavUserCtx(userID string) context.Context {
	return context.WithValue(context.Background(), globals.ContextUserKey,
		&auth.Claims{User: auth.User{Id: userID, Name: userID}})
}

// ===== 路径隔离（账号可见的 "/" 就是绑定目录）=====

func TestBackendRootIsAccountBoundDir(t *testing.T) {
	env := newWebdavTestEnv(t)
	ctx := context.Background()

	root, err := env.backend.Stat(ctx, "/")
	if err != nil {
		t.Fatalf("stat root failed: %v", err)
	}
	if !root.IsDir {
		t.Errorf("expect root to be a directory")
	}

	entries, err := env.backend.ReadDir(ctx, "/", false)
	if err != nil {
		t.Fatalf("readdir root failed: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Path)
	}
	if len(names) != 2 {
		t.Fatalf("expect 2 entries under account root (book.epub, sub), got %v", names)
	}
	for _, n := range names {
		if !strings.HasPrefix(n, "/dav/") {
			t.Errorf("expect href to carry mount prefix, got %q", n)
		}
	}
	if strings.Contains(strings.Join(names, ","), "secret.epub") {
		t.Errorf("sibling dir content leaked into account root: %v", names)
	}
}

func TestBackendCannotEscapeAccountRoot(t *testing.T) {
	env := newWebdavTestEnv(t)
	ctx := context.Background()

	// 兄弟目录（同一用户网盘内、但不在账号绑定目录下）与各种上溯写法都必须失败
	escaping := []string{
		"/xyz",
		"/xyz/secret.epub",
		"/../xyz",
		"/..",
		"..",
		"/../../etc/passwd",
		"/webdav",
		"/sub/../..",
	}
	for _, p := range escaping {
		if _, err := env.backend.Stat(ctx, p); err == nil {
			t.Errorf("expect Stat(%q) to fail (outside account root)", p)
		}
		if _, err := env.backend.ReadDir(ctx, p, true); err == nil {
			t.Errorf("expect ReadDir(%q) to fail (outside account root)", p)
		}
	}

	// 账号根内部的合法路径仍然可用
	if _, err := env.backend.Stat(ctx, "/sub"); err != nil {
		t.Errorf("expect Stat(/sub) ok inside account root, got %v", err)
	}
	if _, err := env.backend.Stat(ctx, "/book.epub"); err != nil {
		t.Errorf("expect Stat(/book.epub) ok inside account root, got %v", err)
	}
}

// ===== handler：认证 / 只读 / 401 =====

func TestHandlerAuthAndReadOnly(t *testing.T) {
	env := newWebdavTestEnv(t)
	// 与 server/router.go 一致：/dav 前缀由 StripPrefix 剥掉后再进 handler
	handler := http.StripPrefix("/dav", NewHandler("/dav"))

	readOnlyUser, readOnlyPwd, _ := makeTestAccount(t, env, true)
	readWriteUser, readWritePwd, readWriteID := makeTestAccount(t, env, false)

	do := func(method, path, user, pwd string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if user != "" {
			req.SetBasicAuth(user, pwd)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// 未带凭据 / 错误凭据 / 已删除目录的账号 → 401 且带挑战头
	if rec := do("PROPFIND", "/dav/", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("expect 401 without credentials, got %d", rec.Code)
	} else if rec.Header().Get("WWW-Authenticate") == "" {
		t.Errorf("expect WWW-Authenticate header on 401")
	}
	if rec := do("PROPFIND", "/dav/", readWriteUser, "wrong-password"); rec.Code != http.StatusUnauthorized {
		t.Errorf("expect 401 with wrong password, got %d", rec.Code)
	}
	if rec := do("PROPFIND", "/dav/", "no-such-webdav-user", "x"); rec.Code != http.StatusUnauthorized {
		t.Errorf("expect 401 with unknown username, got %d", rec.Code)
	}

	// 只读账号：写方法一律 403，读方法正常
	writeMethods := []string{
		http.MethodPut,
		http.MethodDelete,
		"MKCOL",
		"MOVE",
		"COPY",
		"PROPPATCH",
	}
	for _, m := range writeMethods {
		req := httptest.NewRequest(m, "/dav/x.txt", nil)
		req.SetBasicAuth(readOnlyUser, readOnlyPwd)
		if m == "MOVE" || m == "COPY" {
			req.Header.Set("Destination", "/dav/y.txt")
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("expect 403 for read-only account method %s, got %d", m, rec.Code)
		}
	}

	readRec := do("PROPFIND", "/dav/", readOnlyUser, readOnlyPwd)
	if readRec.Code != http.StatusMultiStatus {
		t.Errorf("read method must return 207 for read-only account, got %d", readRec.Code)
	}

	// OPTIONS：200 + Allow 按权限裁剪
	roOpt := do(http.MethodOptions, "/dav/", readOnlyUser, readOnlyPwd)
	if roOpt.Code != http.StatusOK {
		t.Errorf("expect 200 for OPTIONS, got %d", roOpt.Code)
	}
	if allow := roOpt.Header().Get("Allow"); strings.Contains(allow, "PUT") {
		t.Errorf("read-only Allow header must not advertise PUT, got %q", allow)
	}
	rwOpt := do(http.MethodOptions, "/dav/", readWriteUser, readWritePwd)
	if allow := rwOpt.Header().Get("Allow"); !strings.Contains(allow, "PUT") {
		t.Errorf("read-write Allow header must advertise PUT, got %q", allow)
	}

	// 读写账号的读方法正常返回 207（对照：只读账号被拦的只有写方法）
	rwRec := do("PROPFIND", "/dav/", readWriteUser, readWritePwd)
	if rwRec.Code != http.StatusMultiStatus {
		t.Errorf("read-write account PROPFIND should return 207, got %d", rwRec.Code)
	}
	if !strings.Contains(rwRec.Body.String(), "book.epub") {
		t.Errorf("PROPFIND body should list files under the account root, got %q", rwRec.Body.String())
	}

	// 账号被禁用（或绑定目录失效自动禁用）后 → 401，且不再放行读方法
	if err := db.SetWebdavAccountEnabled(readWriteID, false, "test disable"); err != nil {
		t.Fatalf("disable account failed: %v", err)
	}
	if rec := do("PROPFIND", "/dav/", readWriteUser, readWritePwd); rec.Code != http.StatusUnauthorized {
		t.Errorf("expect 401 for disabled account, got %d", rec.Code)
	}
}
