package auth

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

// newTicketTestAuth 构造只用于签发票据的认证器（不触库）
func newTicketTestAuth(secret string) *LocalAuthenticator {
	return &LocalAuthenticator{jwtSecret: []byte(secret), ttl: time.Hour}
}

func TestSignAndVerifyPreviewTicket(t *testing.T) {
	a := newTicketTestAuth("test-secret")
	ticket, expireAt, err := a.SignPreviewTicket("file-1", "user-1", 10*time.Minute)
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}
	if ticket == "" {
		t.Fatal("empty ticket")
	}
	if got := time.Until(expireAt); got < 9*time.Minute || got > 10*time.Minute {
		t.Fatalf("unexpected ttl: %v", got)
	}

	claims, err := a.VerifyPreviewTicket(ticket)
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if claims.FileID != "file-1" || claims.UserID != "user-1" {
		t.Fatalf("claims mismatch: %+v", claims)
	}
	if !claims.ExpireAt.Equal(expireAt.Truncate(time.Second)) {
		t.Fatalf("expire mismatch: %v vs %v", claims.ExpireAt, expireAt)
	}
}

func TestVerifyPreviewTicketExpired(t *testing.T) {
	a := newTicketTestAuth("test-secret")
	ticket, _, err := a.SignPreviewTicket("file-1", "user-1", time.Millisecond)
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}
	time.Sleep(5 * time.Millisecond)

	if _, err := a.VerifyPreviewTicket(ticket); !errors.Is(err, ErrPreviewTicketExpired) {
		t.Fatalf("want ErrPreviewTicketExpired, got %v", err)
	}
}

func TestVerifyPreviewTicketRejectsTamperedSignature(t *testing.T) {
	a := newTicketTestAuth("test-secret")
	ticket, _, err := a.SignPreviewTicket("file-1", "user-1", 10*time.Minute)
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}

	// 翻转签名首字节再编码回去（不能只改 base64 末位：末位的填充比特会被解码器忽略，
	// 那样解码出来的签名字节不变，验签仍然通过）
	parts := strings.Split(ticket, ".")
	if len(parts) != 3 {
		t.Fatalf("unexpected ticket format: %s", ticket)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode signature failed: %v", err)
	}
	sig[0] ^= 0xFF
	tampered := parts[0] + "." + parts[1] + "." + base64.RawURLEncoding.EncodeToString(sig)

	if _, err := a.VerifyPreviewTicket(tampered); !errors.Is(err, ErrPreviewTicketInvalid) {
		t.Fatalf("want ErrPreviewTicketInvalid, got %v", err)
	}
}

func TestVerifyPreviewTicketRejectsTamperedPayload(t *testing.T) {
	a := newTicketTestAuth("test-secret")
	ticket, _, err := a.SignPreviewTicket("file-1", "user-1", 10*time.Minute)
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}

	// 换掉负载正文但保留签名：必须被签名校验挡住
	parts := strings.Split(ticket, ".")
	if len(parts) != 3 {
		t.Fatalf("unexpected ticket format: %s", ticket)
	}
	forged := parts[0] + "." + parts[1] + "AA" + "." + parts[2]
	if _, err := a.VerifyPreviewTicket(forged); !errors.Is(err, ErrPreviewTicketInvalid) {
		t.Fatalf("want ErrPreviewTicketInvalid, got %v", err)
	}
}

func TestVerifyPreviewTicketRejectsOtherSecret(t *testing.T) {
	ticket, _, err := newTicketTestAuth("secret-a").SignPreviewTicket("file-1", "user-1", 10*time.Minute)
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}
	if _, err := newTicketTestAuth("secret-b").VerifyPreviewTicket(ticket); !errors.Is(err, ErrPreviewTicketInvalid) {
		t.Fatalf("want ErrPreviewTicketInvalid, got %v", err)
	}
}

func TestSignPreviewTicketRejectsEmptyBinding(t *testing.T) {
	a := newTicketTestAuth("test-secret")
	if _, _, err := a.SignPreviewTicket("", "user-1", time.Minute); err == nil {
		t.Fatal("want error for empty file id")
	}
	if _, _, err := a.SignPreviewTicket("file-1", "", time.Minute); err == nil {
		t.Fatal("want error for empty user id")
	}
	if _, _, err := a.SignPreviewTicket("file-1", "user-1", 0); err == nil {
		t.Fatal("want error for non-positive ttl")
	}
}

func TestVerifyPreviewTicketRejectsGarbage(t *testing.T) {
	a := newTicketTestAuth("test-secret")
	for _, bad := range []string{"", "v1", "v1.", "v1.a.b.c", "v2.abc.def", "v1.!!!.???"} {
		if _, err := a.VerifyPreviewTicket(bad); !errors.Is(err, ErrPreviewTicketInvalid) {
			t.Fatalf("ticket %q: want ErrPreviewTicketInvalid, got %v", bad, err)
		}
	}
}

// DeriveKey 必须按 label 域分隔：不同用途密钥不同，且都不是原始 JWT 密钥本身
func TestDeriveKeyDomains(t *testing.T) {
	a := newTicketTestAuth("test-secret")
	preview := a.DeriveKey(previewTicketKeyLabel)
	other := a.DeriveKey("another-purpose")

	if string(preview) == "test-secret" {
		t.Fatal("derived key must not equal raw secret")
	}
	if string(preview) == string(other) {
		t.Fatal("different labels must derive different keys")
	}
}

// 用途只影响有效期记录，不影响权限：签什么用途，验出来就是什么用途。
// 空用途按预览处理（老调用方 SignPreviewTicket 走的也是这条）。
func TestSignFileTicketPurpose(t *testing.T) {
	a := newTicketTestAuth("test-secret")

	cases := []struct {
		purpose string
		ttl     time.Duration
		want    string
	}{
		{PurposePreview, 10 * time.Minute, PurposePreview},
		{PurposeDownload, 12 * time.Hour, PurposeDownload},
		{"", time.Minute, PurposePreview},
	}

	for _, c := range cases {
		ticket, expireAt, err := a.SignFileTicket("file-1", "user-1", c.purpose, c.ttl)
		if err != nil {
			t.Fatalf("purpose %q: sign failed: %v", c.purpose, err)
		}
		if got := time.Until(expireAt); got < c.ttl-time.Minute || got > c.ttl {
			t.Fatalf("purpose %q: unexpected ttl %v", c.purpose, got)
		}
		claims, err := a.VerifyPreviewTicket(ticket)
		if err != nil {
			t.Fatalf("purpose %q: verify failed: %v", c.purpose, err)
		}
		if claims.Purpose != c.want {
			t.Fatalf("purpose %q: claims.Purpose = %q, want %q", c.purpose, claims.Purpose, c.want)
		}
	}
}

// 旧版本签发的票据没有 p 字段：必须继续可用，并按预览处理（不能因为加字段就全失效）
func TestVerifyPreviewTicketLegacyPayloadWithoutPurpose(t *testing.T) {
	a := newTicketTestAuth("test-secret")

	payload := `{"f":"file-1","u":"user-1","e":` +
		strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10) + `}`
	body := previewTicketVersion + "." + base64.RawURLEncoding.EncodeToString([]byte(payload))
	ticket := body + "." + base64.RawURLEncoding.EncodeToString(a.signPreviewTicketBody(body))

	claims, err := a.VerifyPreviewTicket(ticket)
	if err != nil {
		t.Fatalf("verify legacy ticket failed: %v", err)
	}
	if claims.Purpose != PurposePreview {
		t.Fatalf("legacy Purpose = %q, want %q", claims.Purpose, PurposePreview)
	}
}
