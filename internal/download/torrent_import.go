package download

import (
	"os"
	"path/filepath"
	"strings"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/pkg/downloader/aria2"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

// ============================================================
// 多文件种子（magnet / BT）导入
//
// aria2 一个 gid 可能产出多个文件，files[].path 相对下载根目录（dir），
// 形如 "TorrentName/sub/a.mkv"。为了让这些文件落在目标目录里还能保持结构，
// 这里在目标目录下建一个以种子名命名的子目录，再按相对路径逐级还原。
// ============================================================

// importableFilePaths 从 aria2 的文件列表挑出真正要入库的文件（纯筛选，返回相对路径）：
//   - 未选中的跳过（用户在 AriaNg 里取消勾选的文件不下、也不入库）
//   - 元数据伪文件跳过（[METADATA]xxx 只存在于内存）
//   - 没下完的跳过（piece 边界上的半成品不能入库）
func importableFilePaths(files []aria2.FileInfo) []string {
	paths := make([]string, 0, len(files))
	for _, f := range files {
		if f.Selected == "false" || f.Path == "" || isMetadataPath(f.Path) {
			continue
		}
		if !fileFullyDownloaded(f) {
			continue
		}
		paths = append(paths, f.Path)
	}
	return paths
}

// fileFullyDownloaded 该文件是否已完整下载（长度字段缺失时按已下完处理）
func fileFullyDownloaded(f aria2.FileInfo) bool {
	length, err1 := parseSize(f.Length)
	done, err2 := parseSize(f.CompletedLength)
	if err1 != nil || err2 != nil {
		return true
	}
	return done >= length
}

// importableFiles 在下载根目录下把可入库文件拼成绝对路径，并跳过磁盘上不存在的
func importableFiles(dir string, files []aria2.FileInfo) []string {
	relatives := importableFilePaths(files)
	paths := make([]string, 0, len(relatives))
	for _, rel := range relatives {
		path := filepath.Join(dir, rel)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			// 空文件 / 未真正落盘：aria2 对空文件不做验证，入网盘没有意义
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

// torrentRootName 种子根目录名：优先用 aria2 的种子名，
// 其次记录里的 sourceName，再退到第一个文件的顶层目录，最后用 gid 兜底
func torrentRootName(dr *native.DownloadRecordORM, btName string, files []string, dir string) string {
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
func cleanupTorrentLeftover(task aria2.StatusInfo) {
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
		dir := &native.FileORM{
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
