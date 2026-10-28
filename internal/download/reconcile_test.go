package download

import (
	"crypto/sha1"
	"encoding/hex"
	"github.com/jenken827/pfm/internal/downloader"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm/utils"
)

func TestClassifyTask(t *testing.T) {
	cases := []struct {
		name string
		info downloader.Task
		want taskRole
	}{
		{
			name: "磁力元数据伪文件",
			info: downloader.Task{Files: []downloader.File{{Path: "[METADATA]data.bin"}}},
			want: roleMetadata,
		},
		{
			name: "种子文件下载阶段",
			info: downloader.Task{Files: []downloader.File{{Path: "/d/test.torrent"}}},
			want: roleTorrent,
		},
		{
			name: "派生了子任务的中间任务",
			info: downloader.Task{
				Files:      []downloader.File{{Path: "/d/a.iso"}},
				FollowedBy: []string{"child"},
			},
			want: roleMetadata,
		},
		{
			name: "真正的产物任务",
			info: downloader.Task{
				Files: []downloader.File{{Path: "Torrent/a.mkv"}, {Path: "Torrent/b.srt"}},
			},
			want: roleFinal,
		},
		{
			name: "空任务",
			info: downloader.Task{},
			want: roleUnknown,
		},
	}
	for _, c := range cases {
		if got := classifyTask(c.info); got != c.want {
			t.Errorf("classifyTask(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

// BT 下完进入做种期时 status 仍是 active，必须靠长度判定完成
func TestIsFinished(t *testing.T) {
	cases := []struct {
		info downloader.Task
		want bool
	}{
		{downloader.Task{Status: "complete"}, true},
		{downloader.Task{Status: "active", TotalLength: "100", CompletedLength: "100"}, true},
		{downloader.Task{Status: "active", TotalLength: "100", CompletedLength: "99"}, false},
		{downloader.Task{Status: "paused", TotalLength: "100", CompletedLength: "100"}, true},
		{downloader.Task{Status: "paused"}, false},
		{downloader.Task{Status: "error", TotalLength: "100", CompletedLength: "100"}, false},
		{downloader.Task{Status: "removed"}, false},
	}
	for _, c := range cases {
		if got := isFinished(c.info); got != c.want {
			t.Errorf("isFinished(%+v) = %v, want %v", c.info, got, c.want)
		}
	}
}

func TestMagnetInfoHash(t *testing.T) {
	const ih = "8fd37116e367f8e77fa20ff721a7f82b278cb23b"
	cases := map[string]string{
		"magnet:?xt=urn:btih:" + ih + "&dn=x":                          ih,
		"magnet:?xt=urn:btih:8FD37116E367F8E77FA20FF721A7F82B278CB23B": ih,
		"magnet:?dn=no-hash":                                           "",
		"http://example.com/a.iso":                                     "",
		"magnet:?xt=urn:btih:short":                                    "",
	}
	for raw, want := range cases {
		if got := magnetInfoHash(raw); got != want {
			t.Errorf("magnetInfoHash(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestTorrentBaseName(t *testing.T) {
	if got := torrentBaseName("https://example.com/a/test.torrent?x=1"); got != "test.torrent" {
		t.Errorf("torrentBaseName = %q, want test.torrent", got)
	}
	if got := torrentBaseName("https://example.com/a.iso"); got != "" {
		t.Errorf("torrentBaseName(non-torrent) = %q, want empty", got)
	}
}

// 磁力：记录里存的是元数据 gid，产物在 followedBy 派生出的 BT 任务里
func TestTargetForMagnetPicksBTProduct(t *testing.T) {
	const ih = "8fd37116e367f8e77fa20ff721a7f82b278cb23b"
	snap := &taskSnapshot{tasks: []downloader.Task{
		{
			Gid:        "meta-gid",
			Status:     "complete",
			InfoHash:   ih,
			Files:      []downloader.File{{Path: "[METADATA]data.bin", Length: "328", Selected: "true"}},
			FollowedBy: []string{"bt-gid"},
		},
		{
			Gid:             "bt-gid",
			Status:          "complete",
			InfoHash:        ih,
			TotalLength:     "200000",
			CompletedLength: "200000",
			Files:           []downloader.File{{Path: "data.bin", Length: "200000", CompletedLength: "200000", Selected: "true"}},
		},
	}}
	snap.index()

	dr := &entity.DownloadRecordORM{
		Gid: utils.StringPtr("meta-gid"),
		Url: utils.StringPtr("magnet:?xt=urn:btih:" + ih + "&dn=data.bin"),
	}
	task, ok := snap.targetFor(dr)
	if !ok {
		t.Fatal("targetFor returned not found")
	}
	if task.Gid != "bt-gid" {
		t.Fatalf("targetFor gid = %q, want bt-gid", task.Gid)
	}
	if got := classifyTask(task); got != roleFinal {
		t.Fatalf("target role = %v, want roleFinal", got)
	}
}

// 元数据还没派生 BT 任务时，只能返回中间任务（调用方据此继续等，不能导入）
func TestTargetForMagnetBeforeBTExists(t *testing.T) {
	const ih = "8fd37116e367f8e77fa20ff721a7f82b278cb23b"
	snap := &taskSnapshot{tasks: []downloader.Task{{
		Gid:      "meta-gid",
		Status:   "complete",
		InfoHash: ih,
		Files:    []downloader.File{{Path: "[METADATA]data.bin"}},
	}}}
	snap.index()

	dr := &entity.DownloadRecordORM{
		Gid: utils.StringPtr("meta-gid"),
		Url: utils.StringPtr("magnet:?xt=urn:btih:" + ih),
	}
	task, ok := snap.targetFor(dr)
	if !ok {
		t.Fatal("targetFor returned not found")
	}
	if classifyTask(task) == roleFinal {
		t.Fatal("metadata task must not be treated as final")
	}
}

// .torrent URL：先下种子文件，再派生 BT 任务
func TestTargetForTorrentURLFollowsFollowedBy(t *testing.T) {
	snap := &taskSnapshot{tasks: []downloader.Task{
		{
			Gid:        "torrent-gid",
			Status:     "complete",
			Dir:        "/tmp/dl",
			Files:      []downloader.File{{Path: "/tmp/dl/test.torrent", Length: "379", Selected: "true"}},
			FollowedBy: []string{"bt-gid"},
		},
		{
			Gid:             "bt-gid",
			Status:          "complete",
			Dir:             "/tmp/dl",
			TotalLength:     "200000",
			CompletedLength: "200000",
			Files:           []downloader.File{{Path: "/tmp/dl/data.bin", Length: "200000", CompletedLength: "200000", Selected: "true"}},
		},
	}}
	snap.index()

	dr := &entity.DownloadRecordORM{
		Gid: utils.StringPtr("torrent-gid"),
		Url: utils.StringPtr("https://example.com/test.torrent"),
	}
	task, ok := snap.targetFor(dr)
	if !ok {
		t.Fatal("targetFor returned not found")
	}
	if task.Gid != "bt-gid" {
		t.Fatalf("targetFor gid = %q, want bt-gid（种子文件本身不能当产物）", task.Gid)
	}
}

// aria2 重启后 followedBy 丢了：靠种子文件里的 infohash 反查 BT 任务
func TestTargetForTorrentURLByInfoHashWhenFollowedByLost(t *testing.T) {
	dir := t.TempDir()
	const infoValue = "d4:name8:test.bin6:lengthi100ee"
	torrent := "d8:announce14:http://t/a.php4:info" + infoValue + "e"
	if err := os.WriteFile(filepath.Join(dir, "test.torrent"), []byte(torrent), 0o600); err != nil {
		t.Fatalf("write torrent: %v", err)
	}
	sum := sha1.Sum([]byte(infoValue))
	ih := hex.EncodeToString(sum[:])

	snap := &taskSnapshot{tasks: []downloader.Task{
		{
			Gid:    "torrent-gid",
			Status: "complete",
			Dir:    dir,
			Files:  []downloader.File{{Path: "test.torrent", Length: "48", Selected: "true"}},
		},
		{
			Gid:             "bt-gid",
			Status:          "complete",
			InfoHash:        ih,
			Dir:             dir,
			TotalLength:     "100",
			CompletedLength: "100",
			Files:           []downloader.File{{Path: "test.bin", Length: "100", CompletedLength: "100", Selected: "true"}},
		},
	}}
	snap.index()

	dr := &entity.DownloadRecordORM{
		Gid: utils.StringPtr("stale-gid"),
		Url: utils.StringPtr("https://example.com/test.torrent"),
	}
	task, ok := snap.targetFor(dr)
	if !ok {
		t.Fatal("targetFor returned not found")
	}
	if task.Gid != "bt-gid" {
		t.Fatalf("targetFor gid = %q, want bt-gid（种子文件不是产物）", task.Gid)
	}
}

// gid 失效时按 URL 找回任务（http 直链场景）
func TestTargetForByURLWhenGidGone(t *testing.T) {
	snap := &taskSnapshot{tasks: []downloader.Task{{
		Gid:             "new-gid",
		Status:          "complete",
		Dir:             "/tmp/dl",
		TotalLength:     "10",
		CompletedLength: "10",
		Files: []downloader.File{{
			Path:            "/tmp/dl/a.iso",
			Length:          "10",
			CompletedLength: "10",
			Selected:        "true",
			URIs:            []downloader.URI{{URI: "http://example.com/a.iso"}},
		}},
	}}}
	snap.index()

	dr := &entity.DownloadRecordORM{
		Gid: utils.StringPtr("stale"),
		Url: utils.StringPtr("http://example.com/a.iso"),
	}
	task, ok := snap.targetFor(dr)
	if !ok || task.Gid != "new-gid" {
		t.Fatalf("targetFor = %q/%v, want new-gid/true", task.Gid, ok)
	}
}

func TestWithinGrace(t *testing.T) {
	now := time.Now()
	if !withinGrace(&entity.DownloadRecordORM{CreatedAt: &now}) {
		t.Error("刚创建的记录应在宽限期内")
	}
	old := now.Add(-time.Hour)
	if withinGrace(&entity.DownloadRecordORM{CreatedAt: &old}) {
		t.Error("一小时前的记录不应在宽限期内")
	}
	if !withinGrace(&entity.DownloadRecordORM{}) {
		t.Error("没有创建时间的记录按宽限期处理")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "x", "y"); got != "x" {
		t.Errorf("firstNonEmpty = %q, want x", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("firstNonEmpty all empty = %q, want empty", got)
	}
}

func TestStoredInfoHash(t *testing.T) {
	cases := map[string]string{
		`{"info_hash":"8FD37116E367F8E77FA20FF721A7F82B278CB23B"}`: "8fd37116e367f8e77fa20ff721a7f82b278cb23b",
		"":            "",
		"not json":    "",
		`{"other":1}`: "",
	}
	for in, want := range cases {
		if got := storedInfoHash(in); got != want {
			t.Errorf("storedInfoHash(%q) = %q, want %q", in, got, want)
		}
	}
}
