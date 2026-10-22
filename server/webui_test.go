package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Web UI 的路由行为："/" 与 "/web/" 出页面，静态资源从内嵌 FS 出，其它路径仍 404
// （兜底路由不能把 API / dav 路径吞成页面），非 GET/HEAD 返回 405。
func TestWebUIRoute(t *testing.T) {
	mux := http.NewServeMux()
	webUIRoute(mux)

	do := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec
	}

	for _, path := range []string{"/", "/index.html", "/web/"} {
		rec := do(http.MethodGet, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("GET %s Content-Type = %q, want text/html", path, ct)
		}
		if body := rec.Body.String(); !strings.Contains(body, "PFM 文件浏览") {
			t.Errorf("GET %s 响应里没有页面标记", path)
		}
	}

	if rec := do(http.MethodGet, "/web"); rec.Code != http.StatusMovedPermanently {
		t.Errorf("GET /web = %d, want 301", rec.Code)
	}

	// 兜底路由不能把未知路径 / API 路径变成页面
	for _, path := range []string{"/nope", "/api/file/list", "/dav/"} {
		if rec := do(http.MethodGet, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}

	if rec := do(http.MethodPost, "/"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST / = %d, want 405", rec.Code)
	}
}

// 静态资源：内嵌 FS 直接出，扩展名决定 Content-Type；不列目录、不存在即 404。
func TestWebUIAssets(t *testing.T) {
	mux := http.NewServeMux()
	webUIRoute(mux)

	cases := []struct {
		path string
		want string
	}{
		{"/web/css/app.css", "text/css"},
		{"/web/js/app.js", "text/javascript"},
		{"/web/js/views/files.js", "text/javascript"},
		{"/web/js/text_preview.js", "text/javascript"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", c.path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, c.want) {
			t.Errorf("GET %s Content-Type = %q, want contains %q", c.path, ct, c.want)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("GET %s 空响应", c.path)
		}
	}

	for _, path := range []string{"/web/js/", "/web/js", "/web/js/missing.js"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404（不列目录 / 不编造文件）", path, rec.Code)
		}
	}

	// 路径穿越：既不能把嵌入树之外的仓库文件读出来，也不能回显目录内容。
	// ServeMux 会先把 ".." 规范化（301/302），所以两种结果都算安全：非 200 或跳到合法路径。
	for _, path := range []string{
		"/web/../server/webui.go",
		"/web/css/../../go.mod",
		"/web/..%2f..%2fgo.mod",
		"/web/css/%2e%2e/%2e%2e/go.mod",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		// ServeMux 会先把 ".." 规范化成 301，跟一次到最终响应再判断
		if loc := rec.Header().Get("Location"); rec.Code >= 300 && rec.Code < 400 && loc != "" {
			rec = httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, loc, nil))
		}
		if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "module ") {
			t.Errorf("GET %s 泄露了仓库文件（%d）", path, rec.Code)
		}
	}
}

// 页面带安全响应头：CSP 禁外链与 object，禁 Referer 外泄（URL 里可能有票据）。
func TestWebUISecurityHeaders(t *testing.T) {
	mux := http.NewServeMux()
	webUIRoute(mux)

	for _, path := range []string{"/", "/web/", "/web/js/app.js"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		csp := rec.Header().Get("Content-Security-Policy")
		if csp == "" {
			t.Fatalf("GET %s 缺少 CSP", path)
		}
		for _, want := range []string{
			"default-src 'self'",
			"script-src 'self'",
			"object-src 'none'",
			"frame-ancestors 'self'",
			"connect-src 'self'",
			"worker-src 'self' blob:",
		} {
			if !strings.Contains(csp, want) {
				t.Errorf("GET %s CSP 缺少 %q：%s", path, want, csp)
			}
		}
		if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("GET %s Referrer-Policy = %q, want no-referrer", path, got)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("GET %s X-Content-Type-Options = %q", path, got)
		}
	}
}

// 内嵌的 JS / CSS 不许引用任何外部地址：页面必须能离线 / 内网使用，也不能把
// 访问痕迹（Referer）带给第三方。CSP 已经在运行时兜底，这里在构建期再挡一次。
func TestWebUIAssetsHaveNoExternalReferences(t *testing.T) {
	// 只挡"会被浏览器真的去加载外部资源"的写法；普通文案里的 URL 示例不算
	extRe := regexp.MustCompile(`(?i)(?:src|href)\s*=\s*["']?\s*(?:https?:)?//|@import\s+url\s*\(|(?:from|import)\s+["']https?:`)

	err := fs.WalkDir(webUIFS, "webui", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".js") && !strings.HasSuffix(path, ".css") {
			return nil
		}
		body, err := fs.ReadFile(webUIFS, path)
		if err != nil {
			return err
		}
		if loc := extRe.FindString(string(body)); loc != "" {
			t.Errorf("%s 含外部地址引用：%q", path, loc)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded webui: %v", err)
	}
}

// 文本预览白名单必须与客户端 Dart 版一致：两端对"哪些文件按文本打开"不能有分叉。
//
// 这里直接读 `frontend/lib/utils/text_preview.dart` 做集合比对——它是同一仓库的
// 唯一真理源；改一处忘了另一处会在这里红掉。
func TestTextWhitelistParityWithDart(t *testing.T) {
	dartPath := filepath.Join("..", "frontend", "lib", "utils", "text_preview.dart")
	dartSrc, err := os.ReadFile(dartPath)
	if err != nil {
		t.Fatalf("读取 Dart 白名单失败（工作目录应为 server/）：%v", err)
	}
	jsSrc, err := fs.ReadFile(webUIFS, "webui/js/text_preview.js")
	if err != nil {
		t.Fatalf("读取内嵌 JS 白名单失败：%v", err)
	}

	cases := []struct{ dartName, jsName string }{
		{"_textExtensions", "TEXT_EXTENSIONS"},
		{"_textFileNames", "TEXT_FILE_NAMES"},
		{"_markdownExtensions", "MARKDOWN_EXTENSIONS"},
	}
	for _, c := range cases {
		dartSet := extractSet(t, "dart", c.dartName, string(dartSrc))
		jsSet := extractSet(t, "js", c.jsName, string(jsSrc))
		if diff := setDiff(dartSet, jsSet); len(diff) > 0 {
			t.Errorf("%s 只在 Dart 里：%v", c.dartName, diff)
		}
		if diff := setDiff(jsSet, dartSet); len(diff) > 0 {
			t.Errorf("%s 只在 JS 里：%v", c.jsName, diff)
		}
	}
}

// extractSet 从 Dart 的 `name = { 'a', 'b' };` 或 JS 的 `name = new Set([ 'a' ]);` 里取字符串集合
func extractSet(t *testing.T, kind, name, src string) map[string]struct{} {
	t.Helper()
	pattern := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(name) + `\s*=\s*new Set\(\[(.*?)\]\);`)
	if kind == "dart" {
		pattern = regexp.MustCompile(`(?s)` + regexp.QuoteMeta(name) + `\s*=\s*\{(.*?)\};`)
	}
	m := pattern.FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("在 %s 源码里找不到集合 %s", kind, name)
	}
	out := map[string]struct{}{}
	for _, s := range regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(m[1], -1) {
		out[s[1]] = struct{}{}
	}
	if len(out) == 0 {
		t.Fatalf("%s 集合 %s 解析为空", kind, name)
	}
	return out
}

func setDiff(a, b map[string]struct{}) []string {
	var out []string
	for k := range a {
		if _, ok := b[k]; !ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// /file-viewer-v3/ 与老的 /file-viewer/ 并存：前者给网页内联预览，后者是 App 用的
// v1 页面。两者都从运行目录读，缺目录时就是 404（不 panic、不返回页面）。
func TestFileViewerV3Route(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "file-viewer-v3", "renderers"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"file-viewer-v3/flyfish-file-viewer-web-full.iife.js": "window.FlyfishFileViewerWebFull={};",
		"file-viewer-v3/renderers/pdf.iife.js":                "// pdf renderer",
	}
	for name, body := range files {
		full := filepath.Join(dir, name)
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Chdir(dir)

	mux := http.NewServeMux()
	fileViewerV3Route(mux)

	for name := range files {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+name, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET /%s = %d, want 200", name, rec.Code)
		}
		if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("GET /%s Referrer-Policy = %q", name, got)
		}
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/file-viewer-v3/missing.js", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("缺失资源 = %d, want 404", rec.Code)
	}
}
