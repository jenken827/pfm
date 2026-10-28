package download

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/downloader"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/utils"
)

// ============================================================
// 多文件种子（magnet / BT）导入
//
// aria2 一个 gid 可能产出多个文件，files[].path 相对下载根目录（dir），
// 形如 "TorrentName/sub/a.mkv"。为了让这些文件落在目标目录里还能保持结构，
// 这里在目标目录下建一个以种子名命名的子目录，再按相对路径逐级还原。
// ============================================================

// importableFiles 在下载根目录下把可入库文件拼成绝对路径：
//   - 未选中的跳过（用户在 AriaNg 里取消勾选的文件不下、也不入库）
//   - 元数据伪文件跳过（[METADATA]xxx 只存在于内存）
//   - 没真正落盘 / 落盘长度不对的跳过（piece 边界上的半成品不能入库）
//
// 完成判据只看磁盘上的实际大小，**不看** aria2 的 files[].completedLength：
// BT 任务一旦进入终态（stopped/complete），aria2 不再保留分片位图，逐文件的
// completedLength 全部回报 0（任务级的 completedLength 总量仍然正确）。按它判断
// 会把已经下好的文件当成半成品全部跳掉，用户侧表现为「aria2 已完成但没有可导入
// 的文件」，而且记录被标失败后 reconcile 不再扫，任务无法自愈。
func importableFiles(dir string, files []downloader.File) []string {
	paths := make([]string, 0, len(files))
	for _, f := range files {
		if f.Selected == "false" || f.Path == "" || isMetadataPath(f.Path) {
			continue
		}
		path := resolveTaskPath(dir, f.Path)
		if !fileOnDiskComplete(path, f.Length) {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

// resolveTaskPath 把 aria2 的 files[].path 还原成本地绝对路径。
//
// aria2 RPC 返回的 path 已经是「下载目录 + 相对路径」的绝对路径，直接
// filepath.Join(dir, path) 会拼成 dir/dir/相对路径，stat 必然失败 —— 表现为
// 每次导入都「没有可导入的文件」。相对路径写法（测试 / 老版本 aria2）也兼容。
func resolveTaskPath(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

// fileOnDiskComplete 文件是否已按种子记录的长度完整落盘。
// 长度字段缺失（元数据等）时按"落盘即算"处理。
func fileOnDiskComplete(path, expectedLength string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	length, err := parseSize(expectedLength)
	if err != nil {
		return true
	}
	return uint64(info.Size()) == length
}

// torrentRootName 种子根目录名：优先用 aria2 的种子名，
// 其次记录里的 sourceName，再退到第一个文件的顶层目录，最后用 gid 兜底
func torrentRootName(dr *entity.DownloadRecordORM, btName string, files []string, dir string) string {
	if name := safeSegment(btName); name != "" {
		return name
	}
	if dr.SourceName != nil && *dr.SourceName != "" {
		if name := safeSegment(*dr.SourceName); name != "" {
			return name
		}
	}
	for _, p := range files {
		segs := relativeSegments(dir, p)
		if len(segs) > 1 {
			return segs[0]
		}
	}
	if dr.Gid != nil {
		return "download-" + *dr.Gid
	}
	return "download"
}

// cleanupTorrentLeftover 清理 aria2 留下的种子文件（bt-save-metadata 会把
// <infohash>.torrent 写进下载目录，它不在 files 列表里，不会被导入，只会越堆越多）
func cleanupTorrentLeftover(task downloader.Task) {
	if task.InfoHash == "" || task.Dir == "" {
		return
	}
	for _, name := range []string{
		strings.ToLower(task.InfoHash) + torrentFileSuffix,
		strings.ToUpper(task.InfoHash) + torrentFileSuffix,
	} {
		path := filepath.Join(task.Dir, name)
		if err := os.Remove(path); err == nil {
			llog.L().Infof("offline download: removed leftover torrent file %s", path)
		}
	}
}

// relativeSegments 把 aria2 的绝对路径拆成相对下载根目录的路径段，
// 并过滤掉空段与 "."、".."（防路径穿越）
func relativeSegments(dir, filePath string) []string {
	rel, err := filepath.Rel(dir, filePath)
	if err != nil {
		rel = filepath.Base(filePath)
	}
	raw := strings.Split(filepath.ToSlash(rel), "/")
	segs := make([]string, 0, len(raw))
	for _, seg := range raw {
		if name := safeSegment(seg); name != "" {
			segs = append(segs, name)
		}
	}
	if len(segs) == 0 {
		segs = append(segs, filepath.Base(filePath))
	}
	return segs
}

// safeSegment 单段目录名净化：去掉路径分隔符与穿越段
func safeSegment(seg string) string {
	seg = strings.TrimSpace(strings.ReplaceAll(seg, "\\", "/"))
	seg = filepath.Base(seg)
	if seg == "" || seg == "." || seg == ".." || seg == "/" {
		return ""
	}
	return seg
}

// ensureFolderPath 在 parentID 下按路径段逐级确保目录存在，返回叶子目录 ID
//
// 与 service.EnsureDirPath 语义一致（mkdir -p），但只依赖 db 层，供后台
// 协程使用（无请求 ctx）。并发同名创建时靠 (owner_id,name,parent_id)
// 唯一索引兜底：冲突后重查已存在目录。
func ensureFolderPath(ownerID, parentID string, segments []string) (string, error) {
	current := parentID
	for _, seg := range segments {
		name := safeSegment(seg)
		if name == "" {
			continue
		}
		existing, err := db.FindFileByParentAndName(current, name, ownerID)
		if err == nil && existing != nil && existing.Id != nil {
			current = *existing.Id
			continue
		}
		dir := &entity.FileORM{
			Name:     utils.StringPtr(name),
			ParentId: utils.StringPtr(current),
			Type:     utils.EnumPtr(native.FileType_FOLDER),
			IsReady:  utils.BoolPtr(true),
			OwnerId:  utils.StringPtr(ownerID),
		}
		if err := db.CreateFile(dir); err != nil {
			// 并发创建同名目录 → 唯一键冲突，重查
			if retry, qErr := db.FindFileByParentAndName(current, name, ownerID); qErr == nil &&
				retry != nil && retry.Id != nil {
				current = *retry.Id
				continue
			}
			llog.L().Errorf("ensure folder %s under %s failed: %v", name, current, err)
			return "", err
		}
		current = *dir.Id
	}
	return current, nil
}
