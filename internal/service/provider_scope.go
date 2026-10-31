package service

import (
	"context"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/pkg/llog"
)

// 本文件统一「一个请求能碰哪些存储提供者」的判定。
//
// 背景：`file_locations` 只按 blake3 hash 归类，没有 owner 列——同一份内容会在不同
// 用户的提供者之间共享物理副本（秒传 / 去重）。所以按 hash 查出来的位置行里可能夹着
// **别人的**提供者。凡是「把位置行下发给用户」或「按用户给的 provider_id 去访问提供者」
// 的入口，都必须先用 providerScope 收口，否则：
//   - 展示层会泄露别人的提供者实例名、驱动类型、物理路径（路径里带对方原文件名）；
//   - 访问层会把别人的提供者当成自己的下载源，流量 / 配额 / 带宽都记在对方账上。
//
// 允许集 = 管理员分配给该用户的 `UserSpace.bound_provider_ids`（与 GetMyBoundProviders
// 同一口径）。注意不要用「目录绑定」当允许集：目录绑定是落盘目标，比分配集更窄，
// 拿它过滤展示会让用户在 A 目录里看不到自己另一块盘上的副本。

// providerScope 是一个请求允许接触的提供者集合。
//
// ok=false 表示「允许集未知」——ctx 里没有当前用户（公开分享、后台任务等），这类路径
// 应改用 scopeForOwner 按文件所有者收口；两者都拿不到时保持原行为，不做过滤。
type providerScope struct {
	ids map[uint32]struct{}
	ok  bool
}

// scopeFromCtx 按当前登录用户解析允许集（REST / MCP / WebDAV 都在 ctx 里注入了 UserSpace）。
func scopeFromCtx(ctx context.Context) providerScope {
	uc := getUserContext(ctx)
	if uc == nil {
		return providerScope{}
	}
	return newProviderScope(uc.BoundProviderIds)
}

// scopeForOwner 按「文件所有者」解析允许集。
//
// 用于 ctx 里没有当前用户的路径：公开分享下载、异步任务、后台缩略图等——这些场景
// 用的是所有者本人的盘，而不是某个访问者的盘。查不到 UserSpace 时返回未知允许集。
func scopeForOwner(ctx context.Context, ownerID string) providerScope {
	if ownerID == "" {
		return providerScope{}
	}
	ids, err := db.GetBoundProviderIdsByOwnerID(ownerID)
	if err != nil {
		llog.C(ctx).Warnf("resolve bound providers for owner %s failed: %v", ownerID, err)
		return providerScope{}
	}
	return newProviderScope(ids)
}

// scopeForFile 取「某次文件访问」的允许集：优先当前登录用户，其次文件所有者。
func scopeForFile(ctx context.Context, ownerID string) providerScope {
	if scope := scopeFromCtx(ctx); scope.ok {
		return scope
	}
	return scopeForOwner(ctx, ownerID)
}

func newProviderScope(ids []uint32) providerScope {
	set := make(map[uint32]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return providerScope{ids: set, ok: true}
}

// contains 判断单个提供者是否在允许集内；允许集未知时一律放行（保持原行为）。
func (s providerScope) contains(providerID uint32) bool {
	if !s.ok {
		return true
	}
	_, ok := s.ids[providerID]
	return ok
}

// narrow 只保留允许集内的位置行；允许集未知时原样返回。
func (s providerScope) narrow(ctx context.Context, locations []*entity.FileLocationORM,
	scene string) []*entity.FileLocationORM {
	if !s.ok {
		return locations
	}
	kept := make([]*entity.FileLocationORM, 0, len(locations))
	for _, loc := range locations {
		if loc == nil || loc.ProviderId == nil {
			continue
		}
		if s.contains(*loc.ProviderId) {
			kept = append(kept, loc)
		}
	}
	if hidden := len(locations) - len(kept); hidden > 0 {
		llog.C(ctx).Debugf("%s: hidden %d locations on providers outside user scope", scene, hidden)
	}
	return kept
}
