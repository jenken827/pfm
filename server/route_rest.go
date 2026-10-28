package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"

	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/internal/models"
	"github.com/jenken827/pfm/pkg/llog"
	"github.com/jenken827/pfm/server/response"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 本文件是 REST 传输层的核心：把注册的 (路径, 方法) 映射到 api 层的业务函数。
//
// 设计要点：
//   - proto 只提供数据结构，路由表在 route_rest.go 中显式维护，不使用 proto 注解。
//   - 请求体为 JSON（proto3 JSON 映射），响应统一为 native.BaseResp（code/message/data(Any)）。
//   - 业务错误一律通过 BaseResp.Code 表达，HTTP 状态码保持 200；仅协议级错误
//     （路径不存在、JSON 非法、鉴权失败）使用非 200。
//
// 用法（route_rest.go）：
//
//	Register(mux, "POST", "/api/file/list",
//	    func(ctx context.Context, req *native.FileListReq) (*native.BaseResp, error) {
//	        files, total, hasMore, err := service.GetFileList(ctx, req)
//	        ...
//	    })

// HandlerFunc 是 api 层业务函数的统一签名：接收已反序列化的请求消息，
// 返回 BaseResp。ctx 已由中间件注入用户身份（globals.ContextUserKey）。
type HandlerFunc[TReq proto.Message] func(ctx context.Context, req TReq) (*native.BaseResp, error)

// Register 将一个业务函数注册到 mux 的指定方法与路径上。
func Register[TReq proto.Message](mux *http.ServeMux, method, path string, fn HandlerFunc[TReq]) {
	// 从零值经 protobuf 反射构造同类型的新消息实例，避免为每个请求做类型断言。
	var zero TReq
	msgType := zero.ProtoReflect().Type()

	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			writeProtoError(w, http.StatusMethodNotAllowed, native.RespCode_PARAMS_ERROR,
				fmt.Sprintf("method not allowed: %s", r.Method))
			return
		}
		req := msgType.New().Interface().(TReq)
		serveREST(w, r, req, fn)
	})
}

// serveREST 执行一次完整的 REST 请求处理：
// 反序列化 -> 校验 -> 调用业务函数 -> 序列化 BaseResp。
func serveREST[TReq proto.Message](
	w http.ResponseWriter,
	r *http.Request,
	req TReq,
	fn HandlerFunc[TReq],
) {
	if err := decodeRequest(r, req); err != nil {
		llog.L().Errorf("decode request failed, path: %s, err: %v", r.URL.Path, err)
		writeProtoError(w, http.StatusBadRequest, native.RespCode_PARAMS_ERROR, err.Error())
		return
	}

	if err := validateMessage(req); err != nil {
		llog.L().Errorf("validate request failed, path: %s, err: %v", r.URL.Path, err)
		writeProtoError(w, http.StatusBadRequest, native.RespCode_PARAMS_ERROR, err.Error())
		return
	}

	resp, err := callHandler(r.Context(), fn, req, r.URL.Path)
	if err != nil {
		writeProtoError(w, http.StatusInternalServerError, native.RespCode_INTERNAL_ERROR, err.Error())
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

// callHandler 调用业务函数并恢复 panic，统一转换为 error。
func callHandler[TReq proto.Message](
	ctx context.Context,
	fn HandlerFunc[TReq],
	req TReq,
	path string,
) (resp *native.BaseResp, err error) {
	defer func() {
		if p := recover(); p != nil {
			llog.C(ctx).Errorf("[REST PANIC] path: %s, panic: %v\n%s", path, p, debug.Stack())
			resp, err = response.ErrResp(&models.Err{
				C: native.RespCode_INTERNAL_ERROR,
				E: fmt.Errorf("internal server error"),
			})
		}
	}()
	return fn(ctx, req)
}

// decodeRequest 把 HTTP 请求解析为 proto 消息。
// GET/DELETE：从 URL query 解析。其他方法：从 JSON body 解析，空 body 视为空消息。
func decodeRequest[TReq proto.Message](r *http.Request, req TReq) error {
	if r.Method == http.MethodGet || r.Method == http.MethodDelete {
		return decodeQuery(r, req)
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes))
	if err != nil {
		return fmt.Errorf("read body failed: %w", err)
	}
	if len(body) == 0 {
		return nil // 空 body 视为空请求消息（如 google.protobuf.Empty 语义）
	}
	if err := protojson.Unmarshal(body, req); err != nil {
		return fmt.Errorf("invalid json body: %w", err)
	}
	return nil
}

// decodeQuery 将 URL query 参数赋值到消息字段。
// 复用 proto3 JSON 映射：把 query 组装为 JSON 对象后交给 protojson，
// 从而自动获得类型转换与字段名兼容（camelCase / snake_case 均可）。
func decodeQuery[TReq proto.Message](r *http.Request, req TReq) error {
	query := r.URL.Query()
	if len(query) == 0 {
		return nil
	}
	obj := make(map[string]any, len(query))
	for k, vs := range query {
		if len(vs) == 0 {
			continue
		}
		obj[k] = coerceQueryValue(vs)
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("marshal query failed: %w", err)
	}
	if err := protojson.Unmarshal(raw, req); err != nil {
		return fmt.Errorf("invalid query params: %w", err)
	}
	return nil
}

// coerceQueryValue 单个 query 值直接使用；重复出现时转为数组（支持 repeated 字段）。
func coerceQueryValue(vs []string) any {
	if len(vs) == 1 {
		return vs[0]
	}
	return vs
}

// marshalOpts 统一响应序列化选项：
//   - 输出 camelCase，与前端 toProto3Json/mergeFromProto3Json 默认一致；
//   - EmitUnpopulated 保持字段存在（Any 展开依赖 protoregistry.GlobalTypes 自动解析）。
var marshalOpts = protojson.MarshalOptions{
	EmitUnpopulated: true,
}

// writeProtoJSON 以 proto3 JSON 写出响应。
func writeProtoJSON(w http.ResponseWriter, status int, msg proto.Message) {
	if msg == nil {
		w.WriteHeader(status)
		return
	}
	data, err := marshalOpts.Marshal(msg)
	if err != nil {
		llog.L().Errorf("marshal response failed, err: %v", err)
		writeProtoError(w, http.StatusInternalServerError, native.RespCode_INTERNAL_ERROR,
			"marshal response failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

// writeProtoError 写出协议级错误（非业务错误），结构与业务响应一致，便于前端统一解析。
func writeProtoError(w http.ResponseWriter, status int, code native.RespCode, message string) {
	resp := &native.BaseResp{
		Code:    &code,
		Message: &message,
	}
	data, err := marshalOpts.Marshal(resp)
	if err != nil {
		http.Error(w, message, status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

// maxRequestBodyBytes 限制请求体大小，防止超大 body 打爆内存。
const maxRequestBodyBytes = 32 << 20 // 32MB
