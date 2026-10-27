package service

import (
	"context"
	"testing"

	_ "jenken827.com/jenken827/pfm/driver"
	"jenken827.com/jenken827/pfm/driver/types"
)

// 驱动元数据列表直接喂给前端"新增存储源"的类型下拉框。
// 早先 GetAllDriverMetadata 直接遍历注册表 map，顺序每次调用都不同：
// 同一个用户在两次打开对话框时看到的第一项（默认选中项）会变，
// 看起来像列表在乱跳。这里锁住"按内置驱动固定顺序且多次调用一致"。
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
		if prev == cur || types.DriverRank(prev) > types.DriverRank(cur) {
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
