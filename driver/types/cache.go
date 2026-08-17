package types

import (
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// CacheUpdater 缓存状态更新器
// 驱动通过此接口更新 FileLocation 的缓存状态，而不是直接 import internal/db
type CacheUpdater interface {
	// UpdateCacheStatus 更新指定 FileLocation 的缓存状态
	UpdateCacheStatus(locationID uint64, status native.CacheStatus) error
}

// CacheUpdaterSetter 可注入缓存更新器的驱动（可选接口）
type CacheUpdaterSetter interface {
	SetCacheUpdater(u CacheUpdater)
}
