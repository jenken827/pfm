package api

import (
	"context"

	"connectrpc.com/connect"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/proto/gen/native/nativeconnect"
	"jenken827.com/jenken827/pfm/server/response"
)

// BackupServer 备份服务
type BackupServer struct{}

var _ = (nativeconnect.BackupServiceHandler)((*BackupServer)(nil))

// CreateBackupTask 创建备份任务
func (s *BackupServer) CreateBackupTask(ctx context.Context, req *connect.Request[native.CreateBackupTaskReq]) (*connect.Response[native.BaseResp], error) {
	if err := service.CreateBackupTask(ctx, req.Msg); err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// UpdateBackupTask 更新备份任务
func (s *BackupServer) UpdateBackupTask(ctx context.Context, req *connect.Request[native.UpdateBackupTaskReq]) (*connect.Response[native.BaseResp], error) {
	if err := service.UpdateBackupTask(ctx, req.Msg); err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// DeleteBackupTask 删除备份任务
func (s *BackupServer) DeleteBackupTask(ctx context.Context, req *connect.Request[native.DeleteBackupTaskReq]) (*connect.Response[native.BaseResp], error) {
	if err := service.DeleteBackupTask(ctx, req.Msg); err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// ListBackupTasks 获取备份任务列表
func (s *BackupServer) ListBackupTasks(ctx context.Context, req *connect.Request[native.ListBackupTasksReq]) (*connect.Response[native.BaseResp], error) {
	data, err := service.ListBackupTasks(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(data)
}

// GetBackupTask 获取备份任务详情
func (s *BackupServer) GetBackupTask(ctx context.Context, req *connect.Request[native.GetBackupTaskReq]) (*connect.Response[native.BaseResp], error) {
	data, err := service.GetBackupTask(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(data)
}

// TriggerBackup 手动触发备份
func (s *BackupServer) TriggerBackup(ctx context.Context, req *connect.Request[native.TriggerBackupReq]) (*connect.Response[native.BaseResp], error) {
	if err := service.TriggerBackup(ctx, req.Msg); err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// ListBackupFiles 获取备份文件记录
func (s *BackupServer) ListBackupFiles(ctx context.Context, req *connect.Request[native.ListBackupFilesReq]) (*connect.Response[native.BaseResp], error) {
	data, err := service.ListBackupFiles(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(data)
}
