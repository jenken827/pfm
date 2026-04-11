package api

import (
	"context"

	"connectrpc.com/connect"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/proto/gen/native/nativeconnect"
	"jenken827.com/jenken827/pfm/server/response"
)

// UploadServer 上传服务
type UploadServer struct{}

var _ = (nativeconnect.UploadServiceHandler)((*UploadServer)(nil))

// UploadSmall 小文件上传（< 10MB）
func (s *UploadServer) UploadSmall(ctx context.Context, req *connect.Request[native.UploadSmallReq]) (*connect.Response[native.BaseResp], error) {
	result, err := service.UploadSmall(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(result)
}

// InitUpload 初始化上传任务
func (s *UploadServer) InitUpload(ctx context.Context, req *connect.Request[native.InitUploadReq]) (*connect.Response[native.BaseResp], error) {

	result, err := service.InitUpload(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(result)
}

// UploadChunk 上传分片（非流式）
func (s *UploadServer) UploadChunk(ctx context.Context, req *connect.Request[native.UploadChunkReq]) (*connect.Response[native.BaseResp], error) {
	err := service.UploadChunkSingle(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// CompleteUpload 完成上传
func (s *UploadServer) CompleteUpload(ctx context.Context, req *connect.Request[native.CompleteUploadReq]) (*connect.Response[native.BaseResp], error) {
	err := service.CompleteUpload(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// CancelUpload 取消上传
func (s *UploadServer) CancelUpload(ctx context.Context, req *connect.Request[native.CancelUploadReq]) (*connect.Response[native.BaseResp], error) {
	err := service.CancelUpload(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// ListUploadRecords 获取上传记录列表
func (s *UploadServer) ListUploadRecords(ctx context.Context, req *connect.Request[native.ListUploadRecordsReq]) (*connect.Response[native.BaseResp], error) {
	result, err := service.ListUploadRecords(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(result)
}

// GetUploadRecordDetail 获取上传记录详情
func (s *UploadServer) GetUploadRecordDetail(ctx context.Context, req *connect.Request[native.GetUploadRecordDetailReq]) (*connect.Response[native.BaseResp], error) {
	result, err := service.GetUploadRecordDetail(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(result)
}
