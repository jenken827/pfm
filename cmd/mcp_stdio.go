package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jenken827/pfm/globals"
	"github.com/jenken827/pfm/internal/config"
	"github.com/jenken827/pfm/internal/mcp"
)

// mcpStdioCmd 把 stdio 上的 MCP 会话转发到本地 /mcp 端点。
//
// 只认 stdio 的客户端（多数桌面客户端）用这条命令就能接 pfm，不用装 npx 转发器：
//
//	stdio（一行一个 JSON-RPC）→ POST <url> → 打印响应行
//
// 走的是同一个鉴权与工具表，所以令牌 scope、审计、写开关与直接连 HTTP 完全一致。
var mcpStdioCmd = &cobra.Command{
	Use:   "stdio",
	Short: "把 stdio 上的 MCP 会话转发到本地 MCP 端点",
	Long: `从 stdin 读 JSON-RPC（一行一条），POST 到 pfm 的 MCP 端点，把响应写回 stdout。

客户端配置示例（把 command 指到 pfm 可执行文件）：
  {"mcpServers":{"pfm":{"command":"/path/to/pfm","args":["-d","<data>","mcp","stdio"],
    "env":{"PFM_MCP_TOKEN":"pfm_mcp_..."}}}}

令牌可以用 --token 或环境变量 PFM_MCP_TOKEN 给；也可以用登录 JWT（本机调试用）。
诊断信息一律写 stderr，stdout 只有 JSON-RPC 响应。`,
	RunE: runMcpStdio,
}

// stdioBridge 桥的运行时参数
type stdioBridge struct {
	// endpoint MCP 端点完整地址（含 /mcp）
	endpoint string
	// token 令牌明文（MCP 令牌或登录 JWT）
	token string
	// client HTTP 客户端（测试里可替换）
	client *http.Client
	// sessionID 旧版协议协商出来的会话 id（新版无状态协议不需要）
	sessionID string
}

// stdioMessage 只解析桥需要看的字段，其余原样转发
type stdioMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  struct {
		Name string         `json:"name"`
		Meta map[string]any `json:"_meta"`
	} `json:"params"`
}

// protocolVersionKey 新版协议在 params._meta 里带版本的键名
const protocolVersionKey = "io.modelcontextprotocol/protocolVersion"

func init() {
	mcpCmd.AddCommand(mcpStdioCmd)
	mcpStdioCmd.Flags().String("url", "", "MCP 端点地址（默认按配置里的 server_url / 监听端口推出来）")
	mcpStdioCmd.Flags().String("token", "", "MCP 令牌或登录 JWT（默认读环境变量 PFM_MCP_TOKEN）")
}

func runMcpStdio(cmd *cobra.Command, args []string) error {
	urlFlag, _ := cmd.Flags().GetString("url")
	tokenFlag, _ := cmd.Flags().GetString("token")

	token := strings.TrimSpace(tokenFlag)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("PFM_MCP_TOKEN"))
	}
	if token == "" {
		return fmt.Errorf("缺少令牌：用 --token 或环境变量 PFM_MCP_TOKEN 给一个 MCP 令牌（或登录 JWT）")
	}
	endpoint, err := resolveStdioEndpoint(urlFlag)
	if err != nil {
		return err
	}

	bridge := &stdioBridge{
		endpoint: endpoint,
		token:    token,
		client:   &http.Client{Timeout: 5 * time.Minute},
	}
	fmt.Fprintf(os.Stderr, "MCP stdio 桥转发到 %s（换目标用 --url）\n", endpoint)
	if err := bridge.run(cmd.Context(), os.Stdin, os.Stdout); err != nil {
		return err
	}
	// stdin 关闭 = 会话结束：旧版协议顺手把会话关掉，别让服务端等 TTL
	bridge.closeSession()
	return nil
}

// resolveStdioEndpoint 决定转发目标。
//
// 默认连**本机监听地址**：桥的调用方就在这台机器上，用 server_url（对外地址，
// 可能是域名 / 反代 / 未改的示例值）反而容易连到别处去。要连远端就显式给 --url；
// 监听端口没配（由前面某个反代接管）时才回落到 server_url。
func resolveStdioEndpoint(urlFlag string) (string, error) {
	if trimmed := strings.TrimSpace(urlFlag); trimmed != "" {
		return trimmed, nil
	}
	globals.NormalizePath()
	config.Init()
	path := mcp.EndpointPath()
	schema := config.GetConfig().Schema
	if schema.Port > 0 {
		addr := strings.TrimSpace(schema.Addr)
		if addr == "" || addr == "0.0.0.0" || addr == "::" {
			addr = "127.0.0.1"
		}
		return fmt.Sprintf("http://%s:%d%s", addr, schema.Port, path), nil
	}
	if base := strings.TrimRight(strings.TrimSpace(config.GetConfig().ServerURL), "/"); base != "" {
		return base + path, nil
	}
	return "", fmt.Errorf("推不出 MCP 端点：配置里既没有监听端口也没有 server_url，请用 --url 指定")
}

// run 逐行读 stdin、转发、写 stdout，直到输入结束
func (b *stdioBridge) run(ctx context.Context, in io.Reader, out io.Writer) error {
	reader := bufio.NewReaderSize(in, 64<<10)
	writer := bufio.NewWriter(out)
	defer func() { _ = writer.Flush() }()

	for {
		line, readErr := readLine(reader)
		if len(line) > 0 {
			if writeErr := b.relay(ctx, line, writer); writeErr != nil {
				fmt.Fprintf(os.Stderr, "转发失败: %v\n", writeErr)
			}
			if flushErr := writer.Flush(); flushErr != nil {
				return flushErr
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return nil
			}
			return readErr
		}
	}
}

// relay 转发一条消息；有响应就写一行，通知（无 id / 202）什么都不写
func (b *stdioBridge) relay(ctx context.Context, line []byte, out *bufio.Writer) error {
	var msg stdioMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		// 客户端发了非 JSON：按 JSON-RPC 解析错误回一条，别让会话卡住
		return writeSyntheticError(out, nil, -32700, fmt.Sprintf("无法解析的消息: %v", err))
	}
	resp, err := b.post(ctx, line, &msg)
	if err != nil {
		return writeSyntheticError(out, msg.ID, -32603, err.Error())
	}
	if len(resp) == 0 {
		return nil
	}
	if _, err := out.Write(append(resp, '\n')); err != nil {
		return err
	}
	return nil
}

// post 发一条消息，返回响应体（可能为空 = 通知）
func (b *stdioBridge) post(ctx context.Context, body []byte, msg *stdioMessage) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.token)
	if msg.Method != "" {
		req.Header.Set("Mcp-Method", msg.Method)
	}
	if msg.Params.Name != "" {
		req.Header.Set("Mcp-Name", msg.Params.Name)
	}
	if version := metaVersion(msg.Params.Meta); version != "" {
		// 新版无状态协议要求头与 body 里的版本一致；旧版没有 _meta，也就不带头
		req.Header.Set("MCP-Protocol-Version", version)
	}
	if b.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", b.sessionID)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连不上 MCP 端点 %s: %w", b.endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if id := resp.Header.Get("Mcp-Session-Id"); id != "" {
		b.sessionID = id
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("读响应失败: %w", err)
	}
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted {
		if resp.StatusCode == http.StatusAccepted {
			return nil, nil
		}
		return bytes.TrimSpace(raw), nil
	}
	// 非 2xx：服务端回的是 JSON-RPC 错误对象就原样给客户端，否则转成一条错误响应
	if len(raw) > 0 && json.Valid(raw) {
		return bytes.TrimSpace(raw), nil
	}
	return nil, fmt.Errorf("%s", describeHTTPFailure(resp.StatusCode, raw))
}

// closeSession 结束旧版会话（新版协议服务端不认会话，调用也只会得到 404，忽略即可）
func (b *stdioBridge) closeSession() {
	if b.sessionID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, b.endpoint, nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+b.token)
	req.Header.Set("Mcp-Session-Id", b.sessionID)
	if resp, err := b.client.Do(req); err == nil {
		_ = resp.Body.Close()
	}
}

// readLine 读一整行（去掉换行符）；MCP 消息可以很长，不用默认的 64KB 限制
func readLine(reader *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := reader.ReadBytes('\n')
		buf = append(buf, chunk...)
		if err == nil {
			return bytes.TrimRight(buf, "\r\n"), nil
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		return bytes.TrimRight(buf, "\r\n"), err
	}
}

// metaVersion 取新版协议在 _meta 里声明的版本
func metaVersion(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	version, _ := meta[protocolVersionKey].(string)
	return strings.TrimSpace(version)
}

// writeSyntheticError 桥自己产生的错误：按 JSON-RPC 形状回给客户端
func writeSyntheticError(out *bufio.Writer, id json.RawMessage, code int, message string) error {
	if len(id) == 0 || string(id) == "null" {
		// 通知没有 id，按协议不回复
		return nil
	}
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"error":   map[string]any{"code": code, "message": message},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = out.Write(append(body, '\n'))
	return err
}

// describeHTTPFailure 非 2xx 且响应不是 JSON 时的错误文案
func describeHTTPFailure(status int, raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return fmt.Sprintf("MCP 端点返回 HTTP %d", status)
	}
	return fmt.Sprintf("MCP 端点返回 HTTP %d: %s", status, truncateRunes(text, 300))
}
