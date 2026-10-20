package download

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"jenken827.com/jenken827/pfm/pkg/downloader/aria2"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

// localAria2Client 连接本机 aria2；未启动 / 没有 token 时跳过用例。
//
// 用 ARIA2_RPC_ENDPOINT / ARIA2_RPC_TOKEN 指定，默认 http://127.0.0.1:6800/jsonrpc。
func localAria2Client(t *testing.T) aria2.Client {
	t.Helper()
	endpoint := os.Getenv("ARIA2_RPC_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:6800/jsonrpc"
	}
	token := os.Getenv("ARIA2_RPC_TOKEN")

	c, err := aria2.New(context.Background(), endpoint, token, 2*time.Second, nil)
	if err != nil {
		t.Skipf("本机 aria2 RPC 不可用，跳过：%v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if _, err := c.GetVersion(); err != nil {
		t.Skipf("本机 aria2 RPC 不可用（%s），跳过：%v", endpoint, err)
	}
	return c
}

// 磁力任务在 aria2 上的真实形状：元数据 gid 自带 infoHash、files 是
// [METADATA] 伪文件。这是「按 infohash 找产物」能成立的前提。
func TestAria2MagnetMetadataContract(t *testing.T) {
	c := localAria2Client(t)

	infohash := randomInfoHash(t)
	uri := "magnet:?xt=urn:btih:" + infohash + "&dn=contract-probe"
	gid, err := c.AddURI([]string{uri}, map[string]any{
		"dir":              t.TempDir(),
		"bt-save-metadata": "false",
	})
	if err != nil {
		t.Fatalf("addUri 失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = c.ForceRemove(gid)
		_, _ = c.RemoveDownloadResult(gid)
	})

	info := waitForTaskRole(t, c, gid, roleMetadata, true)
	if !strings.EqualFold(info.InfoHash, infohash) {
		t.Fatalf("元数据任务 infoHash = %q, want %q", info.InfoHash, infohash)
	}

	// 整条解析链路：真实快照 → 磁力记录 → 中间任务（不能当产物导入）
	snap := fetchTaskSnapshot(c)
	dr := &native.DownloadRecordORM{Gid: utils.StringPtr(gid), Url: utils.StringPtr(uri)}
	task, ok := snap.targetFor(dr)
	if !ok {
		t.Fatal("真实 aria2 快照里找不到刚添加的磁力任务")
	}
	if got := classifyTask(task); got == roleFinal {
		t.Fatalf("磁力元数据任务被当成产物：%+v", task)
	}
	if task.Gid != gid {
		t.Fatalf("targetFor gid = %q, want %q", task.Gid, gid)
	}
}

// 普通 http 直链任务的形状：无 infoHash、files 是真实文件 → 可直接当产物
func TestAria2URLTaskContract(t *testing.T) {
	c := localAria2Client(t)

	gid, err := c.AddURI([]string{"http://127.0.0.1:1/never-reachable.bin"}, map[string]any{
		"dir":            t.TempDir(),
		"pause-metadata": "false",
		"max-tries":      "1",
	})
	if err != nil {
		t.Fatalf("addUri 失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = c.ForceRemove(gid)
		_, _ = c.RemoveDownloadResult(gid)
	})

	info := waitForTaskRole(t, c, gid, roleFinal, false)
	if info.InfoHash != "" {
		t.Fatalf("非 BT 任务不应有 infoHash：%q", info.InfoHash)
	}
}

// waitForTaskRole 轮询任务，直到它稳定处于期望角色
func waitForTaskRole(t *testing.T, c aria2.Client, gid string, want taskRole, requireInfoHash bool) aria2.StatusInfo {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last aria2.StatusInfo
	for time.Now().Before(deadline) {
		info, err := c.TellStatus(gid, aria2TaskKeys...)
		if err != nil {
			t.Fatalf("tellStatus 失败: %v", err)
		}
		last = info
		if classifyTask(info) == want && (!requireInfoHash || info.InfoHash != "") {
			return info
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("等待任务进入角色 %v 超时，最后状态：status=%s infoHash=%q files=%d",
		want, last.Status, last.InfoHash, len(last.Files))
	return last
}

func randomInfoHash(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("生成随机 infohash 失败: %v", err)
	}
	return hex.EncodeToString(buf)
}
