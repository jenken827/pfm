package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/jenken827/pfm-plugin-sdk/proto/gen/plugin"
	"github.com/jenken827/pfm/pkg/llog"
)

// ToolSpec 工具插件自报的一个工具（宿主原样并进 MCP 的 tools/list）
type ToolSpec struct {
	// Name 工具名（不含 tool_id 前缀）
	Name string
	// Title 展示名（可空）
	Title string
	// Description 给模型看的说明
	Description string
	// InputSchema 入参 JSON Schema（已校验是对象根）
	InputSchema string
	// ReadOnly 只读工具；false 时按写开关过滤
	ReadOnly bool
	// Destructive 破坏性操作
	Destructive bool
	// Idempotent 幂等
	Idempotent bool
}

// CallFunc 执行一次工具调用
type CallFunc func(ctx context.Context, name, argumentsJSON string, caller *plugin.ToolCaller) (*plugin.CallToolResp, error)

// ToolPlugin 宿主视角的一个工具插件（kind=tool）。
//
// 它只做两件事：把工具清单交出去（并入 MCP 工具表），把调用转给插件进程。
// MCP 的协议层、鉴权、scope、审计都在 mcp 包里，这个类型不认识 MCP。
type ToolPlugin struct {
	// ToolID 插件身份（plugin.json 的 tool_id），MCP 工具名前缀
	ToolID string
	// Name 插件包名（展示用）
	Name string
	// Version 插件版本（展示用）
	Version string
	specs   []ToolSpec
	call    CallFunc
}

// Specs 工具清单（副本，调用方随便改）
func (t *ToolPlugin) Specs() []ToolSpec {
	if t == nil {
		return nil
	}
	out := make([]ToolSpec, len(t.specs))
	copy(out, t.specs)
	return out
}

// Call 执行一次工具调用
func (t *ToolPlugin) Call(ctx context.Context, name, argumentsJSON string,
	caller *plugin.ToolCaller) (*plugin.CallToolResp, error) {
	if t == nil || t.call == nil {
		return nil, fmt.Errorf("插件 %s 没有可用的调用入口", t.ToolID)
	}
	return t.call(ctx, name, argumentsJSON, caller)
}

// NewToolPlugin 构造工具插件代理（管理器加载与测试共用）
func NewToolPlugin(toolID, name, version string, specs []ToolSpec, call CallFunc) *ToolPlugin {
	return &ToolPlugin{ToolID: toolID, Name: name, Version: version, specs: specs, call: call}
}

// toolNamePattern 工具名规则（与 SDK 侧一致）：小写字母开头，只含小写字母 / 数字 / 下划线
var toolNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// convertToolSpecs 校验插件自报的工具清单
//
// 名字 / Schema 在这里就拒掉，别等客户端拿到一份用不了的 tools/list：
// Schema 必须是对象根，否则宿主的基础校验会整段失效。
func convertToolSpecs(raw []*plugin.ToolSpec) ([]ToolSpec, error) {
	out := make([]ToolSpec, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		name := item.GetName()
		if !toolNamePattern.MatchString(name) {
			return nil, fmt.Errorf("工具名 %q 不合法（小写字母开头，只含小写字母 / 数字 / 下划线）", name)
		}
		if _, dup := seen[name]; dup {
			return nil, fmt.Errorf("工具名 %q 重复", name)
		}
		seen[name] = struct{}{}
		schema := strings.TrimSpace(item.GetInputSchema())
		if schema == "" {
			schema = `{"type":"object","properties":{}}`
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(schema), &parsed); err != nil {
			return nil, fmt.Errorf("工具 %q 的 inputSchema 不是合法 JSON: %w", name, err)
		}
		if kind, _ := parsed["type"].(string); kind != "object" {
			return nil, fmt.Errorf("工具 %q 的 inputSchema 必须以对象为根", name)
		}
		out = append(out, ToolSpec{
			Name:        name,
			Title:       item.GetTitle(),
			Description: item.GetDescription(),
			InputSchema: schema,
			ReadOnly:    item.GetReadOnly(),
			Destructive: item.GetDestructive(),
			Idempotent:  item.GetIdempotent(),
		})
	}
	return out, nil
}

// loadTool 拉起工具插件并登记它的工具清单
func (m *Manager) loadTool(ctx context.Context, dir string, mf *manifest, expectABI uint32) error {
	proc, err := startProcess(mf.execPath(dir), mf.ToolID, expectABI, m.opts.HandshakeTimeout)
	if err != nil {
		return err
	}
	describeCtx, cancel := context.WithTimeout(ctx, m.opts.ConfigureTimeout)
	defer cancel()
	resp := &plugin.ToolDescribeResp{}
	if err := proc.cli.call(describeCtx, "Describe", &plugin.DescribeReq{}, resp); err != nil {
		proc.Close()
		return fmt.Errorf("describe 失败: %w", err)
	}
	if resp.GetToolId() != "" && resp.GetToolId() != mf.ToolID {
		proc.Close()
		return fmt.Errorf("插件声明的 tool_id=%q 与 plugin.json 的 %q 不一致", resp.GetToolId(), mf.ToolID)
	}
	specs, err := convertToolSpecs(resp.GetTools())
	if err != nil {
		proc.Close()
		return err
	}
	if len(specs) == 0 {
		proc.Close()
		return fmt.Errorf("插件没有提供任何工具")
	}
	cli := proc.cli
	host := m.host
	proxy := NewToolPlugin(mf.ToolID, mf.Name, mf.Version, specs,
		func(callCtx context.Context, name, argumentsJSON string, caller *plugin.ToolCaller) (*plugin.CallToolResp, error) {
			req := &plugin.CallToolReq{
				Name:          proto.String(name),
				ArgumentsJson: proto.String(argumentsJSON),
				Caller:        caller,
			}
			if host != nil {
				req.Host = &plugin.HostEndpoint{Addr: proto.String(host.addr), Token: proto.String(host.token)}
			}
			out := &plugin.CallToolResp{}
			if callErr := cli.call(callCtx, "Call", req, out); callErr != nil {
				return nil, callErr
			}
			return out, nil
		})
	m.plugins = append(m.plugins, &loadedPlugin{manifest: mf, dir: dir, proc: proc, tool: proxy})
	llog.L().Infof("插件已加载: tool=%s name=%s version=%s tools=%d addr=%s pid=%d",
		mf.ToolID, mf.Name, mf.Version, len(specs), proc.addr, proc.pid)
	return nil
}

// ===== 工具插件注册表（MCP 层从这里取，避免反向依赖） =====

var (
	toolMu      sync.RWMutex
	toolPlugins []*ToolPlugin
	toolOwner   *Manager
)

// SetToolPlugins 登记当前生效的工具插件（由 Manager 在加载完成后调用）
func SetToolPlugins(owner *Manager, list []*ToolPlugin) {
	toolMu.Lock()
	defer toolMu.Unlock()
	toolOwner = owner
	toolPlugins = list
}

// ClearToolPlugins 清空工具插件；只有当前登记者才能清（热加载时新管理器先加载、旧的再关）
func ClearToolPlugins(owner *Manager) {
	toolMu.Lock()
	defer toolMu.Unlock()
	if toolOwner != owner {
		return
	}
	toolOwner = nil
	toolPlugins = nil
}

// ToolPlugins 返回当前生效的工具插件（按 tool_id 排序，供 MCP 工具表稳定输出）
func ToolPlugins() []*ToolPlugin {
	toolMu.RLock()
	defer toolMu.RUnlock()
	out := make([]*ToolPlugin, len(toolPlugins))
	copy(out, toolPlugins)
	sort.Slice(out, func(i, j int) bool { return out[i].ToolID < out[j].ToolID })
	return out
}

// ===== 插件读文件回调（由 MCP 层注入实现） =====

// FileReader 插件读文件字节的回调：宿主按调用凭据解析 scope 后再读
type FileReader func(ctx context.Context, req *plugin.ReadFileReq) (*plugin.ReadFileResp, error)

var (
	fileReaderMu sync.RWMutex
	fileReader   FileReader
)

// SetFileReader 注入读文件实现（传 nil 表示关掉）
func SetFileReader(reader FileReader) {
	fileReaderMu.Lock()
	defer fileReaderMu.Unlock()
	fileReader = reader
}

// currentFileReader 取当前实现
func currentFileReader() FileReader {
	fileReaderMu.RLock()
	defer fileReaderMu.RUnlock()
	return fileReader
}

// handleReadFile 插件读文件字节（凭据由 MCP 层签发，这里只负责转交）
func (h *hostServer) handleReadFile(w http.ResponseWriter, r *http.Request) {
	req := &plugin.ReadFileReq{}
	if !decodeHostRequest(w, r, req) {
		return
	}
	reader := currentFileReader()
	if reader == nil {
		writeHostError(w, http.StatusServiceUnavailable, "宿主未启用插件读文件能力")
		return
	}
	resp, err := reader(r.Context(), req)
	if err != nil {
		writeHostError(w, http.StatusForbidden, err.Error())
		return
	}
	writeHostProto(w, resp)
}
