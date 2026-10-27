package db_test

import (
	"sort"
	"testing"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/dbtest"
	"jenken827.com/jenken827/pfm/proto/gen/entity"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

// TestGetFileLocationPathsByProvider 只返回指定 provider 的 path，且去重、
// 不按 path 过滤状态（远端孤儿扫描要保守：任何位置行存在都算「有记录」）。
func TestGetFileLocationPathsByProvider(t *testing.T) {
	dbtest.Setup(t)

	create := func(providerID uint32, hash, path string, status native.LocationStatus) {
		t.Helper()
		fl := &entity.FileLocationORM{
			Blake3Hash: utils.StringPtr(hash),
			ProviderId: utils.Uint32Ptr(providerID),
			Path:       utils.StringPtr(path),
			Status:     utils.EnumPtr(status),
			Size:       utils.Uint64Ptr(1),
		}
		if err := db.CreateFileLocation(fl); err != nil {
			t.Fatalf("create location: %v", err)
		}
	}

	create(1, "h1", "/2026/01/a.jpg", native.LocationStatus_PCOMPLETED)
	create(1, "h2", "/2026/01/b.jpg", native.LocationStatus_PPENDING)
	create(1, "h3", "/2026/01/a.jpg", native.LocationStatus_PFAILED) // 同 path 不同行，应去重
	create(2, "h4", "/2026/01/other.jpg", native.LocationStatus_PCOMPLETED)

	paths, err := db.GetFileLocationPathsByProvider(1)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	sort.Strings(paths)
	want := []string{"/2026/01/a.jpg", "/2026/01/b.jpg"}
	if len(paths) != len(want) {
		t.Fatalf("provider 1 应返回 %d 条 path, got %d (%v)", len(want), len(paths), paths)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("paths[%d] = %q, want %q", i, paths[i], want[i])
		}
	}

	// provider 0 / 不存在的 provider 返回空，便于调用方直接遍历
	empty, err := db.GetFileLocationPathsByProvider(0)
	if err != nil || len(empty) != 0 {
		t.Fatalf("providerID 0 应返回空: %v %v", empty, err)
	}
	none, err := db.GetFileLocationPathsByProvider(999)
	if err != nil || len(none) != 0 {
		t.Fatalf("不存在的 provider 应返回空: %v %v", none, err)
	}
}
