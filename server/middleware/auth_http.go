package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"jenken827.com/jenken827/pfm/globals"
)

// AuthHTTPMiddleware HTTP认证中间件（用于非RPC的HTTP handler）
func AuthHTTPMiddleware(w http.ResponseWriter, r *http.Request) (*casdoorsdk.Claims, error) {
	accessToken := r.Header.Get(tokenHeader)
	if accessToken == "" {
		return nil, errors.New("no token provided")
	}

	at, _ := strings.CutPrefix(accessToken, "Bearer ")

	claims, err := verifier.Verify(at)
	if err != nil {
		return nil, err
	}

	// 设置用户信息到 context
	ctx := r.Context()
	ctx = context.WithValue(ctx, globals.ContextUserKey, claims)
	ctx = injectUserSpace(ctx, claims.User.Id)
	*r = *r.WithContext(ctx)

	return claims, nil
}
