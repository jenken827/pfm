package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/jenken827/pfm-plugin-sdk/plugin/sdk"
	"github.com/jenken827/pfm-plugin-sdk/proto/gen/plugin"
)

// selftestTools 自测工具插件的工具清单（在插件进程里被拉起）
func selftestTools() []sdk.Tool {
	return []sdk.Tool{
		{
			Name:        "echo",
			Title:       "回显",
			Description: "把 text 参数原样回显",
			InputSchema: `{"type":"object","properties":{"text":{"type":"string"}}}`,
			ReadOnly:    true,
			Call: func(_ context.Context, in *sdk.ToolCall) (sdk.ToolResult, error) {
				var args struct {
					Text string `json:"text"`
				}
				if err := in.ArgumentsJSON(&args); err != nil {
					return sdk.ToolResult{}, err
				}
				return sdk.TextResult("echo:" + args.Text), nil
			},
		},
		{
			Name:        "touch",
			Description: "可写工具（只用来验证权限标记）",
			Destructive: true,
			Call: func(context.Context, *sdk.ToolCall) (sdk.ToolResult, error) {
				return sdk.TextResult("touched"), nil
			},
		},
	}
}

// writeToolPluginDir 造一个 kind=tool 的插件目录（可执行文件就是测试二进制）
func writeToolPluginDir(t *testing.T, toolID string, extra map[string]any) string {
	t.Helper()
	execPath, err := os.Executable()
	if err != nil {
		t.Fatalf("取测试二进制路径失败: %v", err)
	}
	fields := map[string]any{
		"kind":        "tool",
		"tool_id":     toolID,
		"exec":        execPath,
		"abi_version": 1,
		"name":        "pfm-test-tool",
		"version":     "0.0.1",
	}
	for key, value := range extra {
		fields[key] = value
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("序列化 plugin.json 失败: %v", err)
	}
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "pfm-test-tool")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("创建插件目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), raw, 0o600); err != nil {
		t.Fatalf("写 plugin.json 失败: %v", err)
	}
	return dir
}

// TestManagerLoadsToolPlugin 端到端：拉起工具插件 → Describe → 登记 → Call 跨进程转发
func TestManagerLoadsToolPlugin(t *testing.T) {
	t.Setenv(selfToolEnv, "1")
	mgr := NewManager(Options{
		Dir:              writeToolPluginDir(t, testToolID, nil),
		HandshakeTimeout: 10 * time.Second,
		ConfigureTimeout: 10 * time.Second,
	})
	ctx := context.Background()
	if err := mgr.Load(ctx); err != nil {
		t.Fatalf("加载工具插件失败: %v", err)
	}
	t.Cleanup(func() { mgr.Close() })

	plugins := ToolPlugins()
	if len(plugins) != 1 || plugins[0].ToolID != testToolID {
		t.Fatalf("登记的工具插件不对: %+v", plugins)
	}
	specs := plugins[0].Specs()
	if len(specs) != 2 {
		t.Fatalf("工具数 = %d, 想要 2", len(specs))
	}
	if specs[0].Name != "echo" || !specs[0].ReadOnly || specs[0].Title != "回显" {
		t.Fatalf("第一个工具的元数据不对: %+v", specs[0])
	}
	if specs[1].Name != "touch" || specs[1].ReadOnly || !specs[1].Destructive {
		t.Fatalf("第二个工具的元数据不对: %+v", specs[1])
	}

	// 跨进程转发：参数与调用方信息都要到插件那边
	resp, err := plugins[0].Call(ctx, "echo", `{"text":"hi"}`, &plugin.ToolCaller{
		UserId: proto.String("user-1"),
	})
	if err != nil {
		t.Fatalf("调用工具失败: %v", err)
	}
	if resp.GetIsError() || len(resp.GetContent()) != 1 {
		t.Fatalf("工具结果不对: %+v", resp)
	}
	if got := resp.GetContent()[0].GetText(); got != "echo:hi" {
		t.Fatalf("结果 = %q, want echo:hi", got)
	}

	// 插件侧未知工具是错误（宿主会翻成 isError）
	if _, err := plugins[0].Call(ctx, "nope", "{}", nil); err == nil {
		t.Fatal("未知工具应报错")
	}

	// 关掉之后不再登记
	mgr.Close()
	if len(ToolPlugins()) != 0 {
		t.Fatalf("关闭后仍登记着工具插件: %+v", ToolPlugins())
	}
}

// TestManagerRejectsToolPluginWithoutID tool_id 缺失 / 非法时加载失败（不给半可用工具表）
func TestManagerRejectsToolPluginWithoutID(t *testing.T) {
	t.Setenv(selfToolEnv, "1")
	mgr := NewManager(Options{
		Dir:              writeToolPluginDir(t, "Bad-ID", nil),
		HandshakeTimeout: 5 * time.Second,
		ConfigureTimeout: 5 * time.Second,
	})
	t.Cleanup(func() { mgr.Close() })
	err := mgr.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "tool_id") {
		t.Fatalf("非法 tool_id 应加载失败，实际：%v", err)
	}
	if len(ToolPlugins()) != 0 {
		t.Fatalf("失败的插件不该登记: %+v", ToolPlugins())
	}
}
