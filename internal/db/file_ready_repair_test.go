package db_test

import (
	"testing"

	"github.com/google/uuid"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/dbtest"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

// 造一个 is_ready=false 的文件记录
func createUnreadyFile(t *testing.T, name, hash string, fileType native.FileType) string {
	t.Helper()
	id := uuid.New().String()
	if err := db.CreateFile(&native.FileORM{
		Id:       utils.StringPtr(id),
		Name:     utils.StringPtr(name),
		ParentId: utils.StringPtr(uuid.New().String()),
		OwnerId:  utils.StringPtr(uuid.New().String()),
		Type:     utils.EnumPtr(fileType),
		Hash:     utils.StringPtr(hash),
		Size:     utils.Uint64Ptr(1024),
		IsReady:  utils.BoolPtr(false),
	}); err != nil {
		t.Fatalf("create file %s: %v", name, err)
	}
	return id
}

// 造一个位置行（provider + 状态 + 分片信息）
func createLocation(t *testing.T, hash string, providerID uint32, status native.LocationStatus, chunkNumber, chunkTotal uint32) {
	t.Helper()
	fl := &native.FileLocationORM{
		Blake3Hash:  utils.StringPtr(hash),
		ProviderId:  utils.Uint32Ptr(providerID),
		Status:      utils.EnumPtr(status),
		ChunkNumber: utils.Uint32Ptr(chunkNumber),
		ChunkTotal:  utils.Uint32Ptr(chunkTotal),
		Size:        utils.Uint64Ptr(1024),
	}
	if err := db.CreateFileLocation(fl); err != nil {
		t.Fatalf("create location: %v", err)
	}
}

func fileReady(t *testing.T, id string) bool {
	t.Helper()
	f, err := db.GetFileByID(id)
	if err != nil || f == nil {
		t.Fatalf("get file %s: %v", id, err)
	}
	return f.IsReady != nil && *f.IsReady
}

// 历史上复用旧 upload record 时没刷新 version，分发完成后 cas 命中 0 行，
// 于是「三个 provider 全部 PCOMPLETED」的文件 is_ready 仍是 false。
// 迁移要把这类文件补标就绪，同时不能碰还没传完 / 根本没有位置行的文件。
func TestRepairFileReadyFlags(t *testing.T) {
	dbtest.Setup(t)

	doneHash := uuid.New().String()
	partialHash := uuid.New().String()
	emptyHash := uuid.New().String()

	// 1) 两个 provider 都完整：应被修复
	doneFile := createUnreadyFile(t, "done.jpg", doneHash, native.FileType_IMAGE)
	createLocation(t, doneHash, 1, native.LocationStatus_PCOMPLETED, 0, 1)
	createLocation(t, doneHash, 2, native.LocationStatus_PCOMPLETED, 0, 1)

	// 2) 一个 provider 已完成、另一个仍是 PPENDING：不能动
	partialFile := createUnreadyFile(t, "partial.jpg", partialHash, native.FileType_IMAGE)
	createLocation(t, partialHash, 1, native.LocationStatus_PCOMPLETED, 0, 1)
	createLocation(t, partialHash, 2, native.LocationStatus_PPENDING, 0, 1)

	// 3) 一个位置行都没有（上传还没开始 / 失败）：不能动
	emptyFile := createUnreadyFile(t, "empty.jpg", emptyHash, native.FileType_IMAGE)

	// 4) 分片没传全：3/5 片都已完成也不能算完整
	chunkedHash := uuid.New().String()
	chunkedFile := createUnreadyFile(t, "chunked.bin", chunkedHash, native.FileType_DOCUMENT)
	for i := uint32(0); i < 3; i++ {
		createLocation(t, chunkedHash, 1, native.LocationStatus_PCOMPLETED, i, 5)
	}

	// 5) 目录：没有内容要传，一律就绪
	folderID := createUnreadyFile(t, "目录", "", native.FileType_FOLDER)

	if err := db.RepairFileReadyFlags(); err != nil {
		t.Fatalf("migrateFileReadyFlags: %v", err)
	}

	if !fileReady(t, doneFile) {
		t.Error("位置行全部完成的文件应被补标为就绪")
	}
	if fileReady(t, partialFile) {
		t.Error("仍有 PPENDING 位置行的文件不能标就绪")
	}
	if fileReady(t, emptyFile) {
		t.Error("没有任何位置行的文件不能标就绪")
	}
	if fileReady(t, chunkedFile) {
		t.Error("分片没传全的文件不能标就绪")
	}
	if !fileReady(t, folderID) {
		t.Error("目录应一律为就绪")
	}

	// 幂等：再跑一次不应该报错，也不改变结果
	if err := db.RepairFileReadyFlags(); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !fileReady(t, doneFile) || fileReady(t, partialFile) {
		t.Error("迁移必须是幂等的")
	}
}

// CAS 语义：version 对不上时静默命中 0 行——这正是"位置行都完成了却显示未就绪"
// 的成因，调用方必须检查返回的行数（dispatch 里的告警就靠它）。
func TestUpdateFileIsReadyVersionCAS(t *testing.T) {
	dbtest.Setup(t)

	parentID := uuid.New().String()
	hash := uuid.New().String()
	fileID := uuid.New().String()
	if err := db.CreateFile(&native.FileORM{
		Id:       utils.StringPtr(fileID),
		Name:     utils.StringPtr("cas.jpg"),
		ParentId: utils.StringPtr(parentID),
		OwnerId:  utils.StringPtr(uuid.New().String()),
		Type:     utils.EnumPtr(native.FileType_IMAGE),
		Hash:     utils.StringPtr(hash),
		Size:     utils.Uint64Ptr(10),
		IsReady:  utils.BoolPtr(false),
		Version:  utils.Uint64Ptr(2),
	}); err != nil {
		t.Fatalf("create file: %v", err)
	}

	affected, err := db.UpdateFileIsReady(parentID, "cas.jpg", "", 1)
	if err != nil {
		t.Fatalf("UpdateFileIsReady: %v", err)
	}
	if affected != 0 {
		t.Fatalf("版本不匹配时应命中 0 行, got %d", affected)
	}
	if fileReady(t, fileID) {
		t.Fatal("版本不匹配时不应标就绪")
	}

	affected, err = db.UpdateFileIsReady(parentID, "cas.jpg", "", 2)
	if err != nil {
		t.Fatalf("UpdateFileIsReady: %v", err)
	}
	if affected != 1 {
		t.Fatalf("版本匹配时应命中 1 行, got %d", affected)
	}
	if !fileReady(t, fileID) {
		t.Fatal("版本匹配时应标就绪")
	}
}
