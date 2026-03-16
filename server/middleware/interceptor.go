package middleware

import (
	"context"
	"errors"
	"runtime/debug"

	"connectrpc.com/connect"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/server/response"
	"github.com/google/uuid"
)

// GInterceptor 全局错误处理拦截器
func GInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(
			ctx context.Context,
			req connect.AnyRequest,
		) (r connect.AnyResponse, e error) {
			uuid := uuid.New().String()
			llog.GetLogger().Infof("request received, uuid:%s, procedure:%s, param: %s", uuid, req.Spec().Procedure, req.Any())
			defer func() {
				if p := recover(); p != nil {
					llog.GetLogger().Errorf("panic occurred: %v", p)
					llog.GetLogger().Error(string(debug.Stack()))
					r = response.ErrResp(native.RespCode_CUSTOM_ERROR, errors.New("internal server error"))
				}
				llog.GetLogger().Infof("request completed! uuid: %s", uuid)
			}()
			r, e = next(ctx, req)
			if e != nil {
				switch er := e.(type) {
				case *connect.Error:
					if er.Code() == connect.CodeInvalidArgument {
						return response.ErrResp(native.RespCode_PARAMS_ERROR, er), nil
					}
					return response.ErrResp(native.RespCode_CUSTOM_ERROR, er), nil
				default:
					return response.ErrResp(native.RespCode_CUSTOM_ERROR, e), nil
				}
			}
			return
		}
	}
}
