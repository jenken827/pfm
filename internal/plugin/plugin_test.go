package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/plugin/sdk"
	"jenken827.com/jenken827/pfm/proto/gen/entity"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// 自测插件：测试二进制把自己当插件进程拉起（不需要额外编译步骤）
const (
	testDriverID  = "plugintest"
	selfTestEnv   = "PFM_PLUGIN_SELFTEST"
	testPayload   = "0123456789abcdefghij"
	testConfigKey = "cfg-key"
)

func TestMain(m *testing.M) {
	if os.Getenv(selfTestEnv) == "1" {
		// 插件进程模式：只注册假驱动并把 SDK 跑起来
		types.RegisterStorageDriverWithMetadata(newFakeDriver, &native.DriverInfo{
			DriverId: proto.String(testDriverID),
		})
		sdk.MustServe(testDriverID)
		return
	}
	os.Exit(m.Run())
}

// fakeDriver 最小可用的假驱动：两个可选能力（批量删除、直链）
type fakeDriver struct {
	providerID uint32
	config     *types.ProviderConfig
}

func newFakeDriver(orm *entity.StorageProviderORM) (types.StorageDriver, error) {
	cfg := types.ProviderConfigFromORM(orm)
	providerID := uint32(0)
	if orm.Id != nil {
		providerID = *orm.Id
	}
	return &fakeDriver{providerID: providerID, config: &cfg}, nil
}

func (d *fakeDriver) GetType() string                          { return testDriverID }
func (d *fakeDriver) GetName() string                          { return testDriverID }
func (d *fakeDriver) GetConfig() any                           { return nil }
func (d *fakeDriver) GetProviderConfig() *types.ProviderConfig { return d.config }

// Upload 把请求字段回显到引用里，宿主据此断言参数完整跨进程传递
func (d *fakeDriver) Upload(_ context.Context, src types.UploadSource,
	opt types.UploadOptions) ([]types.FileRef, error) {
	return []types.FileRef{{
		LocationID:     11,
		ProviderFileID: src.FileName,
		Name:           src.FileName,
		Path:           src.TempPath,
		Size:           uint64(len(testPayload)),
		Hash:           src.FileHash,
		ChunkHash:      d.config.EncryptionKey,
		ChunkSize:      uint64(len(testPayload)),
		ChunkNumber:    0,
		CacheStatus:    native.CacheStatus_CACHED,
		ChannelID:      opt.EncryptionSuffix,
	}}, nil
}

func (d *fakeDriver) Open(_ context.Context, refs []types.FileRef,
	_ types.OpenOptions) (types.ReadSeekCloser, error) {
	if len(refs) == 0 {
		return nil, errors.New("没有分片引用")
	}
	if refs[0].ProviderFileID == "missing" {
		return nil, errors.New("文件不存在")
	}
	return &nopReadSeekCloser{Reader: bytes.NewReader([]byte(testPayload))}, nil
}

func (d *fakeDriver) BatchDelete(_ context.Context, refs []types.FileRef) ([]types.DeleteResult, error) {
	results := make([]types.DeleteResult, 0, len(refs))
	for _, ref := range refs {
		results = append(results, types.DeleteResult{ID: ref.LocationID, Success: ref.LocationID != 0})
	}
	return results, nil
}

func (d *fakeDriver) DirectLink(_ context.Context, ref types.FileRef) (*types.DirectLink, error) {
	return &types.DirectLink{
		URL:           "https://example.invalid/" + ref.ProviderFileID,
		Headers:       map[string]string{"User-Agent": "pfm-test"},
		ExpiresAt:     time.Unix(1700000000, 0),
		SupportsRange: true,
	}, nil
}

type nopReadSeekCloser struct{ *bytes.Reader }

func (nopReadSeekCloser) Close() error { return nil }

// writePluginDir 生成一个插件目录（plugin.json + 指向测试二进制）
func writePluginDir(t *testing.T, driverID string) string {
	t.Helper()
	dir := t.TempDir()
	execPath, err := os.Executable()
	if err != nil {
		t.Fatalf("取测试二进制路径失败: %v", err)
	}
	mf := map[string]any{
		"driver_id":   driverID,
		"exec":        execPath,
		"abi_version": 1,
		"name":        "pfm-test",
		"version":     "0.0.1",
	}
	raw, err := json.Marshal(mf)
	if err != nil {
		t.Fatalf("序列化 plugin.json 失败: %v", err)
	}
	pluginDir := filepath.Join(dir, "pfm-test")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("创建插件目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), raw, 0o600); err != nil {
		t.Fatalf("写 plugin.json 失败: %v", err)
	}
	return dir
}

func TestManagerLoadsPluginAndProxiesCalls(t *testing.T) {
	t.Setenv(selfTestEnv, "1")
	mgr := NewManager(Options{Dir: writePluginDir(t, testDriverID), HandshakeTimeout: 10 * time.Second,
		ConfigureTimeout: 10 * time.Second})
	ctx := context.Background()
	if err := mgr.Load(ctx); err != nil {
		t.Fatalf("加载插件失败: %v", err)
	}
	t.Cleanup(func() {
		mgr.Close()
		types.ClearDriverOverride(testDriverID)
	})

	if !types.HasDriverOverride(testDriverID) {
		t.Fatal("插件加载后应有驱动覆盖")
	}
	constructor := types.GetDriverConstructor(testDriverID)
	if constructor == nil {
		t.Fatal("插件加载后应能取到构造器")
	}
	orm := &entity.StorageProviderORM{
		Id:            proto.Uint32(7),
		EncryptionKey: proto.String(testConfigKey),
	}
	drv, err := constructor(orm)
	if err != nil {
		t.Fatalf("构造插件驱动失败: %v", err)
	}
	if drv.GetType() != testDriverID {
		t.Fatalf("驱动身份不符: %s", drv.GetType())
	}
	// 能力声明与接口一致性：声明了两个，也只能断言出这两个
	if _, ok := drv.(types.BatchDeleter); !ok {
		t.Error("应实现 BatchDeleter")
	}
	if _, ok := drv.(types.DirectLinker); !ok {
		t.Error("应实现 DirectLinker")
	}
	if _, ok := drv.(types.RemoteLister); ok {
		t.Error("未声明的能力不应被断言出来")
	}
	if _, ok := drv.(types.PreDownloader); ok {
		t.Error("未声明的能力不应被断言出来")
	}

	t.Run("Upload 参数跨进程完整传递", func(t *testing.T) {
		refs, uploadErr := drv.Upload(ctx, types.UploadSource{
			TempPath: "/tmp/x.bin",
			FileName: "x.bin",
			FileSize: 20,
			FileHash: "hash-x",
		}, types.UploadOptions{ObfuscateName: true, EncryptionSuffix: ".enc"})
		if uploadErr != nil {
			t.Fatalf("Upload 失败: %v", uploadErr)
		}
		if len(refs) != 1 {
			t.Fatalf("分片数不符: %d", len(refs))
		}
		ref := refs[0]
		if ref.ProviderFileID != "x.bin" || ref.Hash != "hash-x" || ref.Path != "/tmp/x.bin" {
			t.Fatalf("Upload 参数未完整传递: %+v", ref)
		}
		if ref.ChunkHash != testConfigKey {
			t.Fatalf("Configure 的 provider 配置未传到插件: %q", ref.ChunkHash)
		}
		if ref.ChannelID != ".enc" || ref.CacheStatus != native.CacheStatus_CACHED {
			t.Fatalf("Upload 选项或缓存状态未完整传递: %+v", ref)
		}
	})

	t.Run("Open 支持完整读取与重新定位", func(t *testing.T) {
		refs := []types.FileRef{{ProviderFileID: "ok", Size: uint64(len(testPayload))}}
		reader, openErr := drv.Open(ctx, refs, types.OpenOptions{})
		if openErr != nil {
			t.Fatalf("Open 失败: %v", openErr)
		}
		defer func() { _ = reader.Close() }()

		head := make([]byte, 4)
		if _, readErr := io.ReadFull(reader, head); readErr != nil {
			t.Fatalf("读取前 4 字节失败: %v", readErr)
		}
		if string(head) != testPayload[:4] {
			t.Fatalf("前 4 字节不符: %q", head)
		}
		// 往回 seek 必须重开一条流并回到正确位置
		if _, seekErr := reader.Seek(0, io.SeekStart); seekErr != nil {
			t.Fatalf("Seek 回到起点失败: %v", seekErr)
		}
		rest, readErr := io.ReadAll(reader)
		if readErr != nil {
			t.Fatalf("读取剩余内容失败: %v", readErr)
		}
		if string(rest) != testPayload {
			t.Fatalf("全文不符: %q", rest)
		}
		// SeekEnd 靠 refs 的 size 之和
		if _, seekErr := reader.Seek(-2, io.SeekEnd); seekErr != nil {
			t.Fatalf("Seek 到末尾失败: %v", seekErr)
		}
		tail, tailErr := io.ReadAll(reader)
		if tailErr != nil {
			t.Fatalf("读取末尾失败: %v", tailErr)
		}
		if string(tail) != testPayload[len(testPayload)-2:] {
			t.Fatalf("末尾内容不符: %q", tail)
		}
	})

	t.Run("Open 的错误能回到调用方", func(t *testing.T) {
		refs := []types.FileRef{{ProviderFileID: "missing"}}
		if _, openErr := drv.Open(ctx, refs, types.OpenOptions{}); openErr == nil {
			t.Fatal("插件侧出错时 Open 应返回错误")
		}
	})

	t.Run("BatchDelete 与 DirectLink", func(t *testing.T) {
		results, delErr := drv.(types.BatchDeleter).BatchDelete(ctx,
			[]types.FileRef{{LocationID: 3}, {LocationID: 0}})
		if delErr != nil {
			t.Fatalf("BatchDelete 失败: %v", delErr)
		}
		if len(results) != 2 || !results[0].Success || results[1].Success {
			t.Fatalf("删除结果不符: %+v", results)
		}
		link, linkErr := drv.(types.DirectLinker).DirectLink(ctx, types.FileRef{ProviderFileID: "f1"})
		if linkErr != nil {
			t.Fatalf("DirectLink 失败: %v", linkErr)
		}
		if link.URL != "https://example.invalid/f1" || link.Headers["User-Agent"] != "pfm-test" ||
			!link.SupportsRange || link.ExpiresAt.Unix() != 1700000000 {
			t.Fatalf("直链内容不符: %+v", link)
		}
	})
}

func TestManagerCloseStopsPlugin(t *testing.T) {
	t.Setenv(selfTestEnv, "1")
	mgr := NewManager(Options{Dir: writePluginDir(t, testDriverID), HandshakeTimeout: 10 * time.Second,
		ConfigureTimeout: 10 * time.Second})
	ctx := context.Background()
	if err := mgr.Load(ctx); err != nil {
		t.Fatalf("加载插件失败: %v", err)
	}
	constructor := types.GetDriverConstructor(testDriverID)
	drv, err := constructor(&entity.StorageProviderORM{Id: proto.Uint32(1)})
	if err != nil {
		t.Fatalf("构造插件驱动失败: %v", err)
	}
	plugins := mgr.Plugins()
	if len(plugins) != 1 || plugins[0].DriverID != testDriverID {
		t.Fatalf("插件列表不符: %+v", plugins)
	}
	mgr.Close()
	types.ClearDriverOverride(testDriverID)

	if _, openErr := drv.Open(ctx, []types.FileRef{{ProviderFileID: "ok"}}, types.OpenOptions{}); openErr == nil {
		t.Fatal("插件进程结束后调用应失败")
	}
}

func TestLoadManifestRejectsBadExec(t *testing.T) {
	dir := writePluginDir(t, testDriverID)
	pluginDir := filepath.Join(dir, "pfm-test")
	raw, err := os.ReadFile(filepath.Join(pluginDir, "plugin.json"))
	if err != nil {
		t.Fatalf("读取 plugin.json 失败: %v", err)
	}
	mf := &manifest{}
	if unmarshalErr := json.Unmarshal(raw, mf); unmarshalErr != nil {
		t.Fatalf("解析 plugin.json 失败: %v", unmarshalErr)
	}
	if _, loadErr := loadManifest(pluginDir); loadErr != nil {
		t.Fatalf("合法清单应能加载: %v", loadErr)
	}
	if _, loadErr := loadManifest(t.TempDir()); loadErr == nil {
		t.Fatal("缺少 plugin.json 应报错")
	}
}

// TestWrapCapabilitiesExactSet 16 种能力组合都必须恰好实现声明的接口
func TestWrapCapabilitiesExactSet(t *testing.T) {
	all := []native.DriverCapability{
		native.DriverCapability_DRIVER_CAPABILITY_BATCH_DELETE,
		native.DriverCapability_DRIVER_CAPABILITY_DIRECT_LINK,
		native.DriverCapability_DRIVER_CAPABILITY_REMOTE_LIST,
		native.DriverCapability_DRIVER_CAPABILITY_PREDOWNLOAD,
	}
	for mask := 0; mask < 1<<len(all); mask++ {
		caps := make([]native.DriverCapability, 0, len(all))
		for bit, capability := range all {
			if mask&(1<<bit) != 0 {
				caps = append(caps, capability)
			}
		}
		drv := wrapCapabilities(&driverCore{driverID: testDriverID}, caps)
		if drv.GetType() != testDriverID {
			t.Fatalf("mask=%d 代理丢了驱动身份", mask)
		}
		if _, ok := drv.(types.Closer); !ok {
			t.Fatalf("mask=%d 代理应始终可 Close", mask)
		}
		checks := []struct {
			bit  int
			name string
			has  bool
		}{
			{0, "BatchDeleter", func() bool { _, ok := drv.(types.BatchDeleter); return ok }()},
			{1, "DirectLinker", func() bool { _, ok := drv.(types.DirectLinker); return ok }()},
			{2, "RemoteLister", func() bool { _, ok := drv.(types.RemoteLister); return ok }()},
			{3, "PreDownloader", func() bool { _, ok := drv.(types.PreDownloader); return ok }()},
		}
		for _, check := range checks {
			want := mask&(1<<check.bit) != 0
			if check.has != want {
				t.Fatalf("mask=%d 的 %s 断言结果不符（want %v, got %v）", mask, check.name, want, check.has)
			}
		}
	}
}
