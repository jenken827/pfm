package middleware

import (
	"context"
	"errors"
	"strings"

	"connectrpc.com/connect"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/server/response"
)

const tokenHeader = "Authorization"

// verifier 全局 token 验证器（在 bootstrap 中初始化）
var verifier *auth.TokenVerifier

// InitVerifier 初始化 token 验证器，必须在 Casdoor 初始化之后调用
func InitVerifier() {
	verifier = auth.NewTokenVerifier()
	llog.L().Info("token verifier initialized")
}

// AuthInterceptor auth middleware
func AuthInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(
			ctx context.Context,
			req connect.AnyRequest,
		) (connect.AnyResponse, error) {

			accessToken := req.Header().Get(tokenHeader)
			if accessToken == "" {
				llog.L().Errorf("no token provided")
				return response.ErrResp(&models.Err{
					E: errors.New("no token provided"), C: native.RespCode_TOKEN_INVALID})

			}
			at, _ := strings.CutPrefix(accessToken, "Bearer ")

			claims, err := verifier.Verify(at)
			if err != nil {
				llog.L().Errorf("token verify failed: %v", err)
				return response.ErrResp(&models.Err{
					E: err, C: native.RespCode_TOKEN_INVALID})
			}

			llog.L().Infof("user info claims: user id: %s, user name: %s, user email: %s", claims.User.Id, claims.User.Name, claims.User.Email)

			// Set user info to context
			ctx = context.WithValue(ctx, globals.ContextUserKey, claims)
			return next(ctx, req)
		}
	}
}
