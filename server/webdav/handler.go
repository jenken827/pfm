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
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// Handler WebDAV HTTP handler
//
// 无状态：每个请求按认证用户构造独立的 gowebdav.Handler / Backend 以实现用户隔离，
// handler 自身不持有可变字段。此前把 FileSystem 存在共享 handler 上再逐请求赋值，
// 并发请求会互相覆盖 rootID/ownerID（用户 A 的请求可能落到用户 B 的根目录）。
type Handler struct {
	// mountPrefix 是路由 StripPrefix 剥掉的前缀（见 server/router.go），用于把
	// 后端内部路径还原成客户端可见路径写进 href（RFC 4918 §8.3 / §9.1）。
	mountPrefix string
}

// NewHandler 创建 WebDAV handler
// 每个请求按认证用户解析其 UserSpace 根目录，实现用户隔离
// mountPrefix 必须与路由上的 StripPrefix 参数一致（例如挂载 "/dav/" 时传 "/dav"）
func NewHandler(mountPrefix string) *Handler {
	return &Handler{mountPrefix: strings.TrimSuffix(mountPrefix, "/")}
}

// ServeHTTP 处理 WebDAV 请求
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Basic Auth 认证（本地用户表）
	username, password, ok := r.BasicAuth()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="PFM WebDAV"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userID, err := auth.GetLocalAuth().Authenticate(username, password)
	if err != nil {
		llog.L().Warnf("webdav auth failed: user=%s, err=%v", username, err)
		w.Header().Set("WWW-Authenticate", `Basic realm="PFM WebDAV"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 获取用户空间（根目录 + 绑定的存储提供者）
	space, err := db.GetUserSpaceByUserID(userID)
	if err != nil {
		llog.L().Warnf("webdav user %s has no UserSpace: %v", userID, err)
		http.Error(w, "User space not configured", http.StatusForbidden)
		return
	}
	rootFileID := ""
	if space.RootFileId != nil {
		rootFileID = *space.RootFileId
	}

	// 注入用户上下文，供 service 层（UploadSmall 等）读取
	claims := &auth.Claims{User: auth.User{Id: userID, Name: username}}
	ctx := context.WithValue(r.Context(), globals.ContextUserKey, claims)
	ctx = context.WithValue(ctx, globals.ContextUserSpaceKey, &models.UserContext{
		UserID:           userID,
		IsAdmin:          db.IsAdmin(userID),
		RootFileID:       rootFileID,
		BoundProviderIds: getBoundProviderIds(space),
	})
	r = r.WithContext(ctx)

	llog.L().Debugf("webdav request: user=%s, method=%s, path=%s", userID, r.Method, r.URL.Path)

	// go-webdav 对 OPTIONS 固定返回 204 No Content(见 emersion/go-webdav
	// internal/server.go handleOptions: w.WriteHeader(http.StatusNoContent)),
	// 而 flutter 的 webdav_client 1.2.2 在读写文件前会先发 OPTIONS 预检并强制
	// 要求 200(见 webdav_dio.dart wdReadWithBytes),204 会被抛成
	// "DioException[bad response]: No Content"——BeeCount 下载/上传同步即报此错。
	// 这里把 OPTIONS 应答改为 200 OK(DAV/Allow 头内容与 go-webdav 对齐)。
	if r.Method == http.MethodOptions {
		w.Header().Set("DAV", "1, 3")
		w.Header().Set("Allow", "OPTIONS, DELETE, PROPFIND, PROPPATCH, COPY, MOVE, HEAD, GET, PUT, MKCOL")
		w.WriteHeader(http.StatusOK)
		return
	}

	// 使用该用户的根目录构造独立后端（每请求一份），实现用户隔离
	fs := &gowebdav.Handler{FileSystem: NewBackend(rootFileID, userID, h.mountPrefix)}
	fs.ServeHTTP(w, r)
}

// getBoundProviderIds 解析 UserSpace 绑定的存储提供者 ID
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
