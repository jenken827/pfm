package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 极简 Web UI 的路由行为："/" 与 "/web/" 出页面，其它路径仍 404（不能被 UI 吞掉），
// 非 GET/HEAD 返回 405。
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
			t.Errorf("GET %s 响应里没有页面标记（body 前 80 字节: %q）", path, body[:min(80, len(body))])
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
