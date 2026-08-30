package webdav

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	gowebdav "github.com/emersion/go-webdav"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// Handler WebDAV HTTP handler
type Handler struct {
	fs *gowebdav.Handler
}

// NewHandler 创建 WebDAV handler
// 每个请求按认证用户解析其 UserSpace 根目录，实现用户隔离
func NewHandler() *Handler {
	return &Handler{
		fs: &gowebdav.Handler{FileSystem: &Backend{}},
	}
}

// ServeHTTP 处理 WebDAV 请求
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Basic Auth 认证（按认证模式：Casdoor ROPC 或本地用户表）
	username, password, ok := r.BasicAuth()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="PFM WebDAV"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userID, err := auth.AuthenticateBasic(username, password)
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
	claims := &casdoorsdk.Claims{User: casdoorsdk.User{Id: userID, Name: username}}
	ctx := context.WithValue(r.Context(), globals.ContextUserKey, claims)
	ctx = context.WithValue(ctx, globals.ContextUserSpaceKey, &models.UserContext{
		UserID:           userID,
		IsAdmin:          db.IsAdmin(userID),
		RootFileID:       rootFileID,
		BoundProviderIds: getBoundProviderIds(space),
	})
	r = r.WithContext(ctx)

	llog.L().Debugf("webdav request: user=%s, method=%s, path=%s", userID, r.Method, r.URL.Path)

	// 使用该用户的根目录构造后端，实现用户隔离
	h.fs.FileSystem = NewBackend(rootFileID, userID)
	h.fs.ServeHTTP(w, r)
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
