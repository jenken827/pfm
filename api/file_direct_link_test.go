package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/dbtest"
)

// 302 回退到代理下载时 Location 必须自带凭证：浏览器跟随 302 时不会重复带
// Authorization 头（否则回退后立刻 401），也不能再用账户级 ?token=。
func TestProxyDownloadURL(t *testing.T) {
	dbtest.Setup(t)

	cases := []struct {
		name string
		url  string
		pid  *uint32
		want string
	}{
		{
			name: "透传 incoming 票据",
			url:  "/api/file/stream/direct?file_id=f1&ticket=t1",
			want: "/api/file/stream/download?file_id=f1&ticket=t1",
		},
		{
			name: "带 provider_id",
			url:  "/api/file/stream/direct?file_id=f1&provider_id=7&ticket=t1",
			pid:  uint32Ptr(7),
			want: "/api/file/stream/download?file_id=f1&provider_id=7&ticket=t1",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, c.url, nil)
			claims := &auth.Claims{User: auth.User{Id: "user-1"}}
			got, err := proxyDownloadURL(r, "f1", c.pid, claims)
			if err != nil {
				t.Fatalf("proxyDownloadURL: %v", err)
			}
			if got != c.want {
				t.Fatalf("proxyDownloadURL = %q, want %q", got, c.want)
			}
		})
	}
}

// 只有 Authorization 头的调用方（App）302 之后没人再带这个头：必须现签一张
// 下载票据放进 Location，且 URL 里不能再出现 token。
func TestProxyDownloadURLSignsTicketForHeaderCaller(t *testing.T) {
	dbtest.Setup(t)

	r := httptest.NewRequest(http.MethodGet, "/api/file/stream/direct?file_id=f1", nil)
	r.Header.Set("Authorization", "Bearer some.jwt")
	claims := &auth.Claims{User: auth.User{Id: "user-1"}}

	got, err := proxyDownloadURL(r, "f1", nil, claims)
	if err != nil {
		t.Fatalf("proxyDownloadURL: %v", err)
	}

	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Query().Get("token") != "" {
		t.Fatalf("URL must not carry access token: %s", got)
	}
	ticket := u.Query().Get("ticket")
	if ticket == "" {
		t.Fatalf("URL must carry a download ticket: %s", got)
	}
	parsed, err := auth.GetLocalAuth().VerifyPreviewTicket(ticket)
	if err != nil {
		t.Fatalf("verify signed ticket: %v", err)
	}
	if parsed.FileID != "f1" || parsed.UserID != "user-1" {
		t.Fatalf("ticket bound to %s/%s, want f1/user-1", parsed.FileID, parsed.UserID)
	}
	if parsed.Purpose != auth.PurposeDownload {
		t.Fatalf("purpose = %q, want %q", parsed.Purpose, auth.PurposeDownload)
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
