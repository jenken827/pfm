package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/storage"
)

// 本文件实现 direct-link 的 --list 模式：列出最近的若干文件，并标注每个文件在各提供者上
// 能否直链，用于"挑一个文件去打表"（三条查询搞定，不做逐文件 N+1）。

// directLinkListFile 列表模式的原始行
type directLinkListFile struct {
	ID   string `gorm:"column:id"`
	Name string `gorm:"column:name"`
	Size uint64 `gorm:"column:size"`
	Hash string `gorm:"column:hash"`
}

// directLinkCandidate 一个候选文件及其各提供者的直链 verdict
type directLinkCandidate struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Size       uint64   `json:"size"`
	Providers  []string `json:"providers"`
	Directable int      `json:"directable_providers"`
}

// listDirectLinkCandidates 列出最近 limit 个文件及可直链情况
func listDirectLinkCandidates(ctx context.Context, limit uint32, jsonOut bool) error {
	files, err := loadRecentFilesForDirectLink(ctx, limit)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("库里没有可用的文件记录")
	}
	total, err := countDirectLinkCandidateFiles(ctx)
	if err != nil {
		return err
	}

	hashes := make([]string, 0, len(files))
	for _, f := range files {
		hashes = append(hashes, f.Hash)
	}
	locations, err := loadLocationsByHashesChunked(hashes)
	if err != nil {
		return err
	}
	byHash := make(map[string][]*entity.FileLocationORM, len(files))
	for _, loc := range locations {
		if loc.Blake3Hash != nil {
			byHash[*loc.Blake3Hash] = append(byHash[*loc.Blake3Hash], loc)
		}
	}
	typeNames, err := loadProviderTypeNames()
	if err != nil {
		return err
	}

	candidates := make([]directLinkCandidate, 0, len(files))
	for _, f := range files {
		candidates = append(candidates, buildDirectLinkCandidate(f, byHash[f.Hash], typeNames))
	}
	if jsonOut {
		out, err := marshalDirectLinkCandidates(candidates)
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	}
	printDirectLinkCandidates(candidates, total)
	return nil
}

// countDirectLinkCandidateFiles 库内候选文件总数（有 hash 的普通文件）
func countDirectLinkCandidateFiles(ctx context.Context) (int64, error) {
	var total int64
	err := db.GetDB().WithContext(ctx).
		Table("files").
		Where("deleted_at IS NULL AND type != ? AND hash IS NOT NULL AND hash <> ''", int32(native.FileType_FOLDER)).
		Count(&total).Error
	if err != nil {
		return 0, fmt.Errorf("统计文件总数失败: %w", err)
	}
	return total, nil
}

// loadLocationsByHashesChunked 分批查位置行：一次性 IN 太多 hash 会撞 Postgres 参数上限
func loadLocationsByHashesChunked(hashes []string) ([]*entity.FileLocationORM, error) {
	const chunkSize = 500
	all := make([]*entity.FileLocationORM, 0, len(hashes))
	for start := 0; start < len(hashes); start += chunkSize {
		end := start + chunkSize
		if end > len(hashes) {
			end = len(hashes)
		}
		locs, err := db.GetFileLocationsByBlake3Hashes(hashes[start:end])
		if err != nil {
			return nil, fmt.Errorf("查询位置行失败: %w", err)
		}
		all = append(all, locs...)
	}
	return all, nil
}

// loadRecentFilesForDirectLink 读最近的文件（只取有 hash 的普通文件）
func loadRecentFilesForDirectLink(ctx context.Context, limit uint32) ([]directLinkListFile, error) {
	var files []directLinkListFile
	err := db.GetDB().WithContext(ctx).
		Table("files").
		Select("id", "name", "size", "hash").
		Where("deleted_at IS NULL AND type != ? AND hash IS NOT NULL AND hash <> ''", int32(native.FileType_FOLDER)).
		Order("created_at DESC").
		Limit(int(limit)).
		Scan(&files).Error
	if err != nil {
		return nil, fmt.Errorf("查询文件列表失败: %w", err)
	}
	return files, nil
}

// loadProviderTypeNames 取 provider ID → 类型名的映射（列表模式只用来显示，不建驱动）
func loadProviderTypeNames() (map[uint32]string, error) {
	providers, err := db.GetAllProviders()
	if err != nil {
		return nil, fmt.Errorf("查询存储提供者失败: %w", err)
	}
	names := make(map[uint32]string, len(providers))
	for _, p := range providers {
		if p.Id == nil || p.DriverId == nil {
			continue
		}
		names[*p.Id] = *p.DriverId
	}
	return names, nil
}

// buildDirectLinkCandidate 汇总单个文件的各提供者 verdict
func buildDirectLinkCandidate(file directLinkListFile, locations []*entity.FileLocationORM,
	typeNames map[uint32]string) directLinkCandidate {
	candidate := directLinkCandidate{ID: file.ID, Name: file.Name, Size: file.Size}

	grouped := storage.GroupLocationsByProvider(locations)
	providerIDs := make([]uint32, 0, len(grouped))
	for pid := range grouped {
		providerIDs = append(providerIDs, pid)
	}
	sort.Slice(providerIDs, func(i, j int) bool { return providerIDs[i] < providerIDs[j] })

	for _, pid := range providerIDs {
		locs := grouped[pid]
		verdict, ok := directLinkLocationVerdict(locs, typeNames[pid], directLinkDriverStateOf(pid))
		if ok {
			candidate.Directable++
		}
		candidate.Providers = append(candidate.Providers, verdict)
	}
	return candidate
}

// directLinkDriverState 提供者侧的直链就绪情况
type directLinkDriverState int

const (
	// directLinkDriverReady 已启用且驱动实现了 DirectLinker
	directLinkDriverReady directLinkDriverState = iota
	// directLinkDriverNoCapability 已启用但驱动没实现直链（没声明 DIRECT_LINK 能力）
	directLinkDriverNoCapability
	// directLinkDriverMissing 未启用 / 未初始化成功，拿不到存储实例
	directLinkDriverMissing
)

// directLinkLocationVerdict 单个提供者的可读结论；第二个返回值表示"能直链"
func directLinkLocationVerdict(locs []*entity.FileLocationORM, typeName string,
	state directLinkDriverState) (string, bool) {
	label := fmt.Sprintf("%s(provider %d)", typeName, providerIDOf(locs))
	encrypted := false
	for _, loc := range locs {
		if loc.Encrypted != nil && *loc.Encrypted {
			encrypted = true
		}
	}
	eligible, reason := directLinkEligibility(len(locs), encrypted)
	switch state {
	case directLinkDriverMissing:
		if eligible {
			reason = "副本本身可直链，启用该提供者后即可用"
		}
		return fmt.Sprintf("%s ⚠️ 提供者未启用或初始化失败（%s）", label, reason), false
	case directLinkDriverNoCapability:
		return fmt.Sprintf("%s ❌ 驱动未实现直链（未声明 DIRECT_LINK 能力）", label), false
	}
	if !eligible {
		return fmt.Sprintf("%s ❌ %s", label, reason), false
	}
	return fmt.Sprintf("%s ✅ 可直链", label), true
}

// directLinkDriverStateOf 查提供者的直链就绪情况（只查内存，不发请求）
func directLinkDriverStateOf(providerID uint32) directLinkDriverState {
	drv := storage.GetStorage(providerID)
	if drv == nil {
		return directLinkDriverMissing
	}
	if _, ok := drv.(types.DirectLinker); !ok {
		return directLinkDriverNoCapability
	}
	return directLinkDriverReady
}

// providerIDOf 取位置行的提供者 ID（同组内必然相同）
func providerIDOf(locs []*entity.FileLocationORM) uint32 {
	if len(locs) == 0 || locs[0].ProviderId == nil {
		return 0
	}
	return *locs[0].ProviderId
}

// printDirectLinkCandidates 打印候选文件（含一行普查：库内总数 / 有可直链副本的文件数）
func printDirectLinkCandidates(candidates []directLinkCandidate, total int64) {
	directable := 0
	for _, c := range candidates {
		if c.Directable > 0 {
			directable++
		}
	}
	fmt.Printf("===== 库内候选文件 %d 个，本次列出 %d 个；其中有可直链副本的 %d 个 =====\n",
		total, len(candidates), directable)
	for _, c := range candidates {
		fmt.Printf("[%s] %s  %s  可直链副本=%d\n", c.ID, humanSize(int64(c.Size)), c.Name, c.Directable)
		for _, p := range c.Providers {
			fmt.Printf("    %s\n", p)
		}
	}
}

// marshalDirectLinkCandidates --json 模式的候选文件输出
func marshalDirectLinkCandidates(candidates []directLinkCandidate) (string, error) {
	out, err := json.MarshalIndent(candidates, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}
