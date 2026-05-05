package middleware

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/server/response"
)

// RateLimiter defines the interface for rate limiting
type RateLimiter interface {
	Allow(ip string, endpoint string) bool
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

	now := time.Now()
	elapsed := now.Sub(tb.lastRefill)
	tokensToAdd := int(elapsed / tb.refillRate)

	if tokensToAdd > 0 {
		tb.tokens = min(tb.tokens+tokensToAdd, tb.maxTokens)
		tb.lastRefill = now
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
	mu      sync.RWMutex
}

// NewEndpointRateLimiter creates a new endpoint-specific rate limiter
func NewEndpointRateLimiter() *EndpointRateLimiter {
	return &EndpointRateLimiter{
		buckets: make(map[string]*TokenBucket),
	}
}

// Allow checks if access to the endpoint is allowed within rate limits
func (erl *EndpointRateLimiter) Allow(endpoint string) bool {
	erl.mu.Lock()
	defer erl.mu.Unlock()

	bucket, exists := erl.buckets[endpoint]
	if !exists {
		// Create new bucket: 3 tokens, refills 1 token per 333ms (allows 3 requests per second)
		bucket = NewTokenBucket(3, 333*time.Millisecond)
		erl.buckets[endpoint] = bucket
	}

	return bucket.Allow()
}

// ipEntry holds per-IP rate limiter state with last access time
type ipEntry struct {
	endpointLimiter *EndpointRateLimiter
	globalBucket    *TokenBucket
	lastAccess      time.Time
}

// IPRateLimiter manages rate limiting per IP address
type IPRateLimiter struct {
	entries map[string]*ipEntry // per IP
	mu      sync.RWMutex
	ttl     time.Duration
}

// NewIPRateLimiter creates a new IP-based rate limiter
func NewIPRateLimiter() *IPRateLimiter {
	irl := &IPRateLimiter{
		entries: make(map[string]*ipEntry),
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

// Allow checks if a request from an IP to an endpoint is allowed
// Returns false if either endpoint-specific or global rate limit is exceeded
func (irl *IPRateLimiter) Allow(ip string, endpoint string) bool {
	irl.mu.Lock()

	entry, exists := irl.entries[ip]
	if !exists {
		entry = &ipEntry{
			endpointLimiter: NewEndpointRateLimiter(),
			globalBucket:    NewTokenBucket(100, 600*time.Millisecond),
			lastAccess:      time.Now(),
		}
		irl.entries[ip] = entry
	}
	entry.lastAccess = time.Now()
	irl.mu.Unlock()

	// Check endpoint-specific rate limit (3 requests per second per endpoint)
	if !entry.endpointLimiter.Allow(endpoint) {
		return false
	}

	// Check global rate limit (100 requests per minute per IP)
	if !entry.globalBucket.Allow() {
		return false
	}

	return true
}

// Global rate limiter instance
var rateLimiter = NewIPRateLimiter()

// RateLimitInterceptor rate limiting middleware
func RateLimitInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(
			ctx context.Context,
			req connect.AnyRequest,
		) (connect.AnyResponse, error) {
			// Extract client IP
			ip := extractClientIP(req.Header())

			// Get endpoint/procedure name
			endpoint := req.Spec().Procedure

			// Check rate limit
			if !rateLimiter.Allow(ip, endpoint) {
				return response.ErrResp(&models.Err{C: native.RespCode_RATE_LIMIT_EXCEEDED, E: connect.NewError(connect.CodeResourceExhausted, nil)})
			}

			return next(ctx, req)
		}
	}
}

// extractClientIP extracts the client IP address from HTTP headers or context
func extractClientIP(header http.Header) string {
	// Check X-Forwarded-For header first (for proxied requests)
	xff := header.Get("X-Forwarded-For")
	if xff != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	xri := header.Get("X-Real-IP")
	if xri != "" {
		return strings.TrimSpace(xri)
	}

	// Check True-Client-IP header (used by some CDNs)
	tci := header.Get("True-Client-IP")
	if tci != "" {
		return strings.TrimSpace(tci)
	}

	// Fall back to RemoteAddr - this will be set by the HTTP server
	// Note: In Connect RPC, the remote address might be available via peer info
	return "unknown"
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
		if !rateLimiter.Allow(ip, endpoint) {
			// Return 429 Too Many Requests
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
