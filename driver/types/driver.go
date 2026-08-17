package types

import (
	"context"
	"io"

	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// ReadSeekCloser 支持定位的 Reader（用于断点续传）
type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

// StorageDriver 存储驱动
type StorageDriver interface {
	GetType() native.StorageProviderType
	GetName() string
	// GetConfig 获取驱动自身配置
	GetConfig() any
	// GetProviderConfig 获取存储提供者的类型化配置视图
	GetProviderConfig() *ProviderConfig
	// Upload 上传文件
	// src: 上传源（明文本地文件）；opt: 上传选项（含 per-provider 加密器）
	// 返回文件在提供者上的引用列表（分片场景为多个）
	Upload(ctx context.Context, src UploadSource, opt UploadOptions) ([]FileRef, error)
	// Open 打开文件（支持多分片合并、断点续传）
	// refs: 该文件在当前存储提供者的所有分片引用（已按 chunk_number 排序）
	// opt: 打开选项（含解密器）
	Open(ctx context.Context, refs []FileRef, opt OpenOptions) (ReadSeekCloser, error)
}

// Closer 关闭接口（可选）
type Closer interface {
	Close()
}

// DeleteResult 删除结果
type DeleteResult struct {
	ID      uint64 // FileLocationORM.Id
	Success bool   // 是否成功
	Error   error  // 失败时的错误信息
}

// BatchDeleter 批量删除接口
type BatchDeleter interface {
	// BatchDelete 批量删除文件
	// 返回值说明：
	// - results=nil, err=nil: 全部执行成功
	// - results=nil, err!=nil: 全部执行失败
	// - results!=nil: 部分成功，需检查每条记录的状态
	BatchDelete(ctx context.Context, refs []FileRef) (results []DeleteResult, err error)
}

// PreDownloader 预下载接口（可选）
// 用于支持大文件预缓存的存储驱动（如 Telegram）
type PreDownloader interface {
	// PreDownload 预下载文件的所有分片
	// refs: 该文件在当前存储提供者的所有分片引用
	// 返回第一个错误，如果没有错误返回 nil
	PreDownload(ctx context.Context, refs []FileRef) error
}
