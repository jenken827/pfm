package types

import (
	"strings"
	"testing"
)

// TestComputeEncryptedChunkSize 验证加密开销预留
func TestComputeEncryptedChunkSize(t *testing.T) {
	limit := int64(1024 * 1024 * 1024)

	// 不加密：返回原值
	if got := ComputeEncryptedChunkSize(limit, false); got != limit {
		t.Errorf("non-encrypted: expected %d, got %d", limit, got)
	}

	// 加密：应小于原值（预留开销）
	if got := ComputeEncryptedChunkSize(limit, true); got >= limit {
		t.Errorf("encrypted: expected < %d, got %d", limit, got)
	}
}

// TestGenerateObfuscatedFilename 验证混淆文件名
func TestGenerateObfuscatedFilename(t *testing.T) {
	name, err := GenerateObfuscatedFilename("测试文件.txt", ".bin")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, ".bin") {
		t.Errorf("expected .bin suffix, got %s", name)
	}
	if strings.Contains(name, "测试文件") {
		t.Errorf("original name should not appear in obfuscated name: %s", name)
	}

	// 空后缀使用默认值
	name2, err := GenerateObfuscatedFilename("a.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name2, DefaultEncryptionSuffix) {
		t.Errorf("expected default suffix %s, got %s", DefaultEncryptionSuffix, name2)
	}
}
