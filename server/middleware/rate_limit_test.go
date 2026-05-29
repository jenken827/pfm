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

func TestEndpointRateLimiter(t *testing.T) {
	limiter := NewEndpointRateLimiter()

	endpoint := "/test/endpoint"

	// First 10 requests should be allowed
	for i := 0; i < 10; i++ {
		if !limiter.Allow(endpoint) {
			t.Errorf("Request %d to endpoint should be allowed", i+1)
		}
	}

	// 11th request should be denied
	if limiter.Allow(endpoint) {
		t.Error("11th request to same endpoint should be denied")
	}

	// Different endpoint should be allowed
	otherEndpoint := "/other/endpoint"
	if !limiter.Allow(otherEndpoint) {
		t.Error("Request to different endpoint should be allowed")
	}
}

func TestIPRateLimiter_EndpointLimit(t *testing.T) {
	limiter := NewIPRateLimiter()

	ip := "192.168.1.1"
	endpoint := "/api/test"

	// First 10 requests to same endpoint should be allowed
	for i := 0; i < 10; i++ {
		if !limiter.Allow(ip, endpoint) {
			t.Errorf("Request %d to endpoint should be allowed", i+1)
		}
	}

	// 11th request to same endpoint should be denied
	if limiter.Allow(ip, endpoint) {
		t.Error("11th request to same endpoint should be denied")
	}

	// Request to different endpoint should still be allowed
	otherEndpoint := "/api/other"
	if !limiter.Allow(ip, otherEndpoint) {
		t.Error("Request to different endpoint should be allowed")
	}
}

func TestIPRateLimiter_GlobalLimit(t *testing.T) {
	// This test verifies the global limit of 200 requests per minute
	// We'll use a shorter timeframe for testing purposes
	limiter := NewIPRateLimiter()

	ip := "192.168.1.100"

	// Make 200 requests to different endpoints (to bypass endpoint limit)
	for i := 0; i < 200; i++ {
		endpoint := "/api/endpoint/" + string(rune(i))
		if !limiter.Allow(ip, endpoint) {
			t.Errorf("Request %d should be allowed within global limit", i+1)
		}
	}

	// 201st request should be denied due to global limit
	endpoint := "/api/final"
	if limiter.Allow(ip, endpoint) {
		t.Error("201st request should be denied due to global limit")
	}
}

func TestRateLimitHTTPMiddleware(t *testing.T) {
	// Reset the global rate limiter for testing
	rateLimiter = NewIPRateLimiter()

	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Success"))
	})

	// Wrap with rate limiting middleware
	handler := RateLimitHTTPMiddleware(testHandler)

	// Create test server
	server := httptest.NewServer(handler)
	defer server.Close()

	// Make 10 requests to same endpoint (should all succeed)
	for i := 0; i < 10; i++ {
		resp, err := http.Get(server.URL + "/api/test")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Request %d: expected status 200, got %d", i+1, resp.StatusCode)
		}
		resp.Body.Close()
	}

	// 11th request should be rate limited
	resp, err := http.Get(server.URL + "/api/test")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("Expected status 429 Too Many Requests, got %d", resp.StatusCode)
	}
}

func TestConcurrentAccess(t *testing.T) {
	limiter := NewIPRateLimiter()

	ip := "10.0.0.1"
	endpoint := "/concurrent/test"

	var wg sync.WaitGroup
	errors := make(chan bool, 100)

	// Launch 10 goroutines, each making 5 requests
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				if !limiter.Allow(ip, endpoint) {
					errors <- true
				}
				time.Sleep(10 * time.Millisecond)
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Count how many requests were allowed
	allowedCount := 50 - len(errors)
	t.Logf("Allowed %d out of 50 concurrent requests", allowedCount)

	// Should have some rejections due to rate limiting
	if allowedCount > 20 {
		t.Error("Too many requests allowed - rate limiting may not be working correctly")
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
				t.Errorf("Expected IP %s, got %s", tt.expectedIP, ip)
			}
		})
	}
}
