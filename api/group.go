package api

import (
	"context"

	"connectrpc.com/connect"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/proto/gen/native/nativeconnect"
	"jenken827.com/jenken827/pfm/server/response"
)

// StorageGroupServer 存储组服务
type StorageGroupServer struct{}

var _ = (nativeconnect.StorageGroupServiceHandler)((*StorageGroupServer)(nil))

// AddStorageGroup 创建存储组
func (s *StorageGroupServer) AddStorageGroup(ctx context.Context, req *connect.Request[native.StorageGroupAddReq]) (*connect.Response[native.BaseResp], error) {
	group, err := service.AddStorageGroup(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}

	return response.SuccessResp(&native.StorageGroupAddRespData{
		Id: group.Id,
	})
}

// GetStorageGroup 获取存储组详情
func (s *StorageGroupServer) GetStorageGroup(ctx context.Context, req *connect.Request[native.StorageGroupGetReq]) (*connect.Response[native.BaseResp], error) {
	data, err := service.GetStorageGroup(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}

	return response.SuccessResp(data)
}

// ListStorageGroups 获取存储组列表
func (s *StorageGroupServer) ListStorageGroups(ctx context.Context, req *connect.Request[native.StorageGroupListReq]) (*connect.Response[native.BaseResp], error) {
	data, err := service.ListStorageGroups(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}

	return response.SuccessResp(data)
}

// UpdateStorageGroup 更新存储组
func (s *StorageGroupServer) UpdateStorageGroup(ctx context.Context, req *connect.Request[native.StorageGroupUpdateReq]) (*connect.Response[native.BaseResp], error) {
	err := service.UpdateStorageGroup(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}

	return response.SuccessResp(nil)
}

// DeleteStorageGroup 删除存储组
func (s *StorageGroupServer) DeleteStorageGroup(ctx context.Context, req *connect.Request[native.StorageGroupDeleteReq]) (*connect.Response[native.BaseResp], error) {
	err := service.DeleteStorageGroup(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}

	return response.SuccessResp(nil)
}
