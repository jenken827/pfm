package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

// ========================================
// 分片完整性检查相关方法
// ========================================

// GroupLocationsByProvider 按提供者分组FileLocation
func GroupLocationsByProvider(locations []*native.FileLocationORM) map[uint32][]*native.FileLocationORM {
	result := make(map[uint32][]*native.FileLocationORM)
	for _, loc := range locations {
		if loc.ProviderId != nil {
			result[*loc.ProviderId] = append(result[*loc.ProviderId], loc)
		}
	}
	return result
}

// CheckProviderComplete 检查单个提供者的所有分片是否都完成
// 返回：是否完整, 预期分片数, 实际分片数
func CheckProviderComplete(locations []*native.FileLocationORM) (bool, int, int) {
	if len(locations) == 0 {
		return false, 0, 0
	}

	// 获取预期的分片总数（从第一个分片）
	firstLoc := locations[0]
	if firstLoc.ChunkTotal == nil {
		// 兼容旧数据：没有分片字段，认为是单文件
		if firstLoc.Status != nil && *firstLoc.Status == int32(native.LocationStatus_PCOMPLETED) {
			return true, 1, 1
		}
		return false, 1, 1
	}

	expectedTotal := int(*firstLoc.ChunkTotal)
	actualTotal := len(locations)

	// 检查分片数量
	if actualTotal != expectedTotal {
		return false, expectedTotal, actualTotal
	}

	// 检查所有分片状态
	for _, loc := range locations {
		if loc.Status == nil || *loc.Status != int32(native.LocationStatus_PCOMPLETED) {
			return false, expectedTotal, actualTotal
		}
	}

	return true, expectedTotal, actualTotal
}

// GetCompleteProviders 获取所有分片都完成的提供者ID列表
func GetCompleteProviders(locations []*native.FileLocationORM) ([]uint32, error) {
	providerMap := GroupLocationsByProvider(locations)

	var completeProviders []uint32
	for providerID, locs := range providerMap {
		complete, _, _ := CheckProviderComplete(locs)

		if complete {
			completeProviders = append(completeProviders, providerID)
		} else {
			llog.L().Debugf("provider %d is incomplete", providerID)
		}
	}

	return completeProviders, nil
}

// GetCompleteProviderLocations 获取指定提供者的所有分片Location（按chunk_number排序）
func GetCompleteProviderLocations(locations []*native.FileLocationORM, providerID uint32) ([]*native.FileLocationORM, error) {
	var result []*native.FileLocationORM

	for _, loc := range locations {
		if loc.ProviderId != nil && *loc.ProviderId == providerID {
			if loc.Status != nil && *loc.Status == int32(native.LocationStatus_PCOMPLETED) {
				result = append(result, loc)
			}
		}
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no complete locations for provider %d", providerID)
	}

	// 按 chunk_number 排序
	sort.Slice(result, func(i, j int) bool {
		if result[i].ChunkNumber == nil || result[j].ChunkNumber == nil {
			return false
		}
		return *result[i].ChunkNumber < *result[j].ChunkNumber
	})

	return result, nil
}

// ========================================
// 文件下载合并相关方法
// ========================================

// DownloadAndMergeFile 下载并合并所有分片到临时文件
// 返回临时文件路径（调用方负责删除）
func DownloadAndMergeFile(
	ctx context.Context,
	sourceStorage types.StorageDriver,
	locations []*native.FileLocationORM,
	encryptor types.Encryptor,
	blake3Hash string,
) (string, error) {
	// 1. 按分片编号排序
	sort.Slice(locations, func(i, j int) bool {
		if locations[i].ChunkNumber == nil || locations[j].ChunkNumber == nil {
			return false
		}
		return *locations[i].ChunkNumber < *locations[j].ChunkNumber
	})

	// 2. 打开文件（多分片自动合并、解密）
	reader, err := sourceStorage.Open(ctx, types.FileRefsFromLocations(locations), types.OpenOptions{Encryptor: encryptor})
	if err != nil {
		return "", fmt.Errorf("open file failed: %w", err)
	}
	defer reader.Close()

	// 3. 创建临时文件
	tempDir := utils.GetFileTempDir(blake3Hash)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("create temp dir failed: %w", err)
	}

	tempFilePath := filepath.Join(tempDir, "merged_file")
	tempFile, err := os.Create(tempFilePath)
	if err != nil {
		return "", fmt.Errorf("create temp file failed: %w", err)
	}
	defer tempFile.Close()

	// 4. 流式写入临时文件（64KB缓冲区）
	buf := make([]byte, 64*1024)
	written, err := io.CopyBuffer(tempFile, reader, buf)
	if err != nil {
		os.Remove(tempFilePath)
		return "", fmt.Errorf("write temp file failed: %w", err)
	}

	llog.C(ctx).Infof("downloaded and merged %d bytes from %d chunks to %s",
		written, len(locations), tempFilePath)

	return tempFilePath, nil
}

// ========================================
// 提供者选择相关方法
// ========================================

// OrderedCompleteProviders 按 Order 升序（值越小优先级越高）返回"完整副本且存储实例可用"的提供者 ID
//
// 这是选源的**唯一排序口径**：SelectBestSourceProvider 与直链的"优先挑可直链提供者"
// 都走这里，避免两处各写一份排序导致行为漂移。
func OrderedCompleteProviders(locations []*native.FileLocationORM) ([]uint32, error) {
	completeProviders, err := GetCompleteProviders(locations)
	if err != nil {
		return nil, err
	}
	if len(completeProviders) == 0 {
		return nil, fmt.Errorf("no complete provider found")
	}

	type providerOrder struct {
		providerID uint32
		order      uint32
	}
	var sorted []providerOrder
	for _, pid := range completeProviders {
		st := GetStorage(pid)
		if st == nil {
			continue
		}
		order := uint32(0)
		if pcfg := st.GetProviderConfig(); pcfg != nil {
			order = pcfg.SortOrder
		}
		sorted = append(sorted, providerOrder{providerID: pid, order: order})
	}
	if len(sorted) == 0 {
		return nil, fmt.Errorf("no available provider")
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].order < sorted[j].order
	})

	ordered := make([]uint32, 0, len(sorted))
	for _, item := range sorted {
		ordered = append(ordered, item.providerID)
	}
	return ordered, nil
}

// SelectBestSourceProvider 选择最优的源提供者（按存储提供者Order排序）
// 返回提供者ID和对应的分片Location列表
func SelectBestSourceProvider(locations []*native.FileLocationORM) (uint32, []*native.FileLocationORM, error) {
	ordered, err := OrderedCompleteProviders(locations)
	if err != nil {
		return 0, nil, err
	}

	bestProviderID := ordered[0]
	providerLocations, err := GetCompleteProviderLocations(locations, bestProviderID)
	if err != nil {
		return 0, nil, err
	}

	return bestProviderID, providerLocations, nil
}

// BuildCompleteProviderMap 构建完整提供者的映射
func BuildCompleteProviderMap(locations []*native.FileLocationORM) map[uint32]bool {
	completeProviders, _ := GetCompleteProviders(locations)
	result := make(map[uint32]bool)
	for _, pid := range completeProviders {
		result[pid] = true
	}
	return result
}
