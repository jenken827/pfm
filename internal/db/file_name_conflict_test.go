package db_test

import (
	"testing"

	"github.com/google/uuid"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/dbtest"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

// TestResolveNameConflict 同名冲突自动改名：无冲突返回空串，冲突时依次尝试 `原名 (N).扩展名`。
// 判定走 (owner_id,name,parent_id) 的部分唯一索引，回收站（软删）里的同名文件不算冲突。
func TestResolveNameConflict(t *testing.T) {
	dbtest.Setup(t)
	ownerID := uuid.New().String()
	parentID := uuid.New().String()

	// 目录里先放 a.txt
	create := func(name string) string {
		t.Helper()
		id := uuid.New().String()
		err := db.CreateFile(&native.FileORM{
			Id:       utils.StringPtr(id),
			Name:     utils.StringPtr(name),
			ParentId: utils.StringPtr(parentID),
			OwnerId:  utils.StringPtr(ownerID),
			Type:     utils.EnumPtr(native.FileType_DOCUMENT),
			Size:     utils.Uint64Ptr(1),
			IsReady:  utils.BoolPtr(true),
		})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return id
	}

	if got := db.ResolveNameConflict(parentID, "a.txt", ownerID); got != "" {
		t.Fatalf("无冲突时应返回空串, got %q", got)
	}

	create("a.txt")
	if got := db.ResolveNameConflict(parentID, "a.txt", ownerID); got != "a (1).txt" {
		t.Fatalf("第一次冲突应为 a (1).txt, got %q", got)
	}

	create("a (1).txt")
	if got := db.ResolveNameConflict(parentID, "a.txt", ownerID); got != "a (2).txt" {
		t.Fatalf("第二次冲突应为 a (2).txt, got %q", got)
	}

	// 无扩展名 / 多点扩展名也要能拆
	create("README")
	if got := db.ResolveNameConflict(parentID, "README", ownerID); got != "README (1)" {
		t.Fatalf("无扩展名冲突应为 README (1), got %q", got)
	}
	create("archive.tar.gz")
	if got := db.ResolveNameConflict(parentID, "archive.tar.gz", ownerID); got != "archive.tar (1).gz" {
		t.Fatalf("多点扩展名按最后一个后缀拆, got %q", got)
	}

	// 软删（回收站）里的同名文件不占名字
	trashed := create("b.txt")
	if err := db.DeleteFiles([]string{trashed}); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if got := db.ResolveNameConflict(parentID, "b.txt", ownerID); got != "" {
		t.Fatalf("回收站里的同名文件不应算冲突, got %q", got)
	}

	// 别的用户 / 别的目录互不影响
	otherOwner := uuid.New().String()
	if got := db.ResolveNameConflict(parentID, "a.txt", otherOwner); got != "" {
		t.Fatalf("其他用户不受影响, got %q", got)
	}
	if got := db.ResolveNameConflict(uuid.New().String(), "a.txt", ownerID); got != "" {
		t.Fatalf("其他目录不受影响, got %q", got)
	}
}
