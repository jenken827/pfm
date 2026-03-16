# PFM

PFM(Personal File Manager)是一款个人文件管理系统。前端使用 Flutter 开发，后端使用 go 开发。

## 概念介绍
在介绍系统特性前，先介绍一个特别重要的概念：存储提供者。
它是实际存放文件的地方，可以是本地硬盘，可以是S3存储桶，也可以是网盘，可以是一切可以存储文件的东西。
后续介绍会涉及到它，所以先说一下，避免大家不理解。

## 特性
- 虚拟目录与文件元数据独立管理（文件树与文件内容分离），不依赖存储提供者的目录
- 多存储提供者挂载支持
- 存储提供者驱动插件化开发
- 文件夹级别的存储绑定策略：管理员分配可用存储提供者，用户在分配范围内为目录选择落盘目标。比如，系统挂载了10个存储提供者，管理员分配了三个给指定用户，此用户在创建文件夹的时候可以选择绑定其中任意一个或多个。
- 上传的文件会根据文件夹绑定的存储提供者自动分发
- 存储提供者实例之间文件同步
- 基于 BLAKE3 的内容去重与秒传，
- 文件夹级别的加密功能
- 可控分享
- 桌面端 / 移动端统一 Flutter 客户端
- 在不涉及实际的文件内容时，请求不会直接打到存储提供者的官方服务器。比如，移动、复制、重命名、列表等操作，在本地的服务器就可以直接完成，能有效降低存储提供者官方 API 的调用频率（例如阿里云盘 openapi 对获取列表接口有明确限频），因为列表与元数据由本地系统维护。
- 方便的相册，视频自动备份功能
- WebDAV
- 离线下载
- 用户之间目录隔离，可以通过分享功能共享数据
- 历史版本

## 当前实现概览

- **后端**：Go 1.25.1+；`net/http` + REST/JSON（proto 只定义数据结构）；GORM + SQLite / PostgreSQL / MySQL；本地账号 + HS256 JWT；驱动以元数据描述自身配置，前端表单据此自动渲染；支持进程外插件驱动，核心只内置 `local`。
- **客户端**：Flutter（Dart SDK `^3.13.0`），`flutter_bloc` + `go_router` + `Dio`，Hive / SQLite 本地持久化，与后端共用 protobuf 数据结构；另带一套无需构建的 Web UI，已 `//go:embed` 进二进制。
- **内置驱动**：`local`。
- **主要能力**：文件树 / 回收站 / 标签 / 历史版本、分片上传与断点续传、BLAKE3 秒传、多提供者副本同步、直链下载（不可直链时自动回退代理）、可控分享、WebDAV、URL 离线下载、缩略图与流式预览、移动端相册与通讯录 / 短信 / 通话记录备份。

## 核心设计要点

核心是**逻辑文件与物理副本分离**：虚拟目录、文件名、标签、历史版本等元数据由本地系统维护，真实字节则存放在某个存储提供者的某个位置。

- 列表、移动、复制、重命名、回收站这类操作只改元数据，不会请求存储提供者，因此不受其官方接口限频影响。
- 一条文件记录可以对应多个提供者上的多份副本；一次上传后其余副本按需同步，不必重复上传。
- 落盘目标由目录绑定解析得出：管理员分配可用提供者作为上限，用户在创建目录时从中选择，子目录继承父目录，最终取交集。
- 内容以 BLAKE3 去重，hash 命中即秒传。
- 用户之间按归属隔离；管理员特权只作用于管理功能，不能操作他人文件。

## 架构

```mermaid
flowchart TB
    Client["客户端<br/>Flutter App / Web UI"]

    subgraph Core["PFM 服务端"]
        direction TB
        API["业务接口层"]
        Meta[("元数据<br/>虚拟目录树")]
        Engine["存储调度<br/>驱动抽象层"]
        API <--> Meta
        API --> Engine
    end

    subgraph Providers["存储提供者（任意挂载）"]
        direction LR
        Local["本地磁盘"]
        S3["S3 / 对象存储"]
        Cloud["网盘<br/>内置驱动或插件"]
    end

    Client <-->|"REST + 流式传输"| API
    Engine <-->|"只搬字节"| Local
    Engine <--> S3
    Engine <--> Cloud
```

- 客户端只跟服务端打交道，看到的目录树来自本地元数据，与存储提供者内部的目录结构无关。
- 存储提供者只是「放字节的地方」，挂本地盘、S3 桶还是网盘，对上层是同一套接口。
- 移动 / 重命名 / 复制 / 列表只改元数据；只有上传与读取字节时才经过驱动。
- 因此换提供者、加副本、一份内容多副本共存，都不影响虚拟目录与元数据。

## 仓库结构

```text
.
├── api/                # REST 业务处理器
├── server/             # HTTP 服务、路由表、中间件、WebDAV、内置 Web UI
├── internal/
│   ├── service/        # 业务编排
│   ├── db/             # 数据访问层
│   ├── storage/        # 上传调度、提供者解析、副本维护
│   └── ...             # auth / config / download / thumbnail / backup
├── driver/             # 存储驱动实现；types/ 为驱动接口与元数据注册
├── plugin/sdk/         # 进程外插件 SDK（把驱动打包成独立进程）
├── proto/              # Protobuf 定义（数据结构唯一真理源，生成物不入库）
├── frontend/           # Flutter 客户端
├── cmd/                # cobra 子命令：server / user / plugin / cleanup-* 等
├── pkg/                # 通用库
├── config.example.yaml # 配置示例
└── dev.sh              # 构建 / 运行 / 测试 / 代码生成统一入口
```

## 如何开发

### 开发环境

| 组件 | 要求 | 用途 |
|------|------|------|
| Go | 1.25.1+ | 后端编译与测试 |
| Flutter / Dart | Flutter 3.47+（Dart `^3.13.0`） | 客户端 |
| Node.js | 能跑 `node --test` 即可 | Web UI 测试（可选） |
| buf + protoc-gen-go + protoc-gen-sgorm | 见下方安装命令 | 生成 Go / Dart 代码（首次克隆也要跑，生成物不入库） |
| golangci-lint | 任意较新版本 | 代码检查（可选） |
| Rust + Android NDK | 可选 | BLAKE3 原生加速，只影响上传哈希速度 |
| PostgreSQL / MySQL | 可选 | 默认 SQLite 是纯 Go 实现，无需额外安装 |

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install github.com/jenken827/protoc-gen-sgorm/cmd/protoc-gen-sgorm@v0.1.0
```

### 上手

```bash
git clone <repo> && cd pfm
bash dev.sh proto                       # 生成 proto/gen/（不入库，首次必须）
cp config.example.yaml data/config.yaml # 按需改 db / server_url / sign_token
bash dev.sh build                       # 编译为 ./pfm
./pfm -d ./data user add <用户名>        # 本地认证没有注册入口，用户需手动创建
./pfm server -d ./data                  # 启动服务；health 检查 /health
```

客户端：

```bash
bash dev.sh flutter get             # 拉依赖（puro flutter pub get）
bash dev.sh flutter build_runner    # 路由 / 序列化代码生成
bash dev.sh flutter run windows     # 运行（设备名可省略，如 android / chrome）
```

登录页填服务端地址，用上面创建的账号登录。其余常用任务（`build` / `run` / `dev` / `test` / `test-web` / `lint` / `clean` / `flutter build` 等）都由 `bash dev.sh` 分发，`bash dev.sh help` 可看全部。

构建产物是单个自包含的 Go 二进制，怎么常驻（systemd、容器、进程管理器…）由部署方决定；配置文件默认读 `<DataDir>/config.yaml`，也可用 `-c` 指定。改了 `server/webui/` 需要重新 `bash dev.sh build` 才会生效。

## 如何开发驱动

驱动只负责「字节怎么进、怎么出」，目录树、移动、重命名、回收站、秒传判定、副本调度、权限与加密编排全在核心完成。所以对接一个网盘通常只用到它官方 API 的少数几类接口：上传、下载（或取直链），需要按目录寻址时再加一个列目录。

1. **声明元数据**：填 `native.DriverInfo`（身份 `driver_id`、显示名、图标、配置表单字段、能力列表）。前端「新增存储源」的表单据此自动渲染，不需要改前端。
2. **实现必选接口** `types.StorageDriver`：`GetType` / `GetName` / `GetConfig` / `GetProviderConfig`，加上两个内容面能力——
   - `Upload`：把明文源文件写到提供者，返回 `[]FileRef`（大文件由驱动自己决定怎么分片）；
   - `Open`：按 `FileRef` 打开字节流，支持按 offset 裁剪以便断点续传与 Range 播放。
   落到提供者的哪个物理目录也由驱动决定：`types.AllocateUploadDir` 配合 `DirBackend` 提供「年 / 月 / 数字子目录」分片，按 ID 寻址的提供者（如 Telegram）直接用自己的 ID 即可。
3. **按需实现可选能力**（实现即生效，不实现也不影响基本可用）：

   | 能力 | 接口 | 作用 |
   |------|------|------|
   | 直链 | `types.DirectLinker` | 客户端直连提供者取字节，省服务端带宽；不满足条件时核心自动回退代理下载 |
   | 远端枚举 | `types.RemoteLister` | `cleanup-storage-orphans` 逆向清理存储侧孤儿文件 |
   | 批量删除 | `types.BatchDeleter` | 删除 / 清空回收站时批量清理远端对象 |
   | 配置回写 | `types.ConfigPersisterSetter` | token 续期后写回提供者配置 |

4. **注册**：在 `init()` 里 `types.RegisterStorageDriverWithMetadata(ctor, metadata)`，内置驱动在 `driver/driver.go` 里 import 一行即生效。
5. **要独立分发就打成插件**：加一个 `plugin.json`，`main` 里 `func main() { sdk.MustServe("<driver_id>") }`；宿主打开 `plugin.enabled` 后通过 loopback RPC 调用同一个驱动。插件与宿主同权限运行，只应安装可信来源的二进制。

边界：驱动包只能依赖 `driver/types`、`proto/gen/*`、`pkg/*`、`utils`，不要 import `internal/*`，否则编不进独立插件进程。

## 配置示例

完整字段见仓库根的 `config.example.yaml`，这里只列必须自己决定的几项：

```yaml
schema:
  port: 10999

server_url: http://127.0.0.1:10999   # 对外地址，用于生成公开分享链接
sign_token: 换成一个稳定随机串        # 更换后旧分享链接失效

db:
  type: sqlite3                      # 也可用 PostgreSQL / MySQL
  name: data.db

auth:
  local:
    jwt_secret: ""                   # 留空则首次启动自动生成到 <DataDir>/jwt_secret
```

存储提供者的配置不在这个文件里：新增提供者时按驱动元数据填表，存进数据库。

## 接口约定

- **形态**：纯 REST/JSON（`net/http`）。业务接口统一 `POST /api/<域>/<动作>`（如 `POST /api/file/list`），请求体为 proto3 JSON；少数裸 HTTP 接口（`/health`、登录、文件流、公开分享、`/dav`、静态资源）用 GET。
- **路由**：业务路由集中在 `server/route_table.go`，新增接口 = 在 `api/` 加处理器 + 这里注册一行；裸 HTTP 接口在 `server/router.go`。
- **返回**：统一 `BaseResp`（`code` / `message` / `data`）；登录接口例外，用 HTTP 401 表达凭据错误。
- **鉴权**：本地用户表 + HS256 JWT，请求带 `Authorization: Bearer <token>`；`/api/admin/*` 要求管理员；`/dav` 使用与登录账号解耦的 WebDAV 独立账号（HTTP Basic，须经 HTTPS 访问）；浏览器 / WebView 发起的预览与下载改用 `POST /api/file/preview-ticket` 签发的单文件票据，不要把 access token 拼进 URL。

## 许可证

本项目以 **GNU AGPL-3.0**（见 `LICENSE`）发布。

---

最后更新：2026-10-27
