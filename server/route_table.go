package server

import (
	"context"
	"net/http"

	"jenken827.com/jenken827/pfm/api"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"google.golang.org/protobuf/types/known/emptypb"
)

// 本文件是 REST 路由表：显式维护「HTTP 方法 + 路径 → api 层方法」的映射。
//
// 约定：
//   - 路径以 /api/ 开头，风格为 /api/<域>/<动作>（与既有手写接口保持一致）。
//   - 请求体为 proto3 JSON；GET 请求从 query 参数解析。
//   - 此处不做任何业务逻辑，只做转发；业务实现全部在 api/ 与 internal/service/。
//
// 新增接口时：在 api/ 层加方法，然后在此处注册一行。

// 各服务的单例。api 层方法无状态，可安全共享。
var (
	fileServer     = &api.FileServer{}
	uploadServer   = &api.UploadServer{}
	providerServer = &api.StorageProviderServer{}
	backupServer   = &api.BackupServer{}
	spaceServer    = &api.UserSpaceServer{}
	dlServer       = &api.DownloadServer{}
)

// registerRESTServices 注册全部业务路由。
func registerRESTServices(mux *http.ServeMux) {
	registerFileRoutes(mux)
	registerUploadRoutes(mux)
	registerProviderRoutes(mux)
	registerBackupRoutes(mux)
	registerUserSpaceRoutes(mux)
	registerDownloadRoutes(mux)
}

// registerFileRoutes 文件服务路由
func registerFileRoutes(mux *http.ServeMux) {
	Register(mux, http.MethodPost, "/api/file/list", fileServer.GetFileList)
	Register(mux, http.MethodPost, "/api/file/folders", fileServer.GetFolderList)
	Register(mux, http.MethodPost, "/api/file/detail", fileServer.GetFileDetail)
	Register(mux, http.MethodPost, "/api/file/info", fileServer.GetFileInfo)
	Register(mux, http.MethodPost, "/api/file/create-dir", fileServer.CreateDir)
	Register(mux, http.MethodPost, "/api/file/update-dir", fileServer.UpdateDir)
	Register(mux, http.MethodPost, "/api/file/delete", fileServer.DeleteFile)
	Register(mux, http.MethodPost, "/api/file/rename", fileServer.RenameFile)
	Register(mux, http.MethodPost, "/api/file/move", fileServer.MoveFile)
	Register(mux, http.MethodPost, "/api/file/cache-status", fileServer.GetFileCacheStatus)
	Register(mux, http.MethodPost, "/api/file/sync-providers", fileServer.GetAvailableProvidersForSync)
	Register(mux, http.MethodPost, "/api/file/sync", fileServer.SyncFileToProviders)
	Register(mux, http.MethodPost, "/api/file/share/create", fileServer.CreateShareLink)
	Register(mux, http.MethodPost, "/api/file/share/cancel", fileServer.CancelShareLink)
	Register(mux, http.MethodPost, "/api/file/share/list", fileServer.ListMyShares)
	Register(mux, http.MethodPost, "/api/file/share/update", fileServer.UpdateShareLink)
	Register(mux, http.MethodPost, "/api/file/trash/restore", fileServer.RestoreFromTrash)
	Register(mux, http.MethodPost, "/api/file/trash/delete-permanently", fileServer.DeletePermanently)
	Register(mux, http.MethodPost, "/api/file/trash/empty", fileServer.EmptyTrash)
	Register(mux, http.MethodPost, "/api/file/trash/list", fileServer.GetTrashList)
	Register(mux, http.MethodPost, "/api/file/history/delete", fileServer.DeleteFileHistory)
	Register(mux, http.MethodPost, "/api/file/history/restore", fileServer.RestoreFileHistory)
	Register(mux, http.MethodPost, "/api/file/tags/update", fileServer.UpdateFileTags)
	Register(mux, http.MethodPost, "/api/file/tags/get", fileServer.GetFileTags)
	Register(mux, http.MethodPost, "/api/file/search", fileServer.SearchFiles)
	Register(mux, http.MethodPost, "/api/file/recent", fileServer.GetRecentFiles)
	Register(mux, http.MethodPost, "/api/file/by-type", fileServer.GetFilesByType)
	Register(mux, http.MethodPost, "/api/file/transfer-config", fileServer.GetTransferConfig)
	Register(mux, http.MethodPost, "/api/file/photo-date-config", fileServer.GetPhotoDateConfig)
	// 相册备份目录辅助（原为手写裸 HTTP，已纳入统一分发器）
	Register(mux, http.MethodPost, "/api/file/get-or-create-subfolder", fileServer.GetOrCreateSubfolder)
	Register(mux, http.MethodPost, "/api/file/ensure-dir-path", fileServer.EnsureDirPath)
}

// registerUploadRoutes 上传服务路由
func registerUploadRoutes(mux *http.ServeMux) {
	Register(mux, http.MethodPost, "/api/upload/small", uploadServer.UploadSmall)
	Register(mux, http.MethodPost, "/api/upload/init", uploadServer.InitUpload)
	Register(mux, http.MethodPost, "/api/upload/chunk", uploadServer.UploadChunk)
	Register(mux, http.MethodPost, "/api/upload/complete", uploadServer.CompleteUpload)
	Register(mux, http.MethodPost, "/api/upload/cancel", uploadServer.CancelUpload)
	Register(mux, http.MethodPost, "/api/upload/records/list", uploadServer.ListUploadRecords)
	Register(mux, http.MethodPost, "/api/upload/records/detail", uploadServer.GetUploadRecordDetail)
	Register(mux, http.MethodPost, "/api/upload/records/retry", uploadServer.RetryUploadRecord)
	Register(mux, http.MethodPost, "/api/upload/records/delete", uploadServer.DeleteUploadRecord)
	Register(mux, http.MethodPost, "/api/upload/records/retry-all", uploadServer.RetryAllFailedUploadRecords)
}

// registerProviderRoutes 存储提供者服务路由（全部管理员专属）
func registerProviderRoutes(mux *http.ServeMux) {
	Register(mux, http.MethodPost, "/api/admin/provider/types", providerServer.GetStorageProviderTypes)
	Register(mux, http.MethodPost, "/api/admin/provider/add", providerServer.AddStorageProvider)
	Register(mux, http.MethodPost, "/api/admin/provider/get", providerServer.GetStorageProvider)
	Register(mux, http.MethodPost, "/api/admin/provider/list", providerServer.ListStorageProviders)
	Register(mux, http.MethodPost, "/api/admin/provider/update", providerServer.UpdateStorageProvider)
	Register(mux, http.MethodPost, "/api/admin/provider/delete", providerServer.DeleteStorageProvider)
}

// registerBackupRoutes 备份服务路由（全部管理员专属）
//
// 说明：服务端备份的 source_path 是**服务端本地文件系统路径**（直接 os.Stat /
// 目录扫描），普通用户持有该能力等同于获得服务器任意目录的读权限，故整体收敛为
// 管理员专属：service 层每个方法以 RequireAdmin 拦截，路由挂 /api/admin/ 前缀。
func registerBackupRoutes(mux *http.ServeMux) {
	Register(mux, http.MethodPost, "/api/admin/backup/task/create", backupServer.CreateBackupTask)
	Register(mux, http.MethodPost, "/api/admin/backup/task/update", backupServer.UpdateBackupTask)
	Register(mux, http.MethodPost, "/api/admin/backup/task/delete", backupServer.DeleteBackupTask)
	Register(mux, http.MethodPost, "/api/admin/backup/task/list", backupServer.ListBackupTasks)
	Register(mux, http.MethodPost, "/api/admin/backup/task/get", backupServer.GetBackupTask)
	Register(mux, http.MethodPost, "/api/admin/backup/trigger", backupServer.TriggerBackup)
	Register(mux, http.MethodPost, "/api/admin/backup/files", backupServer.ListBackupFiles)
}

// registerUserSpaceRoutes 用户空间服务路由
// 说明：ListUsers/ListUserSpaces/UpdateUserSpace/AssignRootDir 为管理员专属（挂 admin 前缀）；
// GetUserSpace/SaveShareToMySpace/GetAsyncTaskProgress/GetMyRoles 为普通用户接口。
func registerUserSpaceRoutes(mux *http.ServeMux) {
	Register(mux, http.MethodPost, "/api/space/get", spaceServer.GetUserSpace)
	Register(mux, http.MethodPost, "/api/space/save-share", spaceServer.SaveShareToMySpace)
	Register(mux, http.MethodPost, "/api/space/async-task-progress", spaceServer.GetAsyncTaskProgress)
	Register(mux, http.MethodPost, "/api/space/my-roles", spaceServer.GetMyRoles)
	Register(mux, http.MethodPost, "/api/space/my-providers", spaceServer.GetMyBoundProviders)
	Register(mux, http.MethodPost, "/api/space/profile/update", spaceServer.UpdateProfile)
	Register(mux, http.MethodPost, "/api/admin/user/list", spaceServer.ListUsers)
	Register(mux, http.MethodPost, "/api/admin/user/spaces", spaceServer.ListUserSpaces)
	Register(mux, http.MethodPost, "/api/admin/user/update-space", spaceServer.UpdateUserSpace)
	Register(mux, http.MethodPost, "/api/admin/user/assign-root", spaceServer.AssignRootDir)
}

// registerDownloadRoutes 离线下载服务路由
func registerDownloadRoutes(mux *http.ServeMux) {
	Register(mux, http.MethodPost, "/api/download/offline/create", dlServer.CreateOfflineDownload)
	Register(mux, http.MethodPost, "/api/download/offline/list", dlServer.ListOfflineDownloads)
	Register(mux, http.MethodPost, "/api/download/offline/delete", dlServer.DeleteOfflineDownload)
	Register(mux, http.MethodPost, "/api/download/offline/retry", dlServer.RetryOfflineDownload)
}

// 编译期断言：确保 emptypb 与 native 被引用（请求类型涉及 Empty 与 native 消息）。
var (
	_ = emptypb.Empty{}
	_ = native.BaseResp{}
	_ context.Context
)
