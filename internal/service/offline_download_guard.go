package service

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/jenken827/pfm/internal/config"
)

// allowPrivateHostFunc 读取 download.allow_private_host（测试可替换；
// 配置未初始化时按"不放行"处理，安全默认）
var allowPrivateHostFunc = func() bool {
	cfg := config.GetConfig()
	return cfg != nil && cfg.Download.AllowPrivateHost
}

// lookupIPFunc 解析主机名（测试可替换，避免依赖外网 DNS）
var lookupIPFunc = func(host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(context.Background(), "ip", host)
}

// isCGNAT 运营商级 NAT 网段 100.64.0.0/10（云厂商内网常见，IsPrivate 不含它）
func isCGNAT(ip net.IP) bool {
	ip4 := ip.To4()
	return ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127
}

// offlineDownloadBlockedReason 返回目标 IP 被拒的原因；空串表示允许
func offlineDownloadBlockedReason(ip net.IP) string {
	switch {
	case ip == nil:
		return "无法解析目标地址"
	case ip.IsLoopback():
		return "回环地址"
	case ip.IsUnspecified():
		return "未指定地址"
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast():
		return "链路本地地址"
	case ip.IsMulticast():
		return "多播地址"
	case ip.IsPrivate():
		return "内网地址"
	case isCGNAT(ip):
		return "运营商级 NAT 地址"
	}
	return ""
}

// validateOfflineDownloadURL 校验离线下载目标地址（SSRF 防护）
//
// 离线下载是"服务端替用户发请求"，不校验就能让任何登录用户读取
// 127.0.0.1 上的服务、局域网设备、云厂商元数据（169.254.169.254）。
// 只拦 aria2 会主动连接的目标协议（http/https/ftp）；magnet/ed2k 走 P2P，不解析主机。
//
// 已知局限：这里是"提交时解析一次"，aria2 之后会自行再解析；DNS 记录在两步之间
// 变化（DNS rebinding）仍可能绕过。要彻底防住得在网络层限制出向流量。
func validateOfflineDownloadURL(u *url.URL) error {
	switch u.Scheme {
	case "http", "https", "ftp":
	default:
		return nil
	}
	if allowPrivateHostFunc() {
		return nil
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return fmt.Errorf("下载 URL 缺少主机名")
	}

	// 字面量 IP：直接判，无需 DNS
	if ip := net.ParseIP(host); ip != nil {
		if reason := offlineDownloadBlockedReason(ip); reason != "" {
			return blockedTargetErr(host, reason, "")
		}
		return nil
	}

	ips, err := lookupIPFunc(host)
	if err != nil {
		return fmt.Errorf("无法解析下载目标主机 %s: %v", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("无法解析下载目标主机 %s", host)
	}
	for _, ip := range ips {
		if reason := offlineDownloadBlockedReason(ip); reason != "" {
			return blockedTargetErr(host, reason, ip.String())
		}
	}
	return nil
}

// blockedTargetErr 统一的拒绝文案（带上怎么放行的提示）
func blockedTargetErr(host, reason, ip string) error {
	target := host
	if ip != "" {
		target = fmt.Sprintf("%s（解析到 %s）", host, ip)
	}
	return fmt.Errorf("不允许离线下载 %s：目标是%s；确需下载内网文件请开启配置 download.allow_private_host", target, reason)
}
