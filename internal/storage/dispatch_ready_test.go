package storage

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/dbtest"
	"github.com/jenken827/pfm/utils"
)

// TestDispatchMarksReadyWhenContentAlreadyStored 「内容已在 provider 里」的捷径分支
// 也必须把文件记录标为就绪。
//
// 触发场景：同一份内容以不同文件名再入库（离线下载改名、重复上传）。
// 位置行按 hash 共享且早已 PCOMPLETED，分发直接走 completeUpload 返回，
// 谁都不去置 is_ready —— 新记录就永远停在"未就绪"，前端无法预览 / 下载。
func TestDispatchMarksReadyWhenContentAlreadyStored(t *testing.T) {
	dbtest.Setup(t)

	ownerID := uuid.New().String()
	parentID := uuid.New().String()
	hash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	name := "same-content-new-name.mp4"

	fileID := uuid.New().String()
	if err := db.CreateFile(&entity.FileORM{
		Id:       utils.StringPtr(fileID),
		Name:     utils.StringPtr(name),
		ParentId: utils.StringPtr(parentID),
		OwnerId:  utils.StringPtr(ownerID),
		Type:     utils.EnumPtr(native.FileType_VIDEO),
		Hash:     utils.StringPtr(hash),
		Size:     utils.Uint64Ptr(1),
		IsReady:  utils.BoolPtr(false),
		Version:  utils.Uint64Ptr(0),
	}); err != nil {
		t.Fatalf("create file: %v", err)
	}

	// 该 hash 在 provider 1 上已有完整副本（单分片、PCOMPLETED、chunk_total 为空 → 旧数据形态）
	if err := db.CreateFileLocation(&entity.FileLocationORM{
		ProviderId: uint32Ptr(1),
		Blake3Hash: utils.StringPtr(hash),
		Status:     int32Ptr(int32(native.LocationStatus_PCOMPLETED)),
		ChunkTotal: uint32Ptr(1),
	}); err != nil {
		t.Fatalf("create location: %v", err)
	}

	ur := &entity.UploadRecordORM{
		FileHash: utils.StringPtr(hash),
		FileName: utils.StringPtr(name),
		ParentId: utils.StringPtr(parentID),
		OwnerId:  utils.StringPtr(ownerID),
		TempPath: utils.StringPtr(t.TempDir()),
		Version:  utils.Uint64Ptr(0),
	}
	dispatchToProviders(context.Background(), ur, []uint32{1}, false)

	got, err := db.GetFileByID(fileID)
	if err != nil {
		t.Fatalf("reload file: %v", err)
	}
	if got.IsReady == nil || !*got.IsReady {
		t.Fatalf("内容已在 provider 里时也必须标为就绪, is_ready=%v", got.IsReady)
	}
}
