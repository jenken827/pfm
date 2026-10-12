package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// 302 回退到代理下载时必须把 token 带上：调用方可能用 Authorization 头鉴权，
// 而浏览器跟随 302 时不会重复带这个头（否则回退后立刻 401）。
func TestProxyDownloadURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		hdr  string
		pid  *uint32
		want string
	}{
		{
			name: "query token",
			url:  "/api/file/stream/direct?file_id=f1&token=t1",
			want: "/api/file/stream/download?file_id=f1&token=t1",
		},
		{
			name: "Authorization 头里的 token 也要带过去",
			url:  "/api/file/stream/direct?file_id=f1",
			hdr:  "Bearer t2",
			want: "/api/file/stream/download?file_id=f1&token=t2",
		},
		{
			name: "带 provider_id",
			url:  "/api/file/stream/direct?file_id=f1&provider_id=7&token=t1",
			pid:  uint32Ptr(7),
			want: "/api/file/stream/download?file_id=f1&provider_id=7&token=t1",
		},
		{
			name: "无 token 时不追加参数",
			url:  "/api/file/stream/direct?file_id=f1",
			want: "/api/file/stream/download?file_id=f1",
		},
		{
			name: "非 Bearer 头不当作 token",
			url:  "/api/file/stream/direct?file_id=f1",
			hdr:  "Basic abc",
			want: "/api/file/stream/download?file_id=f1",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, c.url, nil)
			if c.hdr != "" {
				r.Header.Set("Authorization", c.hdr)
			}
			if got := proxyDownloadURL(r, "f1", c.pid); got != c.want {
				t.Fatalf("proxyDownloadURL = %q, want %q", got, c.want)
			}
		})
	}
}

func TestValueOrZero(t *testing.T) {
	if got := valueOrZero(nil); got != 0 {
		t.Fatalf("valueOrZero(nil) = %d, want 0", got)
	}
	if got := valueOrZero(uint32Ptr(9)); got != 9 {
		t.Fatalf("valueOrZero(9) = %d, want 9", got)
	}
}

func uint32Ptr(v uint32) *uint32 { return &v }
