package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 文件票据（file ticket）：给"只能把凭证写进 URL"的场景用的单文件短期凭证。
//
// 为什么不用 access token：access token 是账户级、30 天有效的 JWT，写进 URL 之后
// 会留在 WebView / 浏览器历史 / 下载历史与 Referer 里，泄露一次就等于交出该用户
// 全部文件的读权限。票据相反——绑定 file_id + user_id、只对这一个文件有效。
//
// 两种用途只差有效期：
//   - PurposePreview 10 分钟：一次性拉取（文本片段 / 图片）；
//   - PurposeDownload 12 小时：媒体播放、Viewer（边翻边发 Range）、下载与历史版本下载
//     （浏览器下载器暂停 / 断网续传时每次都会重新校验票据）。
//
// 格式（三段，点分）：`v1.<base64url(JSON负载)>.<base64url(HMAC-SHA256)>`
// 签名密钥由 JWT 密钥派生（域分隔），因此轮换 jwt_secret 会同时作废旧票据；
// 票据是无状态的，服务端不存记录，撤销手段只有"等它过期"。
const previewTicketVersion = "v1"

// 票据用途（写进负载的 p 字段，校验后回传给调用方）
const (
	// PurposePreview 预览用途（10 分钟）
	PurposePreview = "preview"
	// PurposeDownload 下载用途（12 小时）
	PurposeDownload = "download"
)

// previewTicketKeyLabel 派生票据专用密钥的域分隔标签
const previewTicketKeyLabel = "pfm-preview-ticket-v1"

var (
	// ErrPreviewTicketInvalid 票据格式错误 / 签名不符 / 绑定字段缺失
	ErrPreviewTicketInvalid = errors.New("invalid preview ticket")
	// ErrPreviewTicketExpired 票据已过期
	ErrPreviewTicketExpired = errors.New("preview ticket expired")
)

// PreviewTicketClaims 票据负载：只描述"谁能读哪个文件、到什么时候"
type PreviewTicketClaims struct {
	FileID   string
	UserID   string
	Purpose  string
	ExpireAt time.Time
}

// previewTicketPayload 票据 JSON 负载（字段名取短，票据会出现在 URL 查询串里）
type previewTicketPayload struct {
	FileID   string `json:"f"`
	UserID   string `json:"u"`
	Purpose  string `json:"p,omitempty"`
	ExpireAt int64  `json:"e"`
}

// DeriveKey 用 JWT 密钥派生用途隔离的子密钥：HMAC-SHA256(jwtSecret, label)
//
// 目的是让不同用途的凭证（JWT / 预览票据 / 将来的其他票据）互不可冒用：
// 拿到票据签名密钥也推不出 JWT 密钥，反之亦然。
func (a *LocalAuthenticator) DeriveKey(label string) []byte {
	mac := hmac.New(sha256.New, a.jwtSecret)
	mac.Write([]byte(label))
	return mac.Sum(nil)
}

// SignFileTicket 为 fileID/userID 签发单文件票据，返回票据与过期时间
//
// purpose 决定有效期由调用方给（见 internal/service 的 previewTicketTTL /
// downloadTicketTTL）；purpose 只写进负载用于审计与区分，不改变权限。
func (a *LocalAuthenticator) SignFileTicket(fileID, userID, purpose string, ttl time.Duration) (string, time.Time, error) {
	if fileID == "" || userID == "" {
		return "", time.Time{}, errors.New("file id and user id are required")
	}
	if purpose == "" {
		purpose = PurposePreview
	}
	if ttl <= 0 {
		return "", time.Time{}, errors.New("invalid preview ticket ttl")
	}

	expireAt := time.Now().Add(ttl)
	raw, err := json.Marshal(previewTicketPayload{
		FileID:   fileID,
		UserID:   userID,
		Purpose:  purpose,
		ExpireAt: expireAt.Unix(),
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("marshal preview ticket failed: %w", err)
	}

	body := previewTicketVersion + "." + base64.RawURLEncoding.EncodeToString(raw)
	return body + "." + base64.RawURLEncoding.EncodeToString(a.signPreviewTicketBody(body)), expireAt, nil
}

// SignPreviewTicket 签发预览用途的票据（等价 SignFileTicket(..., PurposePreview, ttl)）
func (a *LocalAuthenticator) SignPreviewTicket(fileID, userID string, ttl time.Duration) (string, time.Time, error) {
	return a.SignFileTicket(fileID, userID, PurposePreview, ttl)
}

// VerifyPreviewTicket 校验票据签名与有效期，返回负载
//
// 只校验"票据本身是否可信"；file_id 是否与本次请求一致由调用方比对
// （票据是给单个文件签的，不能拿 A 文件的票据读 B）。
func (a *LocalAuthenticator) VerifyPreviewTicket(ticket string) (*PreviewTicketClaims, error) {
	version, body, signature, ok := splitPreviewTicket(ticket)
	if !ok || version != previewTicketVersion {
		return nil, ErrPreviewTicketInvalid
	}

	got, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return nil, ErrPreviewTicketInvalid
	}
	if !hmac.Equal(got, a.signPreviewTicketBody(version+"."+body)) {
		return nil, ErrPreviewTicketInvalid
	}

	payload, err := decodePreviewTicketPayload(body)
	if err != nil {
		return nil, err
	}
	if payload.FileID == "" || payload.UserID == "" {
		return nil, ErrPreviewTicketInvalid
	}

	expireAt := time.Unix(payload.ExpireAt, 0)
	if !time.Now().Before(expireAt) {
		return nil, ErrPreviewTicketExpired
	}
	purpose := payload.Purpose
	if purpose == "" {
		// 旧票据没有 p 字段，按预览处理（最长 10 分钟，语义无差别）
		purpose = PurposePreview
	}
	return &PreviewTicketClaims{
		FileID:   payload.FileID,
		UserID:   payload.UserID,
		Purpose:  purpose,
		ExpireAt: expireAt,
	}, nil
}

// splitPreviewTicket 按点拆分三段票据
func splitPreviewTicket(ticket string) (version, body, signature string, ok bool) {
	parts := strings.Split(ticket, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// decodePreviewTicketPayload 解码并解析票据负载
func decodePreviewTicketPayload(body string) (*previewTicketPayload, error) {
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil, ErrPreviewTicketInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var payload previewTicketPayload
	if err := dec.Decode(&payload); err != nil {
		return nil, ErrPreviewTicketInvalid
	}
	return &payload, nil
}

// signPreviewTicketBody 对票据正文做 HMAC-SHA256
func (a *LocalAuthenticator) signPreviewTicketBody(body string) []byte {
	mac := hmac.New(sha256.New, a.DeriveKey(previewTicketKeyLabel))
	mac.Write([]byte(body))
	return mac.Sum(nil)
}
