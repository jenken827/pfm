package webdav

import (
	"encoding/base64"
	"net/http"
	"strings"

	gowebdav "github.com/emersion/go-webdav"
	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/llog"
)

// Handler WebDAV HTTP handler
type Handler struct {
	fs            *gowebdav.Handler
	authenticator *auth.BasicAuthenticator
}

// NewHandler 创建 WebDAV handler
func NewHandler(rootID string) *Handler {
	backend := NewBackend(rootID)
	return &Handler{
		fs:            &gowebdav.Handler{FileSystem: backend},
		authenticator: auth.NewBasicAuthenticator(),
	}
}

// ServeHTTP 处理 WebDAV 请求
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Basic Auth 认证
	username, password, ok := r.BasicAuth()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="PFM WebDAV"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	user, err := h.authenticator.Authenticate(username, password)
	if err != nil {
		llog.L().Warnf("webdav auth failed: user=%s, err=%v", username, err)
		w.Header().Set("WWW-Authenticate", `Basic realm="PFM WebDAV"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	llog.L().Debugf("webdav request: user=%s, method=%s, path=%s", user, r.Method, r.URL.Path)

	// 转发到 go-webdav handler
	h.fs.ServeHTTP(w, r)
}

// parseBasicAuth 手动解析 Basic Auth（备用）
func parseBasicAuth(authHeader string) (username, password string, ok bool) {
	if !strings.HasPrefix(authHeader, "Basic ") {
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(authHeader[6:])
	if err != nil {
		return
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return
	}
	return parts[0], parts[1], true
}
