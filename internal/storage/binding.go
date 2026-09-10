package storage

import (
	"encoding/json"
	"fmt"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
)

// 本文件集中定义「目录绑定 → 落盘存储提供者集合」的解析规则。
// 放在 storage 包而不是 service 包的原因：service 被 backup / download 依赖，
// 而后两者（后台协程，无 HTTP context）也要用同一套规则解析目标 provider，
// 放 service 会造成 import 环。

// ResolveProviderIds 解析「folderID 目录下的文件应落盘到哪些存储提供者」。
//
// 规则（见《设计方案.md》7. 存储提供者绑定策略）：
//  1. ownerID 必须已由管理员在 UserSpace 中配置（AssignRootDir 分配容量 + provider），
//     其 bound_provider_ids 即「用户分配集」assigned；未配置返回 error；
//  2. 从 folderID 沿祖先链向上取最近一个显式绑定的目录 bound
//     （db.GetNearestBoundProviderIds；folderID 可以是文件，等价于从其父目录开始）；
//  3. bound 为空 → 用 assigned（未显式绑定的目录继承用户分配集）；
//  4. bound 非空 → 与 assigned 取交集，越界项记告警日志后丢弃
//     （管理员后来收窄分配时，历史绑定自动失效）；
//  5. 交集为空（分配被整体替换）→ 回退 assigned 并告警，不硬失败。
//
// 返回的切片为新建切片，调用方可安全持有。
func ResolveProviderIds(ownerID, folderID string) ([]uint32, error) {
	if ownerID == "" {
		return nil, fmt.Errorf("ownerID 为空，无法解析存储提供者")
	}

	assigned, err := db.GetBoundProviderIdsByOwnerID(ownerID)
	if err != nil {
		return nil, err
	}
	if len(assigned) == 0 {
		return nil, fmt.Errorf("用户 %s 未绑定存储提供者", ownerID)
	}

	bound, err := db.GetNearestBoundProviderIds(folderID)
	if err != nil {
		return nil, fmt.Errorf("解析目录 %s 的祖先绑定失败: %w", folderID, err)
	}

	if len(bound) == 0 {
		return assigned, nil
	}

	assignedSet := make(map[uint32]struct{}, len(assigned))
	for _, id := range assigned {
		assignedSet[id] = struct{}{}
	}

	kept := make([]uint32, 0, len(bound))
	dropped := make([]uint32, 0)
	seen := make(map[uint32]struct{}, len(bound))
	for _, id := range bound {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		if _, ok := assignedSet[id]; ok {
			kept = append(kept, id)
		} else {
			dropped = append(dropped, id)
		}
	}

	if len(dropped) > 0 {
		llog.L().Warnf("目录 %s 的绑定 %v 超出用户 %s 的分配集，已忽略越界项", folderID, dropped, ownerID)
	}
	if len(kept) == 0 {
		llog.L().Warnf("目录 %s 的绑定与用户 %s 的分配集无交集，回退为分配集 %v", folderID, ownerID, assigned)
		return assigned, nil
	}
	return kept, nil
}

// OutOfAssigned 返回 ids 中不在 assigned 内的项，用于「用户只能把目录绑定到
// 管理员分配给自己的 provider」的校验。返回空切片表示全部合法。
func OutOfAssigned(ids, assigned []uint32) []uint32 {
	assignedSet := make(map[uint32]struct{}, len(assigned))
	for _, id := range assigned {
		assignedSet[id] = struct{}{}
	}
	var out []uint32
	for _, id := range ids {
		if _, ok := assignedSet[id]; !ok {
			out = append(out, id)
		}
	}
	return out
}

// EncodeProviderIds 把 provider ID 列表编码为落库用的 JSON 数组字符串
func EncodeProviderIds(ids []uint32) string {
	b, err := json.Marshal(ids)
	if err != nil {
		// []uint32 不会序列化失败，兜底返回未绑定语义
		return "[]"
	}
	return string(b)
}
