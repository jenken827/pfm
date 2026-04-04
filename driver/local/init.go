package local

import (
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// LocalDriver  storage driver for local
type LocalDriver struct {
	Config *native.LocalDriverConfig
	Orm    *native.StorageProviderORM
}

// GetType 获取驱动类型
func (t *LocalDriver) GetType() native.StorageProviderType {
	return native.StorageProviderType_LOCAL
}

// GetName 获取驱动名称
func (t *LocalDriver) GetName() string {
	return native.StorageProviderType_LOCAL.String()
}

// GetConfig 获取驱动配置
func (t *LocalDriver) GetConfig() any {
	return t.Config
}

// GetOrm 获取存储提供者ORM对象
func (t *LocalDriver) GetOrm() *native.StorageProviderORM {
	return t.Orm
}
