package middleware

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jenken827/pfm/globals"
	"github.com/jenken827/pfm/internal/auth"
	"github.com/jenken827/pfm/internal/dbtest"
)

const (
	ticketTestUserID = "user-preview-ticket"
	ticketTestFileID = "file-preview-ticket"
	ticketTestName   = "a.pdf"
)

// issuePreviewTicket 用真实签名器签发一张票据
func issuePreviewTicket(t *testing.T, ttl time.Duration) string {
	t.Helper()
	ticket, _, err := auth.GetLocalAuth().SignPreviewTicket(ticketTestFileID, ticketTestUserID, ttl)
	if err != nil {
		t.Fatalf("sign preview ticket failed: %v", err)
	}
	return ticket
}

// previewURL 构造 raw 预览请求地址（票据可空）
func previewURL(ticket, fileID string) string {
	target := "/api/file/preview/raw/" + ticketTestName + "?file_id=" + fileID
	if ticket != "" {
		target += "&ticket=" + ticket
	}
	return target
}

// TestAuthPreviewTicketHTTP 票据通过后 claims 只带 user_id，并注入 ctx
func TestAuthPreviewTicketHTTP(t *testing.T) {
	dbtest.Setup(t)

	ticket := issuePreviewTicket(t, time.Minute)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", previewURL(ticket, ticketTestFileID), nil)

	claims, err := AuthPreviewTicketHTTP(rec, req)
	if err != nil {
		t.Fatalf("auth failed: %v", err)
	}
	if claims.User.Id != ticketTestUserID {
		t.Fatalf("claims user = %q, want %q", claims.User.Id, ticketTestUserID)
	}
	if got := req.Context().Value(globals.ContextUserKey); got == nil {
		t.Fatal("claims not injected into context")
	}
}

// TestAuthPreviewTicketHTTPRejectsWrongFile 票据绑定单文件：file_id 不一致必须拒绝
func TestAuthPreviewTicketHTTPRejectsWrongFile(t *testing.T) {
	dbtest.Setup(t)

	ticket := issuePreviewTicket(t, time.Minute)
	req := httptest.NewRequest("GET", previewURL(ticket, "another-file"), nil)

	if _, err := AuthPreviewTicketHTTP(httptest.NewRecorder(), req); err == nil {
		t.Fatal("want error for file_id mismatch")
	}
}

// TestAuthPreviewTicketHTTPRejectsExpired 过期票据必须拒绝
func TestAuthPreviewTicketHTTPRejectsExpired(t *testing.T) {
	dbtest.Setup(t)

	ticket := issuePreviewTicket(t, time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	req := httptest.NewRequest("GET", previewURL(ticket, ticketTestFileID), nil)

	if _, err := AuthPreviewTicketHTTP(httptest.NewRecorder(), req); err == nil {
		t.Fatal("want error for expired ticket")
	}
}

// TestAuthPreviewTicketHTTPRejectsMissingTicket 缺票据必须拒绝
func TestAuthPreviewTicketHTTPRejectsMissingTicket(t *testing.T) {
	dbtest.Setup(t)

	req := httptest.NewRequest("GET", previewURL("", ticketTestFileID), nil)
	if _, err := AuthPreviewTicketHTTP(httptest.NewRecorder(), req); err == nil {
		t.Fatal("want error for missing ticket")
	}
}

// TestPreviewTicketIsNotAccessToken 票据不能被当成 access token 使用（凭证类型隔离）
func TestPreviewTicketIsNotAccessToken(t *testing.T) {
	dbtest.Setup(t)

	ticket := issuePreviewTicket(t, time.Minute)
	req := httptest.NewRequest("GET", "/api/file/list", nil)
	req.Header.Set("Authorization", "Bearer "+ticket)

	if _, err := AuthHTTPMiddleware(httptest.NewRecorder(), req); err == nil {
		t.Fatal("preview ticket must not be accepted as an access token")
	}
}
