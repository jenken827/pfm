package api

import (
	"context"

	"github.com/jenken827/pfm-proto/proto/gen/native"
	"google.golang.org/protobuf/types/known/emptypb"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/server/response"
)

// UploadServer 上传服务
type UploadServer struct{}

// UploadSmall 小文件上传（< 10MB）
func (s *UploadServer) UploadSmall(ctx context.Context, req *native.UploadSmallReq) (*native.BaseResp, error) {
	result, err := service.UploadSmall(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(result)
}

// InitUpload 初始化上传任务
func (s *UploadServer) InitUpload(ctx context.Context, req *native.InitUploadReq) (*native.BaseResp, error) {

	result, err := service.InitUpload(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(result)
}

// UploadChunk 上传分片（非流式）
func (s *UploadServer) UploadChunk(ctx context.Context, req *native.UploadChunkReq) (*native.BaseResp, error) {
	err := service.UploadChunkSingle(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// CompleteUpload 完成上传
func (s *UploadServer) CompleteUpload(ctx context.Context, req *native.CompleteUploadReq) (*native.BaseResp, error) {
	err := service.CompleteUpload(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// CancelUpload 取消上传
func (s *UploadServer) CancelUpload(ctx context.Context, req *native.CancelUploadReq) (*native.BaseResp, error) {
	err := service.CancelUpload(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// ListUploadRecords 获取上传记录列表
func (s *UploadServer) ListUploadRecords(ctx context.Context, req *native.ListUploadRecordsReq) (*native.BaseResp, error) {
	result, err := service.ListUploadRecords(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(result)
}

// GetUploadRecordDetail 获取上传记录详情
func (s *UploadServer) GetUploadRecordDetail(ctx context.Context, req *native.GetUploadRecordDetailReq) (*native.BaseResp, error) {
	result, err := service.GetUploadRecordDetail(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(result)
}

// RetryUploadRecord 重试上传记录
func (s *UploadServer) RetryUploadRecord(ctx context.Context, req *native.RetryUploadRecordReq) (*native.BaseResp, error) {
	err := service.RetryUploadRecord(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// RetryAllFailedUploadRecords 重试所有失败的上传记录
func (s *UploadServer) RetryAllFailedUploadRecords(ctx context.Context, req *emptypb.Empty) (*native.BaseResp, error) {
	err := service.RetryAllFailedUploadRecords(ctx)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// DeleteUploadRecord 删除上传记录
func (s *UploadServer) DeleteUploadRecord(ctx context.Context, req *native.DeleteUploadRecordReq) (*native.BaseResp, error) {
	err := service.DeleteUploadRecord(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}
