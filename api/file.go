package api

import (
	"context"

	"connectrpc.com/connect"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/proto/gen/native/nativeconnect"
	"jenken827.com/jenken827/pfm/server/response"
)

// FileServer 文件服务
type FileServer struct{}

var _ = (nativeconnect.FileServiceHandler)((*FileServer)(nil))

// CreateDir 创建文件夹
func (s *FileServer) CreateDir(ctx context.Context, req *connect.Request[native.CreateDirReq]) (*connect.Response[native.BaseResp], error) {
	err := service.CreateDir(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}

	return response.SuccessResp(nil)
}

// GetFileList 获取文件列表
func (s *FileServer) GetFileList(ctx context.Context, req *connect.Request[native.FileListReq]) (*connect.Response[native.BaseResp], error) {
	files, total, err := service.GetFileList(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}

	return response.SuccessResp(&native.FileListRespData{
		Files: files,
		Total: &total,
	})
}

// UpdateDir 更新文件夹
func (s *FileServer) UpdateDir(ctx context.Context, req *connect.Request[native.UpdateDirReq]) (*connect.Response[native.BaseResp], error) {
	err := service.UpdateDir(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}

	return response.SuccessResp(nil)
}

// DeleteFile 删除文件
func (s *FileServer) DeleteFile(ctx context.Context, req *connect.Request[native.DeleteFileReq]) (*connect.Response[native.BaseResp], error) {
	err := service.DeleteFile(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}

	return response.SuccessResp(nil)
}

// RenameFile 重命名文件
func (s *FileServer) RenameFile(ctx context.Context, req *connect.Request[native.RenameFileReq]) (*connect.Response[native.BaseResp], error) {
	err := service.RenameFile(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}

	return response.SuccessResp(nil)
}

// InitUpload 初始化上传任务
func (s *FileServer) InitUpload(ctx context.Context, req *connect.Request[native.InitUploadReq]) (*connect.Response[native.BaseResp], error) {
	llog.GetLogger().Info("InitUpload request", *req.Msg.Hash)
	result, err := service.InitUpload(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}
	return response.SuccessResp(result)
}

// UploadChunk 上传分片（非流式）
func (s *FileServer) UploadChunk(ctx context.Context, req *connect.Request[native.UploadChunkReq]) (*connect.Response[native.BaseResp], error) {
	err := service.UploadChunkSingle(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}
	return response.SuccessResp(nil)
}

// CompleteUpload 完成上传
func (s *FileServer) CompleteUpload(ctx context.Context, req *connect.Request[native.CompleteUploadReq]) (*connect.Response[native.BaseResp], error) {
	err := service.CompleteUpload(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}
	return response.SuccessResp(nil)
}

// GetUploadProgress 获取上传进度
func (s *FileServer) GetUploadProgress(ctx context.Context, req *connect.Request[native.UploadProgressReq]) (*connect.Response[native.BaseResp], error) {
	result, err := service.GetUploadProgress(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}
	return response.SuccessResp(result)
}

// CancelUpload 取消上传
func (s *FileServer) CancelUpload(ctx context.Context, req *connect.Request[native.CancelUploadReq]) (*connect.Response[native.BaseResp], error) {
	err := service.CancelUpload(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err.Code, err.E), nil
	}
	return response.SuccessResp(nil)
}
