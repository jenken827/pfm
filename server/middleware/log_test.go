package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jenken827/pfm/globals"
	"github.com/jenken827/pfm/pkg/llog"
)

// 验证：request_id 注入 ctx + 回写响应头 + 状态码/字节数记录 + panic 恢复。

func TestHTTPLogMiddlewareInjectsRequestID(t *testing.T) {
	var gotID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = llog.CtxRequestID(r.Context())
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("hello"))
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/file/list", nil)
	HTTPLogMiddleware(inner).ServeHTTP(rec, req)

	if gotID == "" {
		t.Fatal("request_id 未注入 context")
	}
	if h := rec.Header().Get(requestIDHeader); h != gotID {
		t.Fatalf("响应头 %s = %q, want %q", requestIDHeader, h, gotID)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
}

func TestHTTPLogMiddlewareRecordsStatusAndSize(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("abcdef")) // 隐式 200
	})
	rec := httptest.NewRecorder()
	HTTPLogMiddleware(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "abcdef" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestHTTPLogMiddlewareRecoversPanic(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})
	rec := httptest.NewRecorder()
	HTTPLogMiddleware(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/panic", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "INTERNAL_ERROR") {
		t.Fatalf("body 应含 INTERNAL_ERROR, got %q", rec.Body.String())
	}
	// panic 细节不能泄露给客户端
	if strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("panic 细节泄露: %q", rec.Body.String())
	}
}

// 验证 request_id 在 ctx 中可被下游读取（llog.C 的字段注入前提）。
func TestContextRequestIDAccessible(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, ok := r.Context().Value(globals.ContextRequestIDKey).(string); !ok || id == "" {
			t.Error("globals.ContextRequestIDKey 未在 ctx 中")
		}
	})
	HTTPLogMiddleware(inner).ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/y", nil))
}
