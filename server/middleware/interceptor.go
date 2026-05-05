package middleware

import (
	"context"
	"errors"
	"runtime/debug"
	"time"

	"connectrpc.com/connect"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
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
			start := time.Now()
			uuid := uuid.New().String()
			llog.L().Infof("request received, uuid:%s, procedure:%s", uuid, req.Spec().Procedure)
			defer func() {
				if p := recover(); p != nil {
					llog.L().Errorf("panic occurred: %v", p)
					llog.L().Error(string(debug.Stack()))
					r, e = response.ErrResp(&models.Err{
						C: native.RespCode_INTERNAL_ERROR,
						E: errors.New("internal server error")})

				}
				llog.L().Infof("request completed! uuid: %s, procedure: %s, cost: %v", uuid, req.Spec().Procedure, time.Since(start))
			}()
			r, e = next(ctx, req)
			if e != nil {
				switch er := e.(type) {
				case *connect.Error:
					if er.Code() == connect.CodeInvalidArgument {
						return response.ErrResp(&models.Err{C: native.RespCode_PARAMS_ERROR, E: er})
					}
					return
				default:
					return
				}
			}
			return
		}
	}
}
