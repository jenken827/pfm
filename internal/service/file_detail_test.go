package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/driver/local"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/dbtest"
	"github.com/jenken827/pfm/utils"
)

// 文件详情「位置分布」里的 provider 名必须是**存储实例名**（storage_providers.name），
// 而不是驱动类型名——各驱动的 GetName() 返回的是类型字符串（如 "LOCAL"），
// 同类型多实例时无法区分，用户也就无法判断该从哪个源下载。
//
// 本用例不初始化运行时驱动（storage.GetStorage 返回 nil），因此也顺带验证了
// 实例名来自 DB 而不是 driver。
func TestGetFileDetailUsesProviderInstanceName(t *testing.T) {
	dbtest.Setup(t)
	userID := uuid.New().String()
	rootID := createTestFolder(t, userID, "root-"+userID, "")

	// 实例名刻意与驱动类型名（LOCAL）不同，便于发现"用错来源"
	const instanceName = "我的本地盘"
	provider := &entity.StorageProviderORM{
		Name:     utils.StringPtr(instanceName),
		DriverId: utils.StringPtr(local.DriverID),
		Config:   utils.StringPtr(`{"root_path":"/tmp/x"}`),
		Enabled:  utils.BoolPtr(true),
	}
	if err := db.CreateStorageProvider(provider); err != nil {
		t.Fatalf("create provider failed: %v", err)
	}
	if provider.Id == nil {
		t.Fatal("provider id 未被回填，测试无法继续")
	}

	// 文件 + 该 provider 上一条已完成的位置行
	const hash = "deadbeefdeadbeef"
	fileID := uuid.New().String()
	file := &entity.FileORM{
		Id:       utils.StringPtr(fileID),
		Name:     utils.StringPtr("a.txt"),
		ParentId: utils.StringPtr(rootID),
		OwnerId:  utils.StringPtr(userID),
		Type:     utils.EnumPtr(native.FileType_DOCUMENT),
		Size:     utils.Uint64Ptr(3),
		Hash:     utils.StringPtr(hash),
		Mime:     utils.StringPtr("text/plain"),
		IsReady:  utils.BoolPtr(true),
	}
	if err := db.CreateFile(file); err != nil {
		t.Fatalf("create file failed: %v", err)
	}
	loc := &entity.FileLocationORM{
		Blake3Hash:  utils.StringPtr(hash),
		ProviderId:  provider.Id,
		Path:        utils.StringPtr("/tmp/x/a.txt"),
		Status:      utils.Int32Ptr(int32(native.LocationStatus_PCOMPLETED)),
		ChunkNumber: utils.Uint32Ptr(0),
		ChunkTotal:  utils.Uint32Ptr(1),
		Size:        utils.Uint64Ptr(3),
	}
	if err := db.CreateFileLocation(loc); err != nil {
		t.Fatalf("create location failed: %v", err)
	}

	data, mErr := GetFileDetail(webdavTestCtx(userID),
		&native.FileDetailReq{FileId: utils.StringPtr(fileID)})
	if mErr != nil {
		t.Fatalf("GetFileDetail failed: %v", mErr.E)
	}
	if len(data.GetLocations()) != 1 {
		t.Fatalf("want 1 location, got %d", len(data.GetLocations()))
	}
	got := data.GetLocations()[0]
	if got.GetProviderName() != instanceName {
		t.Errorf("ProviderName = %q, want 实例名 %q（不能用驱动类型名）",
			got.GetProviderName(), instanceName)
	}
	if got.GetDriverId() != local.DriverID {
		t.Errorf("DriverId = %q, want %s", got.GetDriverId(), local.DriverID)
	}
	if got.GetStatus() != native.LocationStatus_PCOMPLETED {
		t.Errorf("Status = %v, want PCOMPLETED", got.GetStatus())
	}
}
