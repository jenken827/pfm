package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jenken827/pfm/internal/auth"
)

// 这些用例只验证协议层（头/版本/分派/错误语义），不碰数据库：
// 工具实现里要读库的路径都不在这里走。

func newTestServer() *Server {
	return NewServer(Settings{
		MaxReadBytes: 1024,
		CallTimeout:  5 * time.Second,
		TokenPerMin:  1000,
		Version:      "test",
	})
}

func testPrincipal() *auth.McpPrincipal {
	return &auth.McpPrincipal{
		TokenID:   1,
		UserID:    "u-mcp-test",
		TokenName: "test",
		AllowRead: true,
	}
}

// modernBody 拼一个带 _meta 的 modern 请求体
func modernBody(method, extraParams string) string {
	params := `"_meta":{"io.modelcontextprotocol/protocolVersion":"` + ProtocolVersion + `"}`
	if extraParams != "" {
		params = extraParams + "," + params
	}
	return `{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":{` + params + `}}`
}

func doPost(t *testing.T, server *Server, principal *auth.McpPrincipal, body string,
	headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	ctx := context.Background()
	if principal != nil {
		ctx = WithPrincipal(ctx, principal)
	}
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

// modernHeaders 合规的 modern 请求头
func modernHeaders(method, name string) map[string]string {
	headers := map[string]string{
		headerProtocolVersion: ProtocolVersion,
		headerMethod:          method,
	}
	if name != "" {
		headers[headerName] = name
	}
	return headers
}

// decodeRPC 解出 JSON-RPC 响应
func decodeRPC(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是 JSON: %v，body=%s", err, rec.Body.String())
	}
	return body
}

// rpcErrorCode 取 error.code
func rpcErrorCode(t *testing.T, body map[string]any) float64 {
	t.Helper()
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("响应里没有 error：%v", body)
	}
	code, ok := errObj["code"].(float64)
	if !ok {
		t.Fatalf("error.code 不是数字：%v", errObj)
	}
	return code
}

func TestMissingAuthReturns401(t *testing.T) {
	rec := doPost(t, newTestServer(), nil, modernBody("tools/list", ""), modernHeaders("tools/list", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未认证应以 401 拒绝，实际 %d", rec.Code)
	}
}

func TestModernRequiresProtocolHeaders(t *testing.T) {
	server := newTestServer()
	// body 里没有 _meta、头也没有版本
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	rec := doPost(t, server, testPrincipal(), body, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺协议版本应以 400 拒绝，实际 %d", rec.Code)
	}
	if code := rpcErrorCode(t, decodeRPC(t, rec)); code != codeHeaderMismatch {
		t.Fatalf("错误码应为 %d，实际 %v", codeHeaderMismatch, code)
	}
}

func TestModernVersionMismatch(t *testing.T) {
	server := newTestServer()
	headers := modernHeaders("tools/list", "")
	headers[headerProtocolVersion] = LegacyProtocolVersion // 头与 body 不一致
	rec := doPost(t, server, testPrincipal(), modernBody("tools/list", ""), headers)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("版本不一致应以 400 拒绝，实际 %d", rec.Code)
	}
	if code := rpcErrorCode(t, decodeRPC(t, rec)); code != codeHeaderMismatch {
		t.Fatalf("错误码应为 %d，实际 %v", codeHeaderMismatch, code)
	}
}

func TestModernMethodHeaderMismatch(t *testing.T) {
	server := newTestServer()
	headers := modernHeaders("tools/list", "")
	headers[headerMethod] = "tools/call"
	rec := doPost(t, server, testPrincipal(), modernBody("tools/list", ""), headers)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Mcp-Method 与 body 不一致应以 400 拒绝，实际 %d", rec.Code)
	}
}

func TestUnsupportedVersionListsSupported(t *testing.T) {
	server := newTestServer()
	const bogus = "1900-01-01"
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"` + bogus + `"}}}`
	headers := map[string]string{headerProtocolVersion: bogus, headerMethod: "tools/list"}

	rec := doPost(t, server, testPrincipal(), body, headers)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("不支持的版本应以 400 拒绝，实际 %d", rec.Code)
	}
	response := decodeRPC(t, rec)
	if code := rpcErrorCode(t, response); code != codeUnsupportedVersion {
		t.Fatalf("错误码应为 %d，实际 %v", codeUnsupportedVersion, code)
	}
	errObj := response["error"].(map[string]any)
	data, ok := errObj["data"].(map[string]any)
	if !ok {
		t.Fatalf("错误里应带 supported 列表：%v", errObj)
	}
	supported, _ := data["supported"].([]any)
	if len(supported) == 0 || supported[0] != ProtocolVersion {
		t.Fatalf("supported 应以 %s 开头，实际 %v", ProtocolVersion, supported)
	}
}

func TestUnknownMethodReturns404(t *testing.T) {
	server := newTestServer()
	rec := doPost(t, server, testPrincipal(), modernBody("foo/bar", ""), modernHeaders("foo/bar", ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未知方法应 404，实际 %d", rec.Code)
	}
	if code := rpcErrorCode(t, decodeRPC(t, rec)); code != codeMethodNotFound {
		t.Fatalf("错误码应为 %d，实际 %v", codeMethodNotFound, code)
	}
}

func TestDiscoverAdvertisesVersions(t *testing.T) {
	server := newTestServer()
	rec := doPost(t, server, testPrincipal(), modernBody("server/discover", ""), modernHeaders("server/discover", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("server/discover 应 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	result := decodeRPC(t, rec)["result"].(map[string]any)
	versions, _ := result["supportedVersions"].([]any)
	if len(versions) != len(SupportedVersions) {
		t.Fatalf("supportedVersions 应等于 %v，实际 %v", SupportedVersions, versions)
	}
	if result["resultType"] != resultTypeComplete {
		t.Fatalf("resultType 应为 %q，实际 %v", resultTypeComplete, result["resultType"])
	}
}

func TestToolsListRespectsReadPermission(t *testing.T) {
	server := newTestServer()
	principal := testPrincipal()
	principal.AllowRead = false

	rec := doPost(t, server, principal, modernBody("tools/list", ""), modernHeaders("tools/list", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/list 应 200，实际 %d", rec.Code)
	}
	result := decodeRPC(t, rec)["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	if len(tools) != 0 {
		t.Fatalf("没有读权限时不应暴露任何工具，实际 %d 个", len(tools))
	}

	rec = doPost(t, server, testPrincipal(), modernBody("tools/list", ""), modernHeaders("tools/list", ""))
	result = decodeRPC(t, rec)["result"].(map[string]any)
	tools, _ = result["tools"].([]any)
	if len(tools) == 0 {
		t.Fatalf("有读权限时应至少暴露一个工具")
	}
	first := tools[0].(map[string]any)
	if first["name"] != "list_files" {
		t.Fatalf("工具顺序应确定（第一个是 list_files），实际 %v", first["name"])
	}
	if _, ok := first["inputSchema"]; !ok {
		t.Fatalf("工具定义必须带 inputSchema：%v", first)
	}
}

func TestUnknownToolIsProtocolError(t *testing.T) {
	server := newTestServer()
	body := modernBody("tools/call", `"name":"nope","arguments":{}`)
	rec := doPost(t, server, testPrincipal(), body, modernHeaders("tools/call", "nope"))
	if rec.Code != http.StatusOK {
		t.Fatalf("未知工具应回 200 + JSON-RPC error，实际 %d", rec.Code)
	}
	if code := rpcErrorCode(t, decodeRPC(t, rec)); code != codeInvalidParams {
		t.Fatalf("错误码应为 %d，实际 %v", codeInvalidParams, code)
	}
}

func TestBadArgumentIsToolError(t *testing.T) {
	server := newTestServer()
	body := modernBody("tools/call", `"name":"list_files","arguments":{"limit":"abc"}`)
	rec := doPost(t, server, testPrincipal(), body, modernHeaders("tools/call", "list_files"))
	if rec.Code != http.StatusOK {
		t.Fatalf("参数错误属于工具执行错误，应 200，实际 %d", rec.Code)
	}
	result := decodeRPC(t, rec)["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("参数类型不对时应 isError=true，实际 %v", result)
	}
}

func TestLegacyInitializeAndSession(t *testing.T) {
	server := newTestServer()
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` +
		LegacyProtocolVersion + `","clientInfo":{"name":"legacy-client"},"capabilities":{}}}`
	rec := doPost(t, server, testPrincipal(), body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("initialize 应 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	sessionID := rec.Header().Get(headerSessionID)
	if sessionID == "" {
		t.Fatalf("initialize 响应必须带 %s", headerSessionID)
	}
	result := decodeRPC(t, rec)["result"].(map[string]any)
	if result["protocolVersion"] != LegacyProtocolVersion {
		t.Fatalf("应回显客户端请求的版本，实际 %v", result["protocolVersion"])
	}
	if _, ok := result["resultType"]; ok {
		t.Fatalf("legacy 结果不应带 resultType：%v", result)
	}

	// 带会话的 legacy 请求：不需要 modern 的协议头
	listBody := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	rec = doPost(t, server, testPrincipal(), listBody, map[string]string{headerSessionID: sessionID})
	if rec.Code != http.StatusOK {
		t.Fatalf("带会话的 tools/list 应 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}

	// 未知会话：按旧版规范回 404，让客户端重新握手
	rec = doPost(t, server, testPrincipal(), listBody, map[string]string{headerSessionID: "deadbeef"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未知会话应 404，实际 %d", rec.Code)
	}
}

func TestSameOriginPolicy(t *testing.T) {
	server := NewServer(Settings{
		AllowOrigins: []string{"https://ok.example"},
		Version:      "test",
	})
	headers := modernHeaders("tools/list", "")
	headers["Origin"] = "https://evil.example"
	rec := doPost(t, server, testPrincipal(), modernBody("tools/list", ""), headers)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("非法 Origin 应 403，实际 %d", rec.Code)
	}

	headers["Origin"] = "https://ok.example"
	rec = doPost(t, server, testPrincipal(), modernBody("tools/list", ""), headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("白名单 Origin 应放行，实际 %d", rec.Code)
	}
}

func TestTokenRateLimit(t *testing.T) {
	server := NewServer(Settings{TokenPerMin: 2, Version: "test"})
	headers := modernHeaders("tools/list", "")
	for i := 0; i < 2; i++ {
		if rec := doPost(t, server, testPrincipal(), modernBody("tools/list", ""), headers); rec.Code != http.StatusOK {
			t.Fatalf("第 %d 次调用应放行，实际 %d", i+1, rec.Code)
		}
	}
	rec := doPost(t, server, testPrincipal(), modernBody("tools/list", ""), headers)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("超过每令牌配额应 429，实际 %d", rec.Code)
	}
}

func TestWriteToolsNeedBothSwitches(t *testing.T) {
	// 配置让写（AllowWrite=true）但令牌不给写：写工具既不出现在列表里，也调不动
	server := NewServer(Settings{AllowWrite: true, TokenPerMin: 1000, Version: "test"})
	principal := testPrincipal()
	principal.AllowWrite = false

	rec := doPost(t, server, principal, modernBody("tools/list", ""), modernHeaders("tools/list", ""))
	result := decodeRPC(t, rec)["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	for _, raw := range tools {
		tool := raw.(map[string]any)
		name, _ := tool["name"].(string)
		if name == "create_folder" || name == "delete_file" {
			t.Fatalf("令牌没有写权限时不应暴露写工具 %s", name)
		}
	}

	body := modernBody("tools/call", `"name":"create_folder","arguments":{"path":"/x"}`)
	rec = doPost(t, server, principal, body, modernHeaders("tools/call", "create_folder"))
	if code := rpcErrorCode(t, decodeRPC(t, rec)); code != codeInvalidParams {
		t.Fatalf("无写权限时调用写工具应被拒（%d），实际 %v", codeInvalidParams, code)
	}

	// 两个开关都打开时，写工具必须出现
	principal.AllowWrite = true
	rec = doPost(t, server, principal, modernBody("tools/list", ""), modernHeaders("tools/list", ""))
	result = decodeRPC(t, rec)["result"].(map[string]any)
	tools, _ = result["tools"].([]any)
	found := false
	for _, raw := range tools {
		tool := raw.(map[string]any)
		if tool["name"] == "create_folder" {
			found = true
			annotations, _ := tool["annotations"].(map[string]any)
			if annotations["readOnlyHint"] != false {
				t.Fatalf("写工具的 readOnlyHint 必须是 false：%v", annotations)
			}
		}
	}
	if !found {
		t.Fatalf("两个开关都打开时应暴露 create_folder")
	}
}
