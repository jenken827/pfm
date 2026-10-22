package service

import (
	"context"
	"testing"

	_ "jenken827.com/jenken827/pfm/driver"
)

// 驱动元数据列表直接喂给前端"新增存储源"的类型下拉框。
// 早先 GetAllDriverMetadata 直接遍历注册表 map，顺序每次调用都不同：
// 同一个用户在两次打开对话框时看到的第一项（默认选中项）会变，
// 看起来像列表在乱跳。这里锁住"按枚举值升序且多次调用一致"。
func TestGetStorageProviderTypesStableOrder(t *testing.T) {
	first, err := GetStorageProviderTypes(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetStorageProviderTypes err: %v", err)
	}
	if len(first.GetTypes()) == 0 {
		t.Fatal("没有拿到任何驱动类型，driver 注册包是否被 import？")
	}

	types := first.GetTypes()
	for i := 1; i < len(types); i++ {
		if types[i-1].GetType() >= types[i].GetType() {
			t.Fatalf("类型列表未按枚举值升序：第 %d 项 %s >= 第 %d 项 %s",
				i-1, types[i-1].GetType(), i, types[i].GetType())
		}
	}

	second, err := GetStorageProviderTypes(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetStorageProviderTypes err: %v", err)
	}
	if len(second.GetTypes()) != len(types) {
		t.Fatalf("两次调用数量不一致：%d vs %d", len(types), len(second.GetTypes()))
	}
	for i := range types {
		if types[i].GetType() != second.GetTypes()[i].GetType() {
			t.Fatalf("第 %d 项两次调用顺序不一致：%s vs %s",
				i, types[i].GetType(), second.GetTypes()[i].GetType())
		}
	}
}
