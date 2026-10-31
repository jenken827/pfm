package mcp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	pluginpb "github.com/jenken827/pfm-plugin-sdk/proto/gen/plugin"
	"github.com/jenken827/pfm/globals"
	"github.com/jenken827/pfm/internal/auth"
	hostplugin "github.com/jenken827/pfm/internal/plugin"
	"github.com/jenken827/pfm/internal/service"
	"github.com/jenken827/pfm/server/middleware"
)

// accessTokenTTL 一次工具调用签给插件的短期凭据有效期。
//
// 凭据只活在这次调用里（插件拿它调宿主 ReadFile），比令牌本身短得多：
// 插件日志 / 进程内存里泄漏了也很快失效，且仍然受调用方 scope 约束。
const accessTokenTTL = 5 * time.Minute

// accessGrant 一次调用签发的凭据（进程内，不落库、不进日志）
type accessGrant struct {
	principal *auth.McpPrincipal
	expiresAt time.Time
}

// accessTokens 凭据表（并发安全；过期的在查询时顺手清）
var accessTokens = struct {
	mu     sync.Mutex
	grants map[string]accessGrant
}{grants: make(map[string]accessGrant)}

// issueAccessToken 为一次插件工具调用签发凭据
func issueAccessToken(principal *auth.McpPrincipal) (string, time.Time) {
	expiresAt := time.Now().Add(accessTokenTTL)
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		// 拿不到随机数就没法安全签发：调用方会看到错误，不会退化成可预测的凭据
		return "", expiresAt
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	accessTokens.mu.Lock()
	pruneAccessTokens(time.Now())
	accessTokens.grants[token] = accessGrant{principal: principal, expiresAt: expiresAt}
	accessTokens.mu.Unlock()
	return token, expiresAt
}

// principalForAccessToken 校验凭据并取回调用方
func principalForAccessToken(token string) (*auth.McpPrincipal, error) {
	if token == "" {
		return nil, fmt.Errorf("缺少调用凭据")
	}
	now := time.Now()
	accessTokens.mu.Lock()
	defer accessTokens.mu.Unlock()
	grant, ok := accessTokens.grants[token]
	if !ok {
		return nil, fmt.Errorf("调用凭据无效或已过期")
	}
	if now.After(grant.expiresAt) {
		delete(accessTokens.grants, token)
		return nil, fmt.Errorf("调用凭据已过期")
	}
	return grant.principal, nil
}

// pruneAccessTokens 清理过期凭据（调用方需持有锁）
func pruneAccessTokens(now time.Time) {
	for token, grant := range accessTokens.grants {
		if now.After(grant.expiresAt) {
			delete(accessTokens.grants, token)
		}
	}
}

// preregisterReadFile 把读文件实现接到插件层（MCP 未启用时也安全，插件不会调用）
func preregisterReadFile() {
	hostplugin.SetFileReader(readFileForPlugin)
}

// pluginTools 把当前加载的工具插件并进 MCP 工具表。
//
// 名字是 `<tool_id>.<name>`：命名空间隔离，插件改名不会撞核心工具。
func pluginTools() []*Tool {
	plugins := hostplugin.ToolPlugins()
	out := make([]*Tool, 0, len(plugins))
	for _, tp := range plugins {
		for _, spec := range tp.Specs() {
			out = append(out, buildPluginTool(tp, spec))
		}
	}
	return out
}

// buildPluginTool 一个插件工具 → MCP 工具定义
func buildPluginTool(tp *hostplugin.ToolPlugin, spec hostplugin.ToolSpec) *Tool {
	tool := &Tool{
		Name:        tp.ToolID + "." + spec.Name,
		Title:       spec.Title,
		Description: spec.Description,
		Schema:      mustSchema(spec.InputSchema),
		ReadOnly:    spec.ReadOnly,
		Destructive: spec.Destructive,
		Source:      tp.ToolID,
	}
	tool.Handler = func(ctx context.Context, call *Call) (*Result, error) {
		return callPluginTool(ctx, tp, spec, call)
	}
	return tool
}

// callPluginTool 转发一次调用给插件，并把结果块翻成 MCP 内容块
func callPluginTool(ctx context.Context, tp *hostplugin.ToolPlugin, spec hostplugin.ToolSpec,
	call *Call) (*Result, error) {
	body, err := json.Marshal(call.Args)
	if err != nil {
		return nil, fmt.Errorf("参数序列化失败: %w", err)
	}
	token, expiresAt := issueAccessToken(call.Principal)
	if token == "" {
		return nil, fmt.Errorf("签发调用凭据失败，请重试")
	}
	caller := &pluginpb.ToolCaller{
		TokenId:      proto.Uint64(uint64(call.Principal.TokenID)),
		TokenName:    proto.String(call.Principal.TokenName),
		RootFolderId: proto.String(call.Principal.RootFolderID),
		AccessToken:  proto.String(token),
		ExpiresAt:    proto.Int64(expiresAt.Unix()),
	}
	if call.Principal.UserID != "" {
		caller.UserId = proto.String(call.Principal.UserID)
	}
	resp, err := tp.Call(ctx, spec.Name, string(body), caller)
	if err != nil {
		// 插件侧报错按工具执行错误回给模型（模型可以改参数重试），不升级成协议错误
		return &Result{
			Content: []contentBlock{{Type: "text", Text: fmt.Sprintf("插件 %s 执行失败: %v", tp.ToolID, err)}},
			IsError: true,
		}, nil
	}
	return pluginResult(tp.ToolID, resp), nil
}

// pluginResult 插件返回的内容块 → MCP 内容块
func pluginResult(toolID string, resp *pluginpb.CallToolResp) *Result {
	result := &Result{IsError: resp.GetIsError()}
	for _, block := range resp.GetContent() {
		switch block.GetType() {
		case pluginpb.ToolContentType_TOOL_CONTENT_TYPE_TEXT:
			result.Content = append(result.Content, contentBlock{Type: "text", Text: block.GetText()})
		case pluginpb.ToolContentType_TOOL_CONTENT_TYPE_IMAGE:
			result.Content = append(result.Content, contentBlock{
				Type:     "image",
				Data:     encodeBase64(block.GetData()),
				MimeType: block.GetMimeType(),
			})
		}
	}
	if len(result.Content) == 0 {
		result.Content = []contentBlock{{
			Type: "text",
			Text: fmt.Sprintf("插件 %s 没有返回内容", toolID),
		}}
	}
	return result
}

// ===== 插件读文件（宿主侧实现，插件通过 host 回调调用） =====

// readFileForPlugin 按调用凭据解析文件并读一段字节。
//
// 这是插件能拿到的唯一数据通道：scope 由凭据里的 principal 决定，插件的身份不参与
// 授权——所以插件既读不到别人的文件，也跑不出令牌的可见根目录。
func readFileForPlugin(ctx context.Context, req *pluginpb.ReadFileReq) (*pluginpb.ReadFileResp, error) {
	principal, err := principalForAccessToken(req.GetAccessToken())
	if err != nil {
		return nil, err
	}
	if !principal.AllowRead {
		return nil, fmt.Errorf("该令牌没有读权限")
	}
	if strings.TrimSpace(req.GetFileId()) == "" {
		return nil, fmt.Errorf("缺少 file_id")
	}
	res, err := newResolver(principal)
	if err != nil {
		return nil, err
	}
	file, err := res.load(req.GetFileId())
	if err != nil {
		return nil, err
	}
	if !res.inScope(file) {
		return nil, fmt.Errorf("该文件不在令牌可见范围内")
	}
	if dirType(file) == "folder" {
		return nil, fmt.Errorf("%s 是目录，不能按文件读取", res.DisplayPath(file))
	}

	limit := int(req.GetMaxBytes())
	if maxRead := LoadSettings().MaxReadBytes; limit <= 0 || limit > maxRead {
		limit = maxRead
	}
	body, total, err := readFileBytes(ctx, principal, fieldID(file), req.GetOffset(), limit)
	if err != nil {
		return nil, err
	}
	return &pluginpb.ReadFileResp{
		Data:      body,
		MimeType:  proto.String(fieldMime(file)),
		Filename:  proto.String(fieldName(file)),
		Size:      proto.Uint64(uint64(total)),
		Truncated: proto.Bool(total > int64(req.GetOffset())+int64(len(body))),
	}, nil
}

// readFileBytes 读出 [offset, offset+limit) 的一段（service 层负责归属与落盘分发）
func readFileBytes(ctx context.Context, principal *auth.McpPrincipal, fileID string,
	offset uint64, limit int) ([]byte, int64, error) {
	// hostServer 的 ctx 来自插件请求：claim 与 UserSpace 必须按凭据补回来，
	// 否则 service 层拿不到归属与可用 provider
	callCtx := context.WithValue(ctx, globals.ContextUserKey,
		&auth.Claims{User: auth.User{Id: principal.UserID, Name: principal.TokenName}})
	callCtx = middleware.InjectUserSpace(callCtx, principal.UserID)

	stream, err := service.GetFileStream(callCtx, fileID, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("读取内容失败: %w", err)
	}
	defer func() { _ = stream.Reader.Close() }()
	if stream.IsPreDownload {
		return nil, 0, fmt.Errorf("内容正在从存储提供者预下载，请稍后重试")
	}
	if offset > 0 {
		if _, err := io.CopyN(io.Discard, stream.Reader, int64(offset)); err != nil && !errors.Is(err, io.EOF) {
			return nil, 0, fmt.Errorf("跳过前 %d 字节失败: %w", offset, err)
		}
	}
	body, err := io.ReadAll(io.LimitReader(stream.Reader, int64(limit)))
	if err != nil {
		return nil, 0, fmt.Errorf("读取内容失败: %w", err)
	}
	return body, stream.FileSize, nil
}

// toolSignature 插件工具集签名（tool_id + 工具名），用来判断是否需要重建
func toolSignature(plugins []*hostplugin.ToolPlugin) string {
	var builder strings.Builder
	for _, tp := range plugins {
		builder.WriteString(tp.ToolID)
		builder.WriteByte(':')
		for _, spec := range tp.Specs() {
			builder.WriteString(spec.Name)
			builder.WriteByte(',')
		}
		builder.WriteByte(';')
	}
	return builder.String()
}

// sortTools 按名字稳定排序（tools/list 输出确定）
func sortTools(tools []*Tool) []*Tool {
	sort.SliceStable(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}
