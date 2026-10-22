package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestTokenBucket(t *testing.T) {
	// Create a bucket with 3 tokens, refilling 1 token per 333ms
	bucket := NewTokenBucket(3, 333*time.Millisecond)

	// Should allow 3 requests immediately
	for i := 0; i < 3; i++ {
		if !bucket.Allow() {
			t.Errorf("Request %d should be allowed", i+1)
		}
	}

	// 4th request should be denied
	if bucket.Allow() {
		t.Error("4th request should be denied")
	}

	// Wait for refill
	time.Sleep(400 * time.Millisecond)

	// Should allow again
	if !bucket.Allow() {
		t.Error("Request after refill should be allowed")
	}
}

func TestTokenBucketSustainedRate(t *testing.T) {
	// 10 tokens, refill 1 per 20ms → 在 200ms 内应再放行约 10 个
	bucket := NewTokenBucket(10, 20*time.Millisecond)
	for i := 0; i < 10; i++ {
		if !bucket.Allow() {
			t.Fatalf("Request %d should be allowed", i+1)
		}
	}
	time.Sleep(210 * time.Millisecond)

	allowed := 0
	for i := 0; i < 20; i++ {
		if bucket.Allow() {
			allowed++
		}
	}
	if allowed < 8 || allowed > 12 {
		t.Errorf("expected ~10 tokens after 210ms, got %d", allowed)
	}
}

func TestEndpointRateLimiter(t *testing.T) {
	limiter := NewEndpointRateLimiter(3, 100*time.Millisecond)

	endpoint := "/test/endpoint"

	// First 3 requests should be allowed
	for i := 0; i < 3; i++ {
		if !limiter.Allow(endpoint) {
			t.Errorf("Request %d to endpoint should be allowed", i+1)
		}
	}

	// 4th request should be denied
	if limiter.Allow(endpoint) {
		t.Error("4th request to same endpoint should be denied")
	}

	// Different endpoint should be allowed
	otherEndpoint := "/other/endpoint"
	if !limiter.Allow(otherEndpoint) {
		t.Error("Request to different endpoint should be allowed")
	}
}

func TestIPRateLimiter_EndpointLimit(t *testing.T) {
	limiter := NewIPRateLimiterWithOptions(RateLimitOptions{EndpointBurst: 5, EndpointRefill: time.Hour})

	ip := "192.168.1.1"
	endpoint := "/api/test"

	for i := 0; i < 5; i++ {
		if !limiter.Allow(ip, endpoint, false) {
			t.Errorf("Request %d to endpoint should be allowed", i+1)
		}
	}

	if limiter.Allow(ip, endpoint, false) {
		t.Error("6th request to same endpoint should be denied")
	}

	// Request to different endpoint should still be allowed
	otherEndpoint := "/api/other"
	if !limiter.Allow(ip, otherEndpoint, false) {
		t.Error("Request to different endpoint should be allowed")
	}
}

func TestIPRateLimiter_GlobalLimit(t *testing.T) {
	limiter := NewIPRateLimiterWithOptions(RateLimitOptions{EndpointBurst: 100, EndpointRefill: time.Hour, GlobalBurst: 20, GlobalRefill: time.Hour})

	ip := "192.168.1.100"

	// 每个路径都不重复，绕开 endpoint 桶，只压全局桶
	for i := 0; i < 20; i++ {
		endpoint := "/api/endpoint/" + string(rune('a'+i))
		if !limiter.Allow(ip, endpoint, false) {
			t.Errorf("Request %d should be allowed within global limit", i+1)
		}
	}

	if limiter.Allow(ip, "/api/final", false) {
		t.Error("request beyond global limit should be denied")
	}
}

// 缩略图只用独立配额：不能挤占文档流的 endpoint / global 额度
func TestIPRateLimiter_ThumbnailIsolated(t *testing.T) {
	limiter := NewIPRateLimiterWithOptions(RateLimitOptions{
		EndpointBurst:   10,
		EndpointRefill:  time.Hour,
		GlobalBurst:     2,
		GlobalRefill:    time.Hour,
		ThumbnailBurst:  3,
		ThumbnailRefill: time.Hour,
	})

	ip := "10.1.2.3"

	// 用小额度把全局桶打空（换路径绕开 endpoint 桶）
	if !limiter.Allow(ip, "/api/a", false) || !limiter.Allow(ip, "/api/b", false) {
		t.Fatal("first two requests should be allowed")
	}
	if limiter.Allow(ip, "/api/c", false) {
		t.Fatal("global bucket should be exhausted")
	}

	// 缩略图不受全局桶影响
	for i := 0; i < 3; i++ {
		if !limiter.Allow(ip, "/api/file/stream/preview", true) {
			t.Errorf("thumbnail request %d should be allowed", i+1)
		}
	}
	if limiter.Allow(ip, "/api/file/stream/preview", true) {
		t.Error("4th thumbnail request should hit the thumbnail bucket")
	}

	// 缩略图流量既不吃也不回填全局桶：普通请求依旧被全局桶拒绝
	if limiter.Allow(ip, "/api/a", false) {
		t.Error("global bucket should stay exhausted after thumbnail traffic")
	}
}

func TestIsThumbnailRequest(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		query    string
		expected bool
	}{
		{"小缩略图", "/api/file/stream/preview", "file_id=x&size=small", true},
		{"大缩略图", "/api/file/stream/preview", "file_id=x&size=large", true},
		{"不带 size 默认缩略图", "/api/file/stream/preview", "file_id=x", true},
		{"原图预览", "/api/file/stream/preview", "file_id=x&original=true", false},
		{"原图 + size", "/api/file/stream/preview", "file_id=x&size=large&original=true", false},
		{"其他路径", "/api/file/preview/raw/a.pdf", "file_id=x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path+"?"+tt.query, nil)
			if got := isThumbnailRequest(req); got != tt.expected {
				t.Errorf("isThumbnailRequest(%s?%s) = %v, want %v", tt.path, tt.query, got, tt.expected)
			}
		})
	}
}

func TestRateLimitHTTPMiddleware(t *testing.T) {
	// 用小额度验证中间件行为（默认额度是给生产用的，不适合断言）
	InitRateLimit(RateLimitOptions{EndpointBurst: 2, EndpointRefill: time.Hour, GlobalBurst: 100, GlobalRefill: time.Hour})

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Success"))
	})
	handler := RateLimitHTTPMiddleware(testHandler)
	server := httptest.NewServer(handler)
	defer server.Close()

	for i := 0; i < 2; i++ {
		resp, err := http.Get(server.URL + "/api/test")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Request %d: expected status 200, got %d", i+1, resp.StatusCode)
		}
		resp.Body.Close()
	}

	resp, err := http.Get(server.URL + "/api/test")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("Expected status 429 Too Many Requests, got %d", resp.StatusCode)
	}
	// 429 必须带 Retry-After，客户端据此退避而不是立刻重试
	if got := resp.Header.Get("Retry-After"); got == "" {
		t.Error("429 response should carry Retry-After header")
	}

	// 恢复默认，避免影响同包内其他用例
	InitRateLimit(DefaultRateLimitOptions())
}

func TestConcurrentAccess(t *testing.T) {
	limiter := NewIPRateLimiterWithOptions(RateLimitOptions{
		EndpointBurst:  10,
		EndpointRefill: time.Hour,
		GlobalBurst:    10,
		GlobalRefill:   time.Hour,
	})

	ip := "10.0.0.1"
	endpoint := "/concurrent/test"

	var wg sync.WaitGroup
	denied := make(chan bool, 100)

	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				if !limiter.Allow(ip, endpoint, false) {
					denied <- true
				}
			}
		}()
	}

	wg.Wait()
	close(denied)

	if len(denied) == 0 {
		t.Error("expected some requests to be denied with a 10-token bucket")
	}
}

func TestExtractClientIPFromRequest(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		expectedIP string
	}{
		{
			name:       "X-Forwarded-For single IP",
			remoteAddr: "127.0.0.1:8080",
			headers:    map[string]string{"X-Forwarded-For": "192.168.1.1"},
			expectedIP: "192.168.1.1",
		},
		{
			name:       "X-Forwarded-For multiple IPs",
			remoteAddr: "127.0.0.1:8080",
			headers:    map[string]string{"X-Forwarded-For": "192.168.1.1, 10.0.0.1, 172.16.0.1"},
			expectedIP: "192.168.1.1",
		},
		{
			name:       "X-Real-IP",
			remoteAddr: "127.0.0.1:8080",
			headers:    map[string]string{"X-Real-IP": "192.168.1.2"},
			expectedIP: "192.168.1.2",
		},
		{
			name:       "True-Client-IP",
			remoteAddr: "127.0.0.1:8080",
			headers:    map[string]string{"True-Client-IP": "192.168.1.3"},
			expectedIP: "192.168.1.3",
		},
		{
			name:       "RemoteAddr fallback",
			remoteAddr: "192.168.1.100:8080",
			headers:    map[string]string{},
			expectedIP: "192.168.1.100",
		},
		{
			name:       "Priority: XFF > X-Real-IP > True-Client-IP > RemoteAddr",
			remoteAddr: "127.0.0.1:8080",
			headers: map[string]string{
				"X-Forwarded-For": "192.168.1.1",
				"X-Real-IP":       "192.168.1.2",
				"True-Client-IP":  "192.168.1.3",
			},
			expectedIP: "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tt.remoteAddr

			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}

			ip := extractClientIPFromRequest(req)
			if ip != tt.expectedIP {
				t.Errorf("Expected IP %s, got %s", ip, tt.expectedIP)
			}
		})
	}
}
