package mcp

import (
	"context"
	"testing"

	"google.golang.org/protobuf/proto"

	pluginpb "github.com/jenken827/pfm-plugin-sdk/proto/gen/plugin"
	"github.com/jenken827/pfm/internal/auth"
	hostplugin "github.com/jenken827/pfm/internal/plugin"
)

// registerFakeToolPlugin 登记一个假工具插件（不拉起进程），返回调用时捕获的 caller
func registerFakeToolPlugin(t *testing.T, captured **pluginpb.ToolCaller) {
	t.Helper()
	plugin := hostplugin.NewToolPlugin("imageinfo", "pfm-plugin-tool-imageinfo", "1.0.0",
		[]hostplugin.ToolSpec{
			{
				Name:        "describe",
				Title:       "看图片信息",
				Description: "返回图片尺寸",
				InputSchema: `{"type":"object","properties":{"file_id":{"type":"string"}}}`,
				ReadOnly:    true,
			},
			{
				Name:        "shred",
				Description: "删掉原图",
				InputSchema: `{"type":"object","properties":{}}`,
				Destructive: true,
			},
		},
		func(_ context.Context, name, argumentsJSON string, caller *pluginpb.ToolCaller) (*pluginpb.CallToolResp, error) {
			if captured != nil {
				*captured = caller
			}
			return &pluginpb.CallToolResp{Content: []*pluginpb.ToolContent{{
				Type: pluginpb.ToolContentType_TOOL_CONTENT_TYPE_TEXT.Enum(),
				Text: proto.String(name + " " + argumentsJSON),
			}}}, nil
		})
	hostplugin.SetToolPlugins(nil, []*hostplugin.ToolPlugin{plugin})
	t.Cleanup(func() { hostplugin.SetToolPlugins(nil, nil) })
}

// scopedPrincipal 带写权限与可见根目录的调用方（与 server_test 的 testPrincipal 区分）
func scopedPrincipal() *auth.McpPrincipal {
	return &auth.McpPrincipal{
		TokenID:      7,
		UserID:       "user-uuid",
		TokenName:    "laptop",
		AllowRead:    true,
		AllowWrite:   true,
		RootFolderID: "root-uuid",
	}
}

// 插件工具按 <tool_id>.<name> 并入工具表，读 / 写按 annotations 过滤
func TestRegistryMergesPluginTools(t *testing.T) {
	registerFakeToolPlugin(t, nil)
	registry := newRegistry()

	readNames := toolNames(registry.List(true, false))
	if !contains(readNames, "imageinfo.describe") {
		t.Fatalf("只读列表缺插件工具: %v", readNames)
	}
	if contains(readNames, "imageinfo.shred") {
		t.Fatalf("只读列表不该有写工具: %v", readNames)
	}
	writeNames := toolNames(registry.List(false, true))
	if !contains(writeNames, "imageinfo.shred") {
		t.Fatalf("写列表缺插件工具: %v", writeNames)
	}
	if contains(writeNames, "imageinfo.describe") {
		t.Fatalf("写列表不该有只读工具: %v", writeNames)
	}

	tool, ok := registry.Get("imageinfo.describe")
	if !ok {
		t.Fatal("Get 找不到插件工具")
	}
	if tool.Source != "imageinfo" || tool.Title != "看图片信息" {
		t.Fatalf("工具元数据不对: %+v", tool)
	}
	// 核心工具仍在（顺序在前）
	if names := toolNames(registry.List(true, true)); names[0] != "list_files" {
		t.Fatalf("核心工具应排在前面: %v", names[:3])
	}
}

// 调用插件工具：参数原样透传，并给插件签发可反查的调用凭据
func TestPluginToolCallIssuesAccessToken(t *testing.T) {
	var captured *pluginpb.ToolCaller
	registerFakeToolPlugin(t, &captured)
	registry := newRegistry()
	tool, ok := registry.Get("imageinfo.describe")
	if !ok {
		t.Fatal("Get 找不到插件工具")
	}
	principal := scopedPrincipal()
	result, err := tool.Handler(context.Background(), &Call{
		Args:      map[string]any{"file_id": "f-1"},
		Principal: principal,
		Settings:  Settings{},
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if result.IsError {
		t.Fatalf("不该是 isError: %+v", result)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "text" {
		t.Fatalf("内容块不对: %+v", result.Content)
	}
	if captured == nil {
		t.Fatal("插件没收到调用")
	}
	if captured.GetUserId() != principal.UserID || captured.GetTokenId() != uint64(principal.TokenID) {
		t.Fatalf("调用方信息不对: %+v", captured)
	}
	if captured.GetRootFolderId() != principal.RootFolderID {
		t.Fatalf("可见根目录没传: %+v", captured)
	}
	if captured.GetAccessToken() == "" {
		t.Fatal("没有签发调用凭据")
	}
	// 凭据能反查出同一个 principal（ReadFile 靠它鉴权）
	resolved, err := principalForAccessToken(captured.GetAccessToken())
	if err != nil {
		t.Fatalf("凭据反查失败: %v", err)
	}
	if resolved.UserID != principal.UserID || resolved.RootFolderID != principal.RootFolderID {
		t.Fatalf("反查结果不对: %+v", resolved)
	}
	if _, err := principalForAccessToken("forged"); err == nil {
		t.Fatal("伪造的凭据必须被拒")
	}
}

// 插件报错转成 isError 结果（不是协议错误）
func TestPluginToolCallErrorBecomesIsError(t *testing.T) {
	plugin := hostplugin.NewToolPlugin("broken", "pfm-plugin-tool-broken", "0.1.0",
		[]hostplugin.ToolSpec{{Name: "bang", InputSchema: `{"type":"object","properties":{}}`}},
		func(context.Context, string, string, *pluginpb.ToolCaller) (*pluginpb.CallToolResp, error) {
			return nil, context.DeadlineExceeded
		})
	hostplugin.SetToolPlugins(nil, []*hostplugin.ToolPlugin{plugin})
	t.Cleanup(func() { hostplugin.SetToolPlugins(nil, nil) })

	tool, ok := newRegistry().Get("broken.bang")
	if !ok {
		t.Fatal("Get 找不到插件工具")
	}
	result, err := tool.Handler(context.Background(), &Call{
		Args:      map[string]any{},
		Principal: scopedPrincipal(),
		Settings:  Settings{},
	})
	if err != nil {
		t.Fatalf("不该返回协议错误: %v", err)
	}
	if !result.IsError || result.Content[0].Type != "text" {
		t.Fatalf("应为 isError 文本结果: %+v", result)
	}
}

func toolNames(tools []*Tool) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Name)
	}
	return out
}

func contains(names []string, name string) bool {
	for _, item := range names {
		if item == name {
			return true
		}
	}
	return false
}
