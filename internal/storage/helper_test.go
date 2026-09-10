package storage

import (
	"testing"

	"jenken827.com/jenken827/pfm/proto/gen/native"
)

func int32Ptr(v int32) *int32    { return &v }
func uint32Ptr(v uint32) *uint32 { return &v }

// mkPendingLocation 构造一行「位置行预建」产物：PPENDING、chunk_total 为 nil。
// 这正是 PreallocateFileLocations/getOrCreateFileLocation 在分发完成前写入的形态。
func mkPendingLocation(providerID uint32) *native.FileLocationORM {
	return &native.FileLocationORM{
		ProviderId: uint32Ptr(providerID),
		Status:     int32Ptr(int32(native.LocationStatus_PPENDING)),
	}
}

// TestPendingLocationIsIncomplete 钉住「位置行预建」所依赖的判定语义：
//
// 文件记录切换到新 hash 时，新 hash 只有预建的 PPENDING 行（chunk_total 为 nil）。
// 读取端必须把这种状态判为「无可用 provider」——它落在 service.getFileStream 的
// 降级分支（读本地 staging 明文 / 短轮询等待 provider 完成）；
// 若被判为「完整副本」，读取端会去 open 一个根本不存在的远端文件；
// 若在此 panic，则整个读取路径 500（旧实现直接解引用 *chunk_total）。
func TestPendingLocationIsIncomplete(t *testing.T) {
	locations := []*native.FileLocationORM{
		mkPendingLocation(2),
		mkPendingLocation(4),
	}

	t.Run("CheckProviderComplete 判为不完整且不 panic", func(t *testing.T) {
		complete, expected, actual := CheckProviderComplete([]*native.FileLocationORM{mkPendingLocation(2)})
		if complete {
			t.Fatalf("预建的 PPENDING 行不得被判为完整副本")
		}
		if expected != 1 || actual != 1 {
			t.Fatalf("want (1,1), got (%d,%d)", expected, actual)
		}
	})

	t.Run("GetCompleteProviders 为空", func(t *testing.T) {
		providers, err := GetCompleteProviders(locations)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(providers) != 0 {
			t.Fatalf("预建行不得产生完整提供者，got %v", providers)
		}
	})

	t.Run("SelectBestSourceProvider 返回无可用 provider", func(t *testing.T) {
		// 读取端据此进入降级路径（staging 直读），而不是走「无位置行」的硬 500
		if _, _, err := SelectBestSourceProvider(locations); err == nil {
			t.Fatalf("只有预建行时必须报错，交由调用方降级")
		}
	})
}

// TestCompleteLocationStillWins 保证判定没有被「预建行」逻辑误伤：
// 同一 hash 下存在已完成行时，仍能选出该 provider（含混合形态：预建行 + 完成行）。
func TestCompleteLocationStillWins(t *testing.T) {
	locations := []*native.FileLocationORM{
		mkPendingLocation(4),
		{
			ProviderId:  uint32Ptr(2),
			Status:      int32Ptr(int32(native.LocationStatus_PCOMPLETED)),
			ChunkTotal:  uint32Ptr(1),
			ChunkNumber: uint32Ptr(0),
		},
	}

	providers, err := GetCompleteProviders(locations)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(providers) != 1 || providers[0] != 2 {
		t.Fatalf("want [2], got %v", providers)
	}
}
