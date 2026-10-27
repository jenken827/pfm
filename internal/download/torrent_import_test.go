package download

import (
	"os"
	"path/filepath"
	"testing"

	"jenken827.com/jenken827/pfm/pkg/downloader/aria2"
	"jenken827.com/jenken827/pfm/proto/gen/entity"
	"jenken827.com/jenken827/pfm/utils"
)

// 只导入被选中、非元数据伪文件、且已按种子长度完整落盘的文件
func TestImportableFilesFilters(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, n int) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, make([]byte, n), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("Torrent/a.mkv", 100)
	write("Torrent/b.srt", 10)
	write("Torrent/skip.txt", 10)
	write("Torrent/partial.bin", 7)

	files := []aria2.FileInfo{
		{Path: "Torrent/a.mkv", Selected: "true", Length: "100", CompletedLength: "100"},
		{Path: "Torrent/b.srt", Selected: "true", Length: "10", CompletedLength: "10"},
		{Path: "Torrent/skip.txt", Selected: "false", Length: "10", CompletedLength: "0"},   // 未选中
		{Path: "Torrent/partial.bin", Selected: "true", Length: "10", CompletedLength: "7"}, // 没下完（磁盘只有 7 字节）
		{Path: "[METADATA]data.bin", Selected: "true", Length: "328"},                       // 元数据伪文件
		{Path: "", Selected: "true"}, // 空路径
	}
	got := importableFiles(dir, files)
	want := []string{filepath.Join(dir, "Torrent/a.mkv"), filepath.Join(dir, "Torrent/b.srt")}
	if len(got) != len(want) {
		t.Fatalf("importableFiles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("importableFiles[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

// aria2 的 files[].path 是绝对路径（dir + 相对路径）：不能直接再和 dir 拼
//
// 回归背景：旧实现无条件 filepath.Join(dir, f.Path)，对绝对路径会拼成
// dir/dir/... 的形式，stat 必然失败 → 每次导入都报「没有可导入的文件」。
func TestImportableFilesAcceptsAbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Torrent", "a.bin")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("abcd"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	files := []aria2.FileInfo{
		{Path: path, Selected: "true", Length: "4", CompletedLength: "0"},
		{Path: filepath.Join(dir, "Torrent", "gone.bin"), Selected: "true", Length: "4", CompletedLength: "0"},
	}
	got := importableFiles(dir, files)
	if len(got) != 1 || got[0] != path {
		t.Fatalf("importableFiles = %v, want [%s]", got, path)
	}
}

// 任务已完成后 aria2 逐文件 completedLength 恒为 0：不能因此丢掉已下好的文件
//
// 回归背景：BT 任务进入终态（stopped/complete）后 aria2 不再保留分片位图，
// files[].completedLength 全部回报 0（任务级总量仍然正确）。按它判断会得到
// 「aria2 已完成但没有可导入的文件」，且记录被判失败后 reconcile 不再扫，
// 只能整单重下。
func TestImportableFilesAcceptsZeroCompletedLength(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.bin"), make([]byte, 4), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.bin"), []byte("ab"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	files := []aria2.FileInfo{
		{Path: "a.bin", Selected: "true", Length: "4", CompletedLength: "0"},
		{Path: "b.bin", Selected: "true", Length: "2", CompletedLength: "0"},
		{Path: "c.bin", Selected: "false", Length: "9", CompletedLength: "0"},
	}
	got := importableFiles(dir, files)
	if len(got) != 2 || got[0] != filepath.Join(dir, "a.bin") || got[1] != filepath.Join(dir, "b.bin") {
		t.Fatalf("importableFiles = %v, want 2 files", got)
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
	dr := &entity.DownloadRecordORM{SourceName: utils.StringPtr("我的种子"), Gid: utils.StringPtr("gid1")}

	// aria2 的种子名优先（比 sourceName 更接近真实种子）
	if got := torrentRootName(dr, "TorrentName", files, dir); got != "TorrentName" {
		t.Errorf("torrentRootName = %q, want TorrentName", got)
	}
	// 没有种子名 → 用 sourceName
	if got := torrentRootName(dr, "", files, dir); got != "我的种子" {
		t.Errorf("torrentRootName = %q, want 我的种子", got)
	}
	// 都没有 → 用第一个文件的顶层目录
	dr = &entity.DownloadRecordORM{Gid: utils.StringPtr("gid1")}
	if got := torrentRootName(dr, "", files, dir); got != "Torrent" {
		t.Errorf("torrentRootName = %q, want Torrent", got)
	}
	// 单层文件 → gid 兜底
	single := []string{filepath.Join(dir, "a.mkv")}
	if got := torrentRootName(dr, "", single, dir); got != "download-gid1" {
		t.Errorf("torrentRootName = %q, want download-gid1", got)
	}
	// 种子名 / sourceName 含穿越段 → 净化后回退
	dr = &entity.DownloadRecordORM{SourceName: utils.StringPtr(".."), Gid: utils.StringPtr("gid1")}
	if got := torrentRootName(dr, "..", files, dir); got != "Torrent" {
		t.Errorf("torrentRootName = %q, want Torrent（非法名字回退）", got)
	}
}
