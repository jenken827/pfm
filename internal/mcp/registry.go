package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"

	"github.com/jenken827/pfm/internal/auth"
	"github.com/jenken827/pfm/internal/plugin"
)

// Tool 一个 MCP 工具：定义（name / schema / annotations）+ 实现
type Tool struct {
	// Name 工具名（小写 + 下划线；插件工具为 <tool_id>.<name>）
	Name string
	// Title 人读的短标题（客户端展示用）
	Title string
	// Description 给模型看的说明：做什么、参数口径、失败时怎么办
	Description string
	// Schema inputSchema（JSON Schema，2020-12；用 mustSchema 从 JSON 文本解析）
	Schema map[string]any
	// ReadOnly 只读工具（readOnlyHint）
	ReadOnly bool
	// Destructive 破坏性工具（destructiveHint；删除 / 分享这类）
	Destructive bool
	// Source 工具来源：core 或插件的 tool_id（tools/list 与排查用）
	Source string
	// Handler 实现
	Handler Handler
}

// Handler 工具实现。
//
// 返回 error 表示"工具执行错误"（结果里 isError=true，模型可据此改参数重试），
// 不是 JSON-RPC 协议错误。
type Handler func(ctx context.Context, call *Call) (*Result, error)

// Call 一次工具调用的上下文
type Call struct {
	// Args 原始参数（词典里取值统一走 Str / StrOr / IntOr 等辅助函数）
	Args map[string]any
	// Principal 调用者身份（MCP 令牌，或登录 JWT —— 后者 TokenID 为 0）
	Principal *auth.McpPrincipal
	// Settings 运行时参数
	Settings Settings
}

// Result 工具结果
type Result struct {
	// Content 内容块（text / image / resource_link）
	Content []contentBlock
	// Structured 结构化结果（与 outputSchema 对应，可空）
	Structured any
	// IsError 是否为工具执行错误
	IsError bool
}

// contentBlock MCP 内容块（只用到 text / image 两种）
type contentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	URI      string `json:"uri,omitempty"`
	Name     string `json:"name,omitempty"`
}

// jsonResult 结构化结果：序列化成文本块（兼容只读 content 的客户端）并附 structuredContent
func jsonResult(payload any) *Result {
	body, err := json.Marshal(payload)
	if err != nil {
		return &Result{
			Content: []contentBlock{{Type: "text", Text: fmt.Sprintf("序列化结果失败: %v", err)}},
			IsError: true,
		}
	}
	return &Result{
		Content:    []contentBlock{{Type: "text", Text: string(body)}},
		Structured: payload,
	}
}

// imageResult 图片结果（base64）
func imageResult(mimeType string, data []byte) *Result {
	return &Result{Content: []contentBlock{{
		Type:     "image",
		Data:     encodeBase64(data),
		MimeType: mimeType,
	}}}
}

// Registry 工具注册表（进程内常驻，顺序确定）
type Registry struct {
	// tools 核心工具（顺序即声明顺序，测试按它断言）
	tools []*Tool
	// mu 保护 plugins / signature（插件热加载后第一次 list/call 时刷新）
	mu sync.Mutex
	// plugins 插件工具（按名字排序，不含与核心工具重名的）
	plugins []*Tool
	// signature 当前插件工具集签名（同名同序则跳过重建）
	signature string
}

// newRegistry 构造注册表：核心工具在前，插件工具在后
func newRegistry() *Registry {
	registry := &Registry{}
	for _, tool := range coreTools() {
		registry.add(tool)
	}
	// 插件读文件回调（插件工具按需要文件字节时走这里，鉴权在 readFileForPlugin 里）
	preregisterReadFile()
	registry.refreshPlugins()
	return registry
}

// refreshPlugins 同步插件工具；插件集没变就直接返回（tools/list 是热路径）
func (r *Registry) refreshPlugins() {
	signature := toolSignature(plugin.ToolPlugins())
	r.mu.Lock()
	defer r.mu.Unlock()
	if signature == r.signature {
		return
	}
	r.signature = signature
	taken := make(map[string]struct{}, len(r.tools))
	for _, tool := range r.tools {
		taken[tool.Name] = struct{}{}
	}
	merged := make([]*Tool, 0, len(r.plugins))
	for _, tool := range pluginTools() {
		if _, dup := taken[tool.Name]; dup {
			// 插件不得覆盖核心工具（正常命名空间下不会发生）
			continue
		}
		taken[tool.Name] = struct{}{}
		merged = append(merged, tool)
	}
	r.plugins = sortTools(merged)
}

// all 当前全部工具（核心 + 插件），顺序：核心在前、插件按名字
func (r *Registry) all() []*Tool {
	r.refreshPlugins()
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Tool, 0, len(r.tools)+len(r.plugins))
	out = append(out, r.tools...)
	out = append(out, r.plugins...)
	return out
}

// add 追加一个工具（重名直接跳过：插件工具不得覆盖核心工具）
func (r *Registry) add(tool *Tool) {
	if tool == nil || tool.Name == "" || tool.Handler == nil {
		return
	}
	if _, exists := r.Get(tool.Name); exists {
		return
	}
	r.tools = append(r.tools, tool)
}

// List 返回可用工具：读工具看 includeRead，写工具看 includeWrite。
//
// 权限不足的工具直接不出现（而不是调用时才报错）——不让模型看见做不到的事，
// 也省掉一轮"试了才知道没权限"的往返。
func (r *Registry) List(includeRead, includeWrite bool) []*Tool {
	tools := r.all()
	out := make([]*Tool, 0, len(tools))
	for _, tool := range tools {
		if tool.ReadOnly && !includeRead {
			continue
		}
		if !tool.ReadOnly && !includeWrite {
			continue
		}
		out = append(out, tool)
	}
	return out
}

// Get 按名字取工具
func (r *Registry) Get(name string) (*Tool, bool) {
	for _, tool := range r.all() {
		if tool.Name == name {
			return tool, true
		}
	}
	return nil, false
}

// toJSON 工具的 MCP 定义
func (t *Tool) toJSON() map[string]any {
	out := map[string]any{
		"name":        t.Name,
		"description": t.Description,
		"inputSchema": t.Schema,
	}
	if t.Title != "" {
		out["title"] = t.Title
	}
	out["annotations"] = map[string]any{
		"readOnlyHint":    t.ReadOnly,
		"destructiveHint": t.Destructive,
		"idempotentHint":  t.ReadOnly,
		"openWorldHint":   false,
	}
	return out
}

// ===== 参数取值辅助 =====

// Str 取字符串参数
func (c *Call) Str(name string) (string, bool) {
	value, ok := c.Args[name]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	return text, true
}

// StrOr 取字符串参数（缺省值兜底）
func (c *Call) StrOr(name, fallback string) string {
	if value, ok := c.Str(name); ok && value != "" {
		return value
	}
	return fallback
}

// IntOr 取整数参数（缺省值兜底，超过上限收敛到上限，负数收敛到 0）
func (c *Call) IntOr(name string, fallback, max int) int {
	value, ok := c.Args[name]
	if !ok {
		return fallback
	}
	number, ok := value.(float64)
	if !ok {
		return fallback
	}
	out := int(number)
	if out < 0 {
		return 0
	}
	if max > 0 && out > max {
		return max
	}
	return out
}

// BoolOr 取布尔参数
func (c *Call) BoolOr(name string, fallback bool) bool {
	value, ok := c.Args[name]
	if !ok {
		return fallback
	}
	out, ok := value.(bool)
	if !ok {
		return fallback
	}
	return out
}

// StringSlice 取字符串数组参数
func (c *Call) StringSlice(name string) []string {
	raw, ok := c.Args[name].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

// Uint32Slice 取整数数组参数（JSON 数字是 float64）
func (c *Call) Uint32Slice(name string) []uint32 {
	raw, ok := c.Args[name].([]any)
	if !ok {
		return nil
	}
	out := make([]uint32, 0, len(raw))
	for _, item := range raw {
		number, ok := item.(float64)
		if !ok || number < 0 {
			continue
		}
		out = append(out, uint32(number))
	}
	return out
}

// mustSchema 从 JSON 文本解析 inputSchema（写错的 schema 是编程错误，启动即崩）
func mustSchema(raw string) map[string]any {
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		panic("mcp: 非法 inputSchema: " + err.Error())
	}
	return out
}

// validateArgs 参数的最小校验：必填齐全 + 类型对 + 不许塞未声明的参数。
//
// 不做完整 JSON Schema 校验（不引依赖），但足以把模型最常见的错（拼错参数名、
// 把数字传成字符串）在进入业务前拦下来。
func validateArgs(schema map[string]any, args map[string]any) error {
	properties, _ := schema["properties"].(map[string]any)

	if required, ok := schema["required"].([]any); ok {
		for _, item := range required {
			name, _ := item.(string)
			if _, exists := args[name]; !exists {
				return fmt.Errorf("缺少必填参数 %q", name)
			}
		}
	}

	if additional, ok := schema["additionalProperties"].(bool); ok && !additional {
		for name := range args {
			if _, declared := properties[name]; !declared {
				return fmt.Errorf("未知参数 %q", name)
			}
		}
	}

	for name, value := range args {
		property, ok := properties[name].(map[string]any)
		if !ok {
			continue
		}
		if err := validateValue(name, property, value); err != nil {
			return err
		}
	}
	return nil
}

// validateValue 校验单个参数的类型
func validateValue(name string, property map[string]any, value any) error {
	expect, _ := property["type"].(string)
	switch expect {
	case "":
		return nil
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("参数 %q 必须是字符串", name)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("参数 %q 必须是布尔值", name)
		}
	case "integer":
		number, ok := value.(float64)
		if !ok || number != math.Trunc(number) {
			return fmt.Errorf("参数 %q 必须是整数", name)
		}
	case "number":
		if _, ok := value.(float64); !ok {
			return fmt.Errorf("参数 %q 必须是数字", name)
		}
	case "array":
		if _, ok := value.([]any); !ok {
			return fmt.Errorf("参数 %q 必须是数组", name)
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("参数 %q 必须是对象", name)
		}
	}
	return nil
}
