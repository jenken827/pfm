package plugin

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/jenken827/pfm-plugin-sdk/proto/gen/plugin"
)

// client 宿主侧的插件 RPC 客户端（HTTP + protojson，Open 走裸字节流）
type client struct {
	base  string // http://127.0.0.1:port
	token string
	http  *http.Client
}

func newClient(addr, token string) *client {
	return &client{
		base:  "http://" + addr,
		token: token,
		http: &http.Client{
			// 不设总超时：流式读取（Open）可能持续很久；
			// 单次一元的等待由调用方 context 控制。
			Transport: &http.Transport{MaxIdleConnsPerHost: 4},
		},
	}
}

// call 调用一元方法：请求/响应都是 protojson
func (c *client) call(ctx context.Context, method string, req, resp proto.Message) error {
	body, err := protojson.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal %s request failed: %w", method, err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/rpc/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("调用 %s 失败: %w", method, err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(httpResp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("读取 %s 响应失败: %w", method, err)
	}
	if httpResp.StatusCode != http.StatusOK {
		return toRPCError(method, httpResp.StatusCode, raw)
	}
	if resp == nil {
		return nil
	}
	if err := protojson.Unmarshal(raw, resp); err != nil {
		return fmt.Errorf("解析 %s 响应失败: %w", method, err)
	}
	return nil
}

// open 请求一个字节流；调用方负责 Close
func (c *client) open(ctx context.Context, req *plugin.OpenReq) (io.ReadCloser, error) {
	body, err := protojson.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/rpc/Open", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("打开读取流失败: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		defer func() { _ = httpResp.Body.Close() }()
		raw, _ := io.ReadAll(io.LimitReader(httpResp.Body, 1<<20))
		return nil, toRPCError("Open", httpResp.StatusCode, raw)
	}
	return httpResp.Body, nil
}

// toRPCError 把插件返回的错误体转成带码的错误
func toRPCError(method string, status int, raw []byte) error {
	var rpcErr plugin.Error
	if err := protojson.Unmarshal(raw, &rpcErr); err == nil && rpcErr.GetMessage() != "" {
		return fmt.Errorf("%s: %s: %s", method, rpcErr.GetCode(), rpcErr.GetMessage())
	}
	return fmt.Errorf("%s: 插件返回 HTTP %d", method, status)
}

// newOpenReq 组装 Open 请求（offset/limit 由调用方给）
func newOpenReq(providerID uint32, refs []*plugin.FileRef, offset, limit int64,
	encryption *plugin.Encryption) *plugin.OpenReq {
	return &plugin.OpenReq{
		ProviderId: proto.Uint32(providerID),
		Refs:       refs,
		Offset:     proto.Int64(offset),
		Limit:      proto.Int64(limit),
		Encryption: encryption,
	}
}
