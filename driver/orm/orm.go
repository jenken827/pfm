// Package orm 宿主侧的 ORM / proto 实体到驱动值类型的转换
//
// 这些转换**只给宿主用**（internal/*、cmd/*、内置驱动 driver/local）。单开一个包
// 而不是放进插件 SDK，是为了让插件侧的 driver/types 不 import 宿主的
// 数据模型：插件只要一 import 就会把 gorm 与整张表结构链接进插件二进制，而它
// 从来用不到。
package orm

import (
	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	abi "github.com/jenken827/pfm-plugin-sdk/proto/gen/abi"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
)

// ProviderFromORM 把存储提供者 ORM 转成驱动视角的 Provider
func ProviderFromORM(orm *entity.StorageProviderORM) *types.Provider {
	if orm == nil {
		return &types.Provider{}
	}
	return &types.Provider{
		ID:         idOf(orm),
		DriverID:   stringOf(orm.DriverId),
		Name:       stringOf(orm.Name),
		ConfigJSON: stringOf(orm.Config),
		Config:     ProviderConfigFromORM(orm),
		Proxy:      stringOf(orm.Proxy),
	}
}

// ProviderConfigFromORM 从 StorageProviderORM 提取类型化配置视图
func ProviderConfigFromORM(orm *entity.StorageProviderORM) types.ProviderConfig {
	cfg := types.ProviderConfig{}
	if orm == nil {
		return cfg
	}
	cfg.EncryptionKey = stringOf(orm.EncryptionKey)
	cfg.EncryptionSuffix = stringOf(orm.EncryptionSuffix)
	if orm.SortOrder != nil {
		cfg.SortOrder = *orm.SortOrder
	}
	if orm.FileSizeLimit != nil {
		cfg.FileSizeLimit = *orm.FileSizeLimit
	}
	if orm.MaxConcurrentUploads != nil {
		cfg.MaxConcurrentUploads = *orm.MaxConcurrentUploads
	}
	if orm.HashType != nil {
		cfg.HashType = abi.HashType(*orm.HashType)
	}
	if orm.Enabled != nil {
		cfg.Enabled = *orm.Enabled
	}
	return cfg
}

// FileRefFromLocation 把 FileLocationORM 转为 FileRef（唯一碰 ORM 的转换点）
func FileRefFromLocation(loc *entity.FileLocationORM) types.FileRef {
	ref := types.FileRef{}
	if loc == nil {
		return ref
	}
	if loc.Id != nil {
		ref.LocationID = *loc.Id
	}
	ref.ProviderFileID = stringOf(loc.ProviderFileId)
	ref.Path = stringOf(loc.Path)
	if loc.Size != nil {
		ref.Size = *loc.Size
	}
	if loc.ChunkSize != nil {
		ref.ChunkSize = *loc.ChunkSize
	}
	ref.Hash = stringOf(loc.Hash)
	ref.ChunkHash = stringOf(loc.ChunkHash)
	if loc.ChunkNumber != nil {
		ref.ChunkNumber = *loc.ChunkNumber
	}
	ref.ChannelID = stringOf(loc.ChannelId)
	if loc.MessageId != nil {
		ref.MessageID = int(*loc.MessageId)
	}
	if loc.CacheStatus != nil {
		ref.CacheStatus = abi.CacheStatus(*loc.CacheStatus)
	}
	return ref
}

// FileRefsFromLocations 批量转换
func FileRefsFromLocations(locs []*entity.FileLocationORM) []types.FileRef {
	refs := make([]types.FileRef, 0, len(locs))
	for _, loc := range locs {
		refs = append(refs, FileRefFromLocation(loc))
	}
	return refs
}

// idOf 安全读取主键
func idOf(sp *entity.StorageProviderORM) uint32 {
	if sp.Id == nil {
		return 0
	}
	return *sp.Id
}

// stringOf 安全解引用字符串指针
func stringOf(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
