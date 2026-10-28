package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/dbtest"
	"jenken827.com/jenken827/pfm/internal/service"
)

// mustTicket 用当前本地认证器签一张预览票据（dbtest.Setup 之后才可用）
func mustTicket(t *testing.T, fileID, userID string) string {
	t.Helper()
	ticket, _, err := auth.GetLocalAuth().SignFileTicket(fileID, userID, auth.PurposeDownload, time.Hour)
	if err != nil {
		t.Fatalf("sign ticket: %v", err)
	}
	return ticket
}

// 文件流只接受两种凭证：?ticket=（单文件票据）或 Authorization 头。
// ⚠ ?token=<JWT> 已彻底移除：伪造 / 过期票据必须直接失败，绝不能回落到
// 账户级 access token 再鉴权一次。
func TestAuthenticateFileRequestTicketOnly(t *testing.T) {
	// 票据校验依赖 auth.GetLocalAuth()，它要求配置与数据目录已初始化
	dbtest.Setup(t)

	cases := []struct {
		name string
		url  string
	}{
		{"无任何凭证", "/api/file/stream/download?file_id=f1"},
		{"票据不是三段", "/api/file/stream/download?file_id=f1&ticket=garbage"},
		{"票据版本不对", "/api/file/stream/download?file_id=f1&ticket=v9.abc.def"},
		{"旧 ?token= 不再生效", "/api/file/stream/download?file_id=f1&token=some.jwt"},
		{"伪票据不得回落到 token", "/api/file/stream/download?file_id=f1&ticket=garbage&token=some.jwt"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, c.url, nil)
			if _, err := authenticateFileRequest(httptest.NewRecorder(), r); err == nil {
				t.Fatal("want auth error")
			}
		})
	}
}

// 历史版本端点没有 file_id 参数：票据的文件与历史版本宿主文件的比对在 service 层做，
// 这里只确认鉴权把票据绑定的 file_id 传给了 handler。
func TestAuthenticateFileRequestHistoryTicketCarriesFileID(t *testing.T) {
	dbtest.Setup(t)

	ticket := mustTicket(t, "file-a", "user-1")
	r := httptest.NewRequest(http.MethodGet, "/api/file/stream/history/download?id=1&ticket="+ticket, nil)

	claims, err := authenticateFileRequest(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if claims.PreviewFileID != "file-a" {
		t.Fatalf("PreviewFileID = %q, want file-a", claims.PreviewFileID)
	}
}

// 票据带了 file_id 时仍要求与票据一致（A 文件的票据不能读 B 文件）
func TestAuthenticateFileRequestRejectsOtherFile(t *testing.T) {
	dbtest.Setup(t)

	ticket := mustTicket(t, "file-a", "user-1")
	r := httptest.NewRequest(http.MethodGet, "/api/file/stream/download?file_id=file-b&ticket="+ticket, nil)

	if _, err := authenticateFileRequest(httptest.NewRecorder(), r); err == nil {
		t.Fatal("want mismatch error")
	}
}

// 中文文件名要同时给 ASCII 回退名和 RFC 5987 的 filename*，否则部分浏览器 / 代理
// 会把 header 按 latin-1 解出乱码。
func TestContentDisposition(t *testing.T) {
	header := contentDisposition("attachment", "报告 2024.pdf")
	if !strings.HasPrefix(header, `attachment; filename="__ 2024.pdf"`) {
		t.Fatalf("ASCII 回退名不对：%s", header)
	}
	if !strings.Contains(header, `filename*=UTF-8''%E6%8A%A5%E5%91%8A%202024.pdf`) {
		t.Fatalf("缺少 RFC 5987 filename*：%s", header)
	}

	// 空文件名不能产生 filename=""，否则部分客户端会当成"没有文件名"
	if got := contentDisposition("inline", ""); !strings.Contains(got, `filename="unnamed"`) {
		t.Fatalf("空文件名应回退 unnamed：%s", got)
	}
}

// ?token=<JWT> 已经彻底移除：access token 是账户级 30 天凭证，不能出现在 URL 里。
func TestDownloadHandlerRejectsQueryToken(t *testing.T) {
	dbtest.Setup(t)

	r := httptest.NewRequest(http.MethodGet, "/api/file/stream/download?file_id=f1&token=some.jwt", nil)
	rec := httptest.NewRecorder()
	DownloadFileHandler(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("?token= 应 401，实际 %d", rec.Code)
	}
}

// 带合法票据的下载请求必须过鉴权这一关（后续因文件不存在失败是另一回事）
func TestDownloadHandlerAcceptsTicket(t *testing.T) {
	dbtest.Setup(t)

	ticket := mustTicket(t, "file-a", "user-1")
	r := httptest.NewRequest(http.MethodGet, "/api/file/stream/download?file_id=file-a&ticket="+ticket, nil)
	rec := httptest.NewRecorder()
	DownloadFileHandler(rec, r)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("合法票据不应 401：%s", rec.Body.String())
	}
}

// TestHTTPStatusFromError 鉴权类拒绝必须是 403，不能混进 500（监控会当成服务端故障）
func TestHTTPStatusFromError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"ticket mismatch", service.ErrTicketFileMismatch, http.StatusForbidden},
		{"wrapped ticket mismatch", fmt.Errorf("history: %w", service.ErrTicketFileMismatch), http.StatusForbidden},
		{"not owner", errors.New("permission denied: not owner of file history"), http.StatusForbidden},
		{"missing file", errors.New("file not found"), http.StatusNotFound},
		{"no provider", errors.New("no available provider for hash"), http.StatusServiceUnavailable},
		{"empty hash", errors.New("history hash is empty"), http.StatusInternalServerError},
		{"other", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		if got := httpStatusFromError(c.err); got != c.want {
			t.Errorf("%s: httpStatusFromError = %d, want %d", c.name, got, c.want)
		}
	}
}

// 预下载态（驱动声明 PREDOWNLOAD 能力）：三个流式 handler 都必须回 202 + 状态 JSON，
// 而不是拿 nil Reader 去 Close / Read（漏了就是 HTTP PANIC → 500）。
func TestRespondPreDownload(t *testing.T) {
	rec := httptest.NewRecorder()
	respondPreDownload(rec, map[string]any{
		"status":  "pre_download",
		"file_id": "f1",
		"message": "Large file is being cached, please retry later",
	})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got == "" {
		t.Error("202 应当带 Retry-After，客户端据此稍后重试")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected JSON content type, got %q", ct)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "pre_download" {
		t.Errorf("expected status=pre_download, got %v", body["status"])
	}
}

// pacedWriter：累计写入字节，stop 可重复调用（handler 结束与 defer 都可能触发）
func TestPacedWriterCountsAndStops(t *testing.T) {
	var buf bytes.Buffer
	pace := newPacedWriter(&buf, "a.mp4", 10)
	defer pace.stop()

	if _, err := pace.Write([]byte("hello")); err != nil {
		t.Fatalf("write 失败：%v", err)
	}
	if got := buf.String(); got != "hello" {
		t.Fatalf("应透传到下层 writer，实际 %q", got)
	}
	pace.mu.Lock()
	total := pace.total
	pace.mu.Unlock()
	if total != 5 {
		t.Fatalf("应累计 5 字节，实际 %d", total)
	}

	// 幂等：重复 stop 不应 panic（close 一个已关闭的 channel 会 panic）
	pace.stop()
	pace.stop()
}
