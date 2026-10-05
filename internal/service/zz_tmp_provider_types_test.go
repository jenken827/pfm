package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	_ "jenken827.com/jenken827/pfm/driver"
)

// TestDumpProviderTypes 打印 GetStorageProviderTypes 的实际返回，用于排查
// 「新增存储提供者后前端类型列表不展示」的问题。
func TestDumpProviderTypes(t *testing.T) {
	data, err := GetStorageProviderTypes(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetStorageProviderTypes err: %v", err)
	}
	fmt.Printf("TOTAL=%d\n", len(data.GetTypes()))
	for _, ti := range data.GetTypes() {
		b, _ := json.Marshal(map[string]any{
			"type":         ti.GetType().String(),
			"name":         ti.GetName(),
			"display_name": ti.GetDisplayName(),
			"icon":         ti.GetIconName(),
			"fields":       len(ti.GetConfigFields()),
		})
		fmt.Println(string(b))
	}
}