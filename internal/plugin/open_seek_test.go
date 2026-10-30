package plugin

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	"github.com/jenken827/pfm-plugin-sdk/pkg/crypt"
)

// encryptedStub 只需要「声称已加密」：解密由插件侧完成，宿主拿到的就是明文流；
// 本用例校验的是宿主侧的长度口径，不需要真的做 AES。
type encryptedStub struct{ types.NullEncryptor }

func (encryptedStub) IsEncrypted() bool { return true }

// TestOpenTotalIsPlaintextForEncryptedRefs 回归：加密文件的 Open 流总长必须是明文长度
//
// 线上症状（2026-10-28 起的 WebDAV 500，body 18 字节 = "seeker can't seek"）：
// go-webdav 的 GET/HEAD 走 net/http.ServeContent，它第一步就 Seek(0, io.SeekEnd)
// 取长度。若宿主按 FileRef.Size（存储大小 = 密文）当总长，Seek 内部为跳到
// 「密文末尾」会从明文流里丢弃多出来的 33B+16B/块，提前撞 EOF → Seek 返回错误
// → ServeContent 直接 500。
//
// 这里用真实数字（明文 2144 / 密文 2193）走一遍 Open 契约：
// 只在明文流上做 Seek(End) + 回卷 + 整份读取，全部必须成功。
func TestOpenTotalIsPlaintextForEncryptedRefs(t *testing.T) {
	plain := bytes.Repeat([]byte("p"), 2144)
	cipherSize := crypt.EncryptedSize(int64(len(plain)))
	if cipherSize != 2193 {
		t.Fatalf("前提变了：2144 明文字节的密文长度应为 2193，实际 %d", cipherSize)
	}

	// 假插件：/rpc/Open 直接吐明文（真实插件解密后也是这样）
	var openCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rpc/Open" {
			http.NotFound(w, r)
			return
		}
		openCalls++
		_, _ = w.Write(plain)
	}))
	defer srv.Close()

	core := &driverCore{
		cli:        newClient(strings.TrimPrefix(srv.URL, "http://"), "test-token"),
		providerID: 1,
	}
	refs := []types.FileRef{{ChunkNumber: 0, Size: uint64(cipherSize)}}

	reader, err := core.Open(context.Background(), refs, types.OpenOptions{Encryptor: &encryptedStub{}})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer reader.Close()

	// 1) ServeContent 的 sizeFunc
	size, err := reader.Seek(0, io.SeekEnd)
	if err != nil {
		t.Fatalf("Seek(0, io.SeekEnd) failed: %v（线上表现为 WebDAV GET 500 \"seeker can't seek\"）", err)
	}
	if size != int64(len(plain)) {
		t.Fatalf("Seek(0, io.SeekEnd) = %d, want plaintext size %d（密文 %d）", size, len(plain), cipherSize)
	}

	// 2) ServeContent 回卷到头部后整份读出
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek(0, io.SeekStart) failed: %v", err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read after seek failed: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("读出的字节与明文不一致：got %d bytes, want %d", len(got), len(plain))
	}
	if openCalls == 0 {
		t.Fatalf("应当至少建立一条插件流")
	}
}

// TestPlaintextTotal 长度口径：未加密 = 存储大小；加密 = 逐个分片换算回明文
func TestPlaintextTotal(t *testing.T) {
	const cif = 2193 // 2144 明文字节的密文长度

	tests := []struct {
		name    string
		refs    []types.FileRef
		enc     types.Encryptor
		want    int64
		wantErr bool
	}{
		{
			name: "不加密时就是存储大小之和",
			refs: []types.FileRef{{Size: 100}, {Size: 200}},
			want: 300,
		},
		{
			name: "加密时按明文空间换算",
			refs: []types.FileRef{{Size: cif}},
			enc:  &encryptedStub{},
			want: 2144,
		},
		{
			name: "加密多分片逐个换算后累加",
			refs: []types.FileRef{{Size: cif}, {Size: uint64(crypt.EncryptedSize(1024))}},
			enc:  &encryptedStub{},
			want: 2144 + 1024,
		},
		{
			name:    "加密分片长度装不下文件头时提前失败",
			refs:    []types.FileRef{{ChunkNumber: 2, Size: 10}},
			enc:     &encryptedStub{},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := plaintextTotal(tc.refs, tc.enc)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际得到 %d", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("plaintextTotal failed: %v", err)
			}
			if got != tc.want {
				t.Fatalf("plaintextTotal = %d, want %d", got, tc.want)
			}
		})
	}
}
