package utils

import (
	"os"
	"path/filepath"
	"testing"

	"jenken827.com/jenken827/pfm/globals"
)

// withTempDir 把 globals.TempDir 指向测试临时目录（用例结束恢复）
func withTempDir(t *testing.T) {
	t.Helper()
	old := globals.TempDir
	globals.TempDir = t.TempDir()
	t.Cleanup(func() { globals.TempDir = old })
}

func mustWriteFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestRemoveChunkTempFilesKeepsMergedPlaintext 钉住分片清理的边界：
//
// 分片合并产出的明文 merged_<name> 与分片同处 chunks/，而异步分发正是从这份
// 明文读源（upload_record.TempPath）。若按整目录删除，Dispatch 会报
// "temp file not found"，上传失败且文件记录已落库 → 该 hash 永远没有
// file_location 行（读取端 500、内容随分片一起丢失）。
// 该回归对应 2026-09-21 生产事故（4 个 .amr 文件因此不可读）。
func TestRemoveChunkTempFilesKeepsMergedPlaintext(t *testing.T) {
	withTempDir(t)

	const hash, owner = "ee42d528b345c9450d03b5c6da33dbbaa4e009ac109ac9aa87984caeab9d4f45", "85871cca-4f61-47d3-8cd1-9d9363c8f798"
	dir := GetChunkTempDir(hash, owner)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	chunk0 := GetChunkTempPath(hash, owner, 0)
	chunk1 := GetChunkTempPath(hash, owner, 1)
	merged := filepath.Join(dir, MergedFileName("199 2532 6107_20240908135511.amr"))
	for _, p := range []string{chunk0, chunk1, merged} {
		mustWriteFile(t, p)
	}

	if err := RemoveChunkTempFiles(hash, owner); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, p := range []string{chunk0, chunk1} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("分片应被删除: %s", p)
		}
	}
	if _, err := os.Stat(merged); err != nil {
		t.Fatalf("合并明文必须保留（分发要读的源）: %v", err)
	}
}

// TestRemoveChunkTempFilesMissingDirIsNoop 目录已被终端态清理时视为无事发生
// （清理是幂等的，不应因重复调用产生告警噪声）
func TestRemoveChunkTempFilesMissingDirIsNoop(t *testing.T) {
	withTempDir(t)
	if err := RemoveChunkTempFiles("no-such-hash", "no-such-owner"); err != nil {
		t.Fatalf("目录不存在时应返回 nil，got %v", err)
	}
}

// TestMergedFileName 写入方与读取方共用的文件名约定（避免两侧字符串漂移）
func TestMergedFileName(t *testing.T) {
	if got := MergedFileName("book.epub"); got != "merged_book.epub" {
		t.Fatalf("want merged_book.epub, got %s", got)
	}
	if chunk0 := GetChunkTempPath("h", "o", 0); filepath.Base(chunk0) != "chunk_0" {
		t.Fatalf("分片命名漂移: %s", chunk0)
	}
}
