package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/pkg/llog"
)

// ErrRemoteListUnsupported 该提供者只支持按对象 ID 读写，没有目录树可枚举
// （典型：只能按消息 ID 取文件的驱动）。
var ErrRemoteListUnsupported = errors.New("该存储提供者不支持远端目录枚举")

// remoteScanMaxDepth 远端递归深度上限，防脏数据造成的目录环
const remoteScanMaxDepth = 64

// RemoteOrphan 远端孤儿：提供者上存在、但 file_locations 已无任何记录的物理文件
type RemoteOrphan struct {
	Path string // 远端完整路径（与 file_locations.path 同形态）
	ID   string // 提供者侧文件 ID
	Size int64  // 字节数
}

// RemoteOrphanScan 单个提供者的逆向孤儿扫描结果
type RemoteOrphanScan struct {
	ProviderID   uint32
	ProviderName string
	Root         string // 实际扫描根路径
	Scanned      int    // 扫描到的远端文件数
	Recorded     int    // file_locations 里仍有记录的文件数
	Orphans      []RemoteOrphan
}

// ScanRemoteOrphans 扫描 provider 远端 root 子树，找出 file_locations 已无记录的远端文件。
//
// 路径比对口径：两侧都去掉前导 "/" 后精确比对（兼容老数据 path 有无前导斜杠的差异）。
// 只要 file_locations 里还有该路径（含分片行/待清理行），就不算孤儿——绝不误删有记录的数据。
func ScanRemoteOrphans(ctx context.Context, providerID uint32, providerName, root string,
	progress func(scanned int)) (*RemoteOrphanScan, error) {
	drv, err := storage.EnsureProvider(providerID)
	if err != nil {
		return nil, fmt.Errorf("初始化 provider 失败: %w", err)
	}
	lister, ok := drv.(types.RemoteLister)
	if !ok {
		return nil, ErrRemoteListUnsupported
	}

	paths, err := db.GetFileLocationPathsByProvider(providerID)
	if err != nil {
		return nil, fmt.Errorf("查询 file_locations 失败: %w", err)
	}
	known := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		known[canonicalRemotePath(p)] = struct{}{}
	}

	result := &RemoteOrphanScan{ProviderID: providerID, ProviderName: providerName, Root: root}
	if err := walkRemoteTree(ctx, lister, root, 0, known, result, progress); err != nil {
		return nil, err
	}
	return result, nil
}

// walkRemoteTree 深度优先遍历远端目录，累计扫描/有记录/孤儿统计
func walkRemoteTree(ctx context.Context, lister types.RemoteLister, dir string, depth int,
	known map[string]struct{}, result *RemoteOrphanScan, progress func(int)) error {
	if depth > remoteScanMaxDepth {
		return fmt.Errorf("目录层级超过 %d 层，疑似目录环: %s", remoteScanMaxDepth, dir)
	}
	entries, err := lister.ListRemote(ctx, dir)
	if err != nil {
		return fmt.Errorf("列目录失败 %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.Name == "" { // 脏数据：无名条目既不能比对也不能安全删除，直接跳过
			continue
		}
		if e.IsDir {
			if err := walkRemoteTree(ctx, lister, e.Path, depth+1, known, result, progress); err != nil {
				return err
			}
			continue
		}
		result.Scanned++
		if _, ok := known[canonicalRemotePath(e.Path)]; ok {
			result.Recorded++
		} else {
			result.Orphans = append(result.Orphans, RemoteOrphan{Path: e.Path, ID: e.ID, Size: e.Size})
		}
		if progress != nil && result.Scanned%500 == 0 {
			progress(result.Scanned)
		}
	}
	return nil
}

// canonicalRemotePath 路径归一化：去首尾空白与前导 "/"，仅用于比对
func canonicalRemotePath(p string) string {
	return strings.TrimPrefix(strings.TrimSpace(p), "/")
}

// RemoveRemoteOrphans 调驱动批量删除远端孤儿文件。
//
// 这些路径在 file_locations 中没有任何记录，删除后无需改动数据库；返回成功/失败计数。
func RemoveRemoteOrphans(ctx context.Context, providerID uint32, orphans []RemoteOrphan) (deleted, failed int, err error) {
	drv, err := storage.EnsureProvider(providerID)
	if err != nil {
		return 0, 0, fmt.Errorf("初始化 provider 失败: %w", err)
	}
	deleter, ok := drv.(types.BatchDeleter)
	if !ok {
		return 0, len(orphans), fmt.Errorf("该存储提供者不支持批量删除")
	}

	const batchSize = 100
	for i := 0; i < len(orphans); i += batchSize {
		end := i + batchSize
		if end > len(orphans) {
			end = len(orphans)
		}
		refs := make([]types.FileRef, 0, end-i)
		for _, o := range orphans[i:end] {
			refs = append(refs, types.FileRef{ProviderFileID: o.ID, Path: o.Path})
		}
		results, derr := deleter.BatchDelete(ctx, refs)
		if derr != nil {
			failed += len(refs)
			llog.L().Warnf("cleanup-storage-orphans: 批量删除 %d 个孤儿失败: %v", len(refs), derr)
			continue
		}
		if results == nil { // nil 表示全部成功
			deleted += len(refs)
			continue
		}
		for _, r := range results {
			if r.Success {
				deleted++
				continue
			}
			failed++
			llog.L().Warnf("cleanup-storage-orphans: 删除失败 location_id=%d err=%v", r.ID, r.Error)
		}
	}
	return deleted, failed, nil
}
