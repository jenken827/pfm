package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimitOptions 每 IP 限频的三组令牌桶参数。
//
// 单次突发（Burst）= 桶容量，持续速率 = 每 Refill 补一个 token。任一字段 <= 0
// 时回落到 DefaultRateLimitOptions，避免配置写错直接把服务锁死。
type RateLimitOptions struct {
	EndpointBurst   int
	EndpointRefill  time.Duration
	GlobalBurst     int
	GlobalRefill    time.Duration
	ThumbnailBurst  int
	ThumbnailRefill time.Duration
}

const (
	// 同 IP + 同一路径：突发 120，持续 100 rps。
	// 阅读器（pdf.js / Flyfish）对一个文件会连续发 Range 请求，10 rps 的老额度根本不够。
	defaultEndpointBurst  = 120
	defaultEndpointRefill = 10 * time.Millisecond

	// 同 IP 全部请求（缩略图除外）：突发 600，持续 100 rps。
	defaultGlobalBurst  = 600
	defaultGlobalRefill = 10 * time.Millisecond

	// 缩略图单独计量：突发 300，持续 200 rps。
	// 一个目录几百张图会在 1-2 秒内全部请求，属于正常用法，不该跟文档流抢额度。
	defaultThumbnailBurst  = 300
	defaultThumbnailRefill = 5 * time.Millisecond
)

// DefaultRateLimitOptions 内置默认限频参数
func DefaultRateLimitOptions() RateLimitOptions {
	return RateLimitOptions{
		EndpointBurst:   defaultEndpointBurst,
		EndpointRefill:  defaultEndpointRefill,
		GlobalBurst:     defaultGlobalBurst,
		GlobalRefill:    defaultGlobalRefill,
		ThumbnailBurst:  defaultThumbnailBurst,
		ThumbnailRefill: defaultThumbnailRefill,
	}
}

// normalized 把未配置（<=0）的字段补成默认值
func (o RateLimitOptions) normalized() RateLimitOptions {
	d := DefaultRateLimitOptions()
	if o.EndpointBurst <= 0 {
		o.EndpointBurst = d.EndpointBurst
	}
	if o.EndpointRefill <= 0 {
		o.EndpointRefill = d.EndpointRefill
	}
	if o.GlobalBurst <= 0 {
		o.GlobalBurst = d.GlobalBurst
	}
	if o.GlobalRefill <= 0 {
		o.GlobalRefill = d.GlobalRefill
	}
	if o.ThumbnailBurst <= 0 {
		o.ThumbnailBurst = d.ThumbnailBurst
	}
	if o.ThumbnailRefill <= 0 {
		o.ThumbnailRefill = d.ThumbnailRefill
	}
	return o
}

// TokenBucket implements a token bucket rate limiter
type TokenBucket struct {
	tokens     int
	maxTokens  int
	refillRate time.Duration
	lastRefill time.Time
	mu         sync.Mutex
}

// NewTokenBucket creates a new token bucket rate limiter
// maxTokens: maximum number of tokens in the bucket
// refillRate: time duration to add one token back to the bucket
func NewTokenBucket(maxTokens int, refillRate time.Duration) *TokenBucket {
	if maxTokens <= 0 {
		maxTokens = 1
	}
	if refillRate <= 0 {
		refillRate = time.Millisecond
	}
	return &TokenBucket{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

// Allow checks if a request is allowed and consumes one token if available
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	if tb.tokens >= tb.maxTokens {
		// 满桶：把计时基准推到当前，避免攒出很长的时间差后一次性回补
		tb.lastRefill = time.Now()
	} else {
		elapsed := time.Since(tb.lastRefill)
		if tokensToAdd := int(elapsed / tb.refillRate); tokensToAdd > 0 {
			tb.tokens = min(tb.tokens+tokensToAdd, tb.maxTokens)
			// 只前进"已发放 token"对应的时长，余量留到下次，保证持续速率准确
			tb.lastRefill = tb.lastRefill.Add(time.Duration(tokensToAdd) * tb.refillRate)
		}
	}

	if tb.tokens > 0 {
		tb.tokens--
		return true
	}
	return false
}

// EndpointRateLimiter tracks rate limits per endpoint
type EndpointRateLimiter struct {
	buckets map[string]*TokenBucket
	burst   int
	refill  time.Duration
	mu      sync.RWMutex
}

// NewEndpointRateLimiter creates a new endpoint-specific rate limiter
func NewEndpointRateLimiter(burst int, refill time.Duration) *EndpointRateLimiter {
	opts := RateLimitOptions{EndpointBurst: burst, EndpointRefill: refill}.normalized()
	return &EndpointRateLimiter{
		buckets: make(map[string]*TokenBucket),
		burst:   opts.EndpointBurst,
		refill:  opts.EndpointRefill,
	}
}

// Allow checks if access to the endpoint is allowed within rate limits
func (erl *EndpointRateLimiter) Allow(endpoint string) bool {
	erl.mu.Lock()
	bucket, exists := erl.buckets[endpoint]
	if !exists {
		bucket = NewTokenBucket(erl.burst, erl.refill)
		erl.buckets[endpoint] = bucket
	}
	erl.mu.Unlock()

	return bucket.Allow()
}

// ipEntry holds per-IP rate limiter state with last access time
type ipEntry struct {
	endpointLimiter *EndpointRateLimiter
	globalBucket    *TokenBucket
	thumbnailBucket *TokenBucket
	lastAccess      time.Time
}

// IPRateLimiter manages rate limiting per IP address
type IPRateLimiter struct {
	entries map[string]*ipEntry // per IP
	opts    RateLimitOptions
	mu      sync.RWMutex
	ttl     time.Duration
}

// NewIPRateLimiter creates a rate limiter with the built-in defaults
func NewIPRateLimiter() *IPRateLimiter {
	return NewIPRateLimiterWithOptions(DefaultRateLimitOptions())
}

// NewIPRateLimiterWithOptions creates a rate limiter with explicit parameters
func NewIPRateLimiterWithOptions(opts RateLimitOptions) *IPRateLimiter {
	opts = opts.normalized()
	irl := &IPRateLimiter{
		entries: make(map[string]*ipEntry),
		opts:    opts,
		ttl:     10 * time.Minute,
	}
	go irl.cleanupLoop()
	return irl
}

// cleanupLoop periodically removes stale IP entries
func (irl *IPRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		irl.mu.Lock()
		now := time.Now()
		for ip, entry := range irl.entries {
			if now.Sub(entry.lastAccess) > irl.ttl {
				delete(irl.entries, ip)
			}
		}
		irl.mu.Unlock()
	}
}

// Allow checks if a request from an IP to an endpoint is allowed.
//
// thumbnail 为 true 时只吃缩略图配额：缩略图请求量大但每次只是一次磁盘读，
// 不能让它把文档预览 / 下载的额度（endpoint + global）挤掉。
func (irl *IPRateLimiter) Allow(ip string, endpoint string, thumbnail bool) bool {
	irl.mu.Lock()
	entry, exists := irl.entries[ip]
	if !exists {
		entry = &ipEntry{
			endpointLimiter: NewEndpointRateLimiter(irl.opts.EndpointBurst, irl.opts.EndpointRefill),
			globalBucket:    NewTokenBucket(irl.opts.GlobalBurst, irl.opts.GlobalRefill),
			thumbnailBucket: NewTokenBucket(irl.opts.ThumbnailBurst, irl.opts.ThumbnailRefill),
			lastAccess:      time.Now(),
		}
		irl.entries[ip] = entry
	}
	entry.lastAccess = time.Now()
	irl.mu.Unlock()

	if thumbnail {
		return entry.thumbnailBucket.Allow()
	}

	// 先查路径维度（同一路径持续 10 rps 之类的刷法），再查全局维度
	if !entry.endpointLimiter.Allow(endpoint) {
		return false
	}
	return entry.globalBucket.Allow()
}

// Global rate limiter instance（server.Run 启动时可用配置覆盖）
var rateLimiter = NewIPRateLimiter()

// InitRateLimit 用配置覆盖内置默认参数（启动时调用一次；未配置的字段保持默认）
func InitRateLimit(opts RateLimitOptions) {
	rateLimiter = NewIPRateLimiterWithOptions(opts)
}

// isThumbnailRequest 判定缩略图请求：/api/file/stream/preview 且不要求原图。
//
// 这类请求一个列表页会并发几十上百个（size=small/large 的 JPEG，2-160KB），
// 用独立配额计量，避免把同一路径下的原图预览 / Range 流饿死。
func isThumbnailRequest(r *http.Request) bool {
	if r.URL.Path != "/api/file/stream/preview" {
		return false
	}
	return !strings.EqualFold(r.URL.Query().Get("original"), "true")
}

// RateLimitHTTPMiddleware creates an HTTP middleware that applies rate limiting
// This should be used with Vanguard transcoder to catch all requests before they reach Connect handlers
func RateLimitHTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract client IP
		ip := extractClientIPFromRequest(r)

		// Extract endpoint/procedure from URL path
		endpoint := r.URL.Path

		// Check rate limit
		if !rateLimiter.Allow(ip, endpoint, isThumbnailRequest(r)) {
			// 告诉客户端多久以后再来，免得它立刻重试把桶一直压空
			w.Header().Set("Retry-After", "1")
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		// Continue to next handler
		next.ServeHTTP(w, r)
	})
}

// extractClientIPFromRequest extracts client IP from HTTP request
func extractClientIPFromRequest(r *http.Request) string {
	// Try X-Forwarded-For first
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Try X-Real-IP
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return strings.TrimSpace(xri)
	}

	// Try True-Client-IP
	tci := r.Header.Get("True-Client-IP")
	if tci != "" {
		return strings.TrimSpace(tci)
	}

	// Extract from RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
