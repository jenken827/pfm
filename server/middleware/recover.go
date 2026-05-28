package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"jenken827.com/jenken827/pfm/internal/llog"
)

// HTTPRecoverMiddleware HTTP 请求 panic 恢复中间件
func HTTPRecoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				llog.L().Errorf("[HTTP PANIC] %s %s: %v", r.Method, r.URL.Path, err)
				llog.L().Error(string(debug.Stack()))
				http.Error(w, fmt.Sprintf("internal server error: %v", err), http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
