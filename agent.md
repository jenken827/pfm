# AI Agent 开发手册

本文件面向进入本仓库工作的任何 AI agent。目标不是解释所有历史背景，而是让你在最短时间内知道：

- 这个项目是什么
- 改动应该落在哪一层
- 必须遵守哪些开发规范
- 常用命令是什么

## 1. 项目一句话说明

这是一个"独立维护文件元数据 + 多存储提供者副本分发"的个人文件管理系统。

- 后端：Go
- 前端：Flutter
- 协议契约：Protobuf
- 存储提供者：当前内置 `local`、`telegram`、`pan123`

关键事实：

- 没有存储组 `group`
- 目录通过 `bound_provider_ids` 决定文件落盘目标
- 文件内容和文件元数据分离
- 同一内容可在多个 provider 上有副本

## 2. 进入任务后的第一步

先做这几件事，不要直接写代码：

1. 看 `git status --short`
2. 看当前任务涉及的 `proto`
3. 看对应的后端 `api -> service -> db/storage/driver`
4. 看对应的前端 `FmRepository -> Bloc -> Page`

建议起手命令：

```bash
git status --short
git ls-files
```

## 3. 目录职责

- `main.go`
  - 启动入口
- `bootstrap/`
  - 初始化配置、日志、Casdoor、数据库、下载器、存储实例
- `server/`
  - 路由与中间件
- `api/`
  - Connect handler，薄适配层
- `internal/service/`
  - 业务逻辑主层
- `internal/db/`
  - 数据访问层
- `internal/storage/`
  - 上传调度、同步、源选择、恢复任务
- `driver/`
  - 存储驱动实现
- `proto/`
  - 契约定义与代码生成配置
- `frontend/`
  - Flutter 客户端

## 4. 必须遵守的总规则

### 4.1 Proto 优先

新增字段、新增接口、新增枚举时，优先修改 `proto/`，不要先在前后端各自手写结构。

改完 proto 后必须同时生成：

- Go 代码
- Dart 代码

命令：

```bash
bash build.sh proto          # 同时生成 Go + Dart
bash build.sh proto-go       # 仅生成 Go
bash build.sh proto-dart     # 仅生成 Dart
```

### 4.2 前后端链路一起检查

只改一侧通常是不够的。涉及接口变更时，至少检查：

1. `proto`
2. 后端 `api`
3. 后端 `service`
4. 后端 `db` / `storage` / `driver`
5. 前端 `FmRepository`
6. 前端 `Bloc`
7. 前端页面或组件

### 4.3 前端 JSON 规范

前端与后端交互时：

- 请求使用 `toProto3Json()`
- 响应使用 `mergeFromProto3Json` 或项目已有解包辅助

不要自己重新定义一套 `toJson/fromJson` 契约。

### 4.4 Flutter 命令规范

不要直接使用系统 `flutter` / `dart`。

统一使用：

- `puro flutter`
- `puro dart`

### 4.5 文档与设计以代码为准

如果 README、设计文档、旧注释和实际代码不一致，以当前代码为准，并在提交中顺手修正文档。

### 4.6 GORM First 返回值判空规则

`gorm.G[T].First(ctx)` 在记录不存在时，返回的**第一个参数不是 nil**，而是一个零值指针，同时 error 为 `gorm.ErrRecordNotFound`。因此：

- **禁止**用 `result == nil` 判断记录是否存在
- **必须**通过 `err` 判断：`err == nil` 表示找到记录，`err == gorm.ErrRecordNotFound` 表示未找到

正确模式（db 层）：
```go
func GetXxxByID(id uint64) (*XxxORM, error) {
    record, err := gorm.G[*XxxORM](db).Where("id = ?", id).First(ctx)
    if err != nil {
        return nil, err  // 确保 err 时返回 nil，避免调用方误判
    }
    return record, nil
}
```

正确模式（调用方）：
```go
record, err := db.GetXxxByID(id)
if err == gorm.ErrRecordNotFound {
    // 记录不存在
} else if err != nil {
    // 其他错误
} else {
    // 记录存在，使用 record
}
```

如果 db 层函数已在 err 时返回 nil（如上），调用方也可以用 `record != nil` 判断，但优先使用 err 判断。

### 4.7 方法长度限制

每个方法的代码量不能过大：50行以内最好，最多100行。如果超过了，需要将里面的逻辑封装成单独的方法，以方便后续复用和测试。

拆分原则：

- 提取可独立复用的逻辑块为单独方法
- 提取条件判断/状态处理为独立方法
- 方法命名应准确描述其职责，避免 `handleLogic1` 这类无意义命名

### 4.8 命令必须通过 build.sh 执行

所有构建、生成、测试等命令必须通过 `build.sh` 执行，禁止直接运行 `go build`、`go test`、`buf generate` 等裸命令。原因：

- `build.sh build` 会自动注入 git 版本号
- `build.sh proto` 确保按正确顺序生成 Go + Dart
- 统一入口避免环境不一致、参数遗漏等常见错误

```bash
# 后端（项目根目录）
bash build.sh build          # 编译（不是 go build）
bash build.sh run            # 运行（不是 go run）
bash build.sh test           # 测试（不是 go test）
bash build.sh proto          # 生成 proto（不是 cd proto && buf generate）

# 前端（frontend 目录）
cd frontend
bash build.sh build windows  # 构建（不是 flutter build windows）
```

## 5. 后端开发规则

## 5.1 分层要求

后端改动优先遵循这条路径：

`server -> api -> service -> db/storage -> driver`

各层职责：

- `server`
  - 路由、中间件、HTTP 服务
- `api`
  - 参数接入、调用 service、包装 `BaseResp`
- `service`
  - 业务逻辑
- `db`
  - 数据读写
- `storage`
  - 调度与副本逻辑
- `driver`
  - 存储介质实现

不要把复杂业务逻辑塞进 `api` 或 `db`。

## 5.2 API 真实形态

后端基于 ConnectRPC + Vanguard：

- Connect/JSON 路径示例：
  - `/native.FileService/GetFileList`
- REST 路径示例：
  - `/api/file/list`

注意：

- 不是所有 RPC 都自动有 REST 路径
- 只有在 `.proto` 中声明了 `google.api.http` 的方法才有

## 5.3 认证事实

大多数后端接口都要求 Bearer Token。

- RPC 走 `AuthInterceptor`
- 文件流接口走 `AuthHTTPMiddleware`
- 唯一例外是公开分享下载 `/s/{shareCode}`

不要实现无鉴权的新敏感接口，除非任务明确要求。

## 5.4 存储规则

当前真实规则：

- 目录通过 `bound_provider_ids` 绑定 provider
- 文件继承父目录绑定关系
- 源副本选择按 `sort_order` 升序
- 本地驱动当前忽略 encryptor，始终明文存储
- Telegram 大文件有预下载缓存逻辑

不要在代码里继续引入"存储组 group"概念。

## 6. 前端开发规则

## 6.1 路由

路由必须遵循 `go_router` Type-Safe Routes 现有模式。

相关目录：

- `frontend/lib/router/`

新增页面时优先复用现有路由风格，不要临时手搓另一套路由方案。

## 6.2 状态管理

当前状态管理使用 `flutter_bloc`。

典型链路：

`UI -> Event -> Bloc -> FmRepository -> 后端 -> Bloc State -> UI`

不要在新功能里绕开 `FmRepository` 直接到处发请求。

## 6.3 表单与 UI

优先遵循现有 Material 设计风格与组件拆分方式。

有表单需求时优先看是否已经在用：

- `flutter_form_builder`
- `form_builder_validators`

## 6.4 本地任务

上传下载任务不是简单页面状态，它们会进入本地后台任务系统：

- 桌面端：Isolate
- 移动端：WorkManager

涉及传输功能时，务必同时检查：

- `frontend/lib/services/background_service.dart`
- `frontend/lib/blocs/transfer_bloc/`

## 7. 常用命令

> **重要**：所有命令必须通过 `build.sh` 执行，不要直接运行 `go build`、`buf generate` 等。这确保版本号注入、环境一致性等。

### 7.1 后端（项目根目录）

```bash
bash build.sh build          # 编译 Go 服务器（自动注入版本号）
bash build.sh run            # 编译并运行服务器
bash build.sh test           # 运行所有测试
bash build.sh test-cover     # 运行测试并生成覆盖率报告
bash build.sh tidy           # 执行 go mod tidy
bash build.sh lint           # 运行 golangci-lint 代码检查
bash build.sh clean          # 清理构建产物
```

### 7.2 Proto（项目根目录）

```bash
bash build.sh proto          # 同时生成 Go + Dart protobuf 代码
bash build.sh proto-go       # 仅生成 Go protobuf 代码
bash build.sh proto-dart     # 仅生成 Dart protobuf 代码
```

### 7.3 前端（frontend 目录）

```bash
cd frontend
bash build.sh get            # 拉依赖
bash build.sh gen            # 代码生成（build_runner）
bash build.sh analyze        # 静态检查
bash build.sh test           # 运行测试
bash build.sh run windows    # 运行桌面端
bash build.sh build windows  # 构建桌面端
```

### 7.4 常用排查

```bash
git status --short
git diff --stat
git diff
```

## 8. 改动前的判断规则

### 如果任务是"新增字段"

顺序：

1. 改 proto
2. 生成 Go / Dart
3. 改后端返回
4. 改前端解析和展示
5. 自查接口链路

### 如果任务是"新增后端接口"

顺序：

1. 改 proto service
2. 生成代码
3. 实现 `api`
4. 实现 `service`
5. 必要时改 `db` / `storage` / `driver`
6. 接入前端 `FmRepository`

### 如果任务是"新增存储 provider"

顺序：

1. 在 `enum.proto` 增加 provider 类型
2. 定义 / 复用 provider config proto
3. 生成代码
4. 在 `driver/<provider>/` 实现驱动
5. 注册 `RegisterStorageDriverWithMetadata`
6. 确保前端设置页能拿到元数据并渲染

### 如果任务是"上传 / 下载 / 同步问题"

优先看：

- `internal/service/upload.go`
- `internal/storage/dispatch.go`
- `internal/storage/helper.go`
- 对应 driver 的 `Upload/Open/PreDownload`
- 前端 `BackgroundService` 与 `TransferBloc`

## 9. 提交前自检清单

在结束任务前，至少确认：

- proto 是否需要同步生成
- 前后端字段是否一致
- 新逻辑是否写错层
- 日志是否覆盖关键错误场景
- 文档是否需要同步更新
- 是否使用了正确命令：
  - 后端 `go`
  - 前端 `puro flutter` / `puro dart`
  - proto `buf`

## 10. 当前代码事实，不要误判

- 没有存储组 `group`
- 没有完整全文搜索
- 没有 Swagger / OpenAPI 页面
- 没有健康检查端点
- 前端状态管理不是 Provider / Riverpod
- 源副本选择规则当前是 `sort_order`，不是"本地优先 / 未加密优先"

## 11. 最后建议

做任何中等以上改动时，都把问题看成一条完整链路，而不是单文件补丁：

- 协议
- 后端接入
- 后端业务
- 数据与调度
- 前端请求
- 前端状态
- UI 展示

这个项目的耦合点不在"目录结构混乱"，而在"同一功能横跨前后端与调度层"。沿着链路改，成功率最高。

## 12. 关键方法索引

新增功能前，先查阅此索引确认方法是否已存在，避免重复实现。

### 12.1 `internal/db/` — 数据访问层

**File 相关**
- `GetFileByID(id)` — 按ID获取文件
- `GetFilesByIDs(ids)` — 批量获取文件
- `GetFilesByIDsUnscoped(ids)` — 批量获取文件（含软删除）
- `GetFileByHash(hash)` — 按BLAKE3哈希获取文件
- `GetFileByParentAndName(parentID, name)` — 按父目录+文件名获取
- `CreateFile(file)` — 创建文件（自动生成UUID）
- `UpdateFile(file, fields)` — 更新文件指定字段
- `UpdateFileWithHistory(ctx, file, oldFile)` — 更新文件并记录历史版本
- `UpdateFileIsReady(parentID, fileName)` — 标记文件就绪
- `DeleteFiles(ids)` — 软删除（移至回收站）
- `RestoreFromTrash(ids)` — 从回收站恢复
- `DeletePermanently(ids)` — 永久删除
- `GetTrashList(offset, pageSize)` — 获取回收站列表
- `GetAllChildrenIDs(parentIDs)` — 递归获取子文件ID（BFS）
- `EmptyTrash()` — 清空回收站
- `GetFileList(parentID, offset, pageSize)` — 分页获取文件列表
- `GetFolderList(parentID)` — 获取子文件夹列表
- `UpdateDir(orm)` — 更新文件夹配置
- `RenameFiles(renames)` — 批量重命名
- `UpdateFileLocationStatus(ids, status)` — 批量更新位置记录状态

**FileLocation 相关**
- `CreateFileLocation(fl)` — 创建文件位置记录
- `UpdateFileLocation(fl, fields)` — 更新文件位置指定字段
- `GetFileLocationByProviderAndHash(providerID, hash)` — 按provider+hash获取单条
- `GetFileLocationByFileHashAndProvider(hash, providerID)` — 同上（别名）
- `GetFileLocationByID(id)` — 按ID获取
- `GetFileLocationsByBlake3Hash(hash)` — 按hash获取所有位置
- `GetFileLocationsByFileHash(hash)` — 同上（按provider_id排序）
- `GetFileLocationsByProviderAndHash(providerIDs, hash)` — �多个provider+hash获取
- `UpdateFileLocationCacheStatus(id, status)` — 更新缓存状态
- `UpdateFileLocationCache(id, path, status)` — 更新缓存路径和状态
- `DeleteFileLocation(id)` — 删除单条
- `DeleteFileLocations(ids)` — 批量删除
- `DeleteFileLocationsByBlake3Hash(hash)` — 按hash删除所有位置

**FileHistory 相关**
- `GetFileHistoryByFileID(fileID)` — 按文件ID获取历史
- `GetFileHistoriesByFileID(fileID)` — 同上（排序版）
- `GetFileHistoryByID(id)` — 按ID获取
- `DeleteFileHistoryByFileIDs(fileIDs)` — 按文件ID批量删除历史
- `DeleteFileHistoryByIDs(ids)` — 按历史ID批量删除

**StorageProvider 相关**
- `CreateStorageProvider(provider)` — 创建
- `UpdateStorageProvider(provider)` — 更新
- `DeleteStorageProvider(ids)` — 删除（同时删除关联FileLocation）
- `GetProviderByID(id)` — 按ID获取
- `GetAllProviders()` — 获取全部
- `GetEnabledProviders()` — 获取已启用
- `GetEnabledProvidersByIds(ids)` — 按ID获取已启用
- `GetDefaultBoundEnabledProviders()` — 获取默认绑定且启用
- `GetStorageProvidersByIDs(ids)` — 批量查询
- `GetAvailableProvidersForSync(fileHash)` — 获取可同步的provider
- `UpdateStorageProviderConfig(id, config)` — 更新配置
- `IncrementUsedCapacity(providerID, size)` — 原子增加已用容量
- `DecrementUsedCapacity(providerID, size)` — 原子减少已用容量

**UploadRecord 相关**
- `GetUploadRecordByID(id, withChunk)` — 按ID获取
- `GetUploadRecordByHash(hash, withChunk)` — 按hash获取
- `GetUploadRecordsByStatus(statuses)` — 按状态获取
- `GetChunkRecord(uploadID, chunkIndex)` — 获取分片记录
- `CreateUploadRecord(record)` — 创建
- `UpdateUploadRecord(record, fields)` — 更新指定字段
- `ClearUploadRecords(id)` — 删除记录及分片
- `ListUploadRecords(page, pageSize, status)` — 分页查询

**FileShare 相关**
- `CreateFileShare(fs)` — 创建分享
- `GetFileShareByCode(shareCode)` — 按分享码获取
- `GetFileShareByID(id)` — 按ID获取
- `GetFileSharesByFileID(fileID)` — 按文件ID获取
- `UpdateFileShare(fs, fields)` — 更新
- `IncrementShareAccessCount(id)` — 原子增加访问次数
- `CancelFileShare(id)` — 取消分享
- `ExpireActiveShares()` — 标记过期分享

**FileTag 相关**
- `CreateFileTags(fileID, tags)` — 创建标签
- `CreateFileTagsTx(tx, fileID, tags)` — 事务版
- `DeleteFileTagsByFileID(fileID)` — 删除文件所有标签
- `DeleteFileTagsByFileIDTx(tx, fileID)` — 事务版
- `GetFileTagsByFileID(fileID)` — 获取文件标签

**公共辅助**
- `selectFields(fields)` — GORM Select 字段展开辅助

### 12.2 `internal/service/` — 业务逻辑层

**文件操作**
- `GetFileList(ctx, req)` — 文件列表
- `GetFolderList(ctx, req)` — 文件夹列表
- `CreateDir(ctx, req)` — 创建目录
- `UpdateDir(ctx, req)` — 更新目录
- `DeleteFile(ctx, req)` — 删除文件（软删除）
- `RenameFile(ctx, req)` — 重命名
- `MoveFile(ctx, req)` — 移动文件
- `GetFileDetail(ctx, req)` — 文件详情（含位置、历史、标签）
- `GetFileCacheStatus(ctx, req)` — 缓存状态
- `SyncFileToProviders(ctx, req)` — 同步到指定provider
- `GetAvailableProvidersForSync(ctx, fileHash)` — 可用同步目标
- `RestoreFromTrash(ctx, req)` — 从回收站恢复
- `DeletePermanently(ctx, req)` — 永久删除
- `EmptyTrash(ctx, req)` — 清空回收站
- `GetTrashList(ctx)` — 回收站列表
- `DeleteFileHistory(ctx, ids)` — 删除历史版本
- `UpdateFileTags(ctx, req)` — 更新标签
- `GetFileTags(ctx, req)` — 获取标签

**上传操作**
- `UploadSmall(ctx, req)` — 小文件上传
- `InitUpload(ctx, req)` — 初始化大文件上传
- `UploadChunkSingle(ctx, req)` — 上传分片
- `CompleteUpload(ctx, req)` — 完成上传
- `CancelUpload(ctx, req)` — 取消上传
- `ListUploadRecords(ctx, req)` — 上传记录列表
- `GetUploadRecordDetail(ctx, req)` — 上传记录详情
- `RetryUploadRecord(ctx, req)` — 重试上传
- `DeleteUploadRecord(ctx, req)` — 删除上传记录

**分享操作**
- `CreateShareLink(ctx, req)` — 创建分享
- `CancelShareLink(ctx, req)` — 取消分享

**Provider操作**
- `GetStorageProviderTypes(ctx, req)` — 获取类型列表
- `AddStorageProvider(ctx, req)` — 添加
- `GetStorageProvider(ctx, req)` — 详情
- `ListStorageProviders(ctx)` — 列表
- `UpdateStorageProvider(ctx, req)` — 更新
- `DeleteStorageProvider(ctx, req)` — 删除

**内部辅助**
- `getInheritedProviderIds(parentID)` — 获取继承的provider列表
- `validateAndGetParent(parentID)` — 验证父目录并获取provider
- `checkFileExists(parentID, fileName, fileHash)` — 检查文件存在性
- `handleInstantUpload(...)` — 秒传逻辑
- `handleOverwriteFile(ctx, fileRecord, overwriteID, cleanupPath)` — 覆盖/新建文件
- `checkSameNameFile(sameNameFile, fileName, overwrite)` — 同名文件检查
- `createSyncTask(...)` — 创建同步任务
- `batchDeleteFromProvider(ctx, providerID, locations)` — 从provider批量删除
- `deleteFilesFromProviders(ctx, fileIDs)` — 从所有provider删除文件
- `deleteFileHistories(ctx, fileIDs)` — 删除文件历史
- `parseAndCreateTags(fileID, tagsStr)` — 解析并创建标签
- `parseProviderIDs(boundProviderIDs)` — 解析provider ID列表
- `getFileType(path, filename)` — 检测文件类型
- `validateSmallFileContent(content, hash, size)` — 验证小文件内容hash和大小
- `writeSmallFileTemp(hash, name, content)` — 将小文件写入临时文件
- `createSmallFileRecord(ctx, req, hash, tempPath, overwriteID)` — 创建小文件记录
- `smallFileUploadToProviders(hash, req, parentFile, tempPath, overwriteID)` — 小文件触发后台上传
- `resumeExistingUpload(record, chunks, hash)` — 处理断点续传
- `createNewUploadTask(req, hash, parentFile, sameNameFile)` — 创建新上传任务

### 12.3 `internal/storage/` — 调度层

- `Init()` — 初始化所有已启用provider并恢复未完成任务
- `GetStorage(id)` — 获取驱动实例
- `GetStorages()` — 获取所有驱动实例
- `AddStorage(sp)` — 添加驱动实例（异步）
- `UpdateStorage(sp)` — 更新驱动实例（异步）
- `DeleteStorage(ids)` — 删除驱动实例
- `CloseAll()` — 关闭所有驱动
- `Dispatch(ctx, ur)` — 上传调度入口（核心）
- `dispatchToProviders(ctx, ur, targetProviderIds, isSyncTask)` — 分发到各存储提供者
- `uploadFromTempFile(ctx, ur, needUploadProviders)` — 从临时文件上传
- `syncFromExistingProviders(ctx, ur, needUploadProviders, existingLocations)` — 从现有副本同步
- `batchUploadFromSource(ctx, ur, targetProviderIds, sourcePath)` — 批量上传到多个目标提供者
- `uploadToProvider(ctx, sourcePath, fileName, providerID, fl, fileEncrypt)` — 上传到单个提供者
- `failFileLocation(fl, msg)` — 标记文件位置为失败
- `prepareEncryptor(fileEncrypt, orm, fl, providerID, fileName)` — 生成加密器
- `saveUploadChunks(fl, pinfo, providerID)` — 保存分片上传结果
- `updateFirstChunkLocation(fl, chunkInfo, chunkTotal, chunkNumber)` — 更新首分片记录
- `createChunkLocation(fl, chunkInfo, chunkTotal, chunkNumber)` — 创建后续分片记录
- `getUploadContext(ur)` — 获取上传上下文（文件名、父目录、加密设置）
- `uploadToProviderAsync(ctx, ur, sourcePath, fileName, parentID, fileEncrypt, tid)` — 异步上传到单个提供者
- `finalizeBatchUpload(ur, failedCount, errorMessages)` — 完成批量上传处理
- `getOrCreateFileLocation(providerID, blake3Hash)` — 获取或创建文件位置记录
- `completeUpload(ur)` — 完成上传，清理临时文件
- `markUploadFailed(ctx, ur, reason)` — 标记上传失败
- `markUploadUploading(ctx, ur)` — 标记上传中
- `acquireLock(key)` / `releaseLock(key)` — 并发控制锁
- `resumeDispatch()` — 恢复分发（系统启动时）
- `SelectBestSourceProvider(locations)` — 选择最优源provider
- `DownloadAndMergeFile(ctx, storage, locations, encryptor, hash)` — 下载合并文件
- `GroupLocationsByProvider(locations)` — 按provider分组
- `CheckProviderComplete(locations)` — 检查分片完整性
- `GetCompleteProviders(locations)` — 获取完整副本的provider
- `BuildCompleteProviderMap(locations)` — 构建完整provider映射
- `NewCryptEncryptor(password, salt)` — 创建加密器
- `GetEncryptor(encryptionKey, fileSalt)` — 获取加密器（可能返回NullEncryptor）

### 12.4 `driver/types/` — 驱动接口层

- `StorageDriver` — 核心接口：`Upload`, `Open`, `GetType`, `GetName`, `GetConfig`, `GetOrm`
- `BatchDeleter` — 可选接口：`BatchDelete`
- `PreDownloader` — 可选接口：`PreDownload`
- `Closer` — 可选接口：`Close`
- `Encryptor` — 加密接口：`Encrypt`, `Decrypt`, `IsEncrypted`
- `NullEncryptor` — 空加密器
- `RegisterStorageDriverWithMetadata(constructor, metadata)` — 注册驱动
- `GetDriverMetadata(type)` / `GetAllDriverMetadata()` — 获取驱动元数据
- `GetDriverConstructor(type)` — 获取驱动构造函数

## 13. 文档维护规则

每次改动代码后，必须检查以下文档是否需要同步更新：

1. **`README.md`** — 如果改动涉及：
   - 新增/删除 API 接口或路由
   - 新增/删除存储驱动
   - 配置项变更
   - 运行方式变更
   - 已实现能力列表变化

2. **`设计方案.md`** — 如果改动涉及：
   - 数据模型变更（新增字段、新增表）
   - 架构分层变化
   - 核心流程变化（上传、同步、调度、加密等）
   - 协议暴露方式变化
   - 命名或规则变化

3. **`agent.md`** — 如果改动涉及：
   - 新增公共方法（需同步更新方法索引）
   - 开发规范变化
   - 目录职责变化
   - 新增代码事实（不要误判清单）

**原则**：文档描述与代码不一致时，以代码为准，但必须同时修正文档。
