package server

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// webUIFS 内嵌的 Web UI 静态资源（与二进制同生共死，部署不需要额外目录）。
//
// 拆成 index.html + css/ + js/ 多个文件，用原生 ES module 加载：不引入打包器
// （构建链只有 go build），不开 CDN（离线 / 内网可用，也不泄露访问痕迹）。
//
//go:embed webui
var webUIFS embed.FS

// webUICSP 页面内容安全策略。
//
//   - script-src 'self'：只允许本页自带的模块；'wasm-unsafe-eval' 是内联
//     Flyfish Viewer v3 解 WASM 需要的最小许可（不是 'unsafe-eval'）；
//   - style-src 'unsafe-inline'：Viewer 往 Shadow DOM 里注入 <style>；
//   - worker-src blob:：Viewer 的解析线程从 blob URL 起 Worker；
//   - frame-src 'self'：v1 Viewer 的 iframe 回退路径；
//   - connect-src 'self'：票据只发回本站，任何外发请求都被拦掉。
const webUICSP = "default-src 'self'; " +
	"script-src 'self' 'wasm-unsafe-eval' blob:; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' blob: data:; " +
	"media-src 'self' blob:; " +
	"font-src 'self' data:; " +
	"connect-src 'self'; " +
	"worker-src 'self' blob:; " +
	"child-src 'self' blob:; " +
	"frame-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"form-action 'self'; " +
	"frame-ancestors 'self'"

// webUIRoute 注册 Web UI。
//
// 挂载点：
//   - "/"、"/index.html"：直接访问服务地址即进入（其它未匹配路径仍 404）
//   - "/web/"：页面与静态资源（js / css），便于与其它静态页区分
//
// 鉴权由页面自己完成：登录拿 JWT 后放进 Authorization 头调 REST 接口；文件字节
// （图片 / 文本 / 媒体 / Viewer / 下载）走 ?ticket= 单文件票据——浏览器带不了请求头，
// 而 access token 又绝不能出现在 URL 里（见 internal/auth/preview_ticket.go）。
func webUIRoute(mux *http.ServeMux) {
	sub, err := fs.Sub(webUIFS, "webui")
	if err != nil {
		panic("webui: embed subtree missing: " + err.Error())
	}
	files := http.FileServer(http.FS(sub))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		setWebUISecurityHeaders(w)

		switch {
		case r.URL.Path == "/" || r.URL.Path == "/index.html" || r.URL.Path == "/web/":
			serveWebUIIndex(w, sub)
		case r.URL.Path == "/web":
			http.Redirect(w, r, "/web/", http.StatusMovedPermanently)
		case strings.HasPrefix(r.URL.Path, "/web/"):
			serveWebUIAsset(w, r, files)
		default:
			http.NotFound(w, r)
		}
	})
}

// setWebUISecurityHeaders 页面级安全响应头（所有页面与资源都带）。
//
// 页面 URL 里可能带票据（预览页），因此禁止 Referer 外泄，且一律不缓存。
func setWebUISecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Security-Policy", webUICSP)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-cache")
}

// serveWebUIIndex 输出页面骨架（很小，随二进制更新，禁缓存以免升级后还在跑旧 JS）。
func serveWebUIIndex(w http.ResponseWriter, sub fs.FS) {
	body, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		http.Error(w, "web ui not available", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(body)
}

// serveWebUIAsset 输出 /web/ 下的静态资源；不列目录、不存在即 404。
func serveWebUIAsset(w http.ResponseWriter, r *http.Request, files http.Handler) {
	rest := strings.TrimPrefix(r.URL.Path, "/web/")
	// 不列目录：只放行带扩展名的文件（js / css），其余（含以 / 结尾的目录）一律 404
	if rest == "" || strings.HasSuffix(rest, "/") || !strings.Contains(path.Base(rest), ".") {
		http.NotFound(w, r)
		return
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/" + rest
	files.ServeHTTP(w, r2)
}
