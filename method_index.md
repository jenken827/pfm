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
| `CreateShareLink(ctx, req) (*CreateShareLinkResp, *Err)` | 创建分享链接（归属校验：仅文件所有者/管理员可分享） |
| `CancelShareLink(ctx, req) (*BaseResp, *Err)` | 取消分享链接（归属校验） |
| `ListMyShares(ctx, req) (*ListMySharesRespData, *Err)` | 我的分享列表（分页，含文件状态/分享 URL，实时计算过期） |
| `UpdateShareLink(ctx, req) (*UpdateShareLinkRespData, *Err)` | 更新分享（有效期/永久/访问上限/备注；有效期变更重签 URL） |


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

### ensure_dir.go（按年/月目录归档）

| 方法 | 说明 |
|------|------|
| `EnsureDirPath(ctx, parentID, relPath) (string, *Err)` | 在 parentID 下按相对路径逐级确保目录存在（mkdir -p，并发安全，唯一索引兜底），返回叶子目录 ID。相册备份按"年/月"归档用 |

### photo_date.go（照片日期解析，规则可配置）

规则引擎：按规则表顺序匹配文件名，先命中先返回；未知规则类型跳过（向前兼容新配置）。
规则类型：`prefix_segment_date`（前缀+下划线取段，段内 YYYYMMDD/YYYY-MM-DD）、
`prefix_compact_date`（前缀后紧跟 8 位日期）、`prefix_timestamp`（前缀后跟 10 位秒级时间戳）、
`leading_timestamp`（开头连续数字取前 10 位秒级时间戳）。时间戳有防误判阈值（前两位 >12 且 ≤当前；
年份 [ts_min_year, 当前年+ts_max_year_offset]）。配置源为 `config.photo_date`（整体替换默认规则表，
留空回退内置默认），经 `GetPhotoDateConfig` RPC 下发前端；前端内置默认与后端一致，拉取失败回退缓存/默认。

| 方法 | 说明 |
|------|------|
| `ParsePhotoDateFromFilename(filename) *time.Time` | 从文件名解析拍摄日期（默认规则表：10 位时间戳 / IMG_ / Screenshot_ / PANO_ / VID_ / IMG 紧凑日期 / mmexport / wx_camera_ / microMsg.），无法解析返回 nil |
| `ParsePhotoDateFromFilenameWithConfig(filename, cfg models.PhotoDateConfig) *time.Time` | 用服务端配置的规则解析（migrate-photos 使用），空配置回退默认 |
| `GetPhotoDateConfig() *native.PhotoDateConfig` | 返回下发前端的照片日期解析配置（config 未配置项回退内置默认） |

### photo_migrate.go（旧数据年/月归档迁移）

| 方法 | 说明 |
|------|------|
| `MigratePhotosToYearMonth(baseParentID, dryRun) (*MigratePhotoResult, error)` | 把子树下媒体文件（图片+视频）按文件名日期移到 父目录/年/月（自动创建目录），已就位/无日期跳过，同名冲突自动加后缀；dryRun 只预览 |

### file_dedup.go（重复文件去重）

| 方法 | 说明 |
|------|------|
| `DedupFilesByHash(baseParentID, dryRun) (*DedupResult, error)` | 子树下按 hash 去重：同 hash 多份保留名称最短（同长度保留最早创建），删除其余 files 记录及历史；物理存储按 hash 共享不受影响 |
| `DedupNameSuffixDuplicates(baseParentID, dryRun) (*NameSuffixDedupResult, error)` | 子树下按 同目录同名 + "(N)" 后缀 配对去重：同尺寸需内容核验（JPEG 比较 SOS 后像素数据/其他格式字节比较）通过才删；不同尺寸的图片对做压缩衍生核验（32x32 灰度感知比对，均差 <10 判衍生，删小保大）；JPEG 保留 EXIF 非零 tag 更完整方，保留后缀副本时改名回原名；冗余 hash 无引用时回收其在所有提供者上的物理副本与缩略图；同时清理孤儿 upload_records/chunks；同名不同尺寸且非衍生/内容不同仅列出 |
| `ReclaimOrphanHashStorage(hash) (int64, error)` | 回收一个 hash 在所有提供者上的物理副本（调用方保证 files/histories 无引用）：按需懒加载驱动(storage.EnsureProvider)+batchDeleteFromProvider 删除，成功删 file_locations 行并扣容量，失败标记 DELETED_FAILED 待重试；顺带删缩略图。dedup-name 与 cleanup-orphan-locations 共用 |
| `cleanupUploadTasksByHashes(hashes) (int64, error)` | 按 hash 批量删除 upload_records + 关联 upload_chunk_records（dedup-name 回收阶段调用，避免孤儿任务持续 dispatch） |

### album_subfolder.go（相册备份子目录）

| 方法 | 说明 |
|------|------|
| `GetOrCreateSubfolder(ctx, parentID, prefix) (string, *Err)` | 获取有空位的编号子目录（500 文件封顶），满了自动创建新目录 |

## internal/auth/

| 类型/方法 | 说明 |
|------|------|
| `Claims` / `User` | 认证层自研 claims 类型（JWT 顶层扁平 id/name/email，JSON 布局与历史 token 兼容） |
| `LocalAuthenticator.Authenticate(username, password) (string, error)` | 本地用户表 bcrypt 验密，返回用户 ID（UUID） |
| `LocalAuthenticator.IssueToken(user) (string, error)` | 签发本地 JWT（HS256，顶层扁平 user claims） |
| `LocalAuthenticator.VerifyToken(token) (*Claims, error)` | 校验本地 JWT（拒绝非 HS256 算法） |
| `HashPassword(password) (string, error)` / `VerifyPassword(hash, password) bool` | bcrypt 密码哈希/校验 |
| `InitLocalAuth() error` | 初始化本地认证器（读取/生成 JWT 密钥，持久化到 data/jwt_secret） |
| `GetLocalAuth() *LocalAuthenticator` | 获取全局本地认证器（懒初始化；middleware / webdav / api 直接调用） |

## internal/db/

### user.go（本地认证用户）

| 方法 | 说明 |
|------|------|
| `GetUserByUsername(username) (*UserORM, error)` | 按用户名查询（gorm.ErrRecordNotFound 表示不存在） |
| `GetUserByID(userID) (*UserORM, error)` | 按用户 ID 查询 |
| `CreateUser(user) error` | 创建用户 |
| `ListUsers() ([]UserORM, error)` | 列出所有本地用户 |
| `DeleteUser(userID) error` | 删除用户（硬删除） |
| `UpdateUserPassword(userID, passwordHash) error` | 更新密码哈希 |
| `UpdateUserUsername(userID, newUsername) error` | 修改用户名（user_id 不变，关联不受影响） |

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
| `CountActiveFilesByHashExcludingIDs(hash, excludeIDs) (int64, error)` | 统计引用指定 hash 的未删除文件数（排除指定 ID），用于判断 file_location 是否可安全删除 |
| `CountFilesByHashIncludingDeleted(hash) (int64, error)` | Unscoped 统计引用指定 hash 的全部文件记录数（含回收站软删），用于去重清理物理数据前确认无任何可恢复引用 |
| `CountFileHistoriesByHash(hash) (int64, error)` | 统计指定 hash 在 file_histories 中的引用数，用于判断该 hash 的存储位置是否仍被历史版本引用 |

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
| `EnsureProvider(id) (types.StorageDriver, error)` | 按需懒加载指定 provider 的驱动（幂等），供 CLI 等未整体 Init 的入口使用（如 dedup-name 回收远程副本） |

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

### upload_pack.go（加密/命名管道，消除多驱动复制）

| 方法 | 说明 |
|------|------|
| `PackToTempFile(rawPath, fileName, opt)` | baidu 用：加密则加密到临时文件，返回 (路径, 上传文件名, 大小)。上传文件名无论是否加密都会重命名（明文保留原扩展名） |
| `PackStream(raw, fileName, opt)` | telegram 用：流式包装，返回加密 reader + 上传文件名（同样无条件重命名） |
| `ComputeEncryptedChunkSize(limit, encrypted)` | 加密开销预留后的分片大小 |

### filename.go（上传文件名生成）

| 方法 | 说明 |
|------|------|
| `GenerateObfuscatedFilename(name, suffix)` | 加密混淆名：`随机8位_时间戳.后缀`（去掉原名与扩展名） |
| `GetUniqueFileName(fileName)` | 明文唯一名：`原名_随机8位_时间戳.原扩展名`。物理目录按年/月 sharding 后不同逻辑目录的同名文件会落入同一物理目录，必须无条件重命名防覆盖 |

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

