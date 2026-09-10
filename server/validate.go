package server

import (
	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/proto"
)

// validateMessage 依据 proto 中的 buf.validate.field 注解对请求消息做字段校验。
//
// 迁移说明：原先该校验由 connect 的 validate.NewInterceptor() 在拦截器链中执行；
// 改用 REST 后，在 route_rest.serveREST 中显式调用，校验语义与注解定义完全不变。
// 校验失败返回错误，调用方转成 HTTP 400 + RespCode_PARAMS_ERROR。
func validateMessage(msg proto.Message) error {
	return protovalidate.Validate(msg)
}
