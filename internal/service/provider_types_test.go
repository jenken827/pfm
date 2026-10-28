package service

import (
	"context"
	"testing"

	_ "jenken827.com/jenken827/pfm/driver"
)

// 驱动元数据列表直接喂给前端"新增存储源"的类型下拉框。
// 注册表只保证确定性（driver_id 字典序），"内置驱动排前面"是宿主的展示策略；
// 这里同时锁住两点：内置驱动在前、插件驱动按字典序，且多次调用一致。
func TestGetStorageProviderTypesStableOrder(t *testing.T) {
	first, err := GetStorageProviderTypes(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetStorageProviderTypes err: %v", err)
	}
	if len(first.GetTypes()) == 0 {
		t.Fatal("没有拿到任何驱动类型，driver 注册包是否被 import？")
	}

	drivers := first.GetTypes()
	for i := 1; i < len(drivers); i++ {
		prev, cur := drivers[i-1].GetDriverId(), drivers[i].GetDriverId()
		if prev == cur || driverOrderRank(prev) > driverOrderRank(cur) ||
			(driverOrderRank(prev) == driverOrderRank(cur) && prev > cur) {
			t.Fatalf("驱动列表未按固定顺序：第 %d 项 %s 与第 %d 项 %s",
				i-1, prev, i, cur)
		}
	}

	second, err := GetStorageProviderTypes(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetStorageProviderTypes err: %v", err)
	}
	if len(second.GetTypes()) != len(drivers) {
		t.Fatalf("两次调用数量不一致：%d vs %d", len(drivers), len(second.GetTypes()))
	}
	for i := range drivers {
		if drivers[i].GetDriverId() != second.GetTypes()[i].GetDriverId() {
			t.Fatalf("第 %d 项两次调用顺序不一致：%s vs %s",
				i, drivers[i].GetDriverId(), second.GetTypes()[i].GetDriverId())
		}
	}
}
