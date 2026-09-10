package api

import (
	"context"

	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/server/response"
)

// BackupServer 备份服务
type BackupServer struct{}

// CreateBackupTask 创建备份任务
func (s *BackupServer) CreateBackupTask(ctx context.Context, req *native.CreateBackupTaskReq) (*native.BaseResp, error) {
	if err := service.CreateBackupTask(ctx, req); err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// UpdateBackupTask 更新备份任务
func (s *BackupServer) UpdateBackupTask(ctx context.Context, req *native.UpdateBackupTaskReq) (*native.BaseResp, error) {
	if err := service.UpdateBackupTask(ctx, req); err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// DeleteBackupTask 删除备份任务
func (s *BackupServer) DeleteBackupTask(ctx context.Context, req *native.DeleteBackupTaskReq) (*native.BaseResp, error) {
	if err := service.DeleteBackupTask(ctx, req); err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// ListBackupTasks 获取备份任务列表
func (s *BackupServer) ListBackupTasks(ctx context.Context, req *native.ListBackupTasksReq) (*native.BaseResp, error) {
	data, err := service.ListBackupTasks(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(data)
}

// GetBackupTask 获取备份任务详情
func (s *BackupServer) GetBackupTask(ctx context.Context, req *native.GetBackupTaskReq) (*native.BaseResp, error) {
	data, err := service.GetBackupTask(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(data)
}

// TriggerBackup 手动触发备份
func (s *BackupServer) TriggerBackup(ctx context.Context, req *native.TriggerBackupReq) (*native.BaseResp, error) {
	if err := service.TriggerBackup(ctx, req); err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// ListBackupFiles 获取备份文件记录
func (s *BackupServer) ListBackupFiles(ctx context.Context, req *native.ListBackupFilesReq) (*native.BaseResp, error) {
	data, err := service.ListBackupFiles(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(data)
}
