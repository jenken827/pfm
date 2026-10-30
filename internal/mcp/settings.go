package mcp

import (
	"strings"
	"time"

	"github.com/jenken827/pfm/globals"
	"github.com/jenken827/pfm/internal/auth"
	"github.com/jenken827/pfm/internal/config"
	"github.com/jenken827/pfm/internal/models"
)

// Settings MCP 服务的运行时参数。
//
// 配置里的缺省与越界（<=0、过大）在这里统一收敛一次，运行期只认这份值，
// 避免每个工具各写一遍「如果没配就用默认」。
type Settings struct {
	// AllowWrite 是否允许写工具（还要令牌的 allow_write 也打开）
	AllowWrite bool
	// AllowOrigins 允许的浏览器 Origin（防 DNS rebinding）；空 = 不校验
	AllowOrigins []string
	// MaxReadBytes 单次 read_text 返回上限（字节）
	MaxReadBytes int
	// CallTimeout 单次工具调用超时
	CallTimeout time.Duration
	// TokenPerMin 每个令牌每分钟的调用上限
	TokenPerMin int
	// TicketTTL 发给工具插件读取文件用的单文件票据有效期
	TicketTTL time.Duration
	// ServerURL 对外地址（拼下载链接用，末尾无斜杠）
	ServerURL string
	// Version 宿主版本（serverInfo 用）
	Version string
}

// maxReadBytesLimit read_text 上限的硬顶（再大就不是"读文本"而是搬文件了）
const maxReadBytesLimit = 8 << 20

// LoadSettings 从全局配置构造运行时参数
func LoadSettings() Settings {
	cfg := config.GetConfig()
	mc := cfg.Mcp

	maxRead := mc.MaxReadBytes
	if maxRead <= 0 {
		maxRead = models.DefaultMcpMaxReadBytes
	}
	if maxRead > maxReadBytesLimit {
		maxRead = maxReadBytesLimit
	}

	timeoutSeconds := mc.CallTimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = models.DefaultMcpCallTimeoutSeconds
	}

	perMinute := mc.TokenRatePerMinute
	if perMinute <= 0 {
		perMinute = models.DefaultMcpTokenRatePerMinute
	}

	ticketMinutes := mc.TicketTTLMinutes
	if ticketMinutes <= 0 {
		ticketMinutes = models.DefaultMcpTicketTTLMinutes
	}

	return Settings{
		AllowWrite:   mc.AllowWrite,
		AllowOrigins: mc.AllowOrigins,
		MaxReadBytes: maxRead,
		CallTimeout:  time.Duration(timeoutSeconds) * time.Second,
		TokenPerMin:  perMinute,
		TicketTTL:    time.Duration(ticketMinutes) * time.Minute,
		ServerURL:    strings.TrimRight(strings.TrimSpace(cfg.ServerURL), "/"),
		Version:      globals.Version,
	}
}

// EndpointPath 端点路径（缺省 /mcp，统一补前导斜杠、去尾斜杠）
func EndpointPath() string {
	path := strings.TrimSpace(config.GetConfig().Mcp.Path)
	if path == "" {
		return models.DefaultMcpPath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	path = strings.TrimRight(path, "/")
	if path == "" {
		return models.DefaultMcpPath
	}
	return path
}

// EffectiveWrite 有效写权限 = 宿主配置开关 且 令牌开关
func (s Settings) EffectiveWrite(principal *auth.McpPrincipal) bool {
	return s.AllowWrite && principal != nil && principal.AllowWrite
}
