package api

import (
	"context"

	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/internal/models"
	"github.com/jenken827/pfm/internal/service"
	"github.com/jenken827/pfm/pkg/llog"
	"github.com/jenken827/pfm/server/response"
	"google.golang.org/protobuf/types/known/emptypb"
)

// StorageProviderServer 存储提供者服务
type StorageProviderServer struct{}

// GetStorageProviderTypes 获取支持的存储提供者类型列表
func (s *StorageProviderServer) GetStorageProviderTypes(ctx context.Context, req *native.GetStorageProviderTypesReq) (*native.BaseResp, error) {
	if err := service.RequireAdmin(ctx); err != nil {
		return response.ErrResp(err)
	}
	data, err := service.GetStorageProviderTypes(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}

	return response.SuccessResp(data)
}

// AddStorageProvider 添加存储提供者
func (s *StorageProviderServer) AddStorageProvider(ctx context.Context, req *native.AddStorageProviderReq) (*native.BaseResp, error) {
	if err := service.RequireAdmin(ctx); err != nil {
		return response.ErrResp(err)
	}
	err := service.AddStorageProvider(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}

	return response.SuccessResp(nil)
}

// GetStorageProvider 获取存储提供者详情
func (s *StorageProviderServer) GetStorageProvider(ctx context.Context, req *native.StorageProviderGetReq) (*native.BaseResp, error) {
	if err := service.RequireAdmin(ctx); err != nil {
		return response.ErrResp(err)
	}
	data, err := service.GetStorageProvider(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}
	pb, ce := data.ToPB(ctx)
	if ce != nil {
		llog.C(ctx).Errorf("GetStorageProviders ToPB, id: %d, err: %v", req.GetId(), err)
		return response.ErrResp(&models.Err{E: ce, C: native.RespCode_INTERNAL_ERROR})
	}

	return response.SuccessResp(pb)
}

// ListStorageProviders 获取存储提供者列表
func (s *StorageProviderServer) ListStorageProviders(ctx context.Context, req *emptypb.Empty) (*native.BaseResp, error) {
	if err := service.RequireAdmin(ctx); err != nil {
		return response.ErrResp(err)
	}
	data, err := service.ListStorageProviders(ctx)
	if err != nil {
		return response.ErrResp(err)
	}
	items := make([]*native.StorageProvider, 0, len(data))

	// 构建返回结果
	for _, p := range data {
		pb, err := p.ToPB(ctx)
		if err != nil {
			llog.C(ctx).Errorf("ListStorageProviders ToPB, err: %v", err)
			return response.ErrResp(&models.Err{E: err, C: native.RespCode_INTERNAL_ERROR})
		}

		items = append(items, pb)
	}
	return response.SuccessResp(&native.StorageProviderListRespData{Providers: items})
}

// UpdateStorageProvider 更新存储提供者
func (s *StorageProviderServer) UpdateStorageProvider(ctx context.Context, req *native.UpdateStorageProviderReq) (*native.BaseResp, error) {
	if err := service.RequireAdmin(ctx); err != nil {
		return response.ErrResp(err)
	}
	llog.C(ctx).Infof("UpdateStorageProvider, req: %v", req)
	err := service.UpdateStorageProvider(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}

	return response.SuccessResp(nil)
}

// DeleteStorageProvider 删除存储提供者
func (s *StorageProviderServer) DeleteStorageProvider(ctx context.Context, req *native.StorageProviderDeleteReq) (*native.BaseResp, error) {
	if err := service.RequireAdmin(ctx); err != nil {
		return response.ErrResp(err)
	}
	err := service.DeleteStorageProvider(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}

	return response.SuccessResp(nil)
}
