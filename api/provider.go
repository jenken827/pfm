package api

import (
	"context"

	"connectrpc.com/connect"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/proto/gen/native/nativeconnect"
	"jenken827.com/jenken827/pfm/server/response"
)

// StorageProviderServer 存储提供者服务
type StorageProviderServer struct{}

var _ = (nativeconnect.StorageProviderServiceHandler)((*StorageProviderServer)(nil))

// AddStorageProvider 添加存储提供者
func (s *StorageProviderServer) AddStorageProvider(ctx context.Context, req *connect.Request[native.StorageProviderAddReq]) (*connect.Response[native.BaseResp], error) {
	provider, err := service.AddStorageProvider(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}

	return response.SuccessResp(&native.StorageProviderAddRespData{
		Id: provider.Id,
	})
}

// GetStorageProvider 获取存储提供者详情
func (s *StorageProviderServer) GetStorageProvider(ctx context.Context, req *connect.Request[native.StorageProviderGetReq]) (*connect.Response[native.BaseResp], error) {
	data, err := service.GetStorageProvider(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}

	return response.SuccessResp(data)
}

// ListStorageProviders 获取存储提供者列表
func (s *StorageProviderServer) ListStorageProviders(ctx context.Context, req *connect.Request[native.StorageProviderListReq]) (*connect.Response[native.BaseResp], error) {
	data, err := service.ListStorageProviders(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}

	return response.SuccessResp(data)
}

// UpdateStorageProvider 更新存储提供者
func (s *StorageProviderServer) UpdateStorageProvider(ctx context.Context, req *connect.Request[native.StorageProviderUpdateReq]) (*connect.Response[native.BaseResp], error) {
	err := service.UpdateStorageProvider(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}

	return response.SuccessResp(nil)
}

// DeleteStorageProvider 删除存储提供者
func (s *StorageProviderServer) DeleteStorageProvider(ctx context.Context, req *connect.Request[native.StorageProviderDeleteReq]) (*connect.Response[native.BaseResp], error) {
	err := service.DeleteStorageProvider(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}

	return response.SuccessResp(nil)
}

// GetStorageProviderStats 获取存储提供者统计信息
func (s *StorageProviderServer) GetStorageProviderStats(ctx context.Context, req *connect.Request[native.StorageProviderStatsReq]) (*connect.Response[native.BaseResp], error) {
	data, err := service.GetStorageProviderStats(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}

	return response.SuccessResp(data)
}
