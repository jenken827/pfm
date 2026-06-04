# 后端公共方法索引

本文件记录后端各包的公共方法签名，供开发时快速查阅。每次新增或修改公共方法时必须同步更新此文件。

## internal/service/

### upload.go

| 方法 | 说明 |
|------|------|
| `UploadSmall(ctx, req) (*UploadSmallResp, *Err)` | 小文件上传 |
| `InitUpload(ctx, req) (*InitUploadResp, *Err)` | 初始化大文件分片上传 |
| `UploadChunk(ctx, req) (*UploadChunkResp, *Err)` | 上传单个分片 |
| `CompleteUpload(ctx, req) (*CompleteUploadResp, *Err)` | 完成分片上传，合并文件 |
| `ListUploadRecords(ctx, req) (*ListUploadRecordsResp, *Err)` | 查询上传记录列表（含失败计数） |
| `RetryUploadRecord(ctx, req) (*BaseResp, *Err)` | 重试单个上传记录 |
| `RetryAllFailedUploadRecords(ctx) *Err` | 一键重试所有失败的上传记录 |
| `DeleteUploadRecord(ctx, req) (*BaseResp, *Err)` | 删除上传记录 |
| `ClearCompletedUploadRecords(ctx, req) (*BaseResp, *Err)` | 清除已完成的上传记录 |

### file.go

| 方法 | 说明 |
|------|------|
| `GetFileList(ctx, req) (*GetFileListResp, *Err)` | 获取文件列表 |
| `CreateDirectory(ctx, req) (*CreateDirectoryResp, *Err)` | 创建目录 |
| `UpdateDirectory(ctx, req) (*BaseResp, *Err)` | 更新目录信息 |
| `DeleteFile(ctx, req) (*BaseResp, *Err)` | 删除文件（移入回收站） |
| `RestoreFile(ctx, req) (*BaseResp, *Err)` | 恢复回收站文件 |
| `PermanentDeleteFile(ctx, req) (*BaseResp, *Err)` | 永久删除文件 |
| `EmptyTrash(ctx, req) (*BaseResp, *Err)` | 清空回收站 |
| `RenameFile(ctx, req) (*BaseResp, *Err)` | 重命名文件 |
| `MoveFile(ctx, req) (*BaseResp, *Err)` | 移动文件 |
| `GetFileDetail(ctx, req) (*GetFileDetailResp, *Err)` | 获取文件详情 |

### provider.go

| 方法 | 说明 |
|------|------|
| `AddStorageProvider(ctx, req) (*AddStorageProviderResp, *Err)` | 添加存储提供者 |
| `UpdateStorageProvider(ctx, req) (*BaseResp, *Err)` | 更新存储提供者配置 |
| `DeleteStorageProvider(ctx, req) (*BaseResp, *Err)` | 删除存储提供者 |
| `ListStorageProviders(ctx, req) (*ListStorageProvidersResp, *Err)` | 列出存储提供者 |
| `GetStorageProviderTypeMetadata(ctx, req) (*GetStorageProviderTypeMetadataResp, *Err)` | 获取驱动类型元数据 |

### share_link.go

| 方法 | 说明 |
|------|------|
| `CreateShareLink(ctx, req) (*CreateShareLinkResp, *Err)` | 创建分享链接 |
| `CancelShareLink(ctx, req) (*BaseResp, *Err)` | 取消分享链接 |

## internal/db/

### upload.go

| 方法 | 说明 |
|------|------|
| `CreateUploadRecord(record) error` | 创建上传记录 |
| `GetUploadRecordByID(id) (*UploadRecord, error)` | 按 ID 查询上传记录 |
| `ListUploadRecords(parentID, status, offset, limit) ([]UploadRecord, int64, error)` | 分页查询上传记录 |
| `UpdateUploadRecordStatus(id, status, message) error` | 更新上传记录状态 |
| `CountUploadRecordsByStatus(status) (int64, error)` | 按状态统计上传记录数 |
| `DeleteUploadRecord(id) error` | 删除上传记录 |
| `GetPendingUploadRecords() ([]UploadRecord, error)` | 获取所有待处理记录（启动恢复用） |

### file.go

| 方法 | 说明 |
|------|------|
| `CreateFile(file) error` | 创建文件记录 |
| `GetFileByID(id) (*File, error)` | 按 ID 查询文件 |
| `GetFilesByParentID(parentID) ([]File, error)` | 按父目录查询子文件 |
| `UpdateFile(file) error` | 更新文件记录 |
| `DeleteFile(id) error` | 删除文件记录 |
| `RenameFile(id, newName) error` | 重命名 |
| `MoveFile(id, newParentID) error` | 移动 |

### file_location.go

| 方法 | 说明 |
|------|------|
| `CreateFileLocation(loc) error` | 创建物理副本记录 |
| `GetFileLocationsByHash(hash) ([]FileLocation, error)` | 按哈希查询副本 |
| `GetFileLocationsByProvider(providerID) ([]FileLocation, error)` | 按 Provider 查询副本 |
| `DeleteFileLocation(id) error` | 删除副本记录 |
| `DeleteFileLocationsByHash(hash) error` | 按哈希批量删除 |

### provider.go

| 方法 | 说明 |
|------|------|
| `CreateProvider(provider) error` | 创建存储提供者 |
| `GetProviderByID(id) (*StorageProvider, error)` | 按 ID 查询 |
| `ListProviders() ([]StorageProvider, error)` | 列出所有提供者 |
| `UpdateProvider(provider) error` | 更新提供者配置 |
| `DeleteProvider(id) error` | 删除提供者 |
| `GetEnabledProviders() ([]StorageProvider, error)` | 获取启用的提供者 |

## internal/storage/

### dispatch.go

| 方法 | 说明 |
|------|------|
| `Dispatch(uploadRecordID) error` | 调度上传/同步任务 |
| `RefreshProviderSemaphore(providerID) int` | 刷新 Provider 信号量配置 |

### storage.go

| 方法 | 说明 |
|------|------|
| `Init()` | 初始化存储驱动实例并恢复未完成任务 |

### temp_cleanup.go

| 方法 | 说明 |
|------|------|
| `StartTempCleanup()` | 启动临时文件清理后台任务 |
| `cleanupOrphanedTempFiles()` | 执行一次清理（内部调用） |

### helper.go

| 方法 | 说明 |
|------|------|
| `SelectBestSource(locations) (*FileLocation, error)` | 按 sort_order 选择最优读取源 |
