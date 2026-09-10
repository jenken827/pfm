package api

import (
	"context"

	"connectrpc.com/connect"

	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/proto/gen/native/nativeconnect"
	"jenken827.com/jenken827/pfm/server/response"
)

// DownloadServer URL 离线下载服务实现
type DownloadServer struct{}

var _ = (nativeconnect.DownloadServiceHandler)((*DownloadServer)(nil))

// CreateOfflineDownload 创建离线下载任务
func (s *DownloadServer) CreateOfflineDownload(ctx context.Context, req *connect.Request[native.CreateOfflineDownloadReq]) (*connect.Response[native.BaseResp], error) {
	resp, err := service.CreateOfflineDownload(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(resp)
}

// ListOfflineDownloads 离线下载任务列表
func (s *DownloadServer) ListOfflineDownloads(ctx context.Context, req *connect.Request[native.ListOfflineDownloadsReq]) (*connect.Response[native.BaseResp], error) {
	resp, err := service.ListOfflineDownloads(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(resp)
}

// DeleteOfflineDownload 删除离线下载任务
func (s *DownloadServer) DeleteOfflineDownload(ctx context.Context, req *connect.Request[native.DeleteOfflineDownloadReq]) (*connect.Response[native.BaseResp], error) {
	if err := service.DeleteOfflineDownload(ctx, req.Msg); err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// RetryOfflineDownload 重试离线下载任务
func (s *DownloadServer) RetryOfflineDownload(ctx context.Context, req *connect.Request[native.RetryOfflineDownloadReq]) (*connect.Response[native.BaseResp], error) {
	resp, err := service.RetryOfflineDownload(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(resp)
}
