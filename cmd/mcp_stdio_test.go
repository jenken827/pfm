package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// relayOnce 用一把假端点跑一条消息，返回客户端收到的输出
func relayOnce(t *testing.T, handler http.HandlerFunc, bridge *stdioBridge, line string) string {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	bridge.endpoint = server.URL
	var out bytes.Buffer
	writer := bufio.NewWriter(&out)
	if err := bridge.relay(context.Background(), []byte(line), writer); err != nil {
		t.Fatalf("relay: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	return out.String()
}

// 新版无状态协议：头要按消息内容填（版本 / 方法 / 工具名），响应原样回给客户端
func TestStdioBridgeMirrorsModernHeaders(t *testing.T) {
	var seen *http.Request
	handler := func(w http.ResponseWriter, r *http.Request) {
		seen = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
	}
	line := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_files",` +
		`"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`
	bridge := &stdioBridge{token: "pfm_mcp_test", client: http.DefaultClient}
	out := relayOnce(t, handler, bridge, line)

	if got := seen.Header.Get("Authorization"); got != "Bearer pfm_mcp_test" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := seen.Header.Get("Mcp-Method"); got != "tools/call" {
		t.Fatalf("Mcp-Method = %q", got)
	}
	if got := seen.Header.Get("Mcp-Name"); got != "list_files" {
		t.Fatalf("Mcp-Name = %q", got)
	}
	if got := seen.Header.Get("MCP-Protocol-Version"); got != "2026-07-28" {
		t.Fatalf("MCP-Protocol-Version = %q", got)
	}
	if !strings.Contains(out, `"tools":[]`) {
		t.Fatalf("响应没透传: %q", out)
	}
}

// 旧版协议：没有 _meta 就不带版本头，但要把会话 id 记住并在下次请求带上
func TestStdioBridgeKeepsLegacySession(t *testing.T) {
	var gotSession string
	calls := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotSession = r.Header.Get("Mcp-Session-Id")
		if r.Header.Get("MCP-Protocol-Version") != "" {
			t.Errorf("旧版消息不该带版本头: %q", r.Header.Get("MCP-Protocol-Version"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "sess-1")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}
	bridge := &stdioBridge{token: "t", client: http.DefaultClient}
	relayOnce(t, handler, bridge, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if bridge.sessionID != "sess-1" {
		t.Fatalf("没记住会话 id: %q", bridge.sessionID)
	}
	relayOnce(t, handler, bridge, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if gotSession != "sess-1" {
		t.Fatalf("后续请求没带会话 id: %q", gotSession)
	}
	if calls != 2 {
		t.Fatalf("请求数 = %d", calls)
	}
}

// 通知（202 + 空响应）不能往 stdout 写东西，否则会把客户端的解析搞坏
func TestStdioBridgeSilentOnNotification(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}
	bridge := &stdioBridge{token: "t", client: http.DefaultClient}
	out := relayOnce(t, handler, bridge, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if out != "" {
		t.Fatalf("通知不该有输出: %q", out)
	}
}

// 传输层失败要变成一条 JSON-RPC 错误响应（带原 id），而不是把 stdout 弄脏或静默
func TestStdioBridgeTransportErrorBecomesJSONRPCError(t *testing.T) {
	bridge := &stdioBridge{token: "t", client: &http.Client{}}
	bridge.endpoint = "http://127.0.0.1:1/mcp" // 必然会连接失败
	var out bytes.Buffer
	writer := bufio.NewWriter(&out)
	if err := bridge.relay(context.Background(), []byte(`{"jsonrpc":"2.0","id":"a1","method":"tools/list"}`), writer); err != nil {
		t.Fatalf("relay: %v", err)
	}
	_ = writer.Flush()
	var resp struct {
		ID    string `json:"id"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("不是合法 JSON-RPC 错误: %v (%q)", err, out.String())
	}
	if resp.ID != "a1" || resp.Error.Code != -32603 || resp.Error.Message == "" {
		t.Fatalf("错误响应不对: %+v", resp)
	}
}

// 401 带 JSON-RPC 错误体时原样透传（客户端能看到"令牌无效"这类原因）
func TestStdioBridgePassesThroughJSONRPCAuthError(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":7,"error":{"code":-32001,"message":"令牌无效"}}`))
	}
	bridge := &stdioBridge{token: "bad", client: http.DefaultClient}
	out := relayOnce(t, handler, bridge, `{"jsonrpc":"2.0","id":7,"method":"tools/list","params":{}}`)
	if !strings.Contains(out, "令牌无效") {
		t.Fatalf("错误体没透传: %q", out)
	}
}

// 非 JSON 输入不回响应（没有 id 可回），但也不能崩
func TestStdioBridgeIgnoresGarbageWithoutID(t *testing.T) {
	bridge := &stdioBridge{token: "t", client: http.DefaultClient}
	bridge.endpoint = "http://127.0.0.1:1/mcp"
	var out bytes.Buffer
	writer := bufio.NewWriter(&out)
	if err := bridge.relay(context.Background(), []byte("not json"), writer); err != nil {
		t.Fatalf("relay: %v", err)
	}
	_ = writer.Flush()
	if out.Len() != 0 {
		t.Fatalf("无 id 的坏消息不该有输出: %q", out.String())
	}
}
