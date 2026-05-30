package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/llog"
)

// AuthHTTPMiddleware HTTP认证中间件（用于非RPC的HTTP handler）
func AuthHTTPMiddleware(w http.ResponseWriter, r *http.Request) (*casdoorsdk.Claims, error) {
	accessToken := r.Header.Get(tokenHeader)
	if accessToken == "" {
		llog.L().Errorf("no token provided")
		return nil, errors.New("no token provided")
	}

	at, _ := strings.CutPrefix(accessToken, "Bearer ")
	claims, err := casdoorsdk.ParseJwtToken(at)
	if err != nil {
		llog.L().Errorf("token parse failed, err: %v", err)
		return nil, errors.New("token parse failed")
	}

	if claims.User.Name == "" {
		return nil, errors.New("invalid token")
	}

	// 设置用户信息到 context（可选，如果需要）
	ctx := r.Context()
	ctx = context.WithValue(ctx, globals.ContextUserKey, claims)
	*r = *r.WithContext(ctx)

	return claims, nil
}