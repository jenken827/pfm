package api

import (
	"fmt"
	"net/http"
	"strings"

	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/server/middleware"
)

// DirectLinkRedirectHandler 直链 302：让浏览器 / 第三方下载器不改一行代码就能直连提供者
//
// 满足直链条件（单分片 + 未加密 + 驱动支持）时 302 到提供者直链，否则 302 到服务端代理
// 下载地址（保持 token，浏览器跟随即可下载）——所以本接口**永远不会因为不可直链而失败**。
//
// 代价：302 无法携带自定义请求头，个别提供者的直链需要特定 UA 时浏览器会 403；
// 这种情况应改用 /api/file/stream/download（代理）。
//
// 鉴权与 DownloadFileHandler 一致：Authorization 头或 ?token=<JWT>（见 api.applyQueryToken）。
func DirectLinkRedirectHandler(w http.ResponseWriter, r *http.Request) {
	applyQueryToken(r)
	claims, err := middleware.AuthHTTPMiddleware(w, r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	fileID := r.URL.Query().Get("file_id")
	if fileID == "" {
		http.Error(w, "file_id is required", http.StatusBadRequest)
		return
	}
	providerID := parseProviderID(r)

	directURL, ok := resolveDirectLinkURL(r, fileID, providerID)
	if !ok {
		// 不可直链：退回代理下载（同一次请求内跟随，浏览器体验一致）
		llog.L().Infof("direct link unavailable, redirect to proxy, user: %s, file_id: %s",
			claims.User.Name, fileID)
		http.Redirect(w, r, proxyDownloadURL(r, fileID, providerID), http.StatusFound)
		return
	}
	// ⚠ 直链是临时凭证，日志里只记"发了直链"，不记 URL
	llog.L().Infof("direct link issued, user: %s, file_id: %s, provider: %d",
		claims.User.Name, fileID, valueOrZero(providerID))
	http.Redirect(w, r, directURL, http.StatusFound)
}

// resolveDirectLinkURL 取直链；不可用时返回 false（原因只进日志，不回给调用方）
func resolveDirectLinkURL(r *http.Request, fileID string, providerID *uint32) (string, bool) {
	resp, mErr := service.GetFileDirectLink(r.Context(), &native.DirectLinkFileReq{
		FileId:     &fileID,
		ProviderId: providerID,
	})
	if mErr != nil {
		llog.L().Infof("direct link not available, file_id: %s, code: %v", fileID, mErr.C)
		return "", false
	}
	if resp.GetUrl() == "" {
		return "", false
	}
	return resp.GetUrl(), true
}

// proxyDownloadURL 组装代理下载地址（相对路径），并把 token 带过去
//
// 调用方可能用 Authorization 头鉴权，而 302 之后浏览器不会再带这个头，
// 所以这里把原始 token 也写进查询串（浏览器下载本来就走 ?token=）。
func proxyDownloadURL(r *http.Request, fileID string, providerID *uint32) string {
	url := fmt.Sprintf("/api/file/stream/download?file_id=%s", fileID)
	if providerID != nil {
		url += fmt.Sprintf("&provider_id=%d", *providerID)
	}
	if token := requestToken(r); token != "" {
		url += "&token=" + token
	}
	return url
}

// requestToken 从查询串或 Authorization 头里取原始 JWT
func requestToken(r *http.Request) string {
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}
	auth := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(auth, "Bearer "); ok {
		return after
	}
	return ""
}

// valueOrZero 取指针值，nil 返回 0（仅用于日志）
func valueOrZero(p *uint32) uint32 {
	if p == nil {
		return 0
	}
	return *p
}
