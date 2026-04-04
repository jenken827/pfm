package types

import (
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// DriverRegistry 驱动注册器
type DriverRegistry struct {
	constructor DriverConstructor
	metadata    *native.StorageProviderTypeInfo
}

// driverRegistries 驱动注册表
var driverRegistries = make(map[native.StorageProviderType]*DriverRegistry)

// RegisterStorageDriverWithMetadata 注册存储驱动（带元数据）
func RegisterStorageDriverWithMetadata(constructor DriverConstructor, metadata *native.StorageProviderTypeInfo) {
	driverRegistries[*metadata.Type] = &DriverRegistry{
		constructor: constructor,
		metadata:    metadata,
	}
	// 同时注册到旧的构造函数映射，保持向后兼容
	DriverConstructors[*metadata.Type] = constructor
}

// GetDriverMetadata 获取驱动元数据
func GetDriverMetadata(t native.StorageProviderType) *native.StorageProviderTypeInfo {
	if registry, ok := driverRegistries[t]; ok {
		return registry.metadata
	}
	return nil
}

// GetAllDriverMetadata 获取所有驱动元数据
func GetAllDriverMetadata() []*native.StorageProviderTypeInfo {
	metadatas := make([]*native.StorageProviderTypeInfo, 0, len(driverRegistries))
	for _, registry := range driverRegistries {
		metadatas = append(metadatas, registry.metadata)
	}
	return metadatas
}
