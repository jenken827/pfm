package plugin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 安装测试用的假包：plugin.json + 一个占位可执行文件
type testPackage struct {
	files map[string]string
}

func newTestPackage(driverID, version string) *testPackage {
	mf := map[string]any{
		"driver_id":   driverID,
		"exec":        "./pfm-plugin-" + driverID,
		"abi_version": 1,
		"name":        "pfm-" + driverID,
		"version":     version,
	}
	raw, _ := json.Marshal(mf)
	return &testPackage{files: map[string]string{
		"plugin.json":            string(raw),
		"pfm-plugin-" + driverID: "#!/bin/sh\n",
	}}
}

func (p *testPackage) add(name, content string) *testPackage {
	p.files[name] = content
	return p
}

func (p *testPackage) archive(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)
	tarWriter := tar.NewWriter(gzipWriter)
	names := make([]string, 0, len(p.files))
	for name := range p.files {
		names = append(names, name)
	}
	for _, name := range names {
		content := p.files[name]
		header := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("写 tar 头失败: %v", err)
		}
		if _, err := tarWriter.Write([]byte(content)); err != nil {
			t.Fatalf("写 tar 内容失败: %v", err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("关闭 tar 失败: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("关闭 gzip 失败: %v", err)
	}
	return buf.Bytes()
}

// testIndexServer 起一个签名索引服务，返回 installer 与可改写的索引条目
type testIndexServer struct {
	server   *httptest.Server
	key      ed25519.PrivateKey
	entries  []IndexEntry
	seq      uint64
	packages map[string][]byte // url 后缀 -> 包内容
	t        *testing.T
}

func newTestIndexServer(t *testing.T, dir string) *testIndexServer {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	_ = publicKey
	ts := &testIndexServer{key: privateKey, seq: 1, packages: map[string][]byte{}, t: t}
	mux := http.NewServeMux()
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, r *http.Request) {
		payload, marshErr := json.Marshal(&Index{Seq: ts.seq, Plugins: ts.entries})
		if marshErr != nil {
			t.Errorf("序列化索引失败: %v", marshErr)
			return
		}
		signature := ed25519.Sign(ts.key, payload)
		if strings.HasSuffix(r.URL.Path, ".sig") {
			_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString(signature)))
			return
		}
		_, _ = w.Write(payload)
	})
	mux.HandleFunc("/index.json.sig", func(w http.ResponseWriter, r *http.Request) {
		payload, _ := json.Marshal(&Index{Seq: ts.seq, Plugins: ts.entries})
		_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString(ed25519.Sign(ts.key, payload))))
	})
	mux.HandleFunc("/pkg/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/pkg/")
		payload, ok := ts.packages[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(payload)
	})
	ts.server = httptest.NewServer(mux)
	t.Cleanup(ts.server.Close)
	return ts
}

// publish 发布一个包并登记索引条目
func (ts *testIndexServer) publish(entry IndexEntry, pkg *testPackage) {
	ts.t.Helper()
	name := strings.TrimPrefix(entry.URL, ts.server.URL+"/pkg/")
	payload := pkg.archive(ts.t)
	ts.packages[name] = payload
	sum := sha256.Sum256(payload)
	entry.SHA256 = hex.EncodeToString(sum[:])
	entry.Platform = currentPlatform()
	if entry.ABIVersion == 0 {
		entry.ABIVersion = 1
	}
	ts.entries = append(ts.entries, entry)
}

func (ts *testIndexServer) installer(dir string) *Installer {
	return &Installer{
		Dir:        dir,
		IndexURL:   ts.server.URL + "/index.json",
		Keys:       []ed25519.PublicKey{ts.key.Public().(ed25519.PublicKey)},
		PfmVersion: "9.9.9",
	}
}

// 临时插件根目录
func newPluginRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{stagingDirName, archivedDirName} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("创建目录失败: %v", err)
		}
	}
	return dir
}

func TestInstallVerifiesSignatureAndSHA(t *testing.T) {
	dir := newPluginRoot(t)
	ts := newTestIndexServer(t, dir)
	entry := IndexEntry{
		Name: "pfm-smoke", DriverID: "smoke", Version: "1.0.0",
		URL: ts.server.URL + "/pkg/smoke-1.0.0.tar.gz",
	}
	ts.publish(entry, newTestPackage("smoke", "1.0.0"))
	installer := ts.installer(dir)

	result, err := installer.Install(context.Background(), "pfm-smoke", "")
	if err != nil {
		t.Fatalf("安装失败: %v", err)
	}
	if result.Version != "1.0.0" || result.DriverID != "smoke" || result.Seq != 1 {
		t.Fatalf("安装结果不符: %+v", result)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "pfm-smoke", "plugin.json")); statErr != nil {
		t.Fatalf("插件目录不完整: %v", statErr)
	}
	state, err := loadState(dir)
	if err != nil {
		t.Fatalf("读取状态失败: %v", err)
	}
	if state.Seq != 1 || state.Installed["pfm-smoke"].Version != "1.0.0" {
		t.Fatalf("安装状态不符: %+v", state)
	}
	integrity, err := CheckInstalled(dir)
	if err != nil {
		t.Fatalf("完整性检查失败: %v", err)
	}
	if len(integrity) != 1 || integrity[0].Status != "ok" {
		t.Fatalf("完整性结果不符: %+v", integrity)
	}

	t.Run("签名不对必须拒绝", func(t *testing.T) {
		broken := ts.installer(dir)
		broken.Keys = []ed25519.PublicKey{make(ed25519.PublicKey, ed25519.PublicKeySize)}
		if _, installErr := broken.Install(context.Background(), "pfm-smoke", ""); installErr == nil {
			t.Fatal("签名不匹配时安装应失败")
		}
	})

	t.Run("包内容被替换必须拒绝", func(t *testing.T) {
		ts.packages["smoke-tampered.tar.gz"] = []byte("not the published archive")
		entry := IndexEntry{
			Name: "pfm-smoke", DriverID: "smoke", Version: "1.0.1",
			URL: ts.server.URL + "/pkg/smoke-tampered.tar.gz", SHA256: "deadbeef",
		}
		ts.publish(entry, newTestPackage("smoke", "1.0.1"))
		// publish 会重算 sha256，这里手动改回错误值模拟"索引与实际不符"
		ts.entries[len(ts.entries)-1].SHA256 = "deadbeef"
		if _, installErr := ts.installer(dir).Install(context.Background(), "pfm-smoke", "1.0.1"); installErr == nil {
			t.Fatal("sha256 不匹配时安装应失败")
		}
	})

	t.Run("索引回退必须拒绝", func(t *testing.T) {
		ts.publish(IndexEntry{Name: "pfm-smoke", DriverID: "smoke", Version: "1.1.0",
			URL: ts.server.URL + "/pkg/smoke-1.1.0.tar.gz"}, newTestPackage("smoke", "1.1.0"))
		ts.seq = 5
		if _, installErr := ts.installer(dir).Install(context.Background(), "pfm-smoke", ""); installErr != nil {
			t.Fatalf("seq=5 应能安装: %v", installErr)
		}
		// 服务端退回更旧的索引（攻击者控制镜像时的典型手法）
		ts.seq = 3
		if _, installErr := ts.installer(dir).Install(context.Background(), "pfm-smoke", ""); installErr == nil {
			t.Fatal("索引 seq 回退时应拒绝安装")
		}
	})
}

func TestInstallRejectsUnsafeArchive(t *testing.T) {
	dir := newPluginRoot(t)
	ts := newTestIndexServer(t, dir)
	pkg := newTestPackage("evil", "1.0.0").add("../escape.txt", "x")
	entry := IndexEntry{Name: "pfm-evil", DriverID: "evil", Version: "1.0.0",
		URL: ts.server.URL + "/pkg/evil.tar.gz"}
	ts.publish(entry, pkg)
	if _, err := ts.installer(dir).Install(context.Background(), "pfm-evil", ""); err == nil {
		t.Fatal("包含跨目录路径的包应被拒绝")
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "escape.txt")); err == nil {
		t.Fatal("不应该写出目标目录之外的文件")
	}

	t.Run("driver_id 不一致必须拒绝", func(t *testing.T) {
		mismatch := IndexEntry{Name: "pfm-mismatch", DriverID: "expected", Version: "1.0.0",
			URL: ts.server.URL + "/pkg/mismatch.tar.gz"}
		ts.publish(mismatch, newTestPackage("actual", "1.0.0"))
		if _, err := ts.installer(dir).Install(context.Background(), "pfm-mismatch", ""); err == nil {
			t.Fatal("driver_id 不一致时应失败")
		}
	})

	t.Run("min_pfm_version 不足必须拒绝", func(t *testing.T) {
		entry := IndexEntry{Name: "pfm-future", DriverID: "future", Version: "1.0.0",
			URL: ts.server.URL + "/pkg/future.tar.gz", MinPfmVersion: "99.0.0"}
		ts.publish(entry, newTestPackage("future", "1.0.0"))
		if _, err := ts.installer(dir).Install(context.Background(), "pfm-future", ""); err == nil {
			t.Fatal("pfm 版本过低时应失败")
		}
	})
}

func TestInstallUpgradeAndRollback(t *testing.T) {
	dir := newPluginRoot(t)
	ts := newTestIndexServer(t, dir)
	ts.publish(IndexEntry{Name: "pfm-smoke", DriverID: "smoke", Version: "1.0.0",
		URL: ts.server.URL + "/pkg/smoke-1.0.0.tar.gz"}, newTestPackage("smoke", "1.0.0"))
	ts.publish(IndexEntry{Name: "pfm-smoke", DriverID: "smoke", Version: "2.0.0",
		URL: ts.server.URL + "/pkg/smoke-2.0.0.tar.gz"}, newTestPackage("smoke", "2.0.0"))
	installer := ts.installer(dir)

	if _, err := installer.Install(context.Background(), "pfm-smoke", "1.0.0"); err != nil {
		t.Fatalf("安装 v1 失败: %v", err)
	}
	if _, err := installer.Install(context.Background(), "pfm-smoke", "2.0.0"); err != nil {
		t.Fatalf("升级 v2 失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, archivedDirName, "pfm-smoke", "1.0.0")); err != nil {
		t.Fatalf("旧版本应被归档: %v", err)
	}

	result, err := installer.Rollback("pfm-smoke")
	if err != nil {
		t.Fatalf("回滚失败: %v", err)
	}
	if result.Version != "1.0.0" || result.PreviousVersion != "2.0.0" {
		t.Fatalf("回滚结果不符: %+v", result)
	}
	manifestPath := filepath.Join(dir, "pfm-smoke", "plugin.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("读取回滚后的清单失败: %v", err)
	}
	if !strings.Contains(string(raw), `"1.0.0"`) {
		t.Fatalf("回滚后清单版本不对: %s", raw)
	}
	integrity, err := CheckInstalled(dir)
	if err != nil {
		t.Fatalf("完整性检查失败: %v", err)
	}
	if len(integrity) != 1 || integrity[0].Status != "ok" || integrity[0].Version != "1.0.0" {
		t.Fatalf("回滚后完整性不符: %+v", integrity)
	}

	// 再回滚一次：又回到 2.0.0（history 交替）
	again, err := installer.Rollback("pfm-smoke")
	if err != nil {
		t.Fatalf("第二次回滚失败: %v", err)
	}
	if again.Version != "2.0.0" {
		t.Fatalf("第二次回滚应回到 2.0.0，实际 %s", again.Version)
	}
}

func TestCheckInstalledDetectsModification(t *testing.T) {
	dir := newPluginRoot(t)
	ts := newTestIndexServer(t, dir)
	ts.publish(IndexEntry{Name: "pfm-smoke", DriverID: "smoke", Version: "1.0.0",
		URL: ts.server.URL + "/pkg/smoke.tar.gz"}, newTestPackage("smoke", "1.0.0"))
	if _, err := ts.installer(dir).Install(context.Background(), "pfm-smoke", ""); err != nil {
		t.Fatalf("安装失败: %v", err)
	}
	target := filepath.Join(dir, "pfm-smoke", "pfm-plugin-smoke")
	if err := os.WriteFile(target, []byte("#!/bin/sh\necho hacked\n"), 0o755); err != nil {
		t.Fatalf("改写文件失败: %v", err)
	}
	integrity, err := CheckInstalled(dir)
	if err != nil {
		t.Fatalf("完整性检查失败: %v", err)
	}
	if len(integrity) != 1 || integrity[0].Status != "modified" {
		t.Fatalf("被改动后应报 modified: %+v", integrity)
	}
}

func TestCompareVersion(t *testing.T) {
	cases := []struct {
		left, right string
		want        int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.10", "1.0.9", 1},
		{"2.0.0", "10.0.0", -1},
		{"1.2.3-beta", "1.2.3", 0},
		{"", "0.0.1", -1},
		{"v1.1.0", "1.0.9", 1},
	}
	for _, item := range cases {
		if got := compareVersion(item.left, item.right); got != item.want {
			t.Fatalf("compareVersion(%q, %q) = %d, want %d", item.left, item.right, got, item.want)
		}
	}
	if !versionAtLeast("1.2.0", "") {
		t.Fatal("空的最低版本要求应满足")
	}
}
