package webdav

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	gowebdav "github.com/emersion/go-webdav"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// Handler WebDAV HTTP handler
//
// 无状态：每个请求按认证账号构造独立的 gowebdav.Handler / Backend 以实现隔离，
// handler 自身不持有可变字段。此前把 FileSystem 存在共享 handler 上再逐请求赋值，
// 并发请求会互相覆盖 rootID/ownerID（账号 A 的请求可能落到账号 B 的根目录）。
//
// 认证模型（自 2026-09 重构）：
//   - Basic Auth 校验的是 webdav_accounts 表的**独立账号**（不是登录账号），
//     由 service.AuthenticateWebdav 完成（bcrypt + 内存缓存 + 绑定目录校验）。
//   - 每个账号绑定用户自己的一个目录作为 WebDAV 根：客户端看到的 "/" 即该目录，
//     请求路径相对该目录解析（例如客户端 base=/dav、文件在 abc/readany 下时，
//     GET /dav/readany/x.txt 落到 <绑定目录>/readany/x.txt）。
//   - read_only 账号按方法白名单只放行读方法（GET/HEAD/OPTIONS/PROPFIND）。
//   - ctx 中 IsAdmin 恒为 false：WebDAV 凭据绝不带管理员身份。
type Handler struct {
	// mountPrefix 是路由 StripPrefix 剥掉的前缀（见 server/router.go），用于把
	// 后端内部路径还原成客户端可见路径写进 href（RFC 4918 §8.3 / §9.1）。
	mountPrefix string
}

// NewHandler 创建 WebDAV handler
// mountPrefix 必须与路由上的 StripPrefix 参数一致（例如挂载 "/dav/" 时传 "/dav"）
func NewHandler(mountPrefix string) *Handler {
	return &Handler{mountPrefix: strings.TrimSuffix(mountPrefix, "/")}
}

// readOnlyAllowedMethods 只读账号放行的方法（白名单，其余一律 403）
var readOnlyAllowedMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
	"PROPFIND":         true,
}

// ServeHTTP 处理 WebDAV 请求
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Basic Auth 认证（webdav_accounts 独立账号表）
	username, password, ok := r.BasicAuth()
	if !ok {
		writeUnauthorized(w, "missing basic auth")
		return
	}

	ctx := r.Context()
	account, err := service.AuthenticateWebdav(ctx, username, password)
	if err != nil {
		llog.C(ctx).Warnf("webdav auth failed: user=%s, err=%v", username, err)
		writeUnauthorized(w, err.Error())
		return
	}

	userID := webdavOwnerID(account)
	rootFileID := webdavRootFileID(account)

	// 获取用户空间（根目录 + 管理员分配的存储提供者范围）
	// 账号本身已校验过绑定目录归属，这里失败说明用户空间配置缺失（与旧行为一致：403）
	space, err := db.GetUserSpaceByUserID(userID)
	if err != nil {
		llog.C(ctx).Warnf("webdav user %s has no UserSpace: %v", userID, err)
		http.Error(w, "User space not configured", http.StatusForbidden)
		return
	}

	// 只读账号：写方法一律拒绝（PUT=Create / DELETE=RemoveAll / MKCOL=Mkdir /
	// MOVE=Move / COPY=Copy / PROPPATCH 均不在白名单内）
	if account.ReadOnly != nil && *account.ReadOnly && !readOnlyAllowedMethods[r.Method] {
		llog.C(ctx).Warnf("webdav read-only account %d denied method %s, path=%s", webdavAccountID(account), r.Method, r.URL.Path)
		http.Error(w, "Forbidden: read-only account", http.StatusForbidden)
		return
	}

	// 注入账号上下文，供 service 层读取：
	//   - RootFileID 取账号绑定目录（客户端可见的 "/"），而不是用户网盘根
	//   - IsAdmin 恒为 false（最小权限，避免 service 层管理分支被 WebDAV 顺带命中）
	//   - BoundProviderIds 仍是管理员分配集（可用范围）；实际落盘走
	//     storage.ResolveProviderIds(ownerID, folderID) 按目录绑定解析
	claims := &auth.Claims{User: auth.User{Id: userID, Name: username}}
	ctx = context.WithValue(ctx, globals.ContextUserKey, claims)
	ctx = context.WithValue(ctx, globals.ContextUserSpaceKey, &models.UserContext{
		UserID:           userID,
		IsAdmin:          false,
		RootFileID:       rootFileID,
		BoundProviderIds: getBoundProviderIds(space),
	})
	r = r.WithContext(ctx)

	llog.C(ctx).Debugf("webdav request: account=%d, user=%s, method=%s, path=%s",
		webdavAccountID(account), userID, r.Method, r.URL.Path)

	// go-webdav 对 OPTIONS 固定返回 204 No Content(见 emersion/go-webdav
	// internal/server.go handleOptions: w.WriteHeader(http.StatusNoContent)),
	// 而 flutter 的 webdav_client 1.2.2 在读写文件前会先发 OPTIONS 预检并强制
	// 要求 200(见 webdav_dio.dart wdReadWithBytes),204 会被抛成
	// "DioException[bad response]: No Content"——BeeCount 下载/上传同步即报此错。
	// 这里把 OPTIONS 应答改为 200 OK(DAV/Allow 头内容与 go-webdav 对齐)。
	if r.Method == http.MethodOptions {
		w.Header().Set("DAV", "1, 3")
		w.Header().Set("Allow", allowedMethodsHeader(account))
		w.WriteHeader(http.StatusOK)
		return
	}

	// 使用该账号的绑定目录构造独立后端（每请求一份），实现账号隔离
	fs := &gowebdav.Handler{FileSystem: NewBackend(rootFileID, userID, h.mountPrefix)}
	fs.ServeHTTP(w, r)
}

// allowedMethodsHeader 按账号权限生成 OPTIONS 的 Allow 头（只读账号不宣告写方法）
func allowedMethodsHeader(account *native.WebdavAccountORM) string {
	if account.ReadOnly != nil && *account.ReadOnly {
		return "OPTIONS, PROPFIND, HEAD, GET"
	}
	return "OPTIONS, DELETE, PROPFIND, PROPPATCH, COPY, MOVE, HEAD, GET, PUT, MKCOL"
}

// writeUnauthorized 返回 401 并带上 Basic 认证挑战头（客户端据此重新弹凭据）
// 细节只写服务端日志，不回显给客户端，避免泄露账号是否存在/为何被禁用
func writeUnauthorized(w http.ResponseWriter, reason string) {
	w.Header().Set("WWW-Authenticate", `Basic realm="PFM WebDAV"`)
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}

// webdavAccountID 取账号主键（ORM 字段为指针）
func webdavAccountID(account *native.WebdavAccountORM) uint32 {
	if account == nil || account.Id == nil {
		return 0
	}
	return *account.Id
}

// webdavOwnerID 取账号归属用户 ID（ORM 字段为指针）
func webdavOwnerID(account *native.WebdavAccountORM) string {
	if account == nil || account.UserId == nil {
		return ""
	}
	return *account.UserId
}

// webdavRootFileID 取账号绑定的根目录 ID（ORM 字段为指针）
func webdavRootFileID(account *native.WebdavAccountORM) string {
	if account == nil || account.RootFileId == nil {
		return ""
	}
	return *account.RootFileId
}

// getBoundProviderIds 解析 UserSpace 绑定的存储提供者 ID（管理员分配范围）
func getBoundProviderIds(space *native.UserSpaceORM) []uint32 {
	if space == nil || space.BoundProviderIds == nil {
		return nil
	}
	var ids []uint32
	if err := json.Unmarshal([]byte(*space.BoundProviderIds), &ids); err != nil {
		llog.L().Warnf("webdav invalid bound_provider_ids: %v", err)
		return nil
	}
	return ids
}
