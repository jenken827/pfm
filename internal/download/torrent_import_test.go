package download

import (
	"os"
	"path/filepath"
	"testing"

	"jenken827.com/jenken827/pfm/pkg/downloader/aria2"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

// 只导入被选中、非元数据伪文件、且已下完的文件
func TestImportableFilePaths(t *testing.T) {
	files := []aria2.FileInfo{
		{Path: "Torrent/a.mkv", Selected: "true", Length: "100", CompletedLength: "100"},
		{Path: "Torrent/b.srt", Selected: "true", Length: "10", CompletedLength: "10"},
		{Path: "Torrent/skip.txt", Selected: "false", Length: "10", CompletedLength: "0"},     // 未选中
		{Path: "Torrent/partial.bin", Selected: "true", Length: "10", CompletedLength: "7"},   // 没下完
		{Path: "[METADATA]data.bin", Selected: "true", Length: "328", CompletedLength: "328"}, // 元数据伪文件
		{Path: "", Selected: "true"}, // 空路径
	}
	got := importableFilePaths(files)
	want := []string{"Torrent/a.mkv", "Torrent/b.srt"}
	if len(got) != len(want) {
		t.Fatalf("importableFilePaths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("importableFilePaths[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

// 磁盘上不存在 / 是目录 / 空文件的条目不能在导入阶段炸掉
func TestImportableFilesSkipsMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.mkv"), []byte("data"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	files := []aria2.FileInfo{
		{Path: "a.mkv", Selected: "true"},
		{Path: "missing.bin", Selected: "true"},
		{Path: "sub", Selected: "true"},
	}
	got := importableFiles(dir, files)
	if len(got) != 1 || got[0] != filepath.Join(dir, "a.mkv") {
		t.Fatalf("importableFiles = %v, want [%s]", got, filepath.Join(dir, "a.mkv"))
	}
}

func TestRelativeSegments(t *testing.T) {
	dir := "/data/.offline"
	cases := []struct {
		path string
		want []string
	}{
		{filepath.Join(dir, "Torrent/a.mkv"), []string{"Torrent", "a.mkv"}},
		{filepath.Join(dir, "Torrent/sub/deep/b.srt"), []string{"Torrent", "sub", "deep", "b.srt"}},
		{filepath.Join(dir, "c.bin"), []string{"c.bin"}},
	}
	for _, c := range cases {
		got := relativeSegments(dir, c.path)
		if len(got) != len(c.want) {
			t.Fatalf("relativeSegments(%s) = %v, want %v", c.path, got, c.want)
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("relativeSegments(%s)[%d] = %s, want %s", c.path, i, got[i], c.want[i])
			}
		}
	}
}

// 路径段必须净化：防穿越（".."）与多级路径（"a/b" 只取 basename）
func TestSafeSegment(t *testing.T) {
	cases := map[string]string{
		"..":         "",
		".":          "",
		"":           "",
		"/":          "",
		"a/b":        "b",
		"Torrent":    "Torrent",
		"  spaced  ": "spaced",
		"a\\b":       "b",
	}
	for in, want := range cases {
		if got := safeSegment(in); got != want {
			t.Errorf("safeSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTorrentRootName(t *testing.T) {
	dir := "/data/.offline"
	files := []string{filepath.Join(dir, "Torrent/a.mkv")}
	dr := &native.DownloadRecordORM{SourceName: utils.StringPtr("我的种子"), Gid: utils.StringPtr("gid1")}

	// aria2 的种子名优先（比 sourceName 更接近真实种子）
	if got := torrentRootName(dr, "TorrentName", files, dir); got != "TorrentName" {
		t.Errorf("torrentRootName = %q, want TorrentName", got)
	}
	// 没有种子名 → 用 sourceName
	if got := torrentRootName(dr, "", files, dir); got != "我的种子" {
		t.Errorf("torrentRootName = %q, want 我的种子", got)
	}
	// 都没有 → 用第一个文件的顶层目录
	dr = &native.DownloadRecordORM{Gid: utils.StringPtr("gid1")}
	if got := torrentRootName(dr, "", files, dir); got != "Torrent" {
		t.Errorf("torrentRootName = %q, want Torrent", got)
	}
	// 单层文件 → gid 兜底
	single := []string{filepath.Join(dir, "a.mkv")}
	if got := torrentRootName(dr, "", single, dir); got != "download-gid1" {
		t.Errorf("torrentRootName = %q, want download-gid1", got)
	}
	// 种子名 / sourceName 含穿越段 → 净化后回退
	dr = &native.DownloadRecordORM{SourceName: utils.StringPtr(".."), Gid: utils.StringPtr("gid1")}
	if got := torrentRootName(dr, "..", files, dir); got != "Torrent" {
		t.Errorf("torrentRootName = %q, want Torrent（非法名字回退）", got)
	}
}
