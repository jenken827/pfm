package api

import (
	"context"

	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/internal/config"
	"github.com/jenken827/pfm/internal/service"
	"github.com/jenken827/pfm/pkg/llog"
	"github.com/jenken827/pfm/server/response"
)

// FileServer 文件服务
type FileServer struct{}

// CreateDir 创建文件夹
func (s *FileServer) CreateDir(ctx context.Context, req *native.CreateDirReq) (*native.BaseResp, error) {
	dirId, err := service.CreateDir(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}
	// 返回新创建的目录 ID
	resp := &native.CreateDirRespData{Id: &dirId}
	return response.SuccessResp(resp)
}

// GetFileList 获取文件列表
func (s *FileServer) GetFileList(ctx context.Context, req *native.FileListReq) (*native.BaseResp, error) {
	files, total, hasMore, err := service.GetFileList(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}
	var result []*native.File
	for _, f := range files {
		pb, err := f.ToPB(ctx)
		if err != nil {
			llog.C(ctx).Errorf("convert file to pb failed, err: %v", err)
			total--
			continue
		}
		result = append(result, pb)
	}
	tc := uint32(total)
	return response.SuccessResp(&native.FileListRespData{
		Files:   result,
		Total:   &tc,
		HasMore: &hasMore,
	})
}

// GetFolderList 获取文件夹列表
func (s *FileServer) GetFolderList(ctx context.Context, req *native.FolderListReq) (*native.BaseResp, error) {
	folders, mErr := service.GetFolderList(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	var result []*native.File
	for _, f := range folders {
		pb, err := f.ToPB(ctx)
		if err != nil {
			llog.C(ctx).Errorf("convert folder to pb failed, err: %v", err)
			continue
		}
		result = append(result, pb)
	}
	return response.SuccessResp(&native.FolderListRespData{
		Folders: result,
	})
}

// UpdateDir 更新文件夹
func (s *FileServer) UpdateDir(ctx context.Context, req *native.UpdateDirReq) (*native.BaseResp, error) {
	err := service.UpdateDir(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}

	return response.SuccessResp(nil)
}

// DeleteFile 删除文件
func (s *FileServer) DeleteFile(ctx context.Context, req *native.DeleteFileReq) (*native.BaseResp, error) {
	err := service.DeleteFile(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}

	return response.SuccessResp(nil)
}

// RenameFile 重命名文件
func (s *FileServer) RenameFile(ctx context.Context, req *native.RenameFileReq) (*native.BaseResp, error) {
	err := service.RenameFile(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}

	return response.SuccessResp(nil)
}

// MoveFile 移动文件
func (s *FileServer) MoveFile(ctx context.Context, req *native.MoveFileReq) (*native.BaseResp, error) {
	successCount, failedIDs, failedReasons, err := service.MoveFile(ctx, req)
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
func (s *FileServer) GetFileDetail(ctx context.Context, req *native.FileDetailReq) (*native.BaseResp, error) {
	data, mErr := service.GetFileDetail(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(data)
}

// GetFileInfo 获取文件基本信息
func (s *FileServer) GetFileInfo(ctx context.Context, req *native.GetFileInfoReq) (*native.BaseResp, error) {
	data, mErr := service.GetFileInfo(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(data)
}

// GetFileCacheStatus 查询文件缓存状态
func (s *FileServer) GetFileCacheStatus(ctx context.Context, req *native.GetFileCacheStatusReq) (*native.BaseResp, error) {
	result, err := service.GetFileCacheStatus(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(result)
}

// GetAvailableProvidersForSync 获取可用于同步的存储提供者
func (s *FileServer) GetAvailableProvidersForSync(ctx context.Context, req *native.GetAvailableProvidersReq) (*native.BaseResp, error) {
	providers, err := service.GetAvailableProvidersForSync(ctx, *req.FileHash)
	if err != nil {
		return response.ErrResp(err)
	}

	return response.SuccessResp(&native.GetAvailableProvidersResp{
		Providers: providers,
	})
}

// SyncFileToProviders 同步文件到指定存储提供者
func (s *FileServer) SyncFileToProviders(ctx context.Context, req *native.SyncFileToProvidersReq) (*native.BaseResp, error) {
	err := service.SyncFileToProviders(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}

	return response.SuccessResp(nil)
}

// CreateShareLink 创建分享链接
func (s *FileServer) CreateShareLink(ctx context.Context, req *native.CreateShareLinkReq) (*native.BaseResp, error) {
	data, mErr := service.CreateShareLink(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(data)
}

// CancelShareLink 取消分享链接
func (s *FileServer) CancelShareLink(ctx context.Context, req *native.CancelShareLinkReq) (*native.BaseResp, error) {
	mErr := service.CancelShareLink(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(nil)
}

// ListMyShares 获取我的分享列表
func (s *FileServer) ListMyShares(ctx context.Context, req *native.ListMySharesReq) (*native.BaseResp, error) {
	data, mErr := service.ListMyShares(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(data)
}

// UpdateShareLink 更新分享链接
func (s *FileServer) UpdateShareLink(ctx context.Context, req *native.UpdateShareLinkReq) (*native.BaseResp, error) {
	data, mErr := service.UpdateShareLink(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(data)
}

// RestoreFromTrash 从回收站恢复文件
func (s *FileServer) RestoreFromTrash(ctx context.Context, req *native.RestoreFromTrashReq) (*native.BaseResp, error) {
	mErr := service.RestoreFromTrash(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(nil)
}

// DeletePermanently 彻底删除文件
func (s *FileServer) DeletePermanently(ctx context.Context, req *native.DeletePermanentlyReq) (*native.BaseResp, error) {
	mErr := service.DeletePermanently(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(nil)
}

// EmptyTrash 清空回收站
func (s *FileServer) EmptyTrash(ctx context.Context, req *native.EmptyTrashReq) (*native.BaseResp, error) {
	mErr := service.EmptyTrash(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(nil)
}

// DeleteFileHistory 删除文件历史版本
func (s *FileServer) DeleteFileHistory(ctx context.Context, req *native.DeleteFileHistoryReq) (*native.BaseResp, error) {
	mErr := service.DeleteFileHistory(ctx, req.Ids)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(nil)
}

// RestoreFileHistory 恢复文件历史版本
func (s *FileServer) RestoreFileHistory(ctx context.Context, req *native.RestoreFileHistoryReq) (*native.BaseResp, error) {
	mErr := service.RestoreFileHistory(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(nil)
}

// GetTrashList 获取回收站文件列表
func (s *FileServer) GetTrashList(ctx context.Context, req *native.BaseResp) (*native.BaseResp, error) {
	data, mErr := service.GetTrashList(ctx)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(data)
}

// SearchFiles 搜索文件
func (s *FileServer) SearchFiles(ctx context.Context, req *native.SearchFilesReq) (*native.BaseResp, error) {
	files, total, hasMore, mErr := service.SearchFiles(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	var result []*native.File
	for _, f := range files {
		pb, err := f.ToPB(ctx)
		if err != nil {
			llog.C(ctx).Errorf("convert file to pb failed, err: %v", err)
			continue
		}
		result = append(result, pb)
	}
	tc := uint32(total)
	return response.SuccessResp(&native.SearchFilesRespData{
		Files:   result,
		Total:   &tc,
		HasMore: &hasMore,
	})
}

// UpdateFileTags 更新文件标签
func (s *FileServer) UpdateFileTags(ctx context.Context, req *native.UpdateFileTagsReq) (*native.BaseResp, error) {
	mErr := service.UpdateFileTags(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(nil)
}

// GetFileTags 获取文件标签
func (s *FileServer) GetFileTags(ctx context.Context, req *native.GetFileTagsReq) (*native.BaseResp, error) {
	resp, mErr := service.GetFileTags(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(resp)
}

// GetRecentFiles 获取最近使用的文件
func (s *FileServer) GetRecentFiles(ctx context.Context, req *native.GetRecentFilesReq) (*native.BaseResp, error) {
	files, total, hasMore, mErr := service.GetRecentFiles(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	var result []*native.File
	for _, f := range files {
		pb, err := f.ToPB(ctx)
		if err != nil {
			llog.C(ctx).Errorf("convert file to pb failed, err: %v", err)
			continue
		}
		result = append(result, pb)
	}
	tc := uint32(total)
	return response.SuccessResp(&native.GetRecentFilesRespData{
		Files:   result,
		Total:   &tc,
		HasMore: &hasMore,
	})
}

// GetFilesByType 按文件类型获取文件列表
func (s *FileServer) GetFilesByType(ctx context.Context, req *native.GetFilesByTypeReq) (*native.BaseResp, error) {
	files, total, hasMore, mErr := service.GetFilesByType(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	var result []*native.File
	for _, f := range files {
		pb, err := f.ToPB(ctx)
		if err != nil {
			llog.C(ctx).Errorf("convert file to pb failed, err: %v", err)
			continue
		}
		result = append(result, pb)
	}
	tc := uint32(total)
	return response.SuccessResp(&native.GetFilesByTypeRespData{
		Files:   result,
		Total:   &tc,
		HasMore: &hasMore,
	})
}

// GetTransferConfig 获取传输服务配置（上传/下载高带宽地址）
func (s *FileServer) GetTransferConfig(ctx context.Context, req *native.GetTransferConfigReq) (*native.BaseResp, error) {
	serverURL := config.GetConfig().Transfer.ServerURL
	return response.SuccessResp(&native.GetTransferConfigResp{
		ServerUrl: &serverURL,
	})
}

// GetPhotoDateConfig 获取照片日期解析配置（相册备份按年/月归档用）
func (s *FileServer) GetPhotoDateConfig(ctx context.Context, req *native.GetPhotoDateConfigReq) (*native.BaseResp, error) {
	return response.SuccessResp(service.GetPhotoDateConfig())
}

// GetOrCreateSubfolder 相册备份：获取有空位的编号子目录（都满则新建）
func (s *FileServer) GetOrCreateSubfolder(ctx context.Context, req *native.GetOrCreateSubfolderReq) (*native.BaseResp, error) {
	resp, err := service.GetOrCreateSubfolder(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(resp)
}

// EnsureDirPath 相册备份：按相对路径逐级创建目录（mkdir -p 语义）
func (s *FileServer) EnsureDirPath(ctx context.Context, req *native.EnsureDirPathReq) (*native.BaseResp, error) {
	resp, err := service.EnsureDirPath(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(resp)
}

// GetFileDirectLink 获取文件直链（满足条件时客户端可绕开服务端中转直连提供者）
func (s *FileServer) GetFileDirectLink(ctx context.Context, req *native.DirectLinkFileReq) (*native.BaseResp, error) {
	data, mErr := service.GetFileDirectLink(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(data)
}

// CreatePreviewTicket 签发单文件短期预览票据（替代把 access token 写进预览 URL）
func (s *FileServer) CreatePreviewTicket(ctx context.Context, req *native.CreatePreviewTicketReq) (*native.BaseResp, error) {
	data, mErr := service.CreatePreviewTicket(ctx, req)
	if mErr != nil {
		return response.ErrResp(mErr)
	}
	return response.SuccessResp(data)
}
