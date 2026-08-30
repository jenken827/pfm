package service

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// DedupResult 去重结果统计
type DedupResult struct {
	Scanned          int      // 参与去重的文件数（有 hash 的非文件夹文件）
	DuplicateGroups  int      // 存在重复的 hash 组数
	ToDelete         int      // 待删除的冗余文件数
	Deleted          int      // 实际删除数（dry-run 为 0）
	Kept             int      // 保留的文件数（= DuplicateGroups）
	DryRun           bool     // 是否为预览模式
	Details          []string // 每组明细
}

// DedupFilesByHash 扫描 baseParentID 子树下所有文件，按内容 hash 去重：
// 同一 hash 出现多份时，保留文件名最短的一个（同长度保留最早创建的），
// 其余冗余文件永久删除。
//
// 安全性（物理存储按 hash 共享）：
//   - file_locations 按 blake3_hash 关联，删除副本记录不影响幸存文件的存储位置
//   - 缩略图按 hash 命名，幸存文件继续使用，不清理
//   - 提供者（百度/Telegram 等）上的物理数据按 hash 共享，不删除
//   - 仅删除冗余的 files 记录及其 file_histories 行
//
// dryRun 为 true 时只统计与预览，不删除任何记录。
func DedupFilesByHash(baseParentID string, dryRun bool) (*DedupResult, error) {
	base, err := db.GetFileByID(baseParentID)
	if err != nil {
		return nil, fmt.Errorf("查询基础目录失败: %w", err)
	}
	if base.Type == nil || *base.Type != int32(native.FileType_FOLDER) {
		return nil, fmt.Errorf("parent_id %s 不是文件夹", baseParentID)
	}
	ownerID := strPtrVal(base.OwnerId)
	if ownerID == "" {
		return nil, fmt.Errorf("基础目录缺少 owner_id，无法去重")
	}

	result := &DedupResult{DryRun: dryRun}

	// 递归收集子树下所有非文件夹文件（不含软删除）
	var files []*native.FileORM
	var walk func(parentID string) error
	walk = func(parentID string) error {
		children, err := db.GetFileChildren(parentID, ownerID)
		if err != nil {
			return err
		}
		for _, c := range children {
			if c.Id == nil || c.Name == nil || c.ParentId == nil {
				continue
			}
			if c.Type != nil && *c.Type == int32(native.FileType_FOLDER) {
				if err := walk(*c.Id); err != nil {
					return err
				}
				continue
			}
			files = append(files, c)
		}
		return nil
	}
	if err := walk(baseParentID); err != nil {
		return nil, fmt.Errorf("扫描目录失败: %w", err)
	}

	// 按 hash 分组
	byHash := make(map[string][]*native.FileORM)
	for _, f := range files {
		if f.Hash == nil || *f.Hash == "" {
			continue
		}
		result.Scanned++
		byHash[*f.Hash] = append(byHash[*f.Hash], f)
	}

	// 排序保证输出稳定（按 hash 字典序）
	hashes := make([]string, 0, len(byHash))
	for h := range byHash {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)

	var toDelete []string
	for _, hash := range hashes {
		group := byHash[hash]
		if len(group) < 2 {
			continue
		}
		result.DuplicateGroups++

		keeper := pickKeeper(group)
		result.Details = append(result.Details, formatDedupDetail(hash, keeper, group))

		for _, f := range group {
			if f.Id != nil && *f.Id != *keeper.Id {
				toDelete = append(toDelete, *f.Id)
				result.ToDelete++
			}
		}
	}
	result.Kept = result.DuplicateGroups

	if dryRun || len(toDelete) == 0 {
		return result, nil
	}

	// 执行删除：仅删 files 记录 + file_histories 行。
	// 注意：不走 permanentlyDeleteFiles（其会按 hash 删缩略图、且删历史物理文件），
	// 这里冗余文件与幸存文件共享 hash，缩略图/存储位置/提供者数据必须保留。
	if err := db.DeletePermanently(toDelete); err != nil {
		return nil, fmt.Errorf("删除冗余文件失败: %w", err)
	}
	if err := db.DeleteFileHistoryByFileIDs(toDelete); err != nil {
		llog.L().Warnf("dedup: 清理 file_histories 失败: %v", err)
	}
	result.Deleted = len(toDelete)
	llog.L().Infof("dedup: deleted %d duplicate file records (%d hash groups)", len(toDelete), result.DuplicateGroups)
	return result, nil
}

// pickKeeper 从同 hash 文件组中选出保留者：名称最短，同长度保留最早创建的，再同保留 id 较小
func pickKeeper(group []*native.FileORM) *native.FileORM {
	keeper := group[0]
	for _, f := range group[1:] {
		if betterKeeper(f, keeper) {
			keeper = f
		}
	}
	return keeper
}

// betterKeeper 判断 candidate 是否比 current 更适合保留
func betterKeeper(candidate, current *native.FileORM) bool {
	cLen := nameLen(candidate)
	kLen := nameLen(current)
	if cLen != kLen {
		return cLen < kLen
	}
	cTime := fileCreatedAt(candidate)
	kTime := fileCreatedAt(current)
	if !cTime.Equal(kTime) {
		return cTime.Before(kTime)
	}
	return strPtrVal(candidate.Id) < strPtrVal(current.Id)
}

// nameLen 文件名长度（按字符数计算，中文名按可见长度）
func nameLen(f *native.FileORM) int {
	if f.Name == nil {
		return 0
	}
	return utf8.RuneCountInString(*f.Name)
}

// fileCreatedAt 文件创建时间（nil 按零值处理）
func fileCreatedAt(f *native.FileORM) time.Time {
	if f.CreatedAt != nil {
		return *f.CreatedAt
	}
	return time.Time{}
}

// formatDedupDetail 格式化单组去重明细，便于人工核对
func formatDedupDetail(hash string, keeper *native.FileORM, group []*native.FileORM) string {
	var delNames []string
	for _, f := range group {
		if f.Id != nil && keeper.Id != nil && *f.Id == *keeper.Id {
			continue
		}
		delNames = append(delNames, truncateStr(strPtrVal(f.Name), 30))
	}
	return fmt.Sprintf("hash=%s: 保留 %q (%d字符), 删除 %d 个: %s",
		truncateStr(hash, 10), truncateStr(strPtrVal(keeper.Name), 40),
		nameLen(keeper), len(delNames), strings.Join(delNames, ", "))
}

// truncateStr 截断字符串用于展示
func truncateStr(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}
