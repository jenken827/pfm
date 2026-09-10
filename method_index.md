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

