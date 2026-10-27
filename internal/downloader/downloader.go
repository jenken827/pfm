// Package downloader 定义「离线下载后端」在宿主侧的契约。
//
// 后端（aria2 / qBittorrent / …）以 kind=downloader 的插件形式跑在独立进程里，
// 核心只认这里的 Task / Backend：提交任务、查进度、取消。产物文件落在宿主机上，
// **完成后的入库（算 blake3、建 File 记录、分发到各 provider）仍归核心**。
//
// 数值字段（长度 / 速度）统一用十进制字符串：各后端原生类型不一致（aria2 返回
// 字符串、qBittorrent 返回整数），由插件格式化，核心只负责解析。
package downloader

import (
	"context"
	"errors"
	"sync"
)

// Status 任务状态（与 proto 的 DownloadStatus 对应）
const (
	StatusWaiting  = "waiting"
	StatusActive   = "active"
	StatusPaused   = "paused"
	StatusComplete = "complete"
	StatusError    = "error"
	StatusRemoved  = "removed"
)

// File 任务产物里的一个文件
type File struct {
	// Path 宿主机上的路径（后端写入的实际位置，可能是绝对路径也可能是相对下载目录）
	Path            string
	Length          string
	CompletedLength string
	// Selected "true" / "false"：BT 任务里用户是否勾选了该文件
	Selected string
	URIs     []URI
}

// URI 文件的一个来源地址
type URI struct {
	URI    string
	Status string
}

// Task 一次任务状态查询的结果
type Task struct {
	// Gid 后端方言里的任务 id（aria2 的 gid），核心只当不透明字符串
	Gid             string
	Status          string
	Dir             string
	TotalLength     string
	CompletedLength string
	DownloadSpeed   string
	// InfoHash BT 任务的 infohash（小写十六进制）
	InfoHash     string
	ErrorCode    string
	ErrorMessage string
	// FollowedBy 由本任务派生出的任务 id（磁力先下元数据再派生产物任务）
	FollowedBy []string
	// TorrentName 种子显示名（BT 任务才有）
	TorrentName string
	Files       []File
}

// Options 提交任务时的通用选项（后端无关）
type Options struct {
	// Dir 下载目录（宿主机绝对路径）
	Dir string
	// Out 输出文件名；空表示由后端按 URL 推断
	Out string
}

// Backend 离线下载后端。实现者是 internal/plugin 里的插件代理。
type Backend interface {
	// Name 后端身份（如 aria2），对应插件的 downloader_id
	Name() string
	// Version 后端版本号（仅日志）
	Version(ctx context.Context) (string, error)
	// AddURI 按 URL 列表提交任务，返回任务 id
	AddURI(ctx context.Context, uris []string, opts Options) (gid string, err error)
	// TellStatus 查单个任务
	TellStatus(ctx context.Context, gid string) (Task, error)
	// ListActive 进行中的任务
	ListActive(ctx context.Context) ([]Task, error)
	// ListWaiting 排队中的任务
	ListWaiting(ctx context.Context, offset, limit int) ([]Task, error)
	// ListStopped 已结束的任务（完成 / 失败 / 移除）
	ListStopped(ctx context.Context, offset, limit int) ([]Task, error)
	// Remove 停止任务（保留结果）
	Remove(ctx context.Context, gid string) error
	// RemoveResult 删除任务结果
	RemoveResult(ctx context.Context, gid string) error
	// Close 释放后端连接（插件进程由管理器回收）
	Close() error
}

var (
	mu      sync.RWMutex
	current Backend
)

// SetBackend 登记当前后端（插件加载时由插件管理器调用）
func SetBackend(b Backend) {
	mu.Lock()
	defer mu.Unlock()
	current = b
}

// ClearBackend 撤销后端（插件热加载换版本时调用）。
// 只有当前登记的就是它时才清，避免把刚换上的新后端清掉。
func ClearBackend(b Backend) {
	mu.Lock()
	defer mu.Unlock()
	if current == b {
		current = nil
	}
}

// Current 当前后端；未启用离线下载时为 nil
func Current() Backend {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// Available 离线下载是否可用
func Available() bool { return Current() != nil }

// ErrUnavailable 没有可用后端时的统一错误
var ErrUnavailable = errors.New("离线下载未启用（需要在配置里指定 downloader.plugin 并安装对应插件）")
