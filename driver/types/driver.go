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
	Upload(ctx context.Context, ur *native.UploadRecordORM, encryptRootDir *native.FileORM) (*StorageProviderFileInfo, error)
	Link(ctx context.Context, fileLocation *native.FileLocationORM) (string, error)
	Delete(ctx context.Context, fileLocation *native.FileLocationORM) error
}

// DriverConstructor 驱动构造函数
type DriverConstructor func(config string) (StorageDriver, error)

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
