package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/driver/local"
	"github.com/jenken827/pfm/globals"
	"github.com/jenken827/pfm/internal/auth"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/dbtest"
	"github.com/jenken827/pfm/internal/models"
	"github.com/jenken827/pfm/utils"
)

// 位置行只按 blake3 hash 归类：同一份内容会在不同用户之间共享物理副本，所以按 hash
// 查出来的 location 里可能带着别人的 provider。下面这些用例锁定「每个用户只能看到 /
// 只能访问分配给自己的 provider」这条口径。

// scopeTestCtx 构造带用户空间（含分配集）的 ctx。
func scopeTestCtx(userID string, assigned ...uint32) context.Context {
	ctx := context.WithValue(context.Background(), globals.ContextUserKey,
		&auth.Claims{User: auth.User{Id: userID, Name: userID}})
	return context.WithValue(ctx, globals.ContextUserSpaceKey, &models.UserContext{
		UserID:           userID,
		RootFileID:       "root-" + userID,
		BoundProviderIds: assigned,
	})
}

// createScopeTestProvider 建一个本地驱动 provider（测试里不初始化运行时驱动）。
func createScopeTestProvider(t *testing.T, name string) *entity.StorageProviderORM {
	t.Helper()
	p := &entity.StorageProviderORM{
		Name:     utils.StringPtr(name),
		DriverId: utils.StringPtr(local.DriverID),
		Config:   utils.StringPtr(`{"root_path":"/tmp/scope-test"}`),
		Enabled:  utils.BoolPtr(true),
	}
	if err := db.CreateStorageProvider(p); err != nil {
		t.Fatalf("create provider %s failed: %v", name, err)
	}
	return p
}

// createScopeTestFile 建一个文件记录，返回 (fileID, hash)。
func createScopeTestFile(t *testing.T, ownerID, name, hash string) string {
	t.Helper()
	rootID := createTestFolder(t, ownerID, "root-"+ownerID+"-"+uuid.New().String(), "")
	fileID := uuid.New().String()
	file := &entity.FileORM{
		Id:       utils.StringPtr(fileID),
		Name:     utils.StringPtr(name),
		ParentId: utils.StringPtr(rootID),
		OwnerId:  utils.StringPtr(ownerID),
		Type:     utils.EnumPtr(native.FileType_DOCUMENT),
		Size:     utils.Uint64Ptr(8),
		Hash:     utils.StringPtr(hash),
		Mime:     utils.StringPtr("text/plain"),
		IsReady:  utils.BoolPtr(true),
	}
	if err := db.CreateFile(file); err != nil {
		t.Fatalf("create file %s failed: %v", name, err)
	}
	return fileID
}

// createScopeTestLocation 在 (hash, provider) 上建一条已完成的位置行。
func createScopeTestLocation(t *testing.T, hash string, providerID uint32, path string) {
	t.Helper()
	loc := &entity.FileLocationORM{
		Blake3Hash:  utils.StringPtr(hash),
		ProviderId:  utils.Uint32Ptr(providerID),
		Path:        utils.StringPtr(path),
		Status:      utils.Int32Ptr(int32(native.LocationStatus_PCOMPLETED)),
		ChunkNumber: utils.Uint32Ptr(0),
		ChunkTotal:  utils.Uint32Ptr(1),
		Size:        utils.Uint64Ptr(8),
	}
	if err := db.CreateFileLocation(loc); err != nil {
		t.Fatalf("create location failed: %v", err)
	}
}

// TestGetFileDetailHidesProvidersOutsideScope 两个用户共享同一份内容时，详情里的
// 「位置分布」只保留当前用户分配集内的 provider（实例名与物理路径都不许外泄）。
func TestGetFileDetailHidesProvidersOutsideScope(t *testing.T) {
	dbtest.Setup(t)

	pAlice := createScopeTestProvider(t, "alice-disk-"+uuid.New().String())
	pBob := createScopeTestProvider(t, "bob-disk-"+uuid.New().String())

	hash := "scope-detail-" + uuid.New().String()
	hash = hash[:16]
	aliceID := uuid.New().String()
	bobID := uuid.New().String()
	aliceFileID := createScopeTestFile(t, aliceID, "alice.txt", hash)
	bobFileID := createScopeTestFile(t, bobID, "bob.txt", hash)

	createScopeTestLocation(t, hash, *pAlice.Id, "/alice/2026/09/alice_abcd1234.txt")
	createScopeTestLocation(t, hash, *pBob.Id, "/bob/2026/09/bob_efgh5678.txt")

	alice := scopeTestCtx(aliceID, *pAlice.Id)
	data, mErr := GetFileDetail(alice, &native.FileDetailReq{FileId: utils.StringPtr(aliceFileID)})
	if mErr != nil {
		t.Fatalf("GetFileDetail(alice) failed: %v", mErr.E)
	}
	if len(data.GetLocations()) != 1 {
		t.Fatalf("alice 只应看到自己的 1 个 provider，got %d", len(data.GetLocations()))
	}
	loc := data.GetLocations()[0]
	if loc.GetProviderId() != *pAlice.Id {
		t.Fatalf("alice 看到了别人的 provider: %d", loc.GetProviderId())
	}
	if strings.Contains(loc.GetStoragePath(), "bob/") {
		t.Fatalf("alice 看到了别人的物理路径: %q", loc.GetStoragePath())
	}

	bob := scopeTestCtx(bobID, *pBob.Id)
	data, mErr = GetFileDetail(bob, &native.FileDetailReq{FileId: utils.StringPtr(bobFileID)})
	if mErr != nil {
		t.Fatalf("GetFileDetail(bob) failed: %v", mErr.E)
	}
	if len(data.GetLocations()) != 1 || data.GetLocations()[0].GetProviderId() != *pBob.Id {
		t.Fatalf("bob 应只看到自己的 provider，got %+v", data.GetLocations())
	}
}

// TestGetFileStreamRejectsForeignProviderID 显式指定不属于自己的 provider 时必须拒绝，
// 不能把别人的提供者当成下载源。
func TestGetFileStreamRejectsForeignProviderID(t *testing.T) {
	dbtest.Setup(t)

	pMine := createScopeTestProvider(t, "mine-disk-"+uuid.New().String())
	pOther := createScopeTestProvider(t, "other-disk-"+uuid.New().String())
	hash := "scope-stream-" + uuid.New().String()
	hash = hash[:16]

	ownerID := uuid.New().String()
	fileID := createScopeTestFile(t, ownerID, "stream.txt", hash)
	createScopeTestLocation(t, hash, *pOther.Id, "/other/stream.txt")

	ctx := scopeTestCtx(ownerID, *pMine.Id)
	_, err := GetFileStream(ctx, fileID, pOther.Id)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("越权 provider 应被拒绝，got err=%v", err)
	}

	// 自己分配集内的 provider 不做拦截（这里会因驱动未初始化而失败，但不是越权错误）
	if _, err := GetFileStream(ctx, fileID, pMine.Id); err != nil &&
		strings.Contains(err.Error(), "不在你的可用范围内") {
		t.Fatalf("自己的 provider 不应被判越权: %v", err)
	}
}

// TestGetFileDirectLinkRejectsForeignProviderID 直链同样只能在自己的 provider 上取。
func TestGetFileDirectLinkRejectsForeignProviderID(t *testing.T) {
	dbtest.Setup(t)

	pMine := createScopeTestProvider(t, "mine-link-"+uuid.New().String())
	pOther := createScopeTestProvider(t, "other-link-"+uuid.New().String())
	hash := "scope-link-" + uuid.New().String()
	hash = hash[:16]

	ownerID := uuid.New().String()
	fileID := createScopeTestFile(t, ownerID, "link.txt", hash)
	createScopeTestLocation(t, hash, *pOther.Id, "/other/link.txt")

	ctx := scopeTestCtx(ownerID, *pMine.Id)
	_, mErr := GetFileDirectLink(ctx, &native.DirectLinkFileReq{
		FileId:     utils.StringPtr(fileID),
		ProviderId: pOther.Id,
	})
	if mErr == nil || mErr.C != native.RespCode_PERMISSION_DENIED {
		t.Fatalf("越权 provider 应返回 PERMISSION_DENIED，got %+v", mErr)
	}
}

// TestGetFileCacheStatusRejectsForeignProvider hash 全局共享，别人的提供者上也可能有
// 同一份内容的位置行；缓存状态查询不能跨用户读。
func TestGetFileCacheStatusRejectsForeignProvider(t *testing.T) {
	dbtest.Setup(t)

	pMine := createScopeTestProvider(t, "mine-cache-"+uuid.New().String())
	pOther := createScopeTestProvider(t, "other-cache-"+uuid.New().String())
	hash := "scope-cache-" + uuid.New().String()
	hash = hash[:16]

	ownerID := uuid.New().String()
	createScopeTestFile(t, ownerID, "cache.txt", hash)
	createScopeTestLocation(t, hash, *pOther.Id, "/other/cache.txt")

	ctx := scopeTestCtx(ownerID, *pMine.Id)
	_, mErr := GetFileCacheStatus(ctx, &native.GetFileCacheStatusReq{
		FileHash:   utils.StringPtr(hash),
		ProviderId: pOther.Id,
	})
	if mErr == nil || mErr.C != native.RespCode_PERMISSION_DENIED {
		t.Fatalf("越权 provider 应返回 PERMISSION_DENIED，got %+v", mErr)
	}

	// 自己的 provider 上即使没有位置行，也应是"查不到记录"而不是越权
	if _, mErr := GetFileCacheStatus(ctx, &native.GetFileCacheStatusReq{
		FileHash:   utils.StringPtr(hash),
		ProviderId: pMine.Id,
	}); mErr != nil && mErr.C == native.RespCode_PERMISSION_DENIED {
		t.Fatalf("自己的 provider 不应被判越权: %+v", mErr)
	}
}

// TestGetUploadRecordDetailHidesProvidersOutsideScope 传输队列的落地位置同样只下发
// 当前用户分配集内的 provider。
func TestGetUploadRecordDetailHidesProvidersOutsideScope(t *testing.T) {
	dbtest.Setup(t)

	pMine := createScopeTestProvider(t, "mine-record-"+uuid.New().String())
	pOther := createScopeTestProvider(t, "other-record-"+uuid.New().String())
	hash := "scope-rec-" + uuid.New().String()
	hash = hash[:16]

	ownerID := uuid.New().String()
	record := &entity.UploadRecordORM{
		FileName:          utils.StringPtr("record.txt"),
		FileSize:          utils.Uint64Ptr(8),
		ParentId:          utils.StringPtr(""),
		FileHash:          utils.StringPtr(hash),
		TargetProviderIds: utils.StringPtr("[" + u32ToStr(*pMine.Id) + "]"),
		TempPath:          utils.StringPtr(""),
		Status:            utils.EnumPtr(native.LocationStatus_PCOMPLETED),
		TaskStatus:        utils.EnumPtr(native.UploadStatus_COMPLETED),
		TaskType:          utils.EnumPtr(native.UploadTaskType_UPLOAD),
		OwnerId:           utils.StringPtr(ownerID),
	}
	if err := db.CreateUploadRecord(record); err != nil {
		t.Fatalf("create upload record failed: %v", err)
	}

	createScopeTestLocation(t, hash, *pMine.Id, "/mine/record.txt")
	createScopeTestLocation(t, hash, *pOther.Id, "/other/record.txt")

	resp, mErr := GetUploadRecordDetail(scopeTestCtx(ownerID, *pMine.Id),
		&native.GetUploadRecordDetailReq{Id: record.Id})
	if mErr != nil {
		t.Fatalf("GetUploadRecordDetail failed: %v", mErr.E)
	}
	if len(resp.GetLocations()) != 1 {
		t.Fatalf("只应下发自己的 1 个 provider，got %d", len(resp.GetLocations()))
	}
	if resp.GetLocations()[0].GetProviderId() != *pMine.Id {
		t.Fatalf("下发了别人的 provider: %d", resp.GetLocations()[0].GetProviderId())
	}
}

// u32ToStr 小工具：拼 target_provider_ids 用
func u32ToStr(v uint32) string {
	if v == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
