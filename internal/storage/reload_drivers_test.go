package storage

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	abi "github.com/jenken827/pfm-plugin-sdk/proto/gen/abi"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/dbtest"
	"jenken827.com/jenken827/pfm/utils"
)

// markerDriver 只用来区分「重建前 / 重建后」的实例，不碰文件系统。
type markerDriver struct {
	tag string
}

func (d *markerDriver) GetType() string                          { return "reloadtest" }
func (d *markerDriver) GetName() string                          { return d.tag }
func (d *markerDriver) GetConfig() any                           { return nil }
func (d *markerDriver) GetProviderConfig() *types.ProviderConfig { return &types.ProviderConfig{} }

func (d *markerDriver) Upload(context.Context, types.UploadSource, types.UploadOptions) ([]types.FileRef, error) {
	return nil, fmt.Errorf("not implemented")
}

func (d *markerDriver) Open(context.Context, []types.FileRef, types.OpenOptions) (types.ReadSeekCloser, error) {
	return nil, fmt.Errorf("not implemented")
}

// TestReloadDriversRebuildsInstances 插件热加载后，受影响驱动的 provider 实例必须被重建：
// 旧实例握着的 loopback 连接随旧插件进程一起失效，不重建就等于「provider 看着可用、调用全失败」。
func TestReloadDriversRebuildsInstances(t *testing.T) {
	dbtest.Setup(t)

	const driverID = "reloadtest"
	providerID := utils.Uint32Ptr(5151)
	if err := db.CreateStorageProvider(&entity.StorageProviderORM{
		Id:       providerID,
		Name:     utils.StringPtr("reload-test"),
		DriverId: utils.StringPtr(driverID),
		Config:   utils.StringPtr("{}"),
		Enabled:  utils.BoolPtr(true),
	}); err != nil {
		t.Fatalf("创建 provider 失败: %v", err)
	}
	t.Cleanup(func() { types.ClearDriverOverride(driverID) })

	// 内置注册：模拟「插件加载前」的实例
	types.RegisterStorageDriverWithMetadata(func(*types.Provider) (types.StorageDriver, error) {
		return &markerDriver{tag: "before"}, nil
	}, &abi.DriverInfo{DriverId: proto.String(driverID)})

	sps, err := db.GetEnabledProviders()
	if err != nil {
		t.Fatalf("读取 provider 失败: %v", err)
	}
	if len(sps) != 1 {
		t.Fatalf("want 1 provider, got %d", len(sps))
	}
	if err := createAndStoreDriver(sps[0]); err != nil {
		t.Fatalf("初始化实例失败: %v", err)
	}
	if got := GetStorage(*providerID).(*markerDriver).tag; got != "before" {
		t.Fatalf("初始实例应为 before, got %s", got)
	}

	// 模拟插件热加载：覆盖层换成新构造器
	types.SetDriverOverride(func(*types.Provider) (types.StorageDriver, error) {
		return &markerDriver{tag: "after"}, nil
	}, &abi.DriverInfo{DriverId: proto.String(driverID)})

	rebuilt, err := ReloadDrivers(map[string]struct{}{driverID: {}})
	if err != nil {
		t.Fatalf("重建失败: %v", err)
	}
	if rebuilt != 1 {
		t.Fatalf("want 1 rebuilt, got %d", rebuilt)
	}
	if got := GetStorage(*providerID).(*markerDriver).tag; got != "after" {
		t.Fatalf("重建后实例应为 after, got %s", got)
	}
}

// TestReloadDriversIgnoresUntouchedDrivers 只重建受影响的驱动身份，别的 provider 实例保持原样
// （重建代价是一次 plugin Configure，不该波及所有 provider）。
func TestReloadDriversIgnoresUntouchedDrivers(t *testing.T) {
	dbtest.Setup(t)

	const otherDriverID = "reloadtest-other"
	t.Cleanup(func() { types.ClearDriverOverride(otherDriverID) })

	otherID := utils.Uint32Ptr(6161)
	if err := db.CreateStorageProvider(&entity.StorageProviderORM{
		Id:       otherID,
		Name:     utils.StringPtr("other"),
		DriverId: utils.StringPtr(otherDriverID),
		Config:   utils.StringPtr("{}"),
		Enabled:  utils.BoolPtr(true),
	}); err != nil {
		t.Fatalf("创建 provider 失败: %v", err)
	}
	types.SetDriverOverride(func(*types.Provider) (types.StorageDriver, error) {
		return &markerDriver{tag: "other"}, nil
	}, &abi.DriverInfo{DriverId: proto.String(otherDriverID)})
	if err := createAndStoreDriver(&entity.StorageProviderORM{
		Id:       otherID,
		DriverId: utils.StringPtr(otherDriverID),
		Config:   utils.StringPtr("{}"),
	}); err != nil {
		t.Fatalf("初始化实例失败: %v", err)
	}
	before := GetStorage(*otherID)

	rebuilt, err := ReloadDrivers(map[string]struct{}{"reloadtest-nonexistent": {}})
	if err != nil {
		t.Fatalf("重建失败: %v", err)
	}
	if rebuilt != 0 {
		t.Fatalf("没有匹配的 provider 时不应重建, got %d", rebuilt)
	}
	if GetStorage(*otherID) != before {
		t.Fatal("未受影响的实例不应被替换")
	}
}
