package types

import (
	"context"
	"io"

	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// StorageProviderFileInfo 存储提供商文件信息
type StorageProviderFileInfo struct {
	ID        string
	Name      string
	Path      string
	ChunkSize uint64 // 单位：字节 文件可能进行了加密操作，这个是加密后的大小
	ChunkHash string
	Hash      string // 文件hash，如果触发了分片，记录在第一个分片上
	Size      uint64 // 文件总大小，如果触发了分片，记录在第一个分片上
	S3KeyFlag string // 123pan 专用：用于标识文件在 S3 存储中的唯一标识
}

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
	GetConfig() any
	// GetOrm 获取存储提供者ORM对象
	GetOrm() *native.StorageProviderORM
	// Upload 上传文件
	// record: 上传记录，包含文件路径、文件名、分片大小等信息
	// encryptor: 加密器（可为nil，表示不加密）
	Upload(ctx context.Context, record *native.UploadRecordORM, encryptor Encryptor) ([]*StorageProviderFileInfo, error)
	// Open 打开文件（支持多分片合并、断点续传）
	// fileLocations: 该文件在当前存储提供者的所有分片（已按chunk_number排序）
	// encryptor: 加密器（用于解密，如果存储提供者配置了加密）
	Open(ctx context.Context, fileLocations []*native.FileLocationORM, encryptor Encryptor) (ReadSeekCloser, error)
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
	BatchDelete(ctx context.Context, fileLocations []*native.FileLocationORM) (results []DeleteResult, err error)
}

// PreDownloader 预下载接口（可选）
// 用于支持大文件预缓存的存储驱动（如 Telegram）
type PreDownloader interface {
	// PreDownload 预下载文件的所有分片
	// fileLocations: 该文件在当前存储提供者的所有分片
	// 返回第一个错误，如果没有错误返回 nil
	PreDownload(ctx context.Context, fileLocations []*native.FileLocationORM) error
}
