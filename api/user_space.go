package api

import (
	"context"

	"connectrpc.com/connect"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/proto/gen/native/nativeconnect"
	"jenken827.com/jenken827/pfm/server/response"
	"google.golang.org/protobuf/types/known/emptypb"
)

// UserSpaceServer 用户空间服务
type UserSpaceServer struct{}

var _ = (nativeconnect.UserSpaceServiceHandler)((*UserSpaceServer)(nil))

// GetUserSpace 获取当前用户的空间信息
func (s *UserSpaceServer) GetUserSpace(ctx context.Context, req *connect.Request[emptypb.Empty]) (*connect.Response[native.BaseResp], error) {
	space, err := service.GetUserSpace(ctx)
	if err != nil {
		return response.ErrResp(&models.Err{E: err, C: native.RespCode_PARAMS_ERROR})
	}
	return response.SuccessResp(&native.GetUserSpaceResp{Space: space})
}

// SaveShareToMySpace 保存分享到我的网盘
func (s *UserSpaceServer) SaveShareToMySpace(ctx context.Context, req *connect.Request[native.SaveShareToMySpaceReq]) (*connect.Response[native.BaseResp], error) {
	taskID, err := service.SaveShareToMySpace(
		ctx,
		req.Msg.GetShareCode(),
		req.Msg.GetTargetParentId(),
		req.Msg.GetDisplayName(),
	)
	if err != nil {
		return response.ErrResp(&models.Err{E: err, C: native.RespCode_PARAMS_ERROR})
	}
	return response.SuccessResp(&native.AsyncTaskProgressResp{
		TaskId: &taskID,
		Status: strPtr("PENDING"),
	})
}

// GetAsyncTaskProgress 查询异步任务进度
func (s *UserSpaceServer) GetAsyncTaskProgress(ctx context.Context, req *connect.Request[native.AsyncTaskProgressReq]) (*connect.Response[native.BaseResp], error) {
	task, err := service.GetAsyncTaskProgress(req.Msg.GetTaskId())
	if err != nil {
		return response.ErrResp(&models.Err{E: err, C: native.RespCode_NOT_FOUND})
	}
	total := uint32(task.Total)
	completed := uint32(task.Completed)
	return response.SuccessResp(&native.AsyncTaskProgressResp{
		TaskId:    &task.TaskID,
		Status:    strPtr(string(task.Status)),
		Total:     &total,
		Completed: &completed,
		Error:     strPtr(task.Error),
	})
}

// ListUsers 获取用户列表
func (s *UserSpaceServer) ListUsers(ctx context.Context, req *connect.Request[native.ListUsersReq]) (*connect.Response[native.BaseResp], error) {
	resp, err := service.ListUsers(ctx)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(resp)
}

// GetMyRoles 获取当前用户角色
func (s *UserSpaceServer) GetMyRoles(ctx context.Context, req *connect.Request[emptypb.Empty]) (*connect.Response[native.BaseResp], error) {
	resp, err := service.GetMyRoles(ctx)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(resp)
}

// ListUserSpaces 管理员：列出所有用户空间
func (s *UserSpaceServer) ListUserSpaces(ctx context.Context, req *connect.Request[native.ListUserSpacesReq]) (*connect.Response[native.BaseResp], error) {
	resp, err := service.ListUserSpaces(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(resp)
}

// UpdateUserSpace 管理员：更新用户空间配额
func (s *UserSpaceServer) UpdateUserSpace(ctx context.Context, req *connect.Request[native.UpdateUserSpaceReq]) (*connect.Response[native.BaseResp], error) {
	if err := service.UpdateUserSpace(ctx, req.Msg); err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(nil)
}

// AssignRootDir 管理员：为用户分配根目录
func (s *UserSpaceServer) AssignRootDir(ctx context.Context, req *connect.Request[native.AssignRootDirReq]) (*connect.Response[native.BaseResp], error) {
	resp, err := service.AssignRootDir(ctx, req.Msg)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(resp)
}

func strPtr(s string) *string { return &s }
