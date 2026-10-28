package local

import (
	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	"github.com/jenken827/pfm-proto/proto/gen/native"
)

// LocalDriver  storage driver for local
type LocalDriver struct {
	Config         *native.LocalDriverConfig
	ProviderConfig types.ProviderConfig
}

// GetType 获取驱动类型
func (t *LocalDriver) GetType() string {
	return DriverID
}

// GetName 获取驱动名称
func (t *LocalDriver) GetName() string {
	return DriverID
}

// GetConfig 获取驱动配置
func (t *LocalDriver) GetConfig() any {
	return t.Config
}

// GetProviderConfig 获取存储提供者的类型化配置视图
func (t *LocalDriver) GetProviderConfig() *types.ProviderConfig {
	return &t.ProviderConfig
}
