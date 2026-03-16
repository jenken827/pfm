package middleware

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/server/response"
)

const tokenHeader = "Authentication"

// AuthInterceptor auth middleware
func AuthInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(
			ctx context.Context,
			req connect.AnyRequest,
		) (connect.AnyResponse, error) {
			if req.Header().Get(tokenHeader) == "" {
				// Check token in handlers.
				return response.ErrResp(native.RespCode_TOKEN_INVALID, errors.New("no token provided")), nil

			} else {
				//TODO: 解析token，验证权限等
			}
			return next(ctx, req)
		}
	}
}
