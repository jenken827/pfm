package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/dbtest"
	"github.com/jenken827/pfm/utils"
)

// MCP 令牌是长期凭据，这几条不变式必须有测试兜着：
// 明文只在创建时给出、库里只有 sha256、过期/撤销立即失效、前缀错误不查库。

func TestGenerateAndHashMcpToken(t *testing.T) {
	plain, hash, err := GenerateMcpToken()
	if err != nil {
		t.Fatalf("生成令牌失败: %v", err)
	}
	if len(plain) <= len(McpTokenPrefix) || plain[:len(McpTokenPrefix)] != McpTokenPrefix {
		t.Fatalf("令牌应带 %q 前缀，实际 %q", McpTokenPrefix, plain)
	}
	if hash != HashMcpToken(plain) {
		t.Fatalf("返回的 hash 必须等于明文的 sha256")
	}
	// 两次生成不应重复
	second, _, err := GenerateMcpToken()
	if err != nil {
		t.Fatalf("生成令牌失败: %v", err)
	}
	if second == plain {
		t.Fatalf("两次生成的令牌相同，随机源有问题")
	}
}

func TestVerifyMcpToken(t *testing.T) {
	dbtest.Setup(t)

	plain, hash, err := GenerateMcpToken()
	if err != nil {
		t.Fatalf("生成令牌失败: %v", err)
	}
	const owner = "u-mcp-token"
	token := &entity.McpTokenORM{
		OwnerId:      utils.StringPtr(owner),
		Name:         utils.StringPtr("test"),
		TokenHash:    utils.StringPtr(hash),
		AllowRead:    utils.BoolPtr(true),
		AllowWrite:   utils.BoolPtr(false),
		RootFolderId: utils.StringPtr(""),
	}
	if err := db.CreateMcpToken(token); err != nil {
		t.Fatalf("保存令牌失败: %v", err)
	}

	principal, err := VerifyMcpToken(plain)
	if err != nil {
		t.Fatalf("校验应通过: %v", err)
	}
	if principal.UserID != owner || !principal.AllowRead || principal.AllowWrite {
		t.Fatalf("principal 字段不对: %+v", principal)
	}
	if principal.TokenID == 0 {
		t.Fatalf("principal 必须带 token id（审计要用）")
	}

	// 库里存的是 hash，不是明文：拿明文去查应该查不到
	if _, err := db.GetMcpTokenByHash(plain); err == nil {
		t.Fatalf("库里不应存在明文形式的令牌")
	}

	// 前缀不对：直接判无效，不查库
	if _, err := VerifyMcpToken("plain-token"); !errors.Is(err, ErrMcpTokenInvalid) {
		t.Fatalf("无前缀应返回 ErrMcpTokenInvalid，实际 %v", err)
	}
	// 格式对但不存在
	if _, err := VerifyMcpToken(McpTokenPrefix + "not-exists"); !errors.Is(err, ErrMcpTokenNotFound) {
		t.Fatalf("不存在的令牌应返回 ErrMcpTokenNotFound，实际 %v", err)
	}

	// 撤销（物理删除）后立即失效
	if affected, delErr := db.DeleteMcpToken(utils.DerefUint32(token.Id)); delErr != nil || affected != 1 {
		t.Fatalf("撤销令牌失败: affected=%d err=%v", affected, delErr)
	}
	if _, err := VerifyMcpToken(plain); !errors.Is(err, ErrMcpTokenNotFound) {
		t.Fatalf("撤销后应返回 ErrMcpTokenNotFound，实际 %v", err)
	}
}

func TestVerifyMcpTokenExpired(t *testing.T) {
	dbtest.Setup(t)

	plain, hash, err := GenerateMcpToken()
	if err != nil {
		t.Fatalf("生成令牌失败: %v", err)
	}
	expired := time.Now().Add(-time.Minute)
	token := &entity.McpTokenORM{
		OwnerId:    utils.StringPtr("u-mcp-expired"),
		Name:       utils.StringPtr("expired"),
		TokenHash:  utils.StringPtr(hash),
		AllowRead:  utils.BoolPtr(true),
		AllowWrite: utils.BoolPtr(false),
		ExpiresAt:  &expired,
	}
	if err := db.CreateMcpToken(token); err != nil {
		t.Fatalf("保存令牌失败: %v", err)
	}
	if _, err := VerifyMcpToken(plain); !errors.Is(err, ErrMcpTokenExpired) {
		t.Fatalf("过期令牌应返回 ErrMcpTokenExpired，实际 %v", err)
	}
}
