package plugin

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// IndexEntry 官方索引里的一个插件包（一个版本 + 一个平台）
type IndexEntry struct {
	// Name 插件包名（如 pfm-baidu），安装时用 --name 指定
	Name string `json:"name"`
	// DriverID 该插件提供的驱动身份，必须与包内 plugin.json 一致
	DriverID string `json:"driver_id"`
	// Version 版本号（语义化版本，字符串比较不参与决策，只展示与去重）
	Version string `json:"version"`
	// ABIVersion 插件协议版本
	ABIVersion uint32 `json:"abi_version"`
	// Platform GOOS-GOARCH（如 linux-amd64）
	Platform string `json:"platform"`
	// URL 包下载地址（tar.gz）
	URL string `json:"url"`
	// SHA256 包的 sha256（十六进制小写），安装前必须校验
	SHA256 string `json:"sha256"`
	// MinPfmVersion 要求的最低 pfm 版本；空表示不限制
	MinPfmVersion string `json:"min_pfm_version"`
	// ReleasedAt 发布时间（RFC3339，仅展示）
	ReleasedAt string `json:"released_at"`
}

// Index 官方索引（整个文件被签名，签名文件是 index.json.sig）
type Index struct {
	// Seq 单调递增序号：用来防回滚（拒绝比本地记录更旧的索引）
	Seq     uint64       `json:"seq"`
	Plugins []IndexEntry `json:"plugins"`
}

// Entry 按插件名 + 版本挑一条；version 为空时取该名字下最高版本
func (i *Index) Entry(name, version, platform string) (*IndexEntry, error) {
	var picked *IndexEntry
	for idx := range i.Plugins {
		entry := &i.Plugins[idx]
		if entry.Name != name || entry.Platform != platform {
			continue
		}
		if version != "" && entry.Version != version {
			continue
		}
		if picked == nil || compareVersion(entry.Version, picked.Version) > 0 {
			picked = entry
		}
	}
	if picked == nil {
		if version != "" {
			return nil, fmt.Errorf("索引里没有 %s 的 %s/%s 版本", name, version, platform)
		}
		return nil, fmt.Errorf("索引里没有可用于 %s 的 %s 包", platform, name)
	}
	return picked, nil
}

// indexVerifier 校验索引签名（签名覆盖 index.json 的原始字节）
type indexVerifier struct {
	keys []ed25519.PublicKey
}

// verify 任一把可信公钥验签通过即可
func (v indexVerifier) verify(payload, signature []byte) error {
	if len(v.keys) == 0 {
		return fmt.Errorf("没有配置索引签名公钥：请在配置 plugin.public_keys 里指定（或用官方版本内置的公钥）")
	}
	for _, key := range v.keys {
		if ed25519.Verify(key, payload, signature) {
			return nil
		}
	}
	return fmt.Errorf("索引签名校验失败：可能被篡改，或公钥不匹配")
}

// fetchIndex 下载并校验索引。
//
// 索引与签名分开取：签名文件叫 <index>.sig，内容是 base64 的 ed25519 签名。
func fetchIndex(ctx context.Context, client *http.Client, indexURL string,
	verifier indexVerifier) (*Index, []byte, error) {
	if indexURL == "" {
		return nil, nil, fmt.Errorf("未配置插件索引地址（plugin.index_url）")
	}
	payload, err := httpGet(ctx, client, indexURL, 4<<20)
	if err != nil {
		return nil, nil, fmt.Errorf("下载插件索引失败: %w", err)
	}
	signatureRaw, err := httpGet(ctx, client, indexURL+".sig", 4<<10)
	if err != nil {
		return nil, nil, fmt.Errorf("下载索引签名失败: %w", err)
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signatureRaw)))
	if err != nil {
		return nil, nil, fmt.Errorf("索引签名不是合法的 base64: %w", err)
	}
	if err := verifier.verify(payload, signature); err != nil {
		return nil, nil, err
	}
	index := &Index{}
	if err := unmarshalStrict(payload, index); err != nil {
		return nil, nil, fmt.Errorf("解析插件索引失败: %w", err)
	}
	if index.Seq == 0 {
		return nil, nil, fmt.Errorf("索引缺少 seq（防回滚序号）")
	}
	return index, payload, nil
}

// httpGet 取一个 URL 的响应体（限制大小，避免被灌爆内存）
func httpGet(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s 返回 HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// currentPlatform 当前平台标识（linux-amd64 / windows-arm64 …）
func currentPlatform() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

// compareVersion 比较 x.y.z 形式的版本号（无法解析的段按 0 处理）
func compareVersion(left, right string) int {
	parse := func(v string) []int {
		parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
		out := make([]int, 0, len(parts))
		for _, part := range parts {
			// 容忍 1.2.3-beta 这类后缀
			if idx := strings.IndexFunc(part, func(r rune) bool {
				return r < '0' || r > '9'
			}); idx >= 0 {
				part = part[:idx]
			}
			number, err := strconv.Atoi(part)
			if err != nil {
				number = 0
			}
			out = append(out, number)
		}
		return out
	}
	leftParts, rightParts := parse(left), parse(right)
	for i := 0; i < len(leftParts) || i < len(rightParts); i++ {
		var l, r int
		if i < len(leftParts) {
			l = leftParts[i]
		}
		if i < len(rightParts) {
			r = rightParts[i]
		}
		if l != r {
			if l < r {
				return -1
			}
			return 1
		}
	}
	return 0
}

// versionAtLeast 判断 have >= want（want 为空视为满足）
func versionAtLeast(have, want string) bool {
	if strings.TrimSpace(want) == "" {
		return true
	}
	return compareVersion(have, want) >= 0
}

// defaultHTTPClient 索引与包下载用的客户端（跟随重定向，带整体超时）
func defaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Minute}
}
