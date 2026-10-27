package service

import (
	"testing"

	"github.com/google/uuid"
	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/dbtest"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

// directLinkLocation 构造一条位置行
func directLinkLocation(providerID uint32, encrypted bool) *native.FileLocationORM {
	return &native.FileLocationORM{
		ProviderId:  utils.Uint32Ptr(providerID),
		Encrypted:   utils.BoolPtr(encrypted),
		Status:      utils.EnumPtr(native.LocationStatus_PCOMPLETED),
		ChunkNumber: utils.Uint32Ptr(0),
		ChunkTotal:  utils.Uint32Ptr(1),
	}
}

// TestDirectLinkIneligibleReason 资格判定的硬性口径：单分片 + 未加密
func TestDirectLinkIneligibleReason(t *testing.T) {
	cases := []struct {
		name      string
		locations []*native.FileLocationORM
		wantEmpty bool
	}{
		{"单分片明文", []*native.FileLocationORM{directLinkLocation(1, false)}, true},
		{"单分片加密", []*native.FileLocationORM{directLinkLocation(1, true)}, false},
		{"多分片明文", []*native.FileLocationORM{directLinkLocation(1, false), directLinkLocation(1, false)}, false},
		{"零副本", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reason := directLinkIneligibleReason(c.locations)
			if (reason == "") != c.wantEmpty {
				t.Fatalf("directLinkIneligibleReason = %q, wantEmpty = %v", reason, c.wantEmpty)
			}
		})
	}
}

// TestDirectLinkEnabledDefaultOn 总开关默认开（nil = 启用）
func TestDirectLinkEnabledDefaultOn(t *testing.T) {
	dbtest.Setup(t)
	dl := &config.GetConfig().Transfer.DirectLink
	if !directLinkEnabled() {
		t.Fatal("默认（enabled 为 nil）应视为启用")
	}
	off := false
	dl.Enabled = &off
	if directLinkEnabled() {
		t.Fatal("enabled=false 时应关闭")
	}
	dl.Enabled = nil
}

// TestDirectLinkProviderAllowedWhitelist 白名单按**提供者类型**过滤，留空 = 不限制
func TestDirectLinkProviderAllowedWhitelist(t *testing.T) {
	dbtest.Setup(t)
	provider := &native.StorageProviderORM{
		Name:     utils.StringPtr("123盘"),
		DriverId: utils.StringPtr(types.DriverPan123),
		Config:   utils.StringPtr(`{}`),
		Enabled:  utils.BoolPtr(true),
	}
	if err := db.CreateStorageProvider(provider); err != nil {
		t.Fatalf("create provider failed: %v", err)
	}
	dl := &config.GetConfig().Transfer.DirectLink

	dl.Providers = nil
	if !directLinkProviderAllowed(*provider.Id) {
		t.Fatal("白名单为空应放行所有提供者")
	}

	dl.Providers = []string{"PAN123"}
	if !directLinkProviderAllowed(*provider.Id) {
		t.Fatal("白名单命中类型时应放行")
	}

	dl.Providers = []string{"BAIDU_NETDISK"}
	if directLinkProviderAllowed(*provider.Id) {
		t.Fatal("白名单未命中类型时应拒绝")
	}

	dl.Providers = []string{"PAN123"}
	if directLinkProviderAllowed(999999) {
		t.Fatal("不存在的提供者不应放行")
	}
	dl.Providers = nil
}

// TestGetFileDirectLinkUnavailable 不可直链的文件返回 DIRECT_LINK_UNAVAILABLE（客户端据此回退代理）
func TestGetFileDirectLinkUnavailable(t *testing.T) {
	dbtest.Setup(t)
	userID := uuid.New().String()
	rootID := createTestFolder(t, userID, "root-"+userID, "")

	provider := &native.StorageProviderORM{
		Name:     utils.StringPtr("移动云"),
		DriverId: utils.StringPtr(types.DriverYun139),
		Config:   utils.StringPtr(`{}`),
		Enabled:  utils.BoolPtr(true),
	}
	if err := db.CreateStorageProvider(provider); err != nil {
		t.Fatalf("create provider failed: %v", err)
	}

	const hash = "directlinkhash0001"
	fileID := uuid.New().String()
	file := &native.FileORM{
		Id:       utils.StringPtr(fileID),
		Name:     utils.StringPtr("secret.enc"),
		ParentId: utils.StringPtr(rootID),
		OwnerId:  utils.StringPtr(userID),
		Type:     utils.EnumPtr(native.FileType_DOCUMENT),
		Size:     utils.Uint64Ptr(11),
		Hash:     utils.StringPtr(hash),
		IsReady:  utils.BoolPtr(true),
	}
	if err := db.CreateFile(file); err != nil {
		t.Fatalf("create file failed: %v", err)
	}
	loc := &native.FileLocationORM{
		Blake3Hash:  utils.StringPtr(hash),
		ProviderId:  provider.Id,
		Path:        utils.StringPtr("/x/secret.enc"),
		Status:      utils.EnumPtr(native.LocationStatus_PCOMPLETED),
		ChunkNumber: utils.Uint32Ptr(0),
		ChunkTotal:  utils.Uint32Ptr(1),
		Size:        utils.Uint64Ptr(11),
		Encrypted:   utils.BoolPtr(true),
	}
	if err := db.CreateFileLocation(loc); err != nil {
		t.Fatalf("create location failed: %v", err)
	}

	ctx := webdavTestCtx(userID)
	_, mErr := GetFileDirectLink(ctx, &native.DirectLinkFileReq{FileId: utils.StringPtr(fileID)})
	if mErr == nil {
		t.Fatal("加密文件不应返回直链")
	}
	if mErr.C != native.RespCode_DIRECT_LINK_UNAVAILABLE {
		t.Fatalf("业务码 = %v, want DIRECT_LINK_UNAVAILABLE", mErr.C)
	}

	// 非属主：权限拒绝（先于任何取链动作）
	otherCtx := webdavTestCtx(uuid.New().String())
	_, mErr = GetFileDirectLink(otherCtx, &native.DirectLinkFileReq{FileId: utils.StringPtr(fileID)})
	if mErr == nil || mErr.C != native.RespCode_PERMISSION_DENIED {
		t.Fatalf("非属主应返回 PERMISSION_DENIED, got %v", mErr)
	}

	// 缺少 file_id：参数错误
	_, mErr = GetFileDirectLink(ctx, &native.DirectLinkFileReq{})
	if mErr == nil || mErr.C != native.RespCode_PARAMS_ERROR {
		t.Fatalf("缺少 file_id 应返回 PARAMS_ERROR, got %v", mErr)
	}
}
