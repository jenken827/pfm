package download

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/dbtest"
	"jenken827.com/jenken827/pfm/pkg/downloader/aria2"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

// ============================================================
// 入库链路端到端（DB + 文件系统 + 假 aria2 快照）
//
// 覆盖：磁力记录 → 快照解析出产物任务 → gid 改写 → 按选择/完整性过滤文件 →
// 种子名建目录 → 迁移文件 → 清掉 [METADATA]/种子文件残留。
// ============================================================

const importTestInfoHash = "8fd37116e367f8e77fa20ff721a7f82b278cb23b"

// newImportTestUser 建一个用户空间 + 根目录，返回 (ownerID, rootID)
func newImportTestUser(t *testing.T) (string, string) {
	t.Helper()
	dbtest.Setup(t)

	ownerID := uuid.New().String()
	rootID := uuid.New().String()
	if err := db.CreateFile(&native.FileORM{
		Id:       utils.StringPtr(rootID),
		Name:     utils.StringPtr("root"),
		ParentId: utils.StringPtr(""),
		OwnerId:  utils.StringPtr(ownerID),
		Type:     utils.EnumPtr(native.FileType_FOLDER),
		IsReady:  utils.BoolPtr(true),
	}); err != nil {
		t.Fatalf("create root folder failed: %v", err)
	}
	if err := db.CreateUserSpace(&native.UserSpaceORM{
		UserId:           utils.StringPtr(ownerID),
		RootFileId:       utils.StringPtr(rootID),
		BoundProviderIds: utils.StringPtr("[1]"),
		QuotaBytes:       utils.Uint64Ptr(0),
		UsedBytes:        utils.Uint64Ptr(0),
	}); err != nil {
		t.Fatalf("create user space failed: %v", err)
	}
	return ownerID, rootID
}

func newOfflineRecord(t *testing.T, ownerID, parentID, gid, rawURL string) *native.DownloadRecordORM {
	t.Helper()
	dr := &native.DownloadRecordORM{
		Gid:              utils.StringPtr(gid),
		Blake3Hash:       utils.StringPtr(""),
		SourceLocationId: utils.Uint64Ptr(0),
		State:            utils.EnumPtr(native.DownloadRecordState_DOWNLOAD_PENDING),
		Url:              utils.StringPtr(rawURL),
		OwnerId:          utils.StringPtr(ownerID),
		ParentId:         utils.StringPtr(parentID),
	}
	if err := db.CreateDownloadRecord(dr); err != nil {
		t.Fatalf("create download record failed: %v", err)
	}
	return dr
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// snapshotFromJSON 用 JSON 构造 aria2.StatusInfo（BitTorrent 是匿名结构体，只能反序列化）
func snapshotFromJSON(t *testing.T, raws ...string) *taskSnapshot {
	t.Helper()
	snap := &taskSnapshot{}
	for _, raw := range raws {
		var info aria2.StatusInfo
		if err := json.Unmarshal([]byte(raw), &info); err != nil {
			t.Fatalf("unmarshal status: %v", err)
		}
		snap.tasks = append(snap.tasks, info)
	}
	snap.index()
	return snap
}

// 磁力多文件：只入库选中的文件，种子名建目录，gid 改写成产物 gid，残留种子文件被清理
func TestReconcileImportsTorrentProduct(t *testing.T) {
	ownerID, rootID := newImportTestUser(t)
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "MyTorrent", "a.bin"), "aaa")
	mustWriteFile(t, filepath.Join(dir, "MyTorrent", "sub", "b.bin"), "bbb")
	mustWriteFile(t, filepath.Join(dir, "MyTorrent", "skip.bin"), "skipped")
	mustWriteFile(t, filepath.Join(dir, importTestInfoHash+".torrent"), "d4:infoe")

	metaGid, btGid := "meta-"+uuid.New().String(), "bt-"+uuid.New().String()
	dr := newOfflineRecord(t, ownerID, rootID, metaGid,
		"magnet:?xt=urn:btih:"+importTestInfoHash+"&dn=MyTorrent")

	metaTask := `{"gid":"` + metaGid + `","status":"complete","infoHash":"` + importTestInfoHash + `","dir":"` + dir + `",
		"files":[{"path":"[METADATA]MyTorrent","length":"0","selected":"true"}],"followedBy":["` + btGid + `"]}`
	productTask := `{"gid":"` + btGid + `","status":"active","infoHash":"` + importTestInfoHash + `","dir":"` + dir + `",
		"totalLength":"6","completedLength":"6","bittorrent":{"info":{"name":"MyTorrent"}},
		"files":[
			{"path":"MyTorrent/a.bin","length":"3","completedLength":"3","selected":"true"},
			{"path":"MyTorrent/sub/b.bin","length":"3","completedLength":"3","selected":"true"},
			{"path":"MyTorrent/skip.bin","length":"7","completedLength":"0","selected":"false"}
		]}`

	reconcileOneRecord(dr, snapshotFromJSON(t, metaTask, productTask))

	reloaded, err := db.GetDownloadRecordByID(*dr.Id)
	if err != nil {
		t.Fatalf("reload record: %v", err)
	}
	if got := native.DownloadRecordState(*reloaded.State); got != native.DownloadRecordState_DOWNLOAD_COMPLETED {
		t.Fatalf("record state = %v, want COMPLETED（err: %s）", got, derefString(reloaded.ErrorMessage))
	}
	if got := derefString(reloaded.Gid); got != btGid {
		t.Fatalf("record gid = %q, want %q（必须改写成产物 gid）", got, btGid)
	}

	root, err := db.FindFileByParentAndName(rootID, "MyTorrent", ownerID)
	if err != nil || root == nil || root.Id == nil {
		t.Fatalf("种子目录未创建: %v", err)
	}
	if f, _ := db.FindFileByParentAndName(*root.Id, "a.bin", ownerID); f == nil {
		t.Error("a.bin 未入库")
	}
	sub, _ := db.FindFileByParentAndName(*root.Id, "sub", ownerID)
	if sub == nil || sub.Id == nil {
		t.Fatal("子目录 sub 未创建")
	}
	if f, _ := db.FindFileByParentAndName(*sub.Id, "b.bin", ownerID); f == nil {
		t.Error("sub/b.bin 未入库")
	}
	if f, _ := db.FindFileByParentAndName(*root.Id, "skip.bin", ownerID); f != nil {
		t.Error("未选中的 skip.bin 不应入库")
	}
	if _, err := os.Stat(filepath.Join(dir, importTestInfoHash+".torrent")); !os.IsNotExist(err) {
		t.Error("残留的 <infohash>.torrent 应被清理")
	}
	if _, err := os.Stat(filepath.Join(dir, "MyTorrent")); !os.IsNotExist(err) {
		t.Error("aria2 下载目录应在导入后清理")
	}
}

// 磁力：元数据刚完成、BT 任务还没派生出来 → 保持下载中，不能判失败也不能入库
func TestReconcileWaitsForTorrentProduct(t *testing.T) {
	ownerID, rootID := newImportTestUser(t)
	dir := t.TempDir()
	metaGid := "meta-" + uuid.New().String()
	dr := newOfflineRecord(t, ownerID, rootID, metaGid,
		"magnet:?xt=urn:btih:"+importTestInfoHash)

	metaTask := `{"gid":"` + metaGid + `","status":"complete","infoHash":"` + importTestInfoHash + `","dir":"` + dir + `",
		"files":[{"path":"[METADATA]data","length":"0","selected":"true"}]}`

	reconcileOneRecord(dr, snapshotFromJSON(t, metaTask))

	reloaded, err := db.GetDownloadRecordByID(*dr.Id)
	if err != nil {
		t.Fatalf("reload record: %v", err)
	}
	if got := native.DownloadRecordState(*reloaded.State); got != native.DownloadRecordState_DOWNLOADING {
		t.Fatalf("record state = %v, want DOWNLOADING（元数据阶段不能判失败）", got)
	}
}

// 任务已完成后逐文件 completedLength 恒为 0：已下好的文件仍要入库
//
// 回归背景（真实事故）：aria2 把 BT 任务转入终态后不再保留分片位图，
// files[].completedLength 全部回报 0（任务级 completedLength 正确）。
// 旧逻辑按逐文件长度判断"是否下完"，于是把已下好的文件全部跳过，
// 记录被判「aria2 已完成但没有可导入的文件」，且 FAILED 不再被 reconcile 扫描，
// 已完成的任务只能整单重下。
func TestReconcileImportsCompletedTorrentWithZeroPerFileLength(t *testing.T) {
	ownerID, rootID := newImportTestUser(t)
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "FC2PPV-1", "keep.mp4"), "video")
	mustWriteFile(t, filepath.Join(dir, "FC2PPV-1", "info.txt"), "info")

	btGid := "bt-" + uuid.New().String()
	dr := newOfflineRecord(t, ownerID, rootID, btGid,
		"magnet:?xt=urn:btih:"+importTestInfoHash)

	// 终态任务：任务级 10/10 完成，但逐文件 completedLength 全是 0
	productTask := `{"gid":"` + btGid + `","status":"complete","infoHash":"` + importTestInfoHash + `","dir":"` + dir + `",
		"totalLength":"10","completedLength":"10","bittorrent":{"info":{"name":"FC2PPV-1"}},
		"files":[
			{"path":"FC2PPV-1/keep.mp4","length":"5","completedLength":"0","selected":"true"},
			{"path":"FC2PPV-1/info.txt","length":"4","completedLength":"0","selected":"true"},
			{"path":"FC2PPV-1/skip.mp4","length":"5","completedLength":"0","selected":"false"}
		]}`

	reconcileOneRecord(dr, snapshotFromJSON(t, productTask))

	reloaded, err := db.GetDownloadRecordByID(*dr.Id)
	if err != nil {
		t.Fatalf("reload record: %v", err)
	}
	if got := native.DownloadRecordState(*reloaded.State); got != native.DownloadRecordState_DOWNLOAD_COMPLETED {
		t.Fatalf("record state = %v, want COMPLETED（err: %s）", got, derefString(reloaded.ErrorMessage))
	}

	root, err := db.FindFileByParentAndName(rootID, "FC2PPV-1", ownerID)
	if err != nil || root == nil || root.Id == nil {
		t.Fatalf("种子目录未创建: %v", err)
	}
	if f, _ := db.FindFileByParentAndName(*root.Id, "keep.mp4", ownerID); f == nil {
		t.Error("keep.mp4 未入库")
	}
	if f, _ := db.FindFileByParentAndName(*root.Id, "info.txt", ownerID); f == nil {
		t.Error("info.txt 未入库")
	}
	if f, _ := db.FindFileByParentAndName(*root.Id, "skip.mp4", ownerID); f != nil {
		t.Error("未选中的 skip.mp4 不应入库")
	}
}

// 产物的文件全部未选中 → 没有可导入的文件，判失败并给出原因
func TestReconcileFailsWhenNothingSelected(t *testing.T) {
	ownerID, rootID := newImportTestUser(t)
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "skip.bin"), "skipped")
	btGid := "bt-" + uuid.New().String()
	dr := newOfflineRecord(t, ownerID, rootID, btGid,
		"magnet:?xt=urn:btih:"+importTestInfoHash)

	productTask := `{"gid":"` + btGid + `","status":"complete","infoHash":"` + importTestInfoHash + `","dir":"` + dir + `",
		"totalLength":"7","completedLength":"0",
		"files":[{"path":"skip.bin","length":"7","completedLength":"0","selected":"false"}]}`

	reconcileOneRecord(dr, snapshotFromJSON(t, productTask))

	reloaded, err := db.GetDownloadRecordByID(*dr.Id)
	if err != nil {
		t.Fatalf("reload record: %v", err)
	}
	if got := native.DownloadRecordState(*reloaded.State); got != native.DownloadRecordState_DOWNLOAD_FAILED {
		t.Fatalf("record state = %v, want FAILED", got)
	}
	if derefString(reloaded.ErrorMessage) == "" {
		t.Error("失败应写入可读原因")
	}
}

// .torrent URL + aria2 重启：infohash 记进 callback_info 后，新 gid 也能重新关联
func TestReconcileRecoversTorrentByStoredInfoHash(t *testing.T) {
	ownerID, rootID := newImportTestUser(t)
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "MyTorrent", "data.bin"), "data")

	torrentGid, btGid := "torrent-"+uuid.New().String(), "bt-"+uuid.New().String()
	dr := newOfflineRecord(t, ownerID, rootID, torrentGid, "https://example.com/test.torrent")

	torrentTask := `{"gid":"` + torrentGid + `","status":"complete","dir":"` + dir + `",
		"files":[{"path":"test.torrent","length":"379","selected":"true"}],"followedBy":["` + btGid + `"]}`
	btTask := `{"gid":"` + btGid + `","status":"waiting","infoHash":"` + importTestInfoHash + `","dir":"` + dir + `",
		"totalLength":"4","completedLength":"0","bittorrent":{"info":{"name":"MyTorrent"}},
		"files":[{"path":"MyTorrent/data.bin","length":"4","completedLength":"0","selected":"true"}]}`

	reconcileOneRecord(dr, snapshotFromJSON(t, torrentTask, btTask))

	reloaded, err := db.GetDownloadRecordByID(*dr.Id)
	if err != nil {
		t.Fatalf("reload record: %v", err)
	}
	if got := derefString(reloaded.Gid); got != btGid {
		t.Fatalf("record gid = %q, want %q", got, btGid)
	}
	if got := storedInfoHash(derefString(reloaded.CallbackInfo)); got != importTestInfoHash {
		t.Fatalf("callback_info 里的 infohash = %q, want %q", got, importTestInfoHash)
	}
	if got := native.DownloadRecordState(*reloaded.State); got != native.DownloadRecordState_DOWNLOADING {
		t.Fatalf("record state = %v, want DOWNLOADING", got)
	}

	// 模拟 aria2 重启：gid 全变，只剩产物任务（种子文件任务已消失）
	staleGid := "stale-" + uuid.New().String()
	reloaded.Gid = utils.StringPtr(staleGid)
	if err := db.UpdateDownloadRecord(reloaded, []string{"gid"}); err != nil {
		t.Fatalf("reset gid: %v", err)
	}
	newBtGid := "bt-new-" + uuid.New().String()
	newBtTask := `{"gid":"` + newBtGid + `","status":"active","infoHash":"` + importTestInfoHash + `","dir":"` + dir + `",
		"totalLength":"4","completedLength":"4","bittorrent":{"info":{"name":"MyTorrent"}},
		"files":[{"path":"MyTorrent/data.bin","length":"4","completedLength":"4","selected":"true"}]}`

	reconcileOneRecord(reloaded, snapshotFromJSON(t, newBtTask))

	final, err := db.GetDownloadRecordByID(*dr.Id)
	if err != nil {
		t.Fatalf("reload record: %v", err)
	}
	if got := native.DownloadRecordState(*final.State); got != native.DownloadRecordState_DOWNLOAD_COMPLETED {
		t.Fatalf("record state = %v, want COMPLETED（err: %s）", got, derefString(final.ErrorMessage))
	}
	if got := derefString(final.Gid); got != newBtGid {
		t.Fatalf("record gid = %q, want %q", got, newBtGid)
	}
}

// TestResolveImportName 离线导入的落名规则：同名同内容视为已存在（秒传跳过），
// 同名不同内容自动改名，不覆盖用户已有文件、也不重复堆副本。
func TestResolveImportName(t *testing.T) {
	ownerID, rootID := newImportTestUser(t)

	// 空目录：用原名
	if name, dup := resolveImportName(ownerID, rootID, "clip.mp4", "hash-a"); name != "clip.mp4" || dup {
		t.Fatalf("空目录应为 clip.mp4/false, got %q/%v", name, dup)
	}

	// 目录里已有 clip.mp4（hash-a）：同内容 → 跳过
	if err := db.CreateFile(&native.FileORM{
		Id:       utils.StringPtr(uuid.New().String()),
		Name:     utils.StringPtr("clip.mp4"),
		ParentId: utils.StringPtr(rootID),
		OwnerId:  utils.StringPtr(ownerID),
		Type:     utils.EnumPtr(native.FileType_VIDEO),
		Hash:     utils.StringPtr("hash-a"),
		Size:     utils.Uint64Ptr(1),
		IsReady:  utils.BoolPtr(true),
	}); err != nil {
		t.Fatalf("create existing file: %v", err)
	}
	if name, dup := resolveImportName(ownerID, rootID, "clip.mp4", "hash-a"); name != "clip.mp4" || !dup {
		t.Fatalf("同名同内容应跳过, got %q/%v", name, dup)
	}

	// 同名不同内容 → clip (1).mp4
	name, dup := resolveImportName(ownerID, rootID, "clip.mp4", "hash-b")
	if name != "clip (1).mp4" || dup {
		t.Fatalf("同名不同内容应改名为 clip (1).mp4, got %q/%v", name, dup)
	}

	// 再下一个不同内容 → clip (2).mp4
	if err := db.CreateFile(&native.FileORM{
		Id:       utils.StringPtr(uuid.New().String()),
		Name:     utils.StringPtr("clip (1).mp4"),
		ParentId: utils.StringPtr(rootID),
		OwnerId:  utils.StringPtr(ownerID),
		Type:     utils.EnumPtr(native.FileType_VIDEO),
		Hash:     utils.StringPtr("hash-b"),
		Size:     utils.Uint64Ptr(1),
		IsReady:  utils.BoolPtr(true),
	}); err != nil {
		t.Fatalf("create existing file 2: %v", err)
	}
	if name, dup := resolveImportName(ownerID, rootID, "clip.mp4", "hash-c"); name != "clip (2).mp4" || dup {
		t.Fatalf("应为 clip (2).mp4, got %q/%v", name, dup)
	}

	// 重复下载同一份内容（与 clip (1).mp4 同 hash）→ 秒传跳过，不再堆副本
	if name, dup := resolveImportName(ownerID, rootID, "clip.mp4", "hash-b"); name != "clip (1).mp4" || !dup {
		t.Fatalf("同内容重下应秒传跳过到 clip (1).mp4, got %q/%v", name, dup)
	}
}
