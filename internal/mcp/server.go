package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jenken827/pfm/internal/auth"
	"github.com/jenken827/pfm/pkg/llog"
)

// instructions 给模型的服务器说明（server/discover 与 initialize 都会带）
const instructions = "PFM 是个人文件管理器，提供的是虚拟目录树（与存储提供者内部的目录结构无关）。" +
	"用 path（以 / 开头）或 file_id 定位文件；目录不存在时会给出同层候选名。" +
	"删除只进回收站，可以恢复；写操作可能被管理员关闭。"

// Server MCP 协议入口。
//
// 常驻单例、无会话状态（modern 协议本来就没有会话；legacy 只存版本与客户端能力）。
// 鉴权与用户空间注入在 server 层完成，这里只从 ctx 取令牌身份。
type Server struct {
	settings Settings
	registry *Registry
	sessions *sessionStore
	limiter  *tokenLimiter
}

// NewServer 构造 MCP 服务（工具表在构造时确定，插件工具随热加载扩展）
func NewServer(settings Settings) *Server {
	return &Server{
		settings: settings,
		registry: newRegistry(),
		sessions: newSessionStore(),
		limiter:  newTokenLimiter(),
	}
}

// Settings 当前运行时参数（server 层拼配置时用）
func (s *Server) Settings() Settings { return s.settings }

// DescribeTools 列出全部工具定义（不按权限过滤，供 `pfm mcp tools` 排查）
func (s *Server) DescribeTools() []map[string]any {
	tools := make([]map[string]any, 0, len(s.registry.tools))
	for _, tool := range s.registry.tools {
		item := tool.toJSON()
		item["source"] = tool.Source
		item["readOnly"] = tool.ReadOnly
		tools = append(tools, item)
	}
	return tools
}

// ServeHTTP MCP 端点。要求 ctx 里已有令牌身份（由 server 层鉴权后注入）。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.originAllowed(r) {
		writeRPCError(w, http.StatusForbidden, nil,
			&rpcError{Code: codeInvalidRequest, Message: "Origin 不被允许"})
		return
	}
	// legacy 用 DELETE 结束会话
	if r.Method == http.MethodDelete {
		s.sessions.Delete(r.Header.Get(headerSessionID))
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if r.Method != http.MethodPost {
		writeRPCError(w, http.StatusMethodNotAllowed, nil,
			&rpcError{Code: codeInvalidRequest, Message: "MCP 端点只接受 POST"})
		return
	}

	principal := PrincipalFrom(r.Context())
	if principal == nil {
		writeRPCError(w, http.StatusUnauthorized, nil,
			&rpcError{Code: codeInvalidRequest, Message: "未认证"})
		return
	}
	if !s.limiter.Allow(principal.TokenID, s.settings.TokenPerMin) {
		writeRPCError(w, http.StatusTooManyRequests, nil,
			&rpcError{Code: codeInvalidRequest, Message: "调用过于频繁，请稍后重试"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes))
	if err != nil {
		writeRPCError(w, http.StatusBadRequest, nil,
			&rpcError{Code: codeParseError, Message: fmt.Sprintf("读取请求失败: %v", err)})
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPCError(w, http.StatusBadRequest, nil,
			&rpcError{Code: codeParseError, Message: "请求不是合法 JSON-RPC"})
		return
	}
	if req.Method == "" {
		writeRPCError(w, http.StatusBadRequest, req.ID,
			&rpcError{Code: codeInvalidRequest, Message: "缺少 method"})
		return
	}

	// legacy 握手：唯一一个"建立会话"的入口
	if req.Method == "initialize" {
		s.serveInitialize(w, &req)
		return
	}

	// 客户端通知：接受即可（不回响应体）
	if strings.HasPrefix(req.Method, "notifications/") {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	legacy := false
	if sessionID := r.Header.Get(headerSessionID); sessionID != "" {
		legacy = true
		if s.sessions.Get(sessionID) == nil {
			writeRPCError(w, http.StatusNotFound, req.ID,
				&rpcError{Code: codeInvalidRequest, Message: "会话不存在或已过期，请重新 initialize"})
			return
		}
	} else if status, rpcErr := s.validateModernEnvelope(r, &req); rpcErr != nil {
		writeRPCError(w, status, req.ID, rpcErr)
		return
	}

	result, status, rpcErr := s.dispatch(r.Context(), principal, &req, legacy)
	if rpcErr != nil {
		writeRPCError(w, status, req.ID, rpcErr)
		return
	}
	writeResult(w, req.ID, result)
}

// originAllowed 校验浏览器 Origin（防 DNS rebinding）；未配置白名单时不校验
func (s *Server) originAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" || len(s.settings.AllowOrigins) == 0 {
		return true
	}
	for _, allowed := range s.settings.AllowOrigins {
		if strings.EqualFold(strings.TrimSpace(allowed), origin) {
			return true
		}
	}
	return false
}

// validateModernEnvelope 校验 modern 请求的头与 body 一致性。
//
// 规范要求两头都对：中间层会按头路由/限流，服务端按 body 执行，不一致就是漏洞。
func (s *Server) validateModernEnvelope(r *http.Request, req *rpcRequest) (int, *rpcError) {
	params := &genericParams{}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, params); err != nil {
			return http.StatusBadRequest, &rpcError{Code: codeInvalidParams, Message: "params 不是合法对象"}
		}
	}

	headerVersion := strings.TrimSpace(r.Header.Get(headerProtocolVersion))
	bodyVersion := protocolVersionOf(params.Meta)
	if headerVersion == "" || bodyVersion == "" {
		return http.StatusBadRequest, &rpcError{
			Code:    codeHeaderMismatch,
			Message: "缺少 MCP-Protocol-Version 头或 params._meta 里的协议版本",
		}
	}
	if headerVersion != bodyVersion {
		return http.StatusBadRequest, &rpcError{
			Code:    codeHeaderMismatch,
			Message: fmt.Sprintf("协议版本不一致：请求头 %q，body %q", headerVersion, bodyVersion),
		}
	}
	if isLegacyVersion(headerVersion) || !isSupportedVersion(headerVersion) {
		return http.StatusBadRequest, &rpcError{
			Code:    codeUnsupportedVersion,
			Message: "不支持的协议版本",
			Data: map[string]any{
				"supported": SupportedVersions,
				"requested": headerVersion,
			},
		}
	}

	if method := strings.TrimSpace(r.Header.Get(headerMethod)); method != req.Method {
		return http.StatusBadRequest, &rpcError{
			Code:    codeHeaderMismatch,
			Message: fmt.Sprintf("Mcp-Method 与 body 不一致：请求头 %q，body %q", method, req.Method),
		}
	}

	expectedName := nameOfMethod(req.Method, params)
	if expectedName != "" {
		nameHeader := strings.TrimSpace(r.Header.Get(headerName))
		if nameHeader != expectedName {
			return http.StatusBadRequest, &rpcError{
				Code:    codeHeaderMismatch,
				Message: fmt.Sprintf("Mcp-Name 与 body 不一致：请求头 %q，body %q", nameHeader, expectedName),
			}
		}
	}
	return 0, nil
}

// nameOfMethod 该方法的 Mcp-Name 期望值（不需要的方法返回空串）
func nameOfMethod(method string, params *genericParams) string {
	switch method {
	case "tools/call", "prompts/get":
		return params.Name
	case "resources/read":
		return params.URI
	default:
		return ""
	}
}

// dispatch 方法分派
func (s *Server) dispatch(ctx context.Context, principal *auth.McpPrincipal,
	req *rpcRequest, legacy bool) (any, int, *rpcError) {
	switch req.Method {
	case "server/discover":
		if legacy {
			return nil, http.StatusNotFound, methodNotFound(req.Method)
		}
		return s.discoverResult(), 0, nil
	case "tools/list":
		return s.listTools(principal, req, legacy)
	case "tools/call":
		return s.callTool(ctx, principal, req, legacy)
	case "ping":
		if !legacy {
			return nil, http.StatusNotFound, methodNotFound(req.Method)
		}
		return map[string]any{}, 0, nil
	default:
		return nil, http.StatusNotFound, methodNotFound(req.Method)
	}
}

// methodNotFound 未知方法（规范要求 404 + -32601）
func methodNotFound(method string) *rpcError {
	return &rpcError{Code: codeMethodNotFound, Message: "未实现的方法: " + method}
}

// discoverResult server/discover 的结果
func (s *Server) discoverResult() map[string]any {
	return map[string]any{
		resultTypeKey:       resultTypeComplete,
		"supportedVersions": SupportedVersions,
		"capabilities":      map[string]any{"tools": map[string]any{"listChanged": false}},
		"instructions":      instructions,
		"ttlMs":             3600000,
		"cacheScope":        "private",
		"_meta":             s.serverMeta(),
	}
}

// listTools tools/list 的结果
func (s *Server) listTools(principal *auth.McpPrincipal, req *rpcRequest, legacy bool) (any, int, *rpcError) {
	includeRead := principal.AllowRead
	includeWrite := s.settings.EffectiveWrite(principal)
	tools := make([]any, 0, len(s.registry.tools))
	for _, tool := range s.registry.List(includeRead, includeWrite) {
		tools = append(tools, tool.toJSON())
	}
	result := map[string]any{
		"tools":      tools,
		"ttlMs":      30000,
		"cacheScope": "private",
	}
	s.annotate(result, legacy)
	return result, 0, nil
}

// callTool tools/call 的实现
func (s *Server) callTool(ctx context.Context, principal *auth.McpPrincipal,
	req *rpcRequest, legacy bool) (any, int, *rpcError) {
	params := &callParams{}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, params); err != nil {
			return nil, http.StatusOK, &rpcError{Code: codeInvalidParams, Message: "params 不是合法对象"}
		}
	}
	tool, ok := s.registry.Get(params.Name)
	if !ok {
		return nil, http.StatusOK, &rpcError{
			Code:    codeInvalidParams,
			Message: fmt.Sprintf("未知工具: %s", params.Name),
		}
	}
	if !tool.ReadOnly && !s.settings.EffectiveWrite(principal) {
		return nil, http.StatusOK, &rpcError{
			Code:    codeInvalidParams,
			Message: fmt.Sprintf("工具 %s 需要写权限（配置与令牌都要打开）", tool.Name),
		}
	}

	args := map[string]any{}
	if len(params.Arguments) > 0 {
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, http.StatusOK, &rpcError{Code: codeInvalidParams, Message: "arguments 不是合法对象"}
		}
	}

	callCtx, cancel := context.WithTimeout(ctx, s.settings.CallTimeout)
	defer cancel()

	started := time.Now()
	if err := validateArgs(tool.Schema, args); err != nil {
		s.logToolCall(ctx, principal, tool.Name, args, time.Since(started), err)
		return s.errorResult(err, legacy), http.StatusOK, nil
	}

	call := &Call{Args: args, Principal: principal, Settings: s.settings}
	result, err := tool.Handler(callCtx, call)
	elapsed := time.Since(started)
	if err != nil {
		s.logToolCall(ctx, principal, tool.Name, args, elapsed, err)
		return s.errorResult(err, legacy), http.StatusOK, nil
	}
	s.logToolCall(ctx, principal, tool.Name, args, elapsed, nil)
	return s.toolResult(result, legacy), http.StatusOK, nil
}

// toolResult 包装成功结果
func (s *Server) toolResult(result *Result, legacy bool) map[string]any {
	out := map[string]any{"content": result.Content}
	if result.Structured != nil {
		out["structuredContent"] = result.Structured
	}
	if result.IsError {
		out["isError"] = true
	}
	s.annotate(out, legacy)
	return out
}

// errorResult 工具执行错误（isError=true，模型可据此改参数重试）
func (s *Server) errorResult(err error, legacy bool) map[string]any {
	out := map[string]any{
		"content": []contentBlock{{Type: "text", Text: err.Error()}},
		"isError": true,
	}
	s.annotate(out, legacy)
	return out
}

// annotate 补上 resultType 与 serverInfo（legacy 客户端不认识这两个字段，省掉）
func (s *Server) annotate(result map[string]any, legacy bool) {
	if legacy {
		return
	}
	result[resultTypeKey] = resultTypeComplete
	result["_meta"] = s.serverMeta()
}

// serverMeta 规范要求的 serverInfo（放在 result 的 _meta 里）
func (s *Server) serverMeta() map[string]any {
	return map[string]any{
		metaKeyServerInfo: map[string]any{
			"name":    "pfm",
			"version": s.settings.Version,
		},
	}
}

// serveInitialize legacy 握手：发会话 id，回能力与版本
func (s *Server) serveInitialize(w http.ResponseWriter, req *rpcRequest) {
	params := &initializeParams{}
	if len(req.Params) > 0 {
		_ = json.Unmarshal(req.Params, params)
	}
	version := params.ProtocolVersion
	if !isLegacyVersion(version) {
		// 客户端报的是我们不认识的版本（或 modern 版本）：回我们能提供的最新旧版，
		// 由客户端决定是否继续（旧版客户端没有"往上协商"的机制）。
		version = LegacyProtocolVersion
	}
	session := s.sessions.Create(version, clientNameOf(params.ClientInfo))
	w.Header().Set(headerSessionID, session.ID)
	writeResult(w, req.ID, map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		"serverInfo":      map[string]any{"name": "pfm", "version": s.settings.Version},
		"instructions":    instructions,
	})
}

// 审计只记这些参数（足以定位；正文与凭据一律不记）
var auditArgKeys = []string{
	"path", "file_id", "parent_id", "keyword", "search_type", "limit", "offset", "size",
}

// logToolCall 记一行调用流水（不记结果、不记正文）
func (s *Server) logToolCall(ctx context.Context, principal *auth.McpPrincipal,
	tool string, args map[string]any, elapsed time.Duration, err error) {
	parts := make([]string, 0, len(auditArgKeys)+1)
	for _, key := range auditArgKeys {
		if value, ok := args[key]; ok {
			parts = append(parts, fmt.Sprintf("%s=%v", key, value))
		}
	}
	if _, ok := args["content"]; ok {
		parts = append(parts, "content=<redacted>")
	}
	status := "ok"
	if err != nil {
		status = "error=" + err.Error()
	}
	llog.C(ctx).Infof("mcp tool call tool=%s user=%s token=%d duration=%s args=[%s] %s",
		tool, principal.UserID, principal.TokenID, elapsed.Round(time.Millisecond),
		strings.Join(parts, " "), status)
}
