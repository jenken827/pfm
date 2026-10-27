package plugin

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strings"
)

// officialIndexKeys 官方插件索引的签名公钥（base64 的 raw ed25519 公钥）。
//
// ⚠️ 这里是信任根：只有用这些私钥签出来的索引才会被 install 接受。
// 官方索引仓库（pfm-plugins-index）发布后在此填入公钥；在填入之前，
// 只能通过配置 plugin.public_keys 自建索引（自签名自用）。
var officialIndexKeys = []string{
	// 官方索引签名公钥（私钥由维护者离线保管，仅用于发布 index.json）
	"BTInp/Jn7pFT6CYcz/a+7cPq+cqdmWRtbolRhQxiIIM=",
}

// parsePublicKeys 解析配置里的公钥列表（base64 raw 32 字节）
func parsePublicKeys(encoded []string) ([]ed25519.PublicKey, error) {
	keys := make([]ed25519.PublicKey, 0, len(encoded))
	for _, item := range encoded {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(trimmed)
		if err != nil {
			return nil, fmt.Errorf("公钥不是合法的 base64: %w", err)
		}
		if len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("公钥长度应为 %d 字节，实际 %d", ed25519.PublicKeySize, len(raw))
		}
		keys = append(keys, ed25519.PublicKey(raw))
	}
	return keys, nil
}

// ResolveTrustKeys 合并可信公钥：配置里的（优先，便于自建索引）覆盖内置的官方公钥
func ResolveTrustKeys(configured []string) ([]ed25519.PublicKey, error) {
	source := configured
	if len(source) == 0 {
		source = officialIndexKeys
	}
	return parsePublicKeys(source)
}
