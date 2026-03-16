# Rate Limiting Middleware

## Overview

This middleware implements rate limiting for your Go server using a token bucket algorithm. It provides two levels of rate limiting:

1. **Per-Endpoint Rate Limit**: Same IP cannot access the same endpoint more than 3 times per second
2. **Global Rate Limit**: Same IP cannot make more than 100 requests to all endpoints within 1 minute

## Features

- ✅ Token bucket algorithm for smooth rate limiting
- ✅ Per-IP tracking
- ✅ Per-endpoint rate limiting (3 req/s per endpoint)
- ✅ Global rate limiting (100 req/min per IP)
- ✅ Thread-safe implementation using mutex locks
- ✅ Support for proxied requests (X-Forwarded-For, X-Real-IP, True-Client-IP)
- ✅ HTTP middleware compatible with Vanguard transcoder
- ✅ Comprehensive test coverage

## Architecture

### Components

1. **TokenBucket**: Core rate limiting primitive
   - Maintains a bucket of tokens
   - Tokens are consumed on each request
   - Tokens refill at a configured rate
   - Thread-safe with mutex protection

2. **EndpointRateLimiter**: Manages rate limits per endpoint
   - Creates a separate token bucket for each endpoint
   - Tracks buckets in a concurrent map

3. **IPRateLimiter**: Manages rate limits per IP address
   - Combines endpoint-specific and global rate limiting
   - Checks both limits before allowing a request
   - Automatically creates limiters for new IPs

4. **RateLimitHTTPMiddleware**: HTTP middleware wrapper
   - Extracts client IP from request headers
   - Applies rate limiting before processing requests
   - Returns HTTP 429 (Too Many Requests) when limits exceeded

## Usage

The rate limiting middleware is automatically applied to all HTTP requests through the server's middleware chain:

```go
// In server.go
handlerWithRateLimit := middleware.RateLimitHTTPMiddleware(handlerWithCors)
```

### How It Works

1. **Request arrives** → Middleware extracts client IP
2. **Check endpoint limit** → Allow if ≤ 3 requests/second to this endpoint
3. **Check global limit** → Allow if ≤ 100 requests/minute total
4. **Process or reject** → Continue to handler or return 429

### Client IP Detection

The middleware supports multiple methods to detect the real client IP:

1. **X-Forwarded-For** (highest priority) - For requests behind load balancers/proxies
2. **X-Real-IP** - Alternative proxy header
3. **True-Client-IP** - Used by some CDNs (e.g., Akamai, Cloudflare)
4. **RemoteAddr** - Direct connection IP (lowest priority)

## Configuration

Current limits are hardcoded but can be customized:

```go
// Per-endpoint limit: 3 requests per second
bucket = NewTokenBucket(3, 333*time.Millisecond)

// Global limit: 100 requests per minute
globalBucket = NewTokenBucket(100, 600*time.Millisecond)
```

To adjust limits, modify these values in `rate_limit.go`.

## Response Codes

When rate limited, clients receive:
- **HTTP Status**: 429 Too Many Requests
- **Response Body**: "Rate limit exceeded"

## Testing

Run the comprehensive test suite:

```bash
go test -v ./server/middleware
```

Tests cover:
- Token bucket behavior
- Endpoint-specific rate limiting
- Global rate limiting
- HTTP middleware integration
- Concurrent access patterns
- Client IP extraction

## Example Test Results

```
=== RUN   TestTokenBucket
--- PASS: TestTokenBucket (0.40s)
=== RUN   TestEndpointRateLimiter
--- PASS: TestEndpointRateLimiter (0.00s)
=== RUN   TestIPRateLimiter_EndpointLimit
--- PASS: TestIPRateLimiter_EndpointLimit (0.00s)
=== RUN   TestIPRateLimiter_GlobalLimit
--- PASS: TestIPRateLimiter_GlobalLimit (0.00s)
=== RUN   TestRateLimitHTTPMiddleware
--- PASS: TestRateLimitHTTPMiddleware (0.01s)
=== RUN   TestConcurrentAccess
    rate_limit_test.go:176: Allowed 3 out of 50 concurrent requests
--- PASS: TestConcurrentAccess (0.05s)
PASS
ok      jenken827.com/jenken827/pfm/server/middleware       1.265s
```

## Performance Considerations

- **Memory**: Each unique IP + endpoint combination creates a token bucket
- **Cleanup**: Consider implementing automatic cleanup of stale buckets for long-running servers
- **Concurrency**: All operations are thread-safe with minimal lock contention
- **Scalability**: Works well for moderate traffic; consider Redis-based limiting for distributed systems

## Future Enhancements

Potential improvements:
- Add bucket cleanup mechanism (GC for old IPs)
- Make limits configurable via config file
- Add metrics/monitoring endpoints
- Implement sliding window algorithm as alternative
- Add whitelist/blacklist functionality
- Support for distributed rate limiting (Redis-backed)

## Troubleshooting

### Issue: Legitimate users getting rate limited

**Solution**: Increase the global limit or implement user authentication to bypass IP-based limits

### Issue: Rate limiting not working in production

**Solution**: Ensure your load balancer/proxy is setting X-Forwarded-For or X-Real-IP headers

### Issue: Too many memory allocations

**Solution**: Implement periodic cleanup of inactive IP addresses from the maps
