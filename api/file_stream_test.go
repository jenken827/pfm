package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ?token= 查询参数用于浏览器直接发起的下载 / 预览（<a href> 无法带请求头）。
// 取值规则：非空才覆盖 Authorization 头。
func TestApplyQueryToken(t *testing.T) {
	cases := []struct {
		name      string
		url       string
		presetHdr string
		wantHdr   string
	}{
		{"无 token 不改动", "/api/file/stream/download?file_id=x", "", ""},
		{"空 token 不覆盖已有头", "/x?token=", "Bearer header-token", "Bearer header-token"},
		{"query token 提升为 Bearer", "/x?token=abc.def", "", "Bearer abc.def"},
		{"query token 优先于已有头", "/x?token=query-token", "Bearer header-token", "Bearer query-token"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, c.url, nil)
			if c.presetHdr != "" {
				r.Header.Set("Authorization", c.presetHdr)
			}
			applyQueryToken(r)
			if got := r.Header.Get("Authorization"); got != c.wantHdr {
				t.Errorf("Authorization = %q, want %q", got, c.wantHdr)
			}
		})
	}
}
