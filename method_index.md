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


### permission.go

| 方法 | 说明 |
|------|------|
| `CheckOwnership(ctx, fileID) bool` | 检查用户是否为文件所有者（或管理员） |
| `CheckOwnershipOrAdmin(ctx, fileID) bool` | 同上，语义别名 |
| `CheckPermission(userID, fileID, requiredPerm) bool` | **已废弃** 旧权限检查 |
| `GrantDirectoryPermission(ctx, req, operator) *Err` | **已废弃** 旧权限授予 |

### share_save.go (UserSpace 管理)

| 方法 | 说明 |
|------|------|
| `GetUserSpace(ctx) (*UserSpace, error)` | 获取当前用户的空间信息 |
| `ListUserSpaces(ctx, req) (*ListUserSpacesResp, *Err)` | 管理员：列出所有用户空间 |
| `UpdateUserSpace(ctx, req) *Err` | 管理员：更新用户空间配额 |
| `AssignRootDir(ctx, req) (*AssignRootDirResp, *Err)` | 管理员：为用户分配根目录及存储提供者 |

### auth_helper.go

| 方法 | 说明 |
|------|------|
| `getCurrentUserIDFromCtx(ctx) string` | 从 context 获取当前用户 ID |
| `isAdminUser(ctx) bool` | 检查当前用户是否为管理员 |
| `getUserContext(ctx) *UserContext` | 从 context 获取完整用户空间信息 |
| `getBoundProviderIdsFromCtx(ctx) ([]uint32, *Err)` | 从 context 获取绑定的存储提供者 |
| `getRootFileIDFromCtx(ctx) (string, *Err)` | 从 context 获取用户根目录 ID |
| `GetBoundProviderIdsByOwnerID(ownerID) ([]uint32, error)` | 通过 owner_id 获取绑定的存储提供者（导出） |
| `RequireAdmin(ctx) *Err` | 检查管理员权限，非管理员返回错误 |

## internal/db/

### upload.go

| 方法 | 说明 |
|------|------|
| `CreateUploadRecord(record) error` | 创建上传记录 |
| `GetUploadRecordByID(id) (*UploadRecord, error)` | 按 ID 查询上传记录 |
| `ListUploadRecords(parentID, status, offset, limit) ([]UploadRecord, int64, error)` | 分页查询上传记录 |
| `UpdateUploadRecordStatus(id, status, message) error` | 更新上传记录状态 |
| `TransitionUploadRecordTaskStatus(id, prevStatuses, newStatus, message) error` | 任务状态迁移（乐观锁，操作 task_status 列） |
| `TransitionFileLocationStatus(id, prevStatuses, newStatus, message) error` | 分片状态迁移（乐观锁，操作 status 列） |
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
| `TransitionFileLocationStatus(id, prevStatuses, newStatus, message) error` | 状态迁移（乐观锁：仅当当前状态在 prevStatuses 中才更新） |

### provider.go

| 方法 | 说明 |
|------|------|
| `CreateProvider(provider) error` | 创建存储提供者 |
| `GetProviderByID(id) (*StorageProvider, error)` | 按 ID 查询 |
| `ListProviders() ([]StorageProvider, error)` | 列出所有提供者 |
| `UpdateProvider(provider) error` | 更新提供者配置 |
| `DeleteProvider(id) error` | 删除提供者 |
| `GetEnabledProviders() ([]StorageProvider, error)` | 获取启用的提供者 |


### permission.go

| 方法 | 说明 |
|------|------|
| `GetDirectoryPermission(userID, fileID) (*DirectoryPermissionORM, error)` | 获取用户对指定目录的权限 |
| `GetUserDirectoryPermissions(userID) ([]*DirectoryPermissionORM, error)` | 获取用户的所有目录权限 |
| `GetDirectoryPermissionsByFileID(fileID) ([]*DirectoryPermissionORM, error)` | 获取目录的所有用户权限 |
| `CreateDirectoryPermission(perm) error` | 创建目录权限 |
| `UpdateDirectoryPermission(perm) error` | 更新目录权限 |
| `DeleteDirectoryPermission(userID, fileID) error` | 删除目录权限 |
| `GetAllDirectoryPermissions() ([]*DirectoryPermissionORM, error)` | 获取所有目录权限（管理员用） |
| `CreateAuditLog(log) error` | 创建权限审计日志 |

### admin_user.go

| 方法 | 说明 |
|------|------|
| `IsAdmin(userID) bool` | 检查用户是否为管理员 |
| `GetAdminUser(userID) (*AdminUserORM, error)` | 获取管理员记录 |
| `ListAdminUsers() ([]*AdminUserORM, error)` | 获取所有管理员 |
| `AddAdminUser(userID) error` | 添加管理员 |
| `RemoveAdminUser(userID) error` | 移除管理员 |

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

## driver/types/（驱动抽象层，阶段2 DTO 化）

### driver.go

| 接口/方法 | 说明 |
|------|------|
| `StorageDriver` | 存储驱动接口（Upload/Open 已 DTO 化，不再传 GORM ORM） |
| `Upload(ctx, src UploadSource, opt UploadOptions) ([]FileRef, error)` | 上传（src=轻量源，opt=per-provider 选项） |
| `Open(ctx, refs []FileRef, opt OpenOptions) (ReadSeekCloser, error)` | 打开（refs=分片引用，opt=解密器） |
| `GetProviderConfig() *ProviderConfig` | 类型化配置视图（替代 GetOrm） |
| `BatchDeleter` / `PreDownloader` | 可选接口，入参均为 `[]FileRef` |

### upload_source.go

| 类型/方法 | 说明 |
|------|------|
| `FileRef` | 文件在 provider 上的引用（值类型，双向：Upload 输出 / Open 输入） |
| `UploadSource` | 上传源（TempPath/FileName/FileSize/FileHash） |
| `UploadOptions` / `OpenOptions` | 上传/打开选项 |
| `ProviderConfig` | 类型化配置视图（EncryptionKey/Suffix/SortOrder/FileSizeLimit/HashType 等） |
| `ProviderConfigFromORM(orm)` | 从 ORM 提取配置视图 |
| `FileRefFromLocation(loc)` / `FileRefsFromLocations(locs)` | FileLocationORM → FileRef 转换 |

### upload_pack.go（加密管道，消除多驱动复制）

| 方法 | 说明 |
|------|------|
| `PackToTempFile(rawPath, fileName, opt)` | 加密到临时文件（baidu 用），返回 (路径, 混淆名, 大小) |
| `PackStream(raw, fileName, opt)` | 流式加密（telegram 用） |
| `ComputeEncryptedChunkSize(limit, encrypted)` | 加密开销预留后的分片大小 |

### upload_dir.go（目录 sharding，消除多驱动复制）

| 类型/方法 | 说明 |
|------|------|
| `DirBackend` | 目录后端接口（List/Mkdir，local 用 os、云端用 API） |
| `AllocateUploadDir(ctx, root, backend)` | 年/月/数字子目录 sharding 单一实现 |

### cache.go / persistence.go（驱动回调持久化，替代驱动反写 db）

| 类型 | 说明 |
|------|------|
| `CacheUpdater` / `CacheUpdaterSetter` | 缓存状态更新回调（telegram） |
| `ProviderConfigPersister` / `ConfigPersisterSetter` | 配置持久化回调（baidu token 刷新） |

