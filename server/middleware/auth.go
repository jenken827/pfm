package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"connectrpc.com/connect"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/db"
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

			// Debug 模式：绕过认证，使用模拟的管理员用户
			if globals.Debug {
				claims := &casdoorsdk.Claims{
					User: casdoorsdk.User{
						Id:    "debug-admin-id",
						Name:  "debug-admin",
						Email: "debug@localhost",
						Roles: []*casdoorsdk.Role{
							{Name: "pfm_admin"},
						},
					},
				}
				ctx = context.WithValue(ctx, globals.ContextUserKey, claims)
				return next(ctx, req)
			}

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

			// 注入 UserSpace 信息到 context
			ctx = injectUserSpace(ctx, claims.User.Id)

			return next(ctx, req)
		}
	}
}

// injectUserSpace 查询用户空间信息并注入 context
// 如果用户没有 UserSpace（未配置根目录），不阻断请求，service 层自行判断
func injectUserSpace(ctx context.Context, userID string) context.Context {
	space, err := db.GetUserSpaceByUserID(userID)
	if err != nil {
		llog.L().Debugf("user %s has no UserSpace: %v", userID, err)
		return ctx
	}

	var providerIds []uint32
	if space.BoundProviderIds != nil && *space.BoundProviderIds != "" {
		if err := json.Unmarshal([]byte(*space.BoundProviderIds), &providerIds); err != nil {
			llog.L().Warnf("invalid bound_provider_ids for user %s: %v", userID, err)
		}
	}

	rootFileID := ""
	if space.RootFileId != nil {
		rootFileID = *space.RootFileId
	}

	uc := &models.UserContext{
		UserID:           userID,
		IsAdmin:          db.IsAdmin(userID),
		RootFileID:       rootFileID,
		BoundProviderIds: providerIds,
	}
	return context.WithValue(ctx, globals.ContextUserSpaceKey, uc)
}
