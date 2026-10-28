package download

import (
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm/internal/downloader"
	"github.com/jenken827/pfm/pkg/llog"
)

// ============================================================
// 下载器任务快照与「产物任务」解析
//
// pfm 不管理下载器：暂停 / 选文件 / 继续 / 删任务都由 AriaNg 负责。
// pfm 只做三件事：提交任务、等它下完、找到真正的产物文件并入库。
//
// 难点是磁力与 .torrent 的任务有两段：
//
//	后端 AddURI → 元数据 / 种子文件任务（files 是 [METADATA]xxx 或 xxx.torrent）
//	             → followedBy 派生出真正的 BT 任务（内容文件在后者）
//
// 记录里存的是第一段的 gid，所以既不能直接拿它的 files 导入，也不能只按
// gid 找产物。这里用 infoHash 作为跨 gid / 跨下载器重启的主键，并用任务
// 形态判断「哪个才是产物」。
// ============================================================

const (
	// metadataPathPrefix 下载器 磁力元数据伪文件的路径前缀（只存在于内存，磁盘上没有）
	metadataPathPrefix = "[METADATA]"
	// torrentFileSuffix 种子文件后缀
	torrentFileSuffix = ".torrent"
	// maxFollowDepth 跟随 followedBy / 种子文件的层数上限（防异常数据导致死循环）
	maxFollowDepth = 3
)

// taskRole 任务在「元数据 → 产物」链路里的角色
type taskRole int

const (
	roleUnknown  taskRole = iota
	roleFinal             // 产物任务：files 就是最终要入库的文件
	roleMetadata          // 磁力元数据阶段，或已派生子任务的中间任务
	roleTorrent           // 种子文件阶段：先下 .torrent，再派生真正的 BT 任务
)

// taskSnapshot 一次 RPC 拿到的 下载器 任务全集 + 三类索引
type taskSnapshot struct {
	tasks      []downloader.Task
	byGid      map[string]downloader.Task
	byInfoHash map[string][]downloader.Task
	byURL      map[string][]downloader.Task
}

// fetchTaskSnapshot 拉取活跃 / 等待 / 已停止三类任务并建索引
func fetchTaskSnapshot(c downloader.Backend) *taskSnapshot {
	snap := &taskSnapshot{}
	ctx := context.Background()
	fetches := []func() ([]downloader.Task, error){
		func() ([]downloader.Task, error) { return c.ListActive(ctx) },
		func() ([]downloader.Task, error) { return c.ListWaiting(ctx, 0, 1000) },
		func() ([]downloader.Task, error) { return c.ListStopped(ctx, 0, 1000) },
	}
	for _, fetch := range fetches {
		infos, err := fetch()
		if err != nil {
			llog.L().Warnf("offline download: 拉取下载器任务失败: %v", err)
			continue
		}
		snap.tasks = append(snap.tasks, infos...)
	}
	snap.index()
	return snap
}

// index 建 gid / infoHash / URL 三类索引
func (s *taskSnapshot) index() {
	s.byGid = make(map[string]downloader.Task, len(s.tasks))
	s.byInfoHash = make(map[string][]downloader.Task)
	s.byURL = make(map[string][]downloader.Task)
	for _, info := range s.tasks {
		if info.Gid == "" {
			continue
		}
		s.byGid[info.Gid] = info
		if ih := strings.ToLower(info.InfoHash); ih != "" {
			s.byInfoHash[ih] = append(s.byInfoHash[ih], info)
		}
		for _, f := range info.Files {
			for _, u := range f.URIs {
				if u.URI != "" {
					s.byURL[u.URI] = append(s.byURL[u.URI], info)
				}
			}
		}
	}
}

// classifyTask 判断任务角色（纯函数，便于单测）
func classifyTask(info downloader.Task) taskRole {
	if len(info.Files) == 0 {
		return roleUnknown
	}
	if hasMetadataFile(info.Files) {
		return roleMetadata
	}
	if allTorrentFiles(info.Files) {
		return roleTorrent
	}
	// 派生了子任务的中间任务（metalink 等），本身不是产物
	if len(info.FollowedBy) > 0 {
		return roleMetadata
	}
	return roleFinal
}

// hasMetadataFile 是否含 下载器的元数据伪文件
func hasMetadataFile(files []downloader.File) bool {
	for _, f := range files {
		if isMetadataPath(f.Path) {
			return true
		}
	}
	return false
}

// isMetadataPath 路径是否为元数据伪文件（[METADATA]xxx）
func isMetadataPath(path string) bool {
	return strings.HasPrefix(filepath.Base(path), metadataPathPrefix)
}

// allTorrentFiles 是否全部是 .torrent 文件（种子文件下载阶段）
func allTorrentFiles(files []downloader.File) bool {
	for _, f := range files {
		if !strings.HasSuffix(strings.ToLower(f.Path), torrentFileSuffix) {
			return false
		}
	}
	return true
}

// isFinished 下载是否已完成。
//
// BT 任务下完后 status 仍是 active（做种），只能靠长度判断；paused 同理
// （用户在 AriaNg 里暂停后继续之前，已下的部分不应被当成失败）。
func isFinished(info downloader.Task) bool {
	if info.Status == "complete" {
		return true
	}
	if info.Status != "active" && info.Status != "paused" && info.Status != "waiting" {
		return false
	}
	total, err1 := strconv.ParseUint(info.TotalLength, 10, 64)
	done, err2 := strconv.ParseUint(info.CompletedLength, 10, 64)
	return err1 == nil && err2 == nil && total > 0 && done >= total
}

// isFailedTask 下载器侧已判失败（error / 被移除）
func isFailedTask(info downloader.Task) bool {
	return info.Status == "error" || info.Status == "removed"
}

// magnetInfoHash 从磁力链取 btih（统一小写）；非磁力 / 非 40 位 hex 返回空
func magnetInfoHash(raw string) string {
	if raw == "" || !strings.HasPrefix(strings.ToLower(raw), "magnet:") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	for _, xt := range u.Query()["xt"] {
		const prefix = "urn:btih:"
		if len(xt) > len(prefix) && strings.EqualFold(xt[:len(prefix)], prefix) {
			ih := xt[len(prefix):]
			if len(ih) == 40 {
				return strings.ToLower(ih)
			}
		}
	}
	return ""
}

// torrentBaseName 从 .torrent URL 取文件名（用于 gid 失效时按文件名找回任务）
func torrentBaseName(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if !strings.HasSuffix(strings.ToLower(u.Path), torrentFileSuffix) {
		return ""
	}
	return filepath.Base(u.Path)
}

// recordInfoHash 记录关联的 infohash：磁力链直接解析，.torrent URL 用对齐时记下的值
func recordInfoHash(dr *entity.DownloadRecordORM) string {
	if ih := magnetInfoHash(derefString(dr.Url)); ih != "" {
		return ih
	}
	return storedInfoHash(derefString(dr.CallbackInfo))
}

// storedInfoHash 从 callback_info 里取 infohash
func storedInfoHash(callbackInfo string) string {
	if callbackInfo == "" {
		return ""
	}
	var payload struct {
		InfoHash string `json:"info_hash"`
	}
	if err := json.Unmarshal([]byte(callbackInfo), &payload); err != nil {
		return ""
	}
	return strings.ToLower(payload.InfoHash)
}

// targetFor 为一条下载记录挑出「产物任务」。
//
// 返回的任务可能仍是中间任务（元数据 / 种子文件阶段），调用方用 classifyTask
// 判断；ok == false 表示 下载器里完全找不到这单。
func (s *taskSnapshot) targetFor(dr *entity.DownloadRecordORM) (downloader.Task, bool) {
	// 1. infohash：跨 gid、跨下载器重启都稳定。
	//    磁力链自带；.torrent URL 的 infohash 由对齐时记进 callback_info。
	if ih := recordInfoHash(dr); ih != "" {
		if task, ok := s.pickByInfoHash(ih); ok {
			return s.productOf(task, 0), true
		}
	}

	// 2. 记录里的 gid
	if task, ok := s.byGid[derefString(dr.Gid)]; ok {
		return s.productOf(task, 0), true
	}

	// 3. gid 失效（下载器 重启后换 gid）：按 URL 找回
	if cands := s.byURL[derefString(dr.Url)]; len(cands) > 0 {
		return s.productOf(cands[0], 0), true
	}

	// 4. 种子 URL：gid 和 URL 都失效时，按种子文件名匹配仍在下种子的任务
	if base := torrentBaseName(derefString(dr.Url)); base != "" {
		for _, task := range s.tasks {
			if classifyTask(task) == roleTorrent && filepath.Base(task.Files[0].Path) == base {
				return s.productOf(task, 0), true
			}
		}
	}
	return downloader.Task{}, false
}

// productOf 顺着「元数据 / 种子文件」找到真正的产物任务；找不到时返回原任务
func (s *taskSnapshot) productOf(info downloader.Task, depth int) downloader.Task {
	if depth >= maxFollowDepth {
		return info
	}
	switch classifyTask(info) {
	case roleFinal:
		return info
	case roleMetadata, roleTorrent:
		// 先顺着 followedBy 找子任务
		if child, ok := s.childOf(info); ok {
			return s.productOf(child, depth+1)
		}
		// 种子文件阶段且 followedBy 丢了：算 infohash 反查 BT 任务
		if classifyTask(info) == roleTorrent {
			if child, ok := s.btTaskByTorrentFile(info); ok {
				return s.productOf(child, depth+1)
			}
		}
	}
	return info
}

// childOf 取 followedBy 里第一个还在下载器里的子任务
func (s *taskSnapshot) childOf(info downloader.Task) (downloader.Task, bool) {
	for _, gid := range info.FollowedBy {
		if child, ok := s.byGid[gid]; ok {
			return child, true
		}
	}
	return downloader.Task{}, false
}

// btTaskByTorrentFile 种子文件已下好但 followedBy 关系丢了：算 infohash 反查 BT 任务
func (s *taskSnapshot) btTaskByTorrentFile(info downloader.Task) (downloader.Task, bool) {
	if len(info.Files) == 0 {
		return downloader.Task{}, false
	}
	torrentPath := filepath.Join(info.Dir, info.Files[0].Path)
	ih, err := torrentInfoHash(torrentPath)
	if err != nil || ih == "" {
		return downloader.Task{}, false
	}
	return s.pickByInfoHash(ih)
}

// pickByInfoHash 从同一 infohash 的任务里挑最像产物的那个：
// 已下完的产物 > 产物 > 任一（元数据阶段）
func (s *taskSnapshot) pickByInfoHash(ih string) (downloader.Task, bool) {
	cands := s.byInfoHash[strings.ToLower(ih)]
	if len(cands) == 0 {
		return downloader.Task{}, false
	}
	for _, c := range cands {
		if classifyTask(c) == roleFinal && isFinished(c) {
			return c, true
		}
	}
	for _, c := range cands {
		if classifyTask(c) == roleFinal {
			return c, true
		}
	}
	return cands[0], true
}

// derefString 取指针字符串的值，nil 返回空串
func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// parseSize 解析 下载器返回的字节数字符串（可能为空）
func parseSize(s string) (uint64, error) {
	return strconv.ParseUint(s, 10, 64)
}
