package server

import (
	"net/http"

	"jenken827.com/jenken827/pfm/api"
	"jenken827.com/jenken827/pfm/server/middleware"
	"jenken827.com/jenken827/pfm/server/webdav"
)

// route 装配全部 HTTP 路由。
//
// 分层：
//  1. 裸 HTTP 路由：流式下载/预览、公开分享、健康检查、WebDAV、静态资源——
//     这些接口需要二进制流、Range、URL 签名或非 RPC 语义，不走统一分发器。
//  2. REST 业务路由：由 routeREST 注册（server/route_rest.go 提供分发器），
//     统一走 JSON 编解码 + BaseResp 包装 + 鉴权中间件。
func route(mux *http.ServeMux) {
	// --- 裸 HTTP 路由 ---
	healthRoute(mux)
	authRoute(mux)
	userAvatarRoute(mux)
	webdavRoute(mux)
	fileViewerRoute(mux)
	fileViewerV3Route(mux)
	fileStreamRoute(mux)
	publicDownloadRoute(mux)
	// 极简 Web UI（登录 + 浏览 + 下载），注册为 "/" 兜底；更具体的路径优先
	webUIRoute(mux)

	// --- REST 业务路由（需鉴权）---
	authMux := http.NewServeMux()
	registerRESTServices(authMux)
	// 头像上传为 multipart，需自行鉴权，故注册在鉴权 mux 之外
	authMux.HandleFunc("/api/user/profile/avatar", api.UploadAvatarHandler)
	mux.Handle("/api/", restAuthMiddleware(authMux))
}

// healthRoute 健康检查路由
func healthRoute(mux *http.ServeMux) {
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
}

// authRoute 认证路由（本地认证登录）
func authRoute(mux *http.ServeMux) {
	// 本地认证登录（users 表 + HS256 JWT）——登录接口本身不需要鉴权
	mux.HandleFunc("/api/auth/login", api.LoginHandler)
}

// userAvatarRoute 用户头像读取路由（公开，见 api.AvatarHandler 注释）
func userAvatarRoute(mux *http.ServeMux) {
	mux.HandleFunc("/api/user/avatar/", api.AvatarHandler)
}

// fileStreamRoute 文件流式传输路由（预览/下载）
func fileStreamRoute(mux *http.ServeMux) {
	// 文件预览（直接在浏览器中打开，支持缩略图）
	mux.HandleFunc("/api/file/stream/preview", api.PreviewFileHandler)

	// 文件下载（强制下载）
	mux.HandleFunc("/api/file/stream/download", api.DownloadFileHandler)

	// 视频低码率转码（「流畅播放」）：就绪按 Range 返回 MP4，未就绪回 202 + 进度
	mux.HandleFunc("/api/file/stream/transcode", api.TranscodeFileHandler)

	// 直链下载：302 到提供者直链；不可直链时 302 回代理下载（给浏览器/第三方下载器用）
	mux.HandleFunc("/api/file/stream/direct", api.DirectLinkRedirectHandler)

	// 文件历史版本预览
	mux.HandleFunc("/api/file/history/preview", api.PreviewFileHistoryHandler)

	// 文件历史版本下载
	mux.HandleFunc("/api/file/history/download", api.DownloadFileHistoryHandler)

	// 原始文件预览（用于 Flyfish Viewer 与网页预览）
	// 支持两种路径：
	//   /api/file/preview/raw?file_id=xxx&ticket=yyy
	//   /api/file/preview/raw/filename.pdf?file_id=xxx&ticket=yyy（带文件名，便于 viewer 识别格式）
	mux.HandleFunc("/api/file/preview/raw", api.RawFilePreviewHandler)
	mux.HandleFunc("/api/file/preview/raw/", api.RawFilePreviewHandler)
}

// publicDownloadRoute 公共下载路由（无需认证，签名验证）
func publicDownloadRoute(mux *http.ServeMux) {
	mux.HandleFunc("/s/", api.PublicDownloadHandler)
}

// fileViewerRoute Flyfish Viewer 静态文件路由
//
// 预览页地址里带着 `?url=...&ticket=...`（见 api.RawFilePreviewHandler），
// 必须禁止浏览器把它通过 Referer 带给后续子资源请求与第三方：
// 同源子资源默认会带上完整 URL（含查询串），反代访问日志里就会留下凭证。
func fileViewerRoute(mux *http.ServeMux) {
	viewerDir := "file-viewer"
	fs := http.FileServer(http.Dir(viewerDir))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		fs.ServeHTTP(w, r)
	})
	mux.Handle("/file-viewer/", http.StripPrefix("/file-viewer/", handler))
}

// fileViewerV3Route 新版 Flyfish Viewer（v3，IIFE 包）静态资源路由
//
// 与 v1 的 /file-viewer/ 并存、互不影响：App 仍走 v1 的 index.html，Web UI 优先在
// 页面内 inline mount v3（Shadow DOM 隔离，能拿到主题、工具栏与全格式渲染器），
// 只有这里 404（部署未放置 v3 资源）时才回退到 v1 iframe，再退到直接下载。
//
// 目录由部署方放置，不进仓库（完整分发包 ~139MB，renderers / wasm / vendor 都是
// 按需懒加载，只有实际预览对应格式时才产生这部分流量）。
func fileViewerV3Route(mux *http.ServeMux) {
	viewerDir := "file-viewer-v3"
	fs := http.FileServer(http.Dir(viewerDir))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		// 资源按格式分目录、文件名带版本，可短期缓存
		w.Header().Set("Cache-Control", "public, max-age=3600")
		fs.ServeHTTP(w, r)
	})
	mux.Handle("/file-viewer-v3/", http.StripPrefix("/file-viewer-v3/", handler))
}

// webdavRoute WebDAV 服务路由
func webdavRoute(mux *http.ServeMux) {
	// mountPrefix 必须与下面 StripPrefix 的参数一致：handler 用它把 href 还原成
	// 客户端可见路径（PROPFIND href 与请求 URI 路径不一致时，按路径匹配子项的
	// 客户端会把条目全丢掉）。
	const mountPrefix = "/dav"
	webdavHandler := webdav.NewHandler(mountPrefix)
	mux.Handle(mountPrefix+"/", http.StripPrefix(mountPrefix, noCacheWebdav(webdavHandler)))
}

// noCacheWebdav 给 WebDAV 响应补上显式缓存指令。
//
// go-webdav 只写 Last-Modified 和 ETag，不写 Cache-Control。缺了它，客户端会按
// 「启发式新鲜度」（一般是距 Last-Modified 时长的 10%）直接把响应当成新鲜的复用，
// 连 ETag 都不校验——ReadAny 在 Android 上就因此把几小时前的书文件当成新数据合并
// 回写，丢掉对端的注解。这里要求每次都重新校验。
func noCacheWebdav(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		next.ServeHTTP(w, r)
	})
}

// 编译期断言：确保 middleware 包被引用（鉴权中间件依赖其 AuthHTTPMiddleware）。
var _ = middleware.AuthHTTPMiddleware
