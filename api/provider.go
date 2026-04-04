package api

import (
	"context"

	"connectrpc.com/connect"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/proto/gen/native/nativeconnect"
	"jenken827.com/jenken827/pfm/server/response"
	"google.golang.org/protobuf/types/known/emptypb"
)

// StorageProviderServer 存储提供者服务
type StorageProviderServer struct{}

var _ = (nativeconnect.StorageProviderServiceHandler)((*StorageProviderServer)(nil))

// GetStorageProviderTypes 获取支持的存储提供者类型列表
func (s *StorageProviderServer) GetStorageProviderTypes(ctx context.Context, req *connect.Request[native.GetStorageProviderTypesReq]) (*connect.Response[native.BaseResp], error) {
	data, err := service.GetStorageProviderTypes(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}

	return response.SuccessResp(data)
}

// AddStorageProvider 添加存储提供者
func (s *StorageProviderServer) AddStorageProvider(ctx context.Context, req *connect.Request[native.StorageProvider]) (*connect.Response[native.BaseResp], error) {
	err := service.AddStorageProvider(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}

	return response.SuccessResp(nil)
}

// GetStorageProvider 获取存储提供者详情
func (s *StorageProviderServer) GetStorageProvider(ctx context.Context, req *connect.Request[native.StorageProviderGetReq]) (*connect.Response[native.BaseResp], error) {
	data, err := service.GetStorageProvider(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	pb, ce := data.ToPB(ctx)
	if ce != nil {
		llog.L().Errorf("GetStorageProviders ToPB, id: %d, err: %v", req.Msg.GetId(), err)
		return response.ErrResp(&models.Err{E: ce, C: native.RespCode_INTERNAL_ERROR})
	}

	return response.SuccessResp(&pb)
}

// ListStorageProviders 获取存储提供者列表
func (s *StorageProviderServer) ListStorageProviders(ctx context.Context, req *connect.Request[emptypb.Empty]) (*connect.Response[native.BaseResp], error) {
	data, err := service.ListStorageProviders(ctx)
	if err != nil {
		return response.ErrResp(err)
	}
	items := make([]*native.StorageProvider, 0, len(data))

	// 构建返回结果
	for _, p := range data {
		pb, err := p.ToPB(ctx)
		if err != nil {
			llog.L().Errorf("ListStorageProviders ToPB, err: %v", err)
			return response.ErrResp(&models.Err{E: err, C: native.RespCode_INTERNAL_ERROR})
		}

		items = append(items, &pb)
	}
	return response.SuccessResp(&native.StorageProviderListRespData{Providers: items})
}

// UpdateStorageProvider 更新存储提供者
func (s *StorageProviderServer) UpdateStorageProvider(ctx context.Context, req *connect.Request[native.StorageProvider]) (*connect.Response[native.BaseResp], error) {
	err := service.UpdateStorageProvider(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}

	return response.SuccessResp(nil)
}

// DeleteStorageProvider 删除存储提供者
func (s *StorageProviderServer) DeleteStorageProvider(ctx context.Context, req *connect.Request[native.StorageProviderDeleteReq]) (*connect.Response[native.BaseResp], error) {
	err := service.DeleteStorageProvider(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}

	return response.SuccessResp(nil)
}
