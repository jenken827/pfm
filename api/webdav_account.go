package api

import (
	"context"

	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/server/response"
)

// WebdavAccountServer WebDAV 独立账号服务
//
// 全部为「当前登录用户管理自己账号」的自助接口，管理员不参与（用户资料由用户自管）。
// 路由经 restAuthMiddleware（JWT），与 /dav 的 Basic Auth 凭据完全分离。
type WebdavAccountServer struct{}

// ListWebdavAccounts 列出当前用户的 WebDAV 账号
func (s *WebdavAccountServer) ListWebdavAccounts(ctx context.Context, req *native.WebdavAccountListReq) (*native.BaseResp, error) {
	resp, mErr := service.ListWebdavAccounts(ctx)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(resp)
}

// CreateWebdavAccount 创建 WebDAV 账号（明文密码仅本次响应返回）
func (s *WebdavAccountServer) CreateWebdavAccount(ctx context.Context, req *native.WebdavAccountCreateReq) (*native.BaseResp, error) {
	resp, mErr := service.CreateWebdavAccount(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(resp)
}

// UpdateWebdavAccount 更新 WebDAV 账号（标签/绑定目录/只读/启用）
func (s *WebdavAccountServer) UpdateWebdavAccount(ctx context.Context, req *native.WebdavAccountUpdateReq) (*native.BaseResp, error) {
	if mErr := service.UpdateWebdavAccount(ctx, req); mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(nil)
}

// DeleteWebdavAccount 删除 WebDAV 账号
func (s *WebdavAccountServer) DeleteWebdavAccount(ctx context.Context, req *native.WebdavAccountDeleteReq) (*native.BaseResp, error) {
	if mErr := service.DeleteWebdavAccount(ctx, req); mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(nil)
}

// ResetWebdavAccountPassword 重置 WebDAV 账号密码（明文仅本次响应返回）
func (s *WebdavAccountServer) ResetWebdavAccountPassword(ctx context.Context, req *native.WebdavAccountResetPasswordReq) (*native.BaseResp, error) {
	resp, mErr := service.ResetWebdavAccountPassword(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(resp)
}
