package cmd

import (
	"context"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// fakeRootLister 只实现 RemoteRoot，用于测扫描根解析
type fakeRootLister struct{ root string }

func (f fakeRootLister) RemoteRoot() string { return f.root }

func (f fakeRootLister) ListRemote(_ context.Context, _ string) ([]types.RemoteEntry, error) {
	return nil, nil
}

// TestRemoteListSupported 能力表必须与驱动实现保持一致：Telegram 明确不支持
func TestRemoteListSupported(t *testing.T) {
	supported := []native.StorageProviderType{
		native.StorageProviderType_LOCAL,
		native.StorageProviderType_PAN123,
		native.StorageProviderType_BAIDU_NETDISK,
		native.StorageProviderType_ALIYUNDRIVE_OPEN,
		native.StorageProviderType_YUN139,
		native.StorageProviderType_CLOUD189_PC,
	}
	for _, tp := range supported {
		if !remoteListSupported(tp) {
			t.Fatalf("%v 应支持远端枚举", tp)
		}
	}
	for _, tp := range []native.StorageProviderType{
		native.StorageProviderType_TELEGRAM,
		native.StorageProviderType_DISCORD,
		native.StorageProviderType_HUGGINGFACE,
		native.StorageProviderType_PROVIDER_UNSPECIFIED,
	} {
		if remoteListSupported(tp) {
			t.Fatalf("%v 不应支持远端枚举", tp)
		}
	}
}

// TestResolveOrphanScanRoot 默认扫当前年份子目录；显式 --root-path 优先；根为空要报错
func TestResolveOrphanScanRoot(t *testing.T) {
	year := time.Now().Format("2006")

	got, err := resolveOrphanScanRoot(fakeRootLister{root: "/2026"}, "")
	if err != nil || got != path.Join("/2026", year) {
		t.Fatalf("默认根应为 <root>/<当前年份>, got %q err=%v", got, err)
	}

	got, err = resolveOrphanScanRoot(fakeRootLister{root: "/2026"}, "/")
	if err != nil || got != "/" {
		t.Fatalf("显式 root-path 应原样使用, got %q err=%v", got, err)
	}

	if _, err = resolveOrphanScanRoot(fakeRootLister{root: ""}, ""); err == nil {
		t.Fatalf("provider 未配置根目录且没给 --root-path 时应报错")
	}
}

// TestWriteOrphanList 清单文件一行一个路径，父目录不存在要自动创建
func TestWriteOrphanList(t *testing.T) {
	dir := t.TempDir()
	out := path.Join(dir, "nested", "orphans.txt")
	orphans := []service.RemoteOrphan{
		{Path: "/2026/01/a.jpg"},
		{Path: "/2026/01/子目录/b.jpg"}, // 中文路径也要原样写出
	}
	got, err := writeOrphanList(out, "name/with:bad chars", orphans)
	if err != nil {
		t.Fatalf("写清单失败: %v", err)
	}
	if got != out {
		t.Fatalf("应返回实际路径 %q, got %q", out, got)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("读清单失败: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 || lines[0] != orphans[0].Path || lines[1] != orphans[1].Path {
		t.Fatalf("清单内容不符: %q", lines)
	}
}

// TestSanitizeFileName provider 名里的分隔符要变成安全字符，空名回落 provider
func TestSanitizeFileName(t *testing.T) {
	cases := map[string]string{
		"百度网盘bdun":   "百度网盘bdun",
		"a/b:c d\\e": "a_b_c_d_e",
		"":           "provider",
	}
	for in, want := range cases {
		if got := sanitizeFileName(in); got != want {
			t.Fatalf("sanitizeFileName(%q) = %q, want %q", in, got, want)
		}
	}
}
