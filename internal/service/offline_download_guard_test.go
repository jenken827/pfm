package service

import (
	"context"
	"net"
	"net/url"
	"strings"
	"testing"

	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

func TestOfflineDownloadBlockedReason(t *testing.T) {
	cases := []struct {
		ip         string
		wantReason bool
	}{
		{"127.0.0.1", true},       // 回环
		{"::1", true},             // IPv6 回环
		{"10.1.2.3", true},        // 私网 A
		{"172.16.5.4", true},      // 私网 B
		{"192.168.1.10", true},    // 私网 C
		{"169.254.169.254", true}, // 云元数据（链路本地）
		{"fe80::1", true},         // IPv6 链路本地
		{"fd00::1", true},         // IPv6 ULA
		{"0.0.0.0", true},         // 未指定
		{"100.64.0.1", true},      // 运营商级 NAT
		{"8.8.8.8", false},        // 公网
		{"1.1.1.1", false},        // 公网
		{"2606:4700:4700::1111", false},
	}
	for _, c := range cases {
		reason := offlineDownloadBlockedReason(net.ParseIP(c.ip))
		if got := reason != ""; got != c.wantReason {
			t.Errorf("offlineDownloadBlockedReason(%s) = %q, want blocked=%v", c.ip, reason, c.wantReason)
		}
	}
}

func TestValidateOfflineDownloadURL_LiteralIP(t *testing.T) {
	blocked := []string{
		"http://127.0.0.1:8080/a.iso",
		"http://localhost:10999/api/file/list",
		"https://192.168.1.1/admin",
		"http://169.254.169.254/latest/meta-data/",
		"ftp://10.0.0.2/pub/file.iso",
		"http://[::1]:8080/",
	}
	for _, raw := range blocked {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %s: %v", raw, err)
		}
		if err := validateOfflineDownloadURL(u); err == nil {
			t.Errorf("validateOfflineDownloadURL(%s) = nil, want 拒绝", raw)
		} else if !strings.Contains(err.Error(), "allow_private_host") {
			t.Errorf("validateOfflineDownloadURL(%s) 错误信息缺少放行提示: %v", raw, err)
		}
	}

	allowed := []string{
		"https://8.8.8.8/file.bin",
		"http://1.1.1.1:8080/file.bin",
	}
	for _, raw := range allowed {
		u, _ := url.Parse(raw)
		if err := validateOfflineDownloadURL(u); err != nil {
			t.Errorf("validateOfflineDownloadURL(%s) = %v, want nil", raw, err)
		}
	}
}

// 主机名要解析后再判：解析到私网同样拒绝
func TestValidateOfflineDownloadURL_ResolvedHost(t *testing.T) {
	orig := lookupIPFunc
	t.Cleanup(func() { lookupIPFunc = orig })

	lookupIPFunc = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("192.168.3.4")}, nil
	}
	u, _ := url.Parse("https://files.intranet.example/a.iso")
	if err := validateOfflineDownloadURL(u); err == nil {
		t.Fatal("解析到私网地址的主机名未被拒绝")
	}

	lookupIPFunc = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	if err := validateOfflineDownloadURL(u); err != nil {
		t.Errorf("公网主机名被误拒: %v", err)
	}
}

// magnet 不走 HTTP 拉取，不做主机校验（P2P 连接不在这一层控制）
func TestValidateOfflineDownloadURL_SkipsP2P(t *testing.T) {
	u, err := url.Parse("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567")
	if err != nil {
		t.Fatalf("parse magnet: %v", err)
	}
	if err := validateOfflineDownloadURL(u); err != nil {
		t.Errorf("validateOfflineDownloadURL(magnet) = %v, want nil", err)
	}
}

// 配置开关打开时允许内网目标（自建 / 纯内网部署）
func TestValidateOfflineDownloadURL_AllowPrivateHost(t *testing.T) {
	prev := allowPrivateHostFunc
	allowPrivateHostFunc = func() bool { return true }
	t.Cleanup(func() { allowPrivateHostFunc = prev })

	u, _ := url.Parse("http://127.0.0.1:8080/a.iso")
	if err := validateOfflineDownloadURL(u); err != nil {
		t.Errorf("allow_private_host 开启后仍被拒绝: %v", err)
	}
}

// 校验确实接在创建入口上（不只是有函数没人调）
func TestCreateOfflineDownload_RejectsBlockedTarget(t *testing.T) {
	req := &native.CreateOfflineDownloadReq{
		Url: utils.StringPtr("http://169.254.169.254/latest/meta-data/"),
	}
	_, err := CreateOfflineDownload(context.Background(), req)
	if err == nil {
		t.Fatal("内网目标未被拒绝")
	}
	if err.C != native.RespCode_PARAMS_ERROR {
		t.Errorf("错误码 = %v, want PARAMS_ERROR", err.C)
	}
	if !strings.Contains(err.E.Error(), "allow_private_host") {
		t.Errorf("错误信息缺少放行提示: %v", err.E)
	}
}

func TestCreateOfflineDownload_RejectsEd2k(t *testing.T) {
	req := &native.CreateOfflineDownloadReq{
		Url: utils.StringPtr("ed2k://|file|a.iso|1024|ABCDEF|/"),
	}
	_, err := CreateOfflineDownload(context.Background(), req)
	if err == nil {
		t.Fatal("ed2k 未被拒绝")
	}
	if !strings.Contains(err.E.Error(), "ed2k") {
		t.Errorf("错误信息应明确提到 ed2k: %v", err.E)
	}
}
