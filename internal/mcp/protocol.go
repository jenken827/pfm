package mcp

import (
	"encoding/json"
	"net/http"
)

// MCP 协议常量与线上消息类型。
//
// 宿主实现两个"时代"：
//   - modern（2026-07-28 起）：无状态，每个请求在 params._meta 里带协议版本与
//     客户端能力；HTTP 头 MCP-Protocol-Version / Mcp-Method / Mcp-Name 必须与 body 一致。
//   - legacy（2025-06-18 / 2025-11-25）：先 initialize 拿会话，后续请求带 Mcp-Session-Id。
//
// 只做 modern 等于现网多数客户端用不了，只做 legacy 又会被新客户端判为旧服务器，
// 所以两条都实现（识别方式见 server.go 的 eraOf）。
const (
	// ProtocolVersion 最新协议版本（无状态、每请求元数据）
	ProtocolVersion = "2026-07-28"
	// LegacyProtocolVersion 兼容的最新旧版（initialize 握手 + 会话）
	LegacyProtocolVersion = "2025-11-25"
	// LegacyProtocolVersionV1 更早的旧版
	LegacyProtocolVersionV1 = "2025-06-18"

	// maxRequestBodyBytes 单条 JSON-RPC 消息上限（工具参数远小于此值）
	maxRequestBodyBytes = 1 << 20
	// resultTypeKey 结果里的 resultType 字段名
	resultTypeKey = "resultType"
	// resultTypeComplete 普通结果的 resultType（规范必带字段）
	resultTypeComplete = "complete"
)

// SupportedVersions 本宿主支持的协议版本，新在前（协商失败时原样回给客户端）
var SupportedVersions = []string{ProtocolVersion, LegacyProtocolVersion, LegacyProtocolVersionV1}

// JSON-RPC 错误码。-32020 / -32022 是规范保留区的协议错误码，不要改数值。
const (
	codeParseError         = -32700
	codeInvalidRequest     = -32600
	codeMethodNotFound     = -32601
	codeInvalidParams      = -32602
	codeInternalError      = -32603
	codeHeaderMismatch     = -32020
	codeUnsupportedVersion = -32022
)

// _meta 里的键名（取自规范，带 io.modelcontextprotocol/ 前缀）
const (
	metaKeyProtocolVersion    = "io.modelcontextprotocol/protocolVersion"
	metaKeyClientInfo         = "io.modelcontextprotocol/clientInfo"
	metaKeyClientCapabilities = "io.modelcontextprotocol/clientCapabilities"
	metaKeyServerInfo         = "io.modelcontextprotocol/serverInfo"
)

// HTTP 头（规范化后的 Go 形式）
const (
	headerProtocolVersion = "MCP-Protocol-Version"
	headerMethod          = "Mcp-Method"
	headerName            = "Mcp-Name"
	headerSessionID       = "Mcp-Session-Id"
)

// rpcRequest 一条 JSON-RPC 请求 / 通知
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcError JSON-RPC 错误体
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// rpcResponse JSON-RPC 响应体
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// callParams tools/call 的参数
type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Meta      map[string]any  `json:"_meta"`
}

// genericParams 只取"名字 / URI / _meta"这三样的一元参数壳（头校验用）
type genericParams struct {
	Name string         `json:"name"`
	URI  string         `json:"uri"`
	Meta map[string]any `json:"_meta"`
}

// initializeParams legacy initialize 的参数
type initializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	ClientInfo      map[string]any `json:"clientInfo"`
	Capabilities    map[string]any `json:"capabilities"`
}

// protocolVersionOf 从 params 的 _meta 里取协议版本（没有返回空串）
func protocolVersionOf(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	if v, ok := meta[metaKeyProtocolVersion].(string); ok {
		return v
	}
	return ""
}

// clientNameOf 取客户端名字（只用于日志）
func clientNameOf(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	info, ok := meta[metaKeyClientInfo].(map[string]any)
	if !ok {
		return ""
	}
	name, _ := info["name"].(string)
	return name
}

// isSupportedVersion 判断协议版本是否受支持
func isSupportedVersion(version string) bool {
	for _, v := range SupportedVersions {
		if v == version {
			return true
		}
	}
	return false
}

// isLegacyVersion 是否为需要 initialize 握手的旧版
func isLegacyVersion(version string) bool {
	return version == LegacyProtocolVersion || version == LegacyProtocolVersionV1
}

// writeJSON 写一个 JSON 响应
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeResult 写一条成功响应
func writeResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeJSON(w, http.StatusOK, &rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

// WriteHTTPError 非 JSON-RPC 请求（鉴权 / 方法不允许）失败时写一条 JSON-RPC 错误体。
//
// 供 server 层的鉴权中间件复用：客户端按 JSON-RPC 解析错误，不会因为拿到 HTML 而崩。
func WriteHTTPError(w http.ResponseWriter, status int, message string) {
	writeRPCError(w, status, nil, &rpcError{Code: codeInvalidRequest, Message: message})
}

// writeRPCError 写一条错误响应（状态码按协议语义选，见 spec 的 Server Validation）
func writeRPCError(w http.ResponseWriter, status int, id json.RawMessage, err *rpcError) {
	writeJSON(w, status, &rpcResponse{JSONRPC: "2.0", ID: id, Error: err})
}
