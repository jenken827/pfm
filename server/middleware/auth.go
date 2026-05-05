package middleware

import (
	"context"
	"errors"
	"strings"

	"connectrpc.com/connect"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/server/response"
)

const tokenHeader = "Authorization"

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
			claims, err := casdoorsdk.ParseJwtToken(at)
			if err != nil {
				llog.L().Errorf("token parse failed, err: %v", err)
				return response.ErrResp(&models.Err{
					E: errors.New("token parse failed"), C: native.RespCode_TOKEN_INVALID})
			}
			llog.L().Infof("user info claims: user id: %s, user name: %s, user email: %s", claims.User.Id, claims.User.Name, claims.User.Email)

			if claims.User.Name == "" {
				return response.ErrResp(&models.Err{
					E: errors.New("no token provided"), C: native.RespCode_TOKEN_INVALID})
			}

			// Set user info to context
			ctx = context.WithValue(ctx, globals.ContextUserKey, claims)
			return next(ctx, req)
		}
	}
}
