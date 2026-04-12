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
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// GetFileList 获取文件列表
func (s *FileServer) GetFileList(ctx context.Context, req *connect.Request[native.FileListReq]) (*connect.Response[native.BaseResp], error) {
	files, total, hasMore, err := service.GetFileList(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	var result []*native.File
	for _, f := range files {
		pb, err := f.ToPB(ctx)
		if err != nil {
			llog.L().Errorf("convert file to pb failed, err: %v", err)
			total--
			continue
		}
		result = append(result, &pb)
	}
	tc := uint32(total)
	return response.SuccessResp(&native.FileListRespData{
		Files:   result,
		Total:   &tc,
		HasMore: &hasMore,
	})
}

// GetFolderList 获取文件夹列表
func (s *FileServer) GetFolderList(ctx context.Context, req *connect.Request[native.FolderListReq]) (*connect.Response[native.BaseResp], error) {
	folders, mErr := service.GetFolderList(ctx, req.Msg)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	var result []*native.File
	for _, f := range folders {
		pb, err := f.ToPB(ctx)
		if err != nil {
			llog.L().Errorf("convert folder to pb failed, err: %v", err)
			continue
		}
		result = append(result, &pb)
	}
	return response.SuccessResp(&native.FolderListRespData{
		Folders: result,
	})
}

// UpdateDir 更新文件夹
func (s *FileServer) UpdateDir(ctx context.Context, req *connect.Request[native.UpdateDirReq]) (*connect.Response[native.BaseResp], error) {
	err := service.UpdateDir(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}

	return response.SuccessResp(nil)
}

// DeleteFile 删除文件
func (s *FileServer) DeleteFile(ctx context.Context, req *connect.Request[native.DeleteFileReq]) (*connect.Response[native.BaseResp], error) {
	err := service.DeleteFile(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}

	return response.SuccessResp(nil)
}

// RenameFile 重命名文件
func (s *FileServer) RenameFile(ctx context.Context, req *connect.Request[native.RenameFileReq]) (*connect.Response[native.BaseResp], error) {
	err := service.RenameFile(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}

	return response.SuccessResp(nil)
}

// MoveFile 移动文件
func (s *FileServer) MoveFile(ctx context.Context, req *connect.Request[native.MoveFileReq]) (*connect.Response[native.BaseResp], error) {
	successCount, failedIDs, failedReasons, err := service.MoveFile(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}

	return response.SuccessResp(&native.MoveFileRespData{
		SuccessCount:  &successCount,
		FailedIds:     failedIDs,
		FailedReasons: failedReasons,
	})
}

// GetFileDetail 获取文件详情
func (s *FileServer) GetFileDetail(ctx context.Context, req *connect.Request[native.FileDetailReq]) (*connect.Response[native.BaseResp], error) {
	data, mErr := service.GetFileDetail(ctx, req.Msg)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(data)
}

// GetFileCacheStatus 查询文件缓存状态
func (s *FileServer) GetFileCacheStatus(ctx context.Context, req *connect.Request[native.GetFileCacheStatusReq]) (*connect.Response[native.BaseResp], error) {
	result, err := service.GetFileCacheStatus(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(result)
}

// GetAvailableProvidersForSync 获取可用于同步的存储提供者
func (s *FileServer) GetAvailableProvidersForSync(ctx context.Context, req *connect.Request[native.GetAvailableProvidersReq]) (*connect.Response[native.BaseResp], error) {
	providers, err := service.GetAvailableProvidersForSync(ctx, *req.Msg.FileHash)
	if err != nil {
		return response.ErrResp(err)
	}

	return response.SuccessResp(&native.GetAvailableProvidersResp{
		Providers: providers,
	})
}

// SyncFileToProviders 同步文件到指定存储提供者
func (s *FileServer) SyncFileToProviders(ctx context.Context, req *connect.Request[native.SyncFileToProvidersReq]) (*connect.Response[native.BaseResp], error) {
	err := service.SyncFileToProviders(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}

	return response.SuccessResp(nil)
}

// CreateShareLink 创建分享链接
func (s *FileServer) CreateShareLink(ctx context.Context, req *connect.Request[native.CreateShareLinkReq]) (*connect.Response[native.BaseResp], error) {
	data, mErr := service.CreateShareLink(ctx, req.Msg)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(data)
}

// CancelShareLink 取消分享链接
func (s *FileServer) CancelShareLink(ctx context.Context, req *connect.Request[native.CancelShareLinkReq]) (*connect.Response[native.BaseResp], error) {
	mErr := service.CancelShareLink(ctx, req.Msg)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(nil)
}
