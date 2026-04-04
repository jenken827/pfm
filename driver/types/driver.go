package types

import (
	"context"

	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// StorageProviderFileInfo 存储提供商文件信息
type StorageProviderFileInfo struct {
	ID       string
	Name     string
	Path     string
	HashType native.HashType
	Hash     string
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
	Upload(ctx context.Context, filePath string, fileName string, encryptor Encryptor) (*StorageProviderFileInfo, error)
	// Link 获取文件下载链接
	Link(ctx context.Context, fileLocation *native.FileLocationORM) (string, error)
	// Delete 删除文件
	Delete(ctx context.Context, fileLocation *native.FileLocationORM) error
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
