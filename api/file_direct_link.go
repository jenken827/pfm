package api

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/jenken827/pfm-proto/proto/gen/native"
	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/pkg/llog"
)

// DirectLinkRedirectHandler 直链 302：让浏览器 / 第三方下载器不改一行代码就能直连提供者
//
// 满足直链条件（单分片 + 未加密 + 驱动支持）时 302 到提供者直链，否则 302 到服务端代理
// 下载地址（凭票据，浏览器跟随即可下载）——所以本接口**永远不会因为不可直链而失败**。
//
// 代价：302 无法携带自定义请求头，个别提供者的直链需要特定 UA 时浏览器会 403；
// 这种情况应改用 /api/file/stream/download（代理）。
//
// 鉴权：?ticket=（单文件票据）或 Authorization 头，见 authenticateFileRequest。
func DirectLinkRedirectHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := authenticateFileRequest(w, r)
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
		target, err := proxyDownloadURL(r, fileID, providerID, claims)
		if err != nil {
			http.Error(w, "prepare download failed", http.StatusInternalServerError)
			return
		}
		llog.L().Infof("direct link unavailable, redirect to proxy, user: %s, file_id: %s",
			claims.User.Name, fileID)
		http.Redirect(w, r, target, http.StatusFound)
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

// proxyDownloadURL 组装代理下载地址（相对路径），并把凭证带过去。
//
// 302 之后浏览器不会再带 Authorization 头，所以 Location 里必须自带凭证：
//   - 请求本来就带票据（网页里的下载按钮 / 直链入口）→ 原样透传；
//   - 请求用 Authorization 头（App 等）→ 现签一张下载票据（12 小时，可续传）。
func proxyDownloadURL(r *http.Request, fileID string, providerID *uint32, claims *auth.Claims) (string, error) {
	ticket := r.URL.Query().Get("ticket")
	if ticket == "" {
		signed, err := service.SignDownloadTicket(fileID, claims.User.Id)
		if err != nil {
			llog.L().Errorf("sign download ticket failed: %v", err)
			return "", err
		}
		ticket = signed
	}

	query := url.Values{}
	query.Set("file_id", fileID)
	query.Set("ticket", ticket)
	if providerID != nil {
		query.Set("provider_id", fmt.Sprintf("%d", *providerID))
	}
	return "/api/file/stream/download?" + query.Encode(), nil
}

// valueOrZero 取指针值，nil 返回 0（仅用于日志）
func valueOrZero(p *uint32) uint32 {
	if p == nil {
		return 0
	}
	return *p
}
