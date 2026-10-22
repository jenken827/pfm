package types

import (
	"sort"

	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// DriverRegistry 驱动注册器
type DriverRegistry struct {
	constructor DriverConstructor
	metadata    *native.StorageProviderTypeInfo
}

// DriverConstructor 驱动构造函数
type DriverConstructor func(provider *native.StorageProviderORM) (StorageDriver, error)

// driverRegistries 驱动注册表
var driverRegistries = make(map[native.StorageProviderType]*DriverRegistry)

// RegisterStorageDriverWithMetadata 注册存储驱动（带元数据）
func RegisterStorageDriverWithMetadata(constructor DriverConstructor, metadata *native.StorageProviderTypeInfo) {
	driverRegistries[*metadata.Type] = &DriverRegistry{
		constructor: constructor,
		metadata:    metadata,
	}
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
	// driverRegistries 是 map，遍历顺序每次都不一样。这个列表直接喂给前端"新增存储源"
	// 的类型下拉框，顺序乱跳会让用户每次看到的第一项都不同（也让人以为列表在变），
	// 故按枚举值排序，保持与 enum.proto 的声明顺序一致。
	sort.Slice(metadatas, func(i, j int) bool {
		return metadatas[i].GetType() < metadatas[j].GetType()
	})
	return metadatas
}

// GetDriverConstructor 获取驱动构造函数
func GetDriverConstructor(t native.StorageProviderType) DriverConstructor {
	if registry, ok := driverRegistries[t]; ok {
		return registry.constructor
	}
	return nil
}
