package api

import (
	"context"

	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/internal/service"
	"github.com/jenken827/pfm/server/response"
)

// McpTokenServer MCP 令牌管理（管理台与客户端共用）
type McpTokenServer struct{}

// CreateMcpToken 签发令牌：明文只在这次响应里出现
func (s *McpTokenServer) CreateMcpToken(ctx context.Context, req *native.CreateMcpTokenReq) (*native.BaseResp, error) {
	data, mErr := service.CreateMcpToken(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(data)
}

// ListMcpTokens 列出令牌（普通用户只看自己的）
func (s *McpTokenServer) ListMcpTokens(ctx context.Context, req *native.ListMcpTokensReq) (*native.BaseResp, error) {
	data, mErr := service.ListMcpTokens(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(data)
}

// RevokeMcpToken 撤销令牌
func (s *McpTokenServer) RevokeMcpToken(ctx context.Context, req *native.RevokeMcpTokenReq) (*native.BaseResp, error) {
	if mErr := service.RevokeMcpToken(ctx, req); mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(nil)
}
