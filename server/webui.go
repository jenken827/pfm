package server

import (
	_ "embed"
	"net/http"
)

// webUIIndexHTML 极简 Web UI（单文件，内联 CSS/JS，无外部依赖）。
//
// 编译期 embed 进二进制：部署仍是单文件，不依赖运行目录下的静态资源
// （与 /file-viewer/ 那种需要额外目录的方式不同）。
//
//go:embed webui/index.html
var webUIIndexHTML []byte

// webUIRoute 注册极简 Web UI。
//
// 定位：浏览器里能登录、浏览目录、下载文件即可——不做上传 / 分享 / 备份 / 相册
// 等 App 全功能（那些依赖 Flutter 插件，见 frontend/pubspec.yaml）。
//
// 挂载点：
//   - "/"      直接访问服务地址即进入（其它未匹配路径仍返回 404）
//   - "/web/"  备用前缀，便于与将来的其它静态页区分
//
// 鉴权由页面自己完成：登录拿 JWT 后放进 Authorization 头调 REST 接口；
// 文件下载用 `<a href>`，无法带请求头，故走 /api/file/stream/download 的
// ?token= 查询参数（见 api.applyQueryToken）。
func webUIRoute(mux *http.ServeMux) {
	mux.HandleFunc("/", serveWebUI)
}

func serveWebUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switch r.URL.Path {
	case "/", "/index.html", "/web/":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// 页面很小且随版本更新，禁缓存以免升级后浏览器仍用旧页面
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(webUIIndexHTML)
	case "/web":
		http.Redirect(w, r, "/web/", http.StatusMovedPermanently)
	default:
		http.NotFound(w, r)
	}
}
