package cmd

import (
	"strings"
	"testing"

	"jenken827.com/jenken827/pfm/proto/gen/native"
)

func TestParseContentRangeTotal(t *testing.T) {
	cases := []struct {
		name         string
		contentRange string
		want         int64
	}{
		{"标准格式", "bytes 0-0/12345", 12345},
		{"中间偏移", "bytes 1024-1025/987654321", 987654321},
		{"未知总长", "bytes 0-0/*", 0},
		{"空串", "", 0},
		{"无斜杠", "bytes 0-0", 0},
		{"斜杠结尾", "bytes 0-0/", 0},
		{"非数字", "bytes 0-0/abc", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseContentRangeTotal(c.contentRange); got != c.want {
				t.Fatalf("parseContentRangeTotal(%q) = %d, want %d", c.contentRange, got, c.want)
			}
		})
	}
}

// TestDirectLinkEligibility 锁定直链硬性口径：必须单分片且未加密
func TestDirectLinkEligibility(t *testing.T) {
	cases := []struct {
		name      string
		chunks    int
		encrypted bool
		wantOK    bool
	}{
		{"单分片明文", 1, false, true},
		{"单分片加密", 1, true, false},
		{"多分片明文", 3, false, false},
		{"多分片加密", 3, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ok, reason := directLinkEligibility(c.chunks, c.encrypted)
			if ok != c.wantOK {
				t.Fatalf("directLinkEligibility(%d, %v) = %v (%s), want %v",
					c.chunks, c.encrypted, ok, reason, c.wantOK)
			}
			if !ok && reason == "" {
				t.Fatal("不合格时必须给出原因")
			}
		})
	}
}

func TestHumanSize(t *testing.T) {
	cases := []struct {
		size int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{2048, "2.00 KB"},
		{5 * 1024 * 1024, "5.00 MB"},
		{3 * 1024 * 1024 * 1024, "3.00 GB"},
	}
	for _, c := range cases {
		if got := humanSize(c.size); got != c.want {
			t.Fatalf("humanSize(%d) = %q, want %q", c.size, got, c.want)
		}
	}
}

func TestShortHash(t *testing.T) {
	if got := shortHash("abc123"); got != "abc123" {
		t.Fatalf("短 hash 不应被截断，got %q", got)
	}
	if got := shortHash("0123456789abcdef0123"); got != "0123456789ab..." {
		t.Fatalf("长 hash 应截断为前 12 位，got %q", got)
	}
}

// loc 构造一条位置行（nil 指针字段按"未设置"处理）
func loc(providerID uint32, encrypted bool) *native.FileLocationORM {
	return &native.FileLocationORM{ProviderId: &providerID, Encrypted: &encrypted}
}

// TestDirectLinkLocationVerdict 列表模式的单提供者结论
func TestDirectLinkLocationVerdict(t *testing.T) {
	cases := []struct {
		name     string
		locs     []*native.FileLocationORM
		typeName string
		state    directLinkDriverState
		wantOK   bool
	}{
		{"单副本明文且驱动就绪", []*native.FileLocationORM{loc(3, false)}, "YUN139", directLinkDriverReady, true},
		{"驱动不支持（telegram/local）", []*native.FileLocationORM{loc(2, false)}, "TELEGRAM", directLinkDriverNoCapability, false},
		{"提供者未启用", []*native.FileLocationORM{loc(4, false)}, "PAN123", directLinkDriverMissing, false},
		{"多分片", []*native.FileLocationORM{loc(2, false), loc(2, false)}, "TELEGRAM", directLinkDriverReady, false},
		{"已加密", []*native.FileLocationORM{loc(4, true)}, "ALIYUNDRIVE_OPEN", directLinkDriverReady, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			verdict, ok := directLinkLocationVerdict(c.locs, c.typeName, c.state)
			if ok != c.wantOK {
				t.Fatalf("verdict=%q ok=%v, want ok=%v", verdict, ok, c.wantOK)
			}
			if !strings.Contains(verdict, c.typeName) {
				t.Fatalf("结论里应带提供者类型，got %q", verdict)
			}
		})
	}
}

// TestDirectLinkDriverStateUninitialized 未初始化存储时判为"拿不到实例"而不是"驱动不支持"
func TestDirectLinkDriverStateUninitialized(t *testing.T) {
	if got := directLinkDriverStateOf(99999); got != directLinkDriverMissing {
		t.Fatalf("未启用的 provider 应判为 directLinkDriverMissing，got %v", got)
	}
}
