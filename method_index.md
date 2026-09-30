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
| `checkUploadRecordOwnership(ctx, record) bool` | 上传记录归属校验（**管理员不享有特权**；Cancel/Delete/Retry/Detail 用） |

### version_allocator.go（并发上传版本仲裁）

| 方法 | 说明 |
|------|------|
| `AllocateFileVersion(ownerID, parentID, fileName) uint64` | 请求头到达时分配 per-key 单调递增版本号（后到者赢；首次从 DB max(version) 校准含软删） |
| `WithUploadVersion(ctx, version) context.Context` | 版本号注入 ctx（webdav 入口等调用） |
| `getUploadVersion(ctx) uint64` | 从 ctx 读取版本号（service 内部） |

### offline_download.go（URL 离线下载，任务14）

| 方法 | 说明 |
|------|------|
| `CreateOfflineDownload(ctx, req) (*CreateOfflineDownloadResp, *Err)` | 创建 URL 离线下载：协议白名单(http/https/ftp/magnet/ed2k) + 目标目录归属校验(空=根目录) + aria2 AddURI(dir=DataDir/.offline) + DownloadRecord 落库（失败 ForceRemove 撤销） |
| `ListOfflineDownloads(ctx, req) (*ListOfflineDownloadsResp, *Err)` | 当前用户离线下载任务分页列表（id 倒序） |
| `DeleteOfflineDownload(ctx, req) *Err` | 删除任务：非终态先 aria2 ForceRemove 停下载，再删记录（归属校验，管理员可越权） |
| `RetryOfflineDownload(ctx, req) (*CreateOfflineDownloadResp, *Err)` | 重试失败任务：原 URL 重新 AddURI、换新 gid、重置状态 |

### capacity_updater.go（容量统计异步更新器）

| 方法 | 说明 |
|------|------|
| `MarkCapacityChanged(ownerID)` | 用户容量变动标记：进脏集合，防抖后异步重算写回 used_bytes |
| `MarkFileCapacityChanged(ownerID)` | 便捷入口（单 owner 标记） |
| `StartCapacityStatsUpdater()` | 启动容量更新器（幂等，bootstrap 调用） |
| `RefreshAllCapacityStats()` | 全量刷新所有用户 used_bytes（启动时补历史存量） |

注：文件夹大小展示已取消（子树遍历不划算），无 folder 维度统计。

### file.go

| 方法 | 说明 |
|------|------|
| `GetFileList(ctx, req) (*GetFileListResp, *Err)` | 获取文件列表（文件夹行 size 前端显示 "-"，不统计子树大小） |
| `CreateDir(ctx, req) (*CreateDirResp, *Err)` | 创建目录：写入加密属性、`max_histories`（未传则继承父目录）与 provider 绑定（显式 ≥1 且 ⊆ 分配集，或继承父目录） |
| `UpdateDir(ctx, req) (*BaseResp, *Err)` | 更新目录信息：`name` / `enable_encryption` / `max_histories` / `bound_provider_ids`（后两者字段为 nil 时不改，provider 显式传时校验 ⊆ 分配集）|
| `getInheritedMaxHistories(parentID) uint32`（内部） | 从父目录继承历史版本数（父目录缺失/未配置返回 0 = 不保留历史），与 `getInheritedEncryption` 同语义 |
| `DeleteFile(ctx, req) (*BaseResp, *Err)` | 删除文件（移入回收站；used_bytes 含回收站口径不变，不触发重算） |
| `RestoreFile(ctx, req) (*BaseResp, *Err)` | 恢复回收站文件（不触发重算） |
| `PermanentDeleteFile(ctx, req) (*BaseResp, *Err)` | 永久删除文件（used_bytes 回落，触发重算） |
| `EmptyTrash(ctx, req) (*BaseResp, *Err)` | 清空回收站（触发重算） |
| `permanentlyDeleteFiles(ctx, allFileIDs, logLabel)`（内部） | 同步永久删除（历史 + provider 物理文件 + DB 记录）；删除成功后调 `DisableWebdavAccountsForDeletedRoots` 自动禁用绑定目录被删的 WebDAV 账号 |
| `RenameFile(ctx, req) (*BaseResp, *Err)` | 重命名文件 |
| `MoveFile(ctx, req) (*BaseResp, *Err)` | 移动文件（owner 不变，不触发重算） |
| `GetFileDetail(ctx, req) (*GetFileDetailResp, *Err)` | 获取文件详情 |
| `RestoreFileHistory(ctx, req) *Err` | 恢复历史版本：new_name 空 → 覆盖当前文件（CAS，当前内容进历史）；非空 → 以新名称另存（秒传，不覆盖） |
| `resolveCreateDirProviderIds(ctx, parentID, requested) (string, *Err)`（内部） | 计算新建目录的绑定：显式 `providerIds`（≥1 且 ⊆ 分配集 + 存在且启用）→ 原样使用；未传 → 继承父目录解析结果（`getNearestBound` + 分配集兜底）。`CreateDir` / `UpdateDir` 均先经 `validateProviderIdsAssigned` + `validateProvidersUsable` |
| `SyncFileToProviders(ctx, req) *Err` | 同步文件到指定 provider；目标必须在**文件所在目录的有效绑定**内（`resolveProviderIdsForFolder`），且文件属于当前用户 |
| `GetAvailableProvidersForSync(ctx, fileHash) ([]*StorageProvider, *Err)` | 可同步 provider 列表：先按 owner 定位该 hash 的文件，再用其所在目录的有效绑定过滤（修掉旧实现「列出用户无权同步的 provider」越权展示） |

### file_stream.go（文件流：预览/下载）

| 方法 | 说明 |
|------|------|
| `GetFileStream(ctx, fileID, providerID) (*FileStreamResult, error)` | **用户场景**取文件流，内部强制 `CheckOwnership`（仅 owner 可取） |
| `GetFileStreamForShare(ctx, fileID, providerID) (*FileStreamResult, error)` | **分享场景**取流，跳过归属校验；调用方须已完成分享有效性校验（见 `api/share_link.go`） |
| `GetFileHistoryStream(ctx, historyID, isPreview) (*FileStreamResult, error)` | 取历史版本流；经 `history.FileId` 校验归属 |

### file_detail.go（文件详情）

| 方法 | 说明 |
|------|------|
| `GetFileDetail(ctx, req) (*FileDetailRespData, *Err)` | 获取文件详情（含存储位置、历史、标签）；**仅 owner** |
| `GetFileInfo(ctx, req) (*GetFileInfoRespData, *Err)` | 获取文件基本信息；**仅 owner**。响应含 `boundProviderIds`（JSON 数组，仅文件夹有效），供前端回显/预选绑定 |
| `GetFileCacheStatus(ctx, req) (*GetFileCacheStatusResp, *Err)` | 查询缓存状态；入参为全局 file_hash，须先按 `(hash, owner)` 确认归属 |

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


### share_save.go (UserSpace 管理)

| 方法 | 说明 |
|------|------|
| `GetUserSpace(ctx) (*UserSpace, error)` | 获取当前用户的空间信息 |
| `ListUserSpaces(ctx, req) (*ListUserSpacesResp, *Err)` | 管理员：列出所有用户空间 |
| `UpdateUserSpace(ctx, req) *Err` | 管理员：更新用户空间配额 |
| `AssignRootDir(ctx, req) (*AssignRootDirResp, *Err)` | 管理员：为用户分配根目录及存储提供者（`UserSpace.bound_provider_ids` = 用户的可用范围上限）。已有 UserSpace 时同时把根目录 File 行的 `bound_provider_ids` 写成同一份值（`updateRootDirBinding`），根目录是"未显式绑定目录"的继承兜底点 |
| `updateRootDirBinding(space, boundProviderIds) *Err`（内部） | 同步根目录行的绑定（`db.UpdateDir`），保持与 UserSpace 一致 |

### auth_helper.go

| 方法 | 说明 |
|------|------|
| `getCurrentUserIDFromCtx(ctx) string` | 从 context 获取当前用户 ID |
| `isAdminUser(ctx) bool` | 检查当前用户是否为管理员 |
| `getUserContext(ctx) *UserContext` | 从 context 获取完整用户空间信息 |
| `getBoundProviderIdsFromCtx(ctx) ([]uint32, *Err)` | 从 context 获取**管理员分配给当前用户**的存储提供者（可用范围上限，非落盘目标） |
| `resolveProviderIdsForFolder(ctx, folderID) ([]uint32, *Err)` | 解析当前用户在某目录下应落盘到哪些 provider（`storage.ResolveProviderIds`；未配置 UserSpace 返回"根目录未配置"） |
| `validateProviderIdsAssigned(ctx, ids) *Err` | 校验目录绑定请求：≥1 个且全部 ⊆ 管理员分配集（越界 `PERMISSION_DENIED`） |
| `validateProvidersUsable(ctx, ids) *Err` | 校验 provider 存在且启用（禁用/删除的不能再绑） |
| `inheritBoundProviderIds(ownerID, parentID) string` | 程序化建目录（相册编号子目录 / mkdir -p / 保存分享）继承父目录的绑定，返回 JSON 字符串（空串 = 未显式绑定） |
| `uniqueCount(ids) int` | provider 去重计数（用于数量比对校验） |
| `getRootFileIDFromCtx(ctx) (string, *Err)` | 从 context 获取用户根目录 ID |
| `GetBoundProviderIdsByOwnerID(ownerID) ([]uint32, error)` | 通过 owner_id 获取管理员分配的存储提供者（导出，无 context 场景） |
| `RequireAdmin(ctx) *Err` | 检查管理员权限，非管理员返回错误 |
| `checkOwnershipBatch(ctx, fileIDs) (string, *Err)` | 批量归属校验（单条 `id IN (...)` SQL，替代循环调用 `checkOwnership` 的 N+1）。返回第一个无权限的 id，空串表示全部通过 |

### permission.go（归属与角色）

| 方法 | 说明 |
|------|------|
| `CheckOwnership(ctx, fileID) bool` | 当前用户是否为文件 owner（管理员不享有特权） |
| `GetMyRoles(ctx) (*GetMyRolesResp, *Err)` | 当前用户角色（`pfm_admin` / `pfm_user`） |
| `GetMyBoundProviders(ctx) (*GetMyBoundProvidersResp, *Err)` | 管理员分配给当前用户且已启用的存储提供者（文件夹绑定选择的候选范围）。路由 `POST /api/space/my-providers`；顺序沿用分配顺序，禁用/已删除的分配项不下发 |

### ensure_dir.go（按年/月目录归档）

| 方法 | 说明 |
|------|------|
| `EnsureDirPath(ctx, *native.EnsureDirPathReq) (*native.EnsureDirPathResp, *Err)` | 在 parent_id 下按相对路径逐级确保目录存在（mkdir -p，并发安全，唯一索引兜底）。相册备份按"年/月"归档用。新建目录的 `bound_provider_ids` 继承父目录解析结果（`inheritBoundProviderIds`） |

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
| `GetOrCreateSubfolder(ctx, *native.GetOrCreateSubfolderReq) (*native.GetOrCreateSubfolderResp, *Err)` | 获取有空位的编号子目录（500 文件封顶），满了按最大编号 +1 新建（格式 001/002）。兼容历史命名 01 / 00001（统一按「纯数字」识别）。新建目录的 `bound_provider_ids` 继承父目录解析结果 |

### webdav_account.go（WebDAV 独立账号：自助接口 + `/dav` 认证）

| 方法 | 说明 |
|------|------|
| `ListWebdavAccounts(ctx) (*WebdavAccountListResp, *Err)` | 当前用户账号列表（不含密码哈希；rootPath 相对网盘根渲染，rootValid 用 1 次批量查询判断） |
| `CreateWebdavAccount(ctx, req) (*WebdavAccountCreateResp, *Err)` | 创建账号：校验绑定目录（属主 + 文件夹）、用户名唯一/字符集、密码 ≥12 位；返回一次性明文密码 |
| `UpdateWebdavAccount(ctx, req) *Err` | 更新标签/绑定目录/只读/启用（改后清认证缓存） |
| `DeleteWebdavAccount(ctx, req) *Err` | 删除自己的账号（硬删 + 清缓存） |
| `ResetWebdavAccountPassword(ctx, req) (*WebdavAccountResetPasswordResp, *Err)` | 重置密码（留空生成 16 位随机），返回一次性明文 |
| `AuthenticateWebdav(ctx, username, password) (*WebdavAccountORM, error)` | `/dav` Basic Auth 入口：bcrypt + 10 分钟内存缓存（key=sha256(username\0password)）+ 绑定目录兜底校验；目录失效则自动禁用账号 |
| `DisableWebdavAccountsForDeletedRoots(ctx, rootIDs)` | 删除路径事件钩子：批量自动禁用绑定目录被删的账号并清缓存（DeleteFile / permanentlyDeleteFiles 调用） |
| `invalidateWebdavAuthCache(accountID)`（内部） | 按账号清认证缓存（改密/改绑定/启用禁用/删除/自动禁用后调用） |

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
| `ListUploadRecords(page, pageSize, status, ownerID) ([]UploadRecord, int64, error)` | 分页查询上传记录（**按 ownerID 过滤**，空则全局——管理视角） |
| `UpdateUploadRecordStatus(id, status, message) error` | 更新上传记录状态 |
| `TransitionUploadRecordTaskStatus(id, prevStatuses, newStatus, message) error` | 任务状态迁移（乐观锁，操作 task_status 列） |
| `TransitionFileLocationStatus(id, prevStatuses, newStatus, message) error` | 分片状态迁移（乐观锁，操作 status 列） |
| `CountUploadRecordsByStatus(status, ownerID) (int64, error)` | 按状态统计上传记录数（按 ownerID 过滤，空则全局） |
| `DeleteUploadRecord(id) error` | 删除上传记录 |
| `GetPendingUploadRecords() ([]UploadRecord, error)` | 获取所有待处理记录（启动恢复用） |

### download.go（下载记录）

| 方法 | 说明 |
|------|------|
| `CreateDownloadRecord(dr) error` | 创建下载记录 |
| `GetDownloadRecordByGid(gid) (*DownloadRecordORM, error)` | 按 aria2 gid 查询（notifier 回调用） |
| `GetDownloadRecordByID(id) (*DownloadRecordORM, error)` | 按 ID 查询 |
| `GetDownloadRecordByUploadID(uploadID) (*DownloadRecordORM, error)` | 按关联上传记录查询 |
| `ListOfflineDownloadsByOwner(ownerID, offset, limit) ([]DownloadRecordORM, int64, error)` | 分页列出用户离线下载任务（id 倒序） |
| `UpdateDownloadRecord(dr, updateFields) error` | 更新下载记录（map 更新 + nil 保护） |
| `DeleteDownloadRecord(id) error` | 删除下载记录 |

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
| `UpdateFileWithHistoryCAS(ctx, file, oldFile, myVersion) (bool, error)` | CAS 抢占版本并记录历史：`WHERE id AND version<myVersion` 成功才更新 hash/size/version，旧内容写 file_histories；myVersion=0 退化无条件更新。返回是否抢占成功 |
| `TakeoverFileVersion(fileID, myVersion, newHash, newSize, extra) (bool, error)` | 按版本号 CAS 更新文件内容字段（extra 携带 is_ready 等），低配版无历史写入 |
| `UpdateFileIsReady(parentID, fileName, ownerID, version) error` | 更新 is_ready=true；version>0 时仅当 file 当前 version 等于该值才更新（CAS，防旧分发误标新内容就绪） |
| `SumAllFileSizesByOwner(ownerID) (uint64, error)` | 统计用户所有文件（Unscoped 含回收站软删）size 总和，供容量更新器写回 used_bytes |
| `CountFileHistoriesByHash(hash) (int64, error)` | 统计指定 hash 在 file_histories 中的引用数，用于判断该 hash 的存储位置是否仍被历史版本引用 |
| `GetFilesByIDs(ids []string) ([]*FileORM, error)` | 批量按 ID 查询文件（默认过滤软删），用于批量归属校验等场景，避免 N+1 |
| `GetFileHistoriesByIDs(ids []uint64) ([]*FileHistoryORM, error)` | 批量获取文件历史记录，避免逐条查询 N+1 |
| `GetFileNamesInParent(parentID, ownerID) (map[string]*FileORM, error)` | 取父目录下「文件名 → file」映射（单条 SQL），用于批量同名冲突检测 |
| `GetNearestBoundProviderIds(fileID) ([]uint32, error)` | 沿 `parent_id` 祖先链向上取最近一个显式绑定 `bound_provider_ids` 的目录（256 层上限防环；JSON 脏数据告警后按未绑定继续向上；查不到返回 `nil, nil`）。目录绑定解析的底层原语 |
| `UpdateDir(orm) error` | 更新目录配置（白名单：`name` / `enable_encryption` / `bound_provider_ids` / `max_histories`）；字段为 nil 则不更新。`max_histories` 用 map 写入以绕开 GORM 零值忽略（0 = 不保留历史是合法值） |

### file_location.go

| 方法 | 说明 |
|------|------|
| `CreateFileLocation(loc) error` | 创建物理副本记录 |
| `GetFileLocationsByHash(hash) ([]FileLocation, error)` | 按哈希查询副本 |
| `GetFileLocationsByProvider(providerID) ([]FileLocation, error)` | 按 Provider 查询副本 |
| `DeleteFileLocation(id) error` | 删除副本记录 |
| `DeleteFileLocationsByHash(hash) error` | 按哈希批量删除 |
| `GetFileLocationsByBlake3Hashes(hashes) ([]FileLocation, error)` | 批量按哈希列表查副本（避免逐文件查询 N+1，如批量移动/删除） |
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


### user_space.go

| 方法 | 说明 |
|------|------|
| `GetUserSpaceByUserID(userID) (*UserSpaceORM, error)` | 按用户 ID 查询用户空间 |
| `CreateUserSpace(space) error` | 创建用户空间 |
| `UpdateUserSpace(space) error` | 更新用户空间 |
| `UpdateUserSpaceUsedBytes(userID, usedBytes) error` | 按 user_id 写回已用容量（容量异步更新器调用） |
| `GetAllUserIDs() ([]string, error)` | 返回全部用户空间的 user_id（容量全量刷新用） |
| `ListUserSpaces(offset, pageSize) ([]UserSpaceORM, int64, error)` | 分页列出用户空间（管理员） |

### admin_user.go

| 方法 | 说明 |
|------|------|
| `IsAdmin(userID) bool` | 检查用户是否为管理员 |
| `GetAdminUser(userID) (*AdminUserORM, error)` | 获取管理员记录 |
| `ListAdminUsers() ([]*AdminUserORM, error)` | 获取所有管理员 |
| `AddAdminUser(userID) error` | 添加管理员 |
| `RemoveAdminUser(userID) error` | 移除管理员 |

### webdav_account.go（WebDAV 独立账号）

| 方法 | 说明 |
|------|------|
| `CreateWebdavAccount(a) error` | 创建账号（用户名重复由唯一索引 `idx_wa_un` 兜底） |
| `GetWebdavAccountByUsername(username) (*WebdavAccountORM, error)` | 按 WebDAV 用户名查询（`/dav` 认证热路径，走唯一索引） |
| `GetWebdavAccountByID(id) (*WebdavAccountORM, error)` | 按主键查询 |
| `ListWebdavAccountsByUser(userID) ([]*WebdavAccountORM, error)` | 列出某用户全部账号（id 倒序） |
| `UpdateWebdavAccountFields(id, fields map[string]any) error` | 按 id 更新指定列（必须用 map，结构体 Updates 会跳过 false/空串） |
| `UpdateWebdavAccountPassword(id, passwordHash) error` | 更新密码哈希 |
| `UpdateWebdavAccountLastUsed(id) error` | 记录最后使用时间 |
| `SetWebdavAccountEnabled(id, enabled, reason) error` | 启用/禁用 + 写失效原因（启用时传空串清空） |
| `DeleteWebdavAccount(id) error` | 硬删除（纯配置数据，无保留价值） |
| `DisableWebdavAccountsByRootIDs(rootIDs, reason) ([]uint32, error)` | 绑定目录被删后批量自动禁用账号，返回被禁用 ID（供清认证缓存） |

### file_path.go（目录路径回溯）

| 方法 | 说明 |
|------|------|
| `NewFilePathResolver() *FilePathResolver` | 请求级路径解析器（缓存文件行，避免多账号重复回溯同一祖先） |
| `(*FilePathResolver).DirPath(fileID, stopAtID) (string, error)` | 由 fileID 向上回溯拼出以 stopAtID 为根的可读路径（`/readany/abc`；相等时返回 `/`；节点缺失返回已拼出部分） |

## internal/storage/

### dispatch.go

| 方法 | 说明 |
|------|------|
| `Dispatch(uploadRecordID) error` | 调度上传/同步任务 |
| `RefreshProviderSemaphore(providerID) int` | 刷新 Provider 信号量配置 |
| `uploadToProviderAsync(ur, ...)`（内部） | 单 provider 上传：成功后调 `db.UpdateFileIsReady(parent, name, "", ur.Version)` 按版本 CAS 标记 is_ready |

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

### persist.go

| 方法 | 说明 |
|------|------|
| `PreallocateFileLocations(ctx, blake3Hash, providerIDs)` | 落库（切 hash）前为新 hash 预建 `PPENDING` 位置行，维持「文件记录指向的 hash 必有 file_location 行」不变式：消除 PUT 后立即 GET 的硬 500 窗口，读端转入 staging 直读降级。预建与分发共用 `(hash, provider)` 锁（防重复行被判为分片数不符），失败仅告警不阻断上传 |

### binding.go（目录绑定 → 落盘 provider 解析）

放在 storage 包（而非 service）的原因：service 依赖 backup/download，而后两者也要用同一规则，放 service 会成 import 环。

| 方法 | 说明 |
|------|------|
| `ResolveProviderIds(ownerID, folderID) ([]uint32, error)` | 解析目录下的文件应落盘到哪些 provider：目录绑定 → 沿祖先链继承 → 回退 `UserSpace.bound_provider_ids` → 与分配集取交集（越界项告警丢弃，交集空则回退分配集）。owner 未配置 UserSpace 时返回 error |
| `OutOfAssigned(ids, assigned) []uint32` | 返回 `ids` 中不在分配集内的项（校验"用户只能绑定被分配的 provider"）。空切片 = 全部合法 |
| `EncodeProviderIds(ids) string` | 把 provider ID 列表编码为落库用 JSON 数组字符串 |

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

### multi_chunk_reader.go（多分片合并读取）

| 类型/方法 | 说明 |
|------|------|
| `MultiChunkReader` | 多分片合并的 `ReadSeekCloser`。**长度与偏移一律在明文空间**：加密分片的 `FileRef.ChunkSize` 是密文长度，先经 `crypt.DecryptedSize` 换算再累加，`Seek(io.SeekEnd)` 返回明文总长 |
| `NewMultiChunkReader(ctx, opener, locations, encryptor) (*MultiChunkReader, error)` | 预计算明文总长与各分片起始偏移；加密分片长度非法（数据损坏）时返回错误，让调用方在写响应头之前失败 |
| `TotalSize()` | 返回明文总大小 |

> 长度语义是硬约束：`api/file_stream.go` 用 `files.Size` 作 `Content-Length`，WebDAV 则由
> `emersion/go-webdav` 内部的 `http.ServeContent` 以 `Seek(io.SeekEnd)` 的返回值覆盖
> `Content-Length`。任一环节返回密文长度都会使声明长度大于实际输出，Go 在 flush 时判定响应
> 不完整、丢弃缓冲并关闭连接。

## driver/pan123/（123云盘，开放平台 Open API 模式）

认证走开放平台而非账号密码（官方已下线密码模式）；参考实现 OpenList `drivers/123_open`。
端点、认证语义、限速值见 `README.md`「123云盘驱动」与 `设计方案.md` 11.3.1。

### token.go（认证）

| 类型/方法 | 说明 |
|------|------|
| `tokenManager` | access_token 有效期与刷新；`blockRefresh` 在刷新失败后置位，避免每次请求都打续期接口 |
| `initTokenManager(cfg)` | 有 refresh_token 或 client 凭据 → 主动模式（清空手填 access_token）；仅手填 → 被动模式（按 90 天过期，123 JWT 的 exp 不可靠） |
| `tm.getAccessToken(ctx, creds, force)` | 返回可用 token；命中缓存要求 `now < expiredAt-5min`；`force=true` 用于 401 后强刷 |
| `tm.flush(ctx, creds)` | 刷新顺序即优先级：oplist renewapi（有 refresh_token）→ 官方 client_id/client_secret → 报错 |
| `requestAccessToken(ctx, clientID, clientSecret)` | `POST /api/v1/access_token`，按 `expiredAt`(RFC3339) 算过期 |
| `renewViaOnlineAPI(ctx, apiURL, refreshUI)` | oplist renewapi 在线续期；返回新 access_token 与**轮换后的 refresh_token** |
| `d.persistTokens()` | 先写回内存配置再序列化（顺序不能反），经 `ConfigPersisterSetter` 持久化到 `storage_providers.config` |

### api.go（请求层与限速）

| 类型/方法 | 说明 |
|------|------|
| `Pan123Driver` | 驱动结构（Config/Orm/creds/tm/uid/client） |
| `credentials` | 驱动持有的认证配置（clientID/clientSecret/apiURL） |
| `newHTTPClient()` | 专用 http.Client：**不设 Timeout**（大文件传输途中会被强断），只用 ResponseHeaderTimeout 约束响应头等待 |
| `rateLimiter` / `d.limiterFor(endpoint)` | 按端点的令牌桶限速（QPS 表见 `types.go` 的 `apiEndpoints`，0 = 不限速） |
| `d.requestJSON(ctx, method, endpoint, query, body)` | 统一鉴权请求：`code=0` 成功；`401` 强制刷新重试；`429` 等 500ms 重试；最多 3 次 |
| `d.getUserInfo` / `d.getFiles` / `d.getDownloadLink` / `d.mkdir` / `d.trash` | user/info、file/list（游标分页）、download_info、mkdir、批量 trash |
| `pathJoin(base, name)` / `splitPath(p)` | 123 侧路径拼接（保留前导 /）与分段 |

### driver.go（上传/读取/删除/目录）

| 方法 | 说明 |
|------|------|
| `d.rootFileID()` | 解析配置 root_file_id，非法/负数回落 0 |
| `d.Upload(ctx, src, opt)` | 加密准备 → 年/月目录分配 → MD5+SHA1 → SHA1 秒传 → etag 秒传 → 分片 → complete 轮询 |
| `d.Open(ctx, refs, opt)` | 复用 `types.NewMultiChunkReader`（明文空间偏移） |
| `d.OpenChunk(ctx, ref, offset)` | `types.ChunkOpener`：download_info 取直链 + HTTP Range；服务端返回 200（忽略 Range）时手动丢弃前置字节 |
| `d.OpenChunkRangeSeek(ctx, ref)` | 供加密文件从头读取的 Range+Seek 闭包 |
| `d.BatchDelete(ctx, refs)` | `types.BatchDeleter`：按 fileId 批量 trash；单个 fileId 非法逐条标 `results[i].Error`，不影响同批合法项 |
| `dirBackend`（`List`/`Mkdir`/`resolve`/`findChild`） | `types.DirBackend` 实现：把 `AllocateUploadDir` 的路径（`/2026/09`）逐级解析/创建为 fileId，缓存 `路径→fileId` |

### upload.go（上传编排）

| 方法 | 说明 |
|------|------|
| `computeFileHashes(file)` | 一次顺序读取同时产出 MD5（etag）与 SHA1，并复位文件游标 |
| `d.sha1Reuse(ctx, parentFileID, filename, sha1, size)` | SHA1 秒传（`duplicate=2` 同名自动重命名） |
| `d.create(ctx, parentFileID, filename, etag, size)` | 创建上传任务（带 MD5，命中秒传时 `reuse=true`） |
| `d.uploadSlices(ctx, file, createResp, fileName, fileSize)` | 并发分片上传；并发度取 `upload_thread`(1-32) 且不超分片数；**每分片单独取 token**（长上传中 token 可能中途过期） |
| `d.uploadSlice(...)` | 单分片 multipart 上传（`preuploadID`/`sliceNo`(从1)/`sliceMD5`/文件字段 `slice`） |
| `d.complete(ctx, preuploadID)` | 轮询合并至 `completed=true` 且 `fileID!=0`（最多 60 次，每次 1s） |
| `d.uploadThreads()` | 解析 upload_thread，越界/非法回落 3 |

### init.go（注册与元数据）

| 方法 | 说明 |
|------|------|
| `init()` | 注册 `PAN123` 驱动及 ConfigFields 元数据（client_id/client_secret/refresh_token/access_token/api_url/root_file_id/upload_thread），前端据此渲染配置表单 |
| `newDriver(provider)` | 构造驱动：解析配置 → 凭据预检（缺失仅告警不阻断）→ 初始化 token 管理器 |
| `d.warmup()` | 预取 token、持久化、读 user/info 记 uid 与配额（失败仅告警） |
| `checkCredentials(cfg)` | 校验至少配了一组凭据 |

FileRef 映射：`ProviderFileID`=fileId（删除/下载的唯一依据）、`Path`=123 侧完整路径（保留前导 /）、`Hash`=内容 MD5。

## driver/aliyundrive_open/（阿里云盘，开放平台 Open API 模式）

参考实现 OpenList `drivers/aliyundrive_open`。认证/限流/秒传语义见 `README.md`「阿里云盘驱动」
与 `设计方案.md` 11.3.2。与 123pan 的关键差异：**限流按账号而非按端点**。

### token.go（认证，含 JWT sub 校验）

| 类型/方法 | 说明 |
|------|------|
| `tokenManager` | access_token/refresh_token 管理；`blockRefresh` 续期连续失败后置位 |
| `initTokenManager(cfg)` | 用解析后的配置初始化 |
| `tm.getAccessToken(ctx, cfg)` | 命中缓存要求 `now < expiredAt-5min`；否则续期 |
| `tm.renewForced(ctx, cfg)` | 强制续期（接口返回 token 失效码时调用） |
| `tm.renew(ctx, cfg)` | 有 `api_url` 走在线续期，否则本地 client 凭据续期 |
| `renewViaOnlineAPI(ctx, cfg, refreshUI)` | oplist renewapi（`driver_txt=alicloud_qr`），返回轮换后的 refresh_token |
| `renewViaLocalAPI(ctx, cfg, refreshToken)` | `POST /oauth/access_token`；**校验续期前后 JWT `sub` 一致**，不一致报错 |
| `jwtSubject(token)` | 解 JWT 第二段取 `sub`（段数不对/base64 非法均报错） |
| `d.persistTokens()` | 先写回内存配置再序列化，经 `ConfigPersisterSetter` 持久化 |

### limiter.go（按账号分桶）

| 类型/方法 | 说明 |
|------|------|
| `limiter` | 一个账号的三个 `rate.Limiter`（list 3.9/s、link 0.9/s、other 14.9/s，burst=1）+ `usedBy` 引用计数 |
| `getLimiterForUser(userID)` | 全局表取/建并 `usedBy++`（同账号多 provider 共享） |
| `releaseLimiter(userID)` | `usedBy--`，归零即从全局表删除；全局限流器永不回收 |
| `l.wait(ctx, typ)` | 按类别阻塞等令牌；`l == nil` 返回 `errNotInit` |

### api.go（驱动结构与请求层）

| 类型/方法 | 说明 |
|------|------|
| `AliyundriveOpenDriver` | 驱动结构（Config/Orm/cfg/tm/driveID/limiterKey/client） |
| `newHTTPClient()` | 专用 http.Client：不设 Timeout（避免大文件传输被强断），只约束响应头等待 |
| `d.request(ctx, typ, endpoint, method, body)` | 统一请求：先过限流器；token 失效码（`AccessTokenInvalid`/`AccessTokenExpired`/`I400JD`）触发续期并重试一次 |
| `d.getDriveInfo` / `driveIDFor(info, driveType)` | 取盘信息；按 default/resource/backup 取对应 drive_id |
| `d.getSpaceInfo` | 个人空间配额 |
| `d.listFiles(ctx, parentFileID)` | 列目录（marker 游标分页，limit 200） |
| `d.createFolder(ctx, parent, name)` | 建目录（`check_name_mode=ignore`） |
| `d.getDownloadURL(ctx, fileID)` | 下载直链；LIVP 无 url 时回退 `streamsUrl` |
| `d.removeFile(ctx, fileID)` | 按 `remove_way` 走 trash 或 delete |

### driver.go（上传编排/读取/删除/目录）

| 方法 | 说明 |
|------|------|
| `d.Upload(ctx, src, opt)` | 加密准备 → 年/月目录 → pre_hash 秒传 → 分片并发 → complete |
| `d.tryCreateWithPreHash(...)` | 带 pre_hash 的 create；命中 `PreHashMatched` 则走 `retryCreateWithContentHash` |
| `d.retryCreateWithContentHash(...)` | 补完整 SHA1 + proof_code 重试；命中返回 FileRef |
| `d.Open` / `d.OpenChunk` / `d.OpenChunkRangeSeek` | `types.ChunkOpener`：download_url + HTTP Range；加密文件从头读 |
| `d.BatchDelete(ctx, refs)` | 逐个删除（接口一次仅接受一个 file_id）；单个失败标 `results[i].Error` 不影响其它 |
| `d.Close()` | 归还限流器引用（`releaseLimiter`） |
| `dirBackend`（`List`/`Mkdir`/`resolve`/`findChild`） | `types.DirBackend`：路径 `/2026/09` 逐级解析/创建为 file_id，缓存 `路径→file_id` |

### upload.go（分片与秒传算法）

| 方法 | 说明 |
|------|------|
| `calPartSize(fileSize)` | 按大小选分片（20MB 起，>128GB 递增，>1TB 用 5GB），保证 ≤10000 片；阈值同 OpenList |
| `partInfoList(count)` / `partCountOf(size, partSize)` | 生成 `part_info_list` / 计算分片数 |
| `proofRange(accessToken, size)` | `md5(token)` 前 16 位十六进制当无符号数 % size 得起点，取 8 字节（超文件尾则截断） |
| `d.calcProofCode(file, accessToken, size)` | 按 proof_range 读片段并 base64 |
| `preHashOf(file, size)` | 首 1024 字节 SHA1（秒传第一步） |
| `fullSHA1Of(file)` | 整文件 SHA1（秒传第二步的 content_hash） |
| `d.createUploadFile(...)` / `d.createByContentHash(...)` | 两种 create：前者带 pre_hash，后者带 content_hash+proof_code |
| `d.refreshUploadURL(...)` | 重新取分片直传地址（OSS 地址约 24h 有效，上传超 50 分钟续期） |
| `d.uploadPart(ctx, url, r)` | PUT 单片到 OSS；`internal_upload` 时替换为内网域名；409 视为已存在 |
| `d.uploadPartsConcurrent(...)` | 并发 PUT 全部分片（`upload_thread` 控并发，不超分片数） |
| `d.completeUpload(...)` | `complete` 合并 |

### init.go（注册与元数据）

| 方法 | 说明 |
|------|------|
| `init()` | 注册 `ALIYUNDRIVE_OPEN` 及 ConfigFields 元数据（refresh_token/api_url/client_id/client_secret/access_token/drive_type/root_folder_id/upload_thread/remove_way/rapid_upload/internal_upload） |
| `newDriver(provider)` | 构造：解析配置 → 初始化 token 管理器 → 借全局限流器 → 凭据预检（仅告警）→ warmup |
| `d.warmup()` | 续期 → 持久化 → getDriveInfo 定 drive_id → 换成按 user_id 的限流器 |
| `checkCredentials(cfg)` | refresh_token 必填；未配 api_url 时必须有 client 凭据 |

## driver/yun139/（移动云盘，仅新版个人云 personal_new）

参考 OpenList `drivers/139`。认证/签名/续期语义见 `README.md`「移动云盘驱动」与 `设计方案.md` 11.3.3。
**未实现**：家庭云/群组云/分享挂载/旧版个人云、邮箱 Cookie 登录、密码登录、SMS 验证码登录。

### auth.go（Authorization 解析与自动续期）

| 类型/方法 | 说明 |
|------|------|
| `authInfo` | 解析结果：encryptKey / account / token（第三段原文）/ expiration（毫秒） |
| `parseAuthorization(authorization)` | base64 解码 → 按 `:` 拆 ≥3 段 → 第三段按 `\|` 拆 ≥4 字段 → 取第 4 字段为过期时间；任一步不符返回错误 |
| `buildAuthorization(info, tokenSegment)` | 用新第三段重新 base64 拼装 Authorization |
| `needsRefresh(info, now)` | 剩余 ≤ 15 天（`refreshThreshold`）或已过期则需刷新 |
| `replaceTokenSegment(oldSegment, newToken, expireMillis)` | 替换第三段首字段并保留其余 `\|...`；`expireMillis>0` 时同时刷新第 4 字段。**不更新过期时间会导致每次请求都重复刷新** |
| `tokenRefresher` | `refresh(ctx, onRenewed)` 按需续期；`blockRefresh` 续期失败后置位 |
| `requestTokenRefresh(ctx, token, account)` | 调 `tellin/authTokenRefresh.do`（XML）；`return=="0"` 为成功；返回新 token 与 expiretime |
| `calSign(body, ts, randStr)` | 请求签名：URI 编码（空格 `%20`）→ 按字符排序 → base64 → md5(prev)+md5(ts+":"+rand) → md5 大写 |
| `encodeURIComponent` / `randomString` / `md5Hex` | 签名辅助（空格必须编 `%20` 而非 `+`） |
| `d.persistAuthorization(newAuth)` | 写回内存配置并经 `ConfigPersisterSetter` 持久化 |

### api.go（驱动结构与请求层）

| 类型/方法 | 说明 |
|------|------|
| `Yun139Driver` | 驱动结构（Config/Orm/refresher/cloudHost/cron/uploadThread/customPartSize/client） |
| `newHTTPClient()` | 不设 Timeout（分片直传/下载途中会被强断），只约束响应头等待 |
| `setPersonalHeaders(req, auth, ts, rand, sign)` | 固定请求头 + `Mcloud-Sign` |
| `d.post(ctx, endpoint, body, out)` | 序列化 body → `doRequest` → 反序列化到 out |
| `d.doRequest(...)` | 请求前 `ensureAuthorization`；`authFailed` 时 `forceRefresh` 后重试一次 |
| `d.sendOnce(...)` | 返回 `(respBody, authFailed, err)`；**鉴权失败返回 authFailed=true 而非 error**，以便上层区分「可恢复的鉴权失败」与传输错误 |
| `isAuthError(respBody)` | code 含 AUTH/TOKEN/UNAUTHORIZED/FORBIDDEN/EXPIRE 判定为鉴权失败 |
| `d.ensureAuthorization` / `d.forceRefresh` / `d.currentAuthorization` / `d.account` | 续期与凭据访问 |
| `d.listFiles(ctx, parentFileID)` | `/file/list` 游标分页（pageSize 100） |
| `d.getDownloadURL(ctx, fileID)` | `/file/getDownloadUrl`，兼容 data.url 与顶层 url |

### driver.go（上传编排/读取/删除/目录）

| 方法 | 说明 |
|------|------|
| `d.Upload(ctx, src, opt)` | 加密准备 → 年/月目录 → SHA256 → `uploadFile` |
| `d.uploadFile(...)` | create → exist/无分片即秒传 → 上传首批 → >100 片补取地址并上传 → complete |
| `d.rootID()` | 配置 root_id，默认 `/` |
| `d.createFolder(ctx, parent, name)` | 移动云无独立 mkdir，用 create 上传 `type=folder` |
| `d.Open` / `d.OpenChunk` / `d.OpenChunkRangeSeek` | `types.ChunkOpener`：download_url + HTTP Range；加密文件从头读 |
| `d.BatchDelete(ctx, refs)` | 逐个 `/file/trash`；单个失败标 `results[i].Error` 不影响其它 |
| `d.removeFile(ctx, fileID)` | 移入回收站 |
| `d.Close()` | **停止 12 小时刷新 cron**（由 `internal/storage.closeAndDeleteStorage` 在 provider 更新/删除/禁用时调用） |
| `dirBackend`（`List`/`Mkdir`/`resolve`/`findChild`） | `types.DirBackend`：路径逐级解析/创建为 fileId，缓存 `路径→fileId` |

### upload.go（分片与秒传）

| 方法 | 说明 |
|------|------|
| `d.partSizeFor(size)` | 自动分片 1-8MB（随文件增大），`custom_upload_part_size` 优先 |
| `buildPartInfos(size, partSize)` | 生成 partInfos（含 `parallelHashCtx.partOffset`），末片取实际剩余字节 |
| `computeSHA256(file)` | 整文件 SHA256（移动云秒传必需） |
| `d.createUpload(...)` | `/file/create`，`partInfos` 首批最多 100 片，`fileRenameMode=auto_rename` |
| `d.getMorePartURLs(...)` | `/file/getUploadUrl` 按批（每批 100 片）补取直传地址 |
| `d.uploadPart(ctx, url, file, offset, size)` | PUT 单片（SectionReader + Content-Length + Origin/Referer） |
| `d.uploadPartInfos(...)` | 按 partNumber 升序与 allParts 对齐后并发上传（`upload_thread` 控并发） |
| `d.completeUpload(...)` | `/file/complete` 合并 |

### init.go（注册与元数据）

| 方法 | 说明 |
|------|------|
| `init()` | 注册 `YUN139` 及 ConfigFields 元数据（authorization/root_id/custom_upload_part_size/upload_thread） |
| `newDriver(provider)` | 构造：解析配置 → 初始化 refresher → warmup（失败仅告警）→ 启动 cron |
| `d.warmup()` | 校验/续期 Authorization → 列根目录验证凭据 → 记账号 |
| `d.startRefreshCron()` / `d.cronRefresh()` | 每 12 小时（`0 0 */12 * * *`）主动刷新一次 |
| `parseUploadThread(s)` | 1-32，越界/非法回落 3 |

## driver/cloud189pc/（天翼云盘，仅个人云）

参考 OpenList `drivers/189pc`。认证/签名语义见 `README.md`「天翼云盘驱动」与 `设计方案.md` 11.3.4。
**未实现**：家庭云、账号密码+OCR 登录、二维码登录、torrent/CAS 秒传、老版上传。

### help.go（签名/加密/分片算法）

| 函数 | 说明 |
|------|------|
| `signatureOfHmac(secret, key, operate, fullURL, date, param)` | HMAC-SHA1 签名：`SessionKey=..&Operate=..&RequestURI=..&Date=..[&params=..]`，大写 hex |
| `extractURLPath(fullURL)` | 正则 `://[^/]+((/[^/\s?#]+)*)` 提取 path（签名只用 path，不含 host） |
| `aesECBEncrypt(data, key)` | PKCS#7 填充 + 分块 ECB + 大写 hex（params 用，key=sessionSecret 前 16 字节） |
| `pkcs7Padding` | 整块时也补满一块 |
| `parseHTTPHeader(str)` | 解析 `k1:v1,k2:v2` 或换行分隔的分片直传请求头 |
| `partSize(size)` | 10MiB 起；>999 片用 20MiB；超大按 `ceil(size/1999/10MiB)` 放大（保底 5 倍） |
| `clientSuffix()` | 固定查询参数（clientType/version/channelId/rand） |
| `httpDateStr()` / `newRequestID()` / `md5UpperHex()` / `urlEscape` / `formatDate` | 辅助 |
| `rsaEncrypt` | RSA 加密（账号密码登录用；本驱动未实现登录，保留以便扩展） |

### auth.go（token 两段式续期）

| 类型/方法 | 说明 |
|------|------|
| `tokenManager` | accessToken/refreshToken/sessionKey/sessionSecret 管理；`blockRefresh` 续期失败置位 |
| `initTokenManager(access, refresh)` | 初始化 |
| `tm.ensureToken(ctx, d)` | 无 session 且有 refreshToken 时取一次 |
| `tm.invalidate()` | 标记 session 失效 |
| `tm.refreshSessionWithRetry(ctx, d, n)` | 先用现 accessToken 刷 session；`UserInvalidOpenToken` 则先换 token 再刷，**最多 3 次** |
| `tm.refreshTokenWithRetry(ctx, d, n)` | 调 refreshToken.action 换新 token；超限清空 refreshToken 并报错 |
| `tm.applyTokens` / `tm.applySessionKeys` | **分开落库**：刷 session 路径只写 session 密钥，避免覆盖刚轮换的 refreshToken |
| `d.fetchSession(ctx, accessToken)` | getSessionForPC.action 取 session |
| `d.exchangeRefreshToken(ctx, refreshToken)` | refreshToken.action 换 token |
| `d.warmup()` | 取 token/session + 读容量信息验证凭据（失败仅告警） |
| `d.persistTokens()` | 写回内存配置并经 `ConfigPersisterSetter` 持久化 |
| `isTokenInvalidError` | 判定 `UserInvalidOpenToken` |

### api.go（驱动结构与请求层）

| 类型/方法 | 说明 |
|------|------|
| `Cloud189PcDriver` | 驱动结构（Config/Orm/tm/uploadThread/orderBy/orderDir/rootFolderID/client） |
| `newHTTPClient()` | 不设 Timeout（直传/下载途中会被强断），只约束响应头等待 |
| `d.request(...)` | 序列化 → `doRequest` → 反序列化 |
| `d.doRequest(...)` | 请求前 `ensureToken`；`sessionDead` 时 `invalidate` + `refreshSessionWithRetry` 后重试一次 |
| `d.sendOnce(...)` | 返回 `(respBody, sessionDead, err)`；**session 失效返回标志而非 error**，让上层区分可恢复失败与传输错误 |
| `d.rawGet` / `d.rawPostForm` | 无签名请求（登录相关接口） |
| `isSessionInvalid` / `resCodeOK` / `RespErr.HasError` | 失效识别与 `res_code` 判定（数字 0 为成功，与 OpenList types.go:29 一致） |
| `d.listFiles` | listFiles.action 分页（pageSize 1000） |
| `d.createFolder` / `d.getDownloadURL` / `d.getCapacityInfo` | 建目录（`&amp;`→`&`、http→https 修正）/ 下载直链 / 容量 |

### driver.go（上传编排/读取/删除/目录）

| 方法 | 说明 |
|------|------|
| `d.Upload(ctx, src, opt)` | 加密准备 → 年/月目录 → `uploadFile` |
| `d.uploadFile(...)` | 分片 MD5 → initMultiUpload → 并发上传 → commitMultiUploadFile |
| `d.Open` / `d.OpenChunk` / `d.OpenChunkRangeSeek` | `types.ChunkOpener`：download_url + HTTP Range；加密文件从头读 |
| `d.BatchDelete` | 逐个走批量任务删除；单个失败标 `results[i].Error` |
| `d.deleteFile` / `d.waitBatchTask` / `buildBatchTaskForm` | createBatchTask + 轮询 checkBatchTask（300ms 间隔、60s 超时；status 4 成功 / 2 冲突） |
| `d.Close()` | 无长驻资源，对齐 `types.Closer` |
| `dirBackend`（`List`/`Mkdir`/`resolve`/`findChild`） | `types.DirBackend`：路径逐级解析/创建为 fileId |

### upload.go（分片上传）

| 方法 | 说明 |
|------|------|
| `d.initMultiUpload(...)` | params 含 parentFolderId/fileName(转义)/fileSize/sliceSize/lazyCheck |
| `d.getMultiUploadURLs(...)` | `partInfo` 逗号分隔；返回按 partNumber 升序的直传地址 |
| `computePartDescriptors` | 各分片 MD5（大写 hex + base64）、偏移与大小 |
| `hashWholeFile` / `sliceMD5` | 整文件 MD5；单分片直接用、多分片换行拼接后再 MD5 |
| `partInfoString` | `{分片号}-{base64(分片MD5)}` |
| `d.uploadPart` / `d.uploadPartsConcurrent` | PUT 单片 / 并发上传（`upload_thread` 控并发，逐片取地址） |
| `d.commitUpload(...)` | commitMultiUploadFile（fileMd5/sliceMd5/opertype=1） |

### init.go（注册与元数据）

| 方法 | 说明 |
|------|------|
| `init()` | 注册 `CLOUD189_PC` 及 ConfigFields（refresh_token/access_token/root_id/order_by/order_direction/upload_thread） |
| `newDriver(provider)` | 构造：解析配置 → 初始化 token 管理器 → warmup（失败仅告警） |
| `parseUploadThread` / `parseOrderBy` / `parseOrderDirection` / `parseRootID` | 配置边界收敛（越界回落默认值） |

## server/（REST 传输层）

### route_rest.go（REST 分发器）

| 类型/方法 | 说明 |
|------|------|
| `HandlerFunc[TReq proto.Message]` | 业务处理器签名：`func(ctx, TReq) (*native.BaseResp, error)` |
| `Register[TReq](mux, method, path, fn)` | 注册一条 REST 路由；泛型从零值经 protobuf 反射构造请求实例 |
| `serveREST` / `decodeRequest` / `decodeQuery` | 反序列化（POST 取 JSON body、GET/DELETE 取 query）→ 校验 → 调用 → 序列化 |
| `callHandler` | 调用业务函数并 recover panic（转 INTERNAL_ERROR，HTTP 仍 200） |
| `writeProtoJSON` / `writeProtoError` | proto3 JSON 响应输出（camelCase、EmitUnpopulated，Any 自动展开） |

### route_table.go（路由表，显式维护）

| 方法 | 说明 |
|------|------|
| `registerRESTServices(mux)` | 注册全部业务路由的总入口 |
| `registerFileRoutes` / `registerUploadRoutes` / `registerProviderRoutes` / `registerBackupRoutes` / `registerUserSpaceRoutes` / `registerDownloadRoutes` | 各服务分组路由注册（共 67 条，路径形如 `/api/file/list`） |

### middleware_rest.go / validate.go

| 方法 | 说明 |
|------|------|
| `restAuthMiddleware(next)` | REST 业务路由鉴权（等价原 connect AuthInterceptor；含 Debug 绕过；失败返回 TOKEN_INVALID） |
| `validateMessage(msg)` | 按 proto 的 `buf.validate.field` 注解校验请求（等价原 validate 拦截器） |

### server/middleware/log.go（请求日志 + panic 恢复）

| 方法 | 说明 |
|------|------|
| `HTTPLogMiddleware(next)` | 合并中间件（替代 connect GInterceptor）：生成 uuid 注入 ctx（`globals.ContextRequestIDKey`）+ 回写响应头 `X-Request-Id`；记录「request received / request completed」两条 Info 日志（method/path/uuid/status/size/cost）；panic 时同样记录完成日志并转为 `BaseResp(INTERNAL_ERROR)` + HTTP 500 |
| `statusRecorder` | 包装 ResponseWriter 捕获状态码与响应字节数（不改变对外行为） |

### internal/llog（请求级日志关联）

| 方法 | 说明 |
|------|------|
| `llog.C(ctx) *logrus.Entry` | 若 ctx 含 request_id 则自动携带该字段（`llog.C(ctx).Errorf(...)`），使日志可按请求关联；无则等价于 `logrus.NewEntry(L())` |
| `llog.CtxRequestID(ctx) string` | 从 ctx 取请求 ID |

注：原 connect 拦截器（`GInterceptor` / `AuthInterceptor` / `PermInterceptor` / `RateLimitInterceptor`）已随传输层改造移除；panic 恢复与请求日志由 `HTTPLogMiddleware` 承担，限流由 `middleware.RateLimitHTTPMiddleware` 承担。

## utils/

| 方法 | 说明 |
|------|------|
| `MergedFileName(fileName) string` | 分片合并产物文件名（`merged_<name>`）；写入方 `service.processCompleteUpload` 与读取方 `service.tryServeFromLocalTemp` 共用，避免两侧字符串漂移 |
| `RemoveChunkTempFiles(fileHash, ownerID) error` | 只删 owner 隔离目录下的分片 `chunk_*`，**保留合并明文 `merged_<name>`**（异步分发读的源，整目录删除会让 Dispatch 报 `temp file not found`）；目录不存在视为无事发生。整目录清理由 dispatch 终态的 `cleanupUploadTemp` 负责 |

