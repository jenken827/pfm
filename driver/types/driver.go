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
	// filePath: 本地文件路径
	// fileName: 目标文件名
	// encryptor: 加密器（可为nil，表示不加密）
	Upload(ctx context.Context, filePath string, fileName string, encryptor Encryptor) ([]*StorageProviderFileInfo, error)
	// Open 打开文件（支持多分片合并、断点续传）
	// fileLocations: 该文件在当前存储提供者的所有分片（已按chunk_number排序）
	// encryptor: 加密器（用于解密，如果存储提供者配置了加密）
	Open(ctx context.Context, fileLocations []*native.FileLocationORM, encryptor Encryptor) (ReadSeekCloser, error)
	// Link 获取文件下载链接
	Link(ctx context.Context, fileLocation *native.FileLocationORM) (string, error)
	// Delete 删除文件
	Delete(ctx context.Context, fileLocation *native.FileLocationORM) error

	Close()
}

// PreDownloader 预下载接口（可选）
// 用于支持大文件预缓存的存储驱动（如 Telegram）
type PreDownloader interface {
	// PreDownload 预下载文件的所有分片
	// fileLocations: 该文件在当前存储提供者的所有分片
	// 返回第一个错误，如果没有错误返回 nil
	PreDownload(ctx context.Context, fileLocations []*native.FileLocationORM) error
}

// DriverConstructor 驱动构造函数
type DriverConstructor func(provider *native.StorageProviderORM) (StorageDriver, error)

// DriverConstructors 驱动构造函数映射
var DriverConstructors map[native.StorageProviderType]DriverConstructor

// RegisterStorageDriver 注册存储驱动
func RegisterStorageDriver(d DriverConstructor, t native.StorageProviderType) {
	DriverConstructors[t] = d
}

// GetDriverConstructor 获取驱动构造函数
func GetDriverConstructor(t native.StorageProviderType) DriverConstructor {
	if c, ok := DriverConstructors[t]; ok {
		return c
	}
	return nil
}

func init() {
	DriverConstructors = make(map[native.StorageProviderType]DriverConstructor)
}
