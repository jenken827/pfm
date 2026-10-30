package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeManifestDir 写一个只有 plugin.json + 占位可执行文件的插件目录
func writeManifestDir(t *testing.T, fields map[string]any) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), raw, 0o600); err != nil {
		t.Fatalf("写 plugin.json 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatalf("写可执行文件失败: %v", err)
	}
	return dir
}

func baseManifestFields() map[string]any {
	return map[string]any{
		"kind":        KindDriver,
		"driver_id":   "smoke",
		"exec":        "./run",
		"abi_version": 1,
	}
}

// TestManifestRequiresKnownKind kind 是必填项：宿主不知道该怎么用这个插件时
// 必须在「装上之前」就报错，而不是拉起一个进程再猜。
func TestManifestRequiresKnownKind(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{"缺少 kind", func(m map[string]any) { delete(m, "kind") }, "缺少 kind"},
		{"空 kind", func(m map[string]any) { m["kind"] = "  " }, "缺少 kind"},
		{"未知 kind", func(m map[string]any) { m["kind"] = "notifier" }, "未知插件类型"},
		{"driver 缺 driver_id", func(m map[string]any) { delete(m, "driver_id") }, "driver_id"},
		{"abi 与 kind 不符", func(m map[string]any) { m["abi_version"] = 2 }, "协议版本"},
		{"abi 缺失", func(m map[string]any) { delete(m, "abi_version") }, "协议版本"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields := baseManifestFields()
			tc.mutate(fields)
			_, err := loadManifest(writeManifestDir(t, fields))
			if err == nil {
				t.Fatal("应该报错")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("错误信息应包含 %q，实际：%v", tc.want, err)
			}
		})
	}
}

// TestManifestAcceptsDriverKind 合法清单照常通过，kind 会被原样读出来
func TestManifestAcceptsDriverKind(t *testing.T) {
	mf, err := loadManifest(writeManifestDir(t, baseManifestFields()))
	if err != nil {
		t.Fatalf("合法清单不该报错: %v", err)
	}
	if mf.Kind != KindDriver {
		t.Fatalf("kind = %q, want %q", mf.Kind, KindDriver)
	}
	if mf.DriverID != "smoke" {
		t.Fatalf("driver_id = %q", mf.DriverID)
	}
}

// TestKindsRegistered 当前登记了 driver / downloader / tool；加新类型时这条会提醒同步 ABI 表与加载分派
func TestKindsRegistered(t *testing.T) {
	kinds := Kinds()
	want := []string{KindDownloader, KindDriver, KindTool}
	if len(kinds) != len(want) {
		t.Fatalf("已登记的插件类型 = %v, want %v", kinds, want)
	}
	for idx, kind := range want {
		if kinds[idx] != kind {
			t.Fatalf("已登记的插件类型 = %v, want %v", kinds, want)
		}
	}
	for _, kind := range want {
		if abi, ok := ABI(kind); !ok || abi != 1 {
			t.Fatalf("%s 的 ABI 应为 1，实际：%d（ok=%v）", kind, abi, ok)
		}
	}
	if _, ok := ABI("notifier"); ok {
		t.Fatal("未登记的类型不应返回协议版本")
	}
}

// TestManifestRequiresToolID tool 类型必须声明 tool_id，且不会拿 driver_id 顶替
func TestManifestRequiresToolID(t *testing.T) {
	fields := map[string]any{
		"kind":        KindTool,
		"exec":        "./run",
		"abi_version": 1,
	}
	_, err := loadManifest(writeManifestDir(t, fields))
	if err == nil || !strings.Contains(err.Error(), "tool_id") {
		t.Fatalf("缺少 tool_id 应报错，实际：%v", err)
	}
	fields["tool_id"] = "imageinfo"
	if _, err := loadManifest(writeManifestDir(t, fields)); err != nil {
		t.Fatalf("合法 tool 清单不该报错: %v", err)
	}
	fields["tool_id"] = "Bad-ID"
	if _, err := loadManifest(writeManifestDir(t, fields)); err == nil {
		t.Fatal("非法 tool_id 应报错")
	}
}

// TestManifestRequiresDownloaderID downloader 类型必须声明 downloader_id，
// 且它不会被当成 driver_id（两种类型的身份校验互不串用）
func TestManifestRequiresDownloaderID(t *testing.T) {
	fields := map[string]any{
		"kind":        KindDownloader,
		"exec":        "./run",
		"abi_version": 1,
	}
	_, err := loadManifest(writeManifestDir(t, fields))
	if err == nil || !strings.Contains(err.Error(), "downloader_id") {
		t.Fatalf("缺少 downloader_id 应报错，实际：%v", err)
	}
	fields["downloader_id"] = "aria2"
	if _, err := loadManifest(writeManifestDir(t, fields)); err != nil {
		t.Fatalf("合法 downloader 清单不该报错: %v", err)
	}
	fields["downloader_id"] = "Aria2 RPC"
	if _, err := loadManifest(writeManifestDir(t, fields)); err == nil {
		t.Fatal("download_id 含空格/大写时应报错")
	}
}

// TestInstallRejectsKindMismatch 包内 kind 与索引声明不一致（拿驱动包冒充别的类型）必须拒绝
func TestInstallRejectsKindMismatch(t *testing.T) {
	dir := newPluginRoot(t)
	ts := newTestIndexServer(t, dir)
	pkg := newTestPackage("smoke", "1.0.0")
	// 包里把 kind 改成未知值，索引仍声明 driver
	var mf map[string]any
	if err := json.Unmarshal([]byte(pkg.files["plugin.json"]), &mf); err != nil {
		t.Fatalf("解析插件清单失败: %v", err)
	}
	mf["kind"] = "notifier"
	raw, _ := json.Marshal(mf)
	pkg.files["plugin.json"] = string(raw)
	ts.publish(IndexEntry{
		Kind: KindDriver, Name: "pfm-smoke", DriverID: "smoke", Version: "1.0.0",
		URL: ts.server.URL + "/pkg/smoke.tar.gz",
	}, pkg)
	if _, err := ts.installer(dir).Install(t.Context(), "pfm-smoke", ""); err == nil {
		t.Fatal("包内 kind 与索引不一致时应拒绝安装")
	}
}
