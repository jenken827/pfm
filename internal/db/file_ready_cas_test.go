package db_test

import (
	"testing"

	"github.com/google/uuid"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/dbtest"
	"jenken827.com/jenken827/pfm/proto/gen/entity"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

func fileReady(t *testing.T, id string) bool {
	t.Helper()
	f, err := db.GetFileByID(id)
	if err != nil || f == nil {
		t.Fatalf("get file %s: %v", id, err)
	}
	return f.IsReady != nil && *f.IsReady
}

// CAS 语义：version 对不上时静默命中 0 行——这正是"位置行都完成了却显示未就绪"
// 的成因，调用方必须检查返回的行数（dispatch 里的告警就靠它）。
func TestUpdateFileIsReadyVersionCAS(t *testing.T) {
	dbtest.Setup(t)

	parentID := uuid.New().String()
	hash := uuid.New().String()
	fileID := uuid.New().String()
	if err := db.CreateFile(&entity.FileORM{
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
