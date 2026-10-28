package service

import (
	"context"
	"testing"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/dbtest"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/utils"
)

// 普通用户可达的两个接口（我的存储源 / 同步源）不能把驱动配置原文发出去：
// config 里有 bot_token / refresh_token / client_secret，encryption_key 是解密密钥。
func TestUserFacingProviderResponsesDropSecrets(t *testing.T) {
	dbtest.Setup(t)

	const userID = "u-secrets-1"
	const providerID uint32 = 777
	// 绑定该 provider，并把它标成本地驱动（构造器存在，ResolveProviderIds 才认）
	sp := &entity.StorageProviderORM{
		Id:            utils.Uint32Ptr(providerID),
		Name:          utils.StringPtr("secret-provider"),
		DriverId:      utils.StringPtr("local"),
		Config:        utils.StringPtr(`{"bot_token":"t","refresh_token":"r","client_secret":"s"}`),
		EncryptionKey: utils.StringPtr("KEY-MUST-NOT-LEAK"),
		Enabled:       utils.BoolPtr(true),
		Proxy:         utils.StringPtr(""),
	}
	if err := db.CreateStorageProvider(sp); err != nil {
		t.Fatalf("create provider failed: %v", err)
	}

	ctx := context.WithValue(context.Background(), globals.ContextUserKey, &auth.Claims{
		User: auth.User{Id: userID, Name: "u"},
	})
	ctx = context.WithValue(ctx, globals.ContextUserSpaceKey, &models.UserContext{
		UserID:           userID,
		RootFileID:       "root-of-" + userID,
		BoundProviderIds: []uint32{providerID},
	})

	bound, mErr := GetMyBoundProviders(ctx)
	if mErr != nil {
		t.Fatalf("GetMyBoundProviders err: %v", mErr)
	}
	if len(bound.GetProviders()) != 1 {
		t.Fatalf("应返回绑定的 1 个 provider，got %d", len(bound.GetProviders()))
	}
	p := bound.GetProviders()[0]
	if p.GetConfig() != "" || p.GetEncryptionKey() != "" {
		t.Fatalf("我的存储源泄露了驱动配置：config=%q key=%q", p.GetConfig(), p.GetEncryptionKey())
	}
	if p.GetName() != "secret-provider" {
		t.Fatalf("非敏感字段应保留，name=%q", p.GetName())
	}
}

// 同一份数据走管理接口（provider 详情）必须仍然带 config：管理员要用它编辑配置。
func TestStripProviderSecretsKeepsAdminPath(t *testing.T) {
	pb := &native.StorageProvider{
		Config:        utils.StringPtr(`{"bot_token":"t"}`),
		EncryptionKey: utils.StringPtr("K"),
		Name:          utils.StringPtr("n"),
	}
	stripProviderSecrets(pb)
	if pb.GetConfig() != "" || pb.GetEncryptionKey() != "" {
		t.Fatal("stripProviderSecrets 应清空 config / encryptionKey")
	}
	if pb.GetName() != "n" {
		t.Fatal("stripProviderSecrets 不应动其它字段")
	}
}
