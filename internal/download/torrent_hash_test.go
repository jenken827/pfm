package download

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// 种子文件的 infohash = info 字典 bencode 原始字节的 sha1
func TestTorrentInfoHash(t *testing.T) {
	dir := t.TempDir()
	const infoValue = "d4:name8:test.bin6:lengthi100ee"
	// 顶层字典里 info 不是第一个 key，且前面还有嵌套列表，验证跳过逻辑
	content := "d8:announce14:http://t/a.php13:announce-listll14:http://t/a.phpee4:info" + infoValue + "e"
	path := filepath.Join(dir, "test.torrent")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write torrent: %v", err)
	}

	sum := sha1.Sum([]byte(infoValue))
	want := hex.EncodeToString(sum[:])
	got, err := torrentInfoHash(path)
	if err != nil {
		t.Fatalf("torrentInfoHash: %v", err)
	}
	if got != want {
		t.Errorf("torrentInfoHash = %s, want %s", got, want)
	}
}

func TestTorrentInfoHashInvalid(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"不是字典":   "i123e",
		"缺 info": "d8:announce14:http://t/a.phpe",
		"截断":     "d4:info",
	}
	for name, content := range cases {
		path := filepath.Join(dir, "bad.torrent")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write torrent: %v", err)
		}
		if _, err := torrentInfoHash(path); err == nil {
			t.Errorf("%s: torrentInfoHash 应报错", name)
		}
	}
	if _, err := torrentInfoHash(filepath.Join(dir, "not-exist.torrent")); err == nil {
		t.Error("文件不存在时应报错")
	}
}
