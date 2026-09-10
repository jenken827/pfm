package middleware

import (
	"context"
	"errors"
	"net/http"
	"runtime/debug"
	"time"

	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/server/response"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
)

// requestIDHeader 请求 ID 响应头：前端报错时可带上该值反查服务端日志。
const requestIDHeader = "X-Request-Id"

// errInternal 统一的内部错误文案（不向客户端泄露 panic 细节）。
var errInternal = errors.New("internal server error")

// statusRecorder 包装 ResponseWriter 以捕获实际写入的状态码与响应字节数，
// 供访问日志记录（不改变对外行为）。
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// HTTPLogMiddleware 请求日志 + panic 恢复（合并中间件，替代原 connect GInterceptor）。
//
// 职责（对应原 GInterceptor 的能力，按 HTTP 语义重塑）：
//   - 为每个请求生成 uuid，注入 ctx（globals.ContextRequestIDKey）并回写响应头
//     X-Request-Id，便于用该 id 关联服务端全部日志；
//   - 记录请求进入与完成两条 Info 日志（含 method / path / uuid / status /
//     响应字节数 / 耗时）；panic 时同样记录完成日志（cost 截至 panic 点）；
//   - 捕获 panic 并转为统一 BaseResp(INTERNAL_ERROR)，HTTP 返回 500（协议级错误，
//     尚未写出响应体时）。
//
// 日志级别为 Info：每个请求两条。
func HTTPLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := uuid.New().String()

		// 注入 request_id 与响应头：后续 handler 可通过 llog.C(ctx) 自动携带该字段。
		ctx := context.WithValue(r.Context(), globals.ContextRequestIDKey, reqID)
		w.Header().Set(requestIDHeader, reqID)

		method := r.Method
		path := r.URL.Path
		llog.L().Infof("request received, uuid: %s, method: %s, path: %s", reqID, method, path)

		rec := &statusRecorder{ResponseWriter: w}
		defer func() {
			if p := recover(); p != nil {
				llog.L().Errorf("[HTTP PANIC] uuid: %s, method: %s, path: %s, panic: %v\n%s",
					reqID, method, path, p, debug.Stack())
				writePanicResp(rec)
			}
			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			llog.L().Infof("request completed, uuid: %s, method: %s, path: %s, status: %d, size: %d, cost: %v",
				reqID, method, path, rec.status, rec.bytes, time.Since(start))
		}()

		next.ServeHTTP(rec, r.WithContext(ctx))
	})
}

// writePanicResp 把 panic 转为统一 BaseResp 错误响应（HTTP 500）。
// 若响应头已写出则无法再改状态码，此时仅记录，由完成日志体现。
func writePanicResp(w http.ResponseWriter) {
	resp, err := response.ErrResp(&models.Err{
		C: native.RespCode_INTERNAL_ERROR,
		E: errInternal,
	})
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	data, mErr := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(resp)
	if mErr != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write(data)
}
