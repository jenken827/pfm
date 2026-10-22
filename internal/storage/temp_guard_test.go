package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/dbtest"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

// TestCleanupUploadTempKeepsDirsSharedWithActiveUpload 钉住临时目录清理的并发保护：
//
// 临时文件按内容 hash 共享（内容寻址层 + owner 隔离层）。一条记录分发成功就删
// 整个 hash 目录，会把另一条仍在途（PENDING/UPLOADING）的同 hash 上传的源文件
// 一起删掉，那条记录随后必然在 temp file not found 上判失败。
func TestCleanupUploadTempKeepsDirsSharedWithActiveUpload(t *testing.T) {
	dbtest.Setup(t)

	hash := "bbbbccccddddeeeeffff00001111aaaabbbbccccddddeeeeffff000011112222"
	ownerID := uuid.New().String()

	contentDir := filepath.Join(globals.TempDir, hash)
	ownerDir := filepath.Join(globals.TempDir, globals.UsersTempDirName, ownerID, hash)
	for _, dir := range []string{contentDir, ownerDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s failed: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "payload.bin"), []byte("x"), 0o600); err != nil {
			t.Fatalf("write payload in %s failed: %v", dir, err)
		}
	}

	// upload_records 上有 (file_hash, owner_id) 唯一索引：同一 owner 同 hash 只有
	// 一条记录。这里的"另一条在途记录"用另一个 owner 表示另一种并发来源（不同
	// 用户的同内容上传共用内容寻址层 TempDir/<hash>/）。
	finished := newTestUploadRecord(t, hash, ownerID, native.UploadStatus_COMPLETED)
	active := newTestUploadRecord(t, hash, uuid.New().String(), native.UploadStatus_PENDING)

	cleanupUploadTemp(finished)
	for _, dir := range []string{contentDir, ownerDir} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("temp dir %s must survive while another upload is active: %v", dir, err)
		}
	}

	// 在途记录结束后才允许回收
	if err := db.TransitionUploadRecordTaskStatus(*active.Id,
		[]native.UploadStatus{native.UploadStatus_PENDING, native.UploadStatus_UPLOADING},
		native.UploadStatus_FAILED, utils.StringPtr("test")); err != nil {
		t.Fatalf("transition active record failed: %v", err)
	}
	cleanupUploadTemp(finished)
	for _, dir := range []string{contentDir, ownerDir} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("temp dir %s should be removed when no active upload uses the hash, err = %v", dir, err)
		}
	}
}

// newTestUploadRecord 建一条指定状态的上传记录（每个测试用独立 hash+owner，避免撞唯一索引）
func newTestUploadRecord(t *testing.T, hash, ownerID string, status native.UploadStatus) *native.UploadRecordORM {
	t.Helper()
	ur := &native.UploadRecordORM{
		FileHash:          utils.StringPtr(hash),
		FileName:          utils.StringPtr("payload.bin"),
		FileSize:          utils.Uint64Ptr(1),
		ParentId:          utils.StringPtr(uuid.New().String()),
		TempPath:          utils.StringPtr(filepath.Join(globals.TempDir, hash, "payload.bin")),
		OwnerId:           utils.StringPtr(ownerID),
		Status:            utils.EnumPtr(native.LocationStatus_PPENDING),
		TaskStatus:        utils.EnumPtr(status),
		TaskType:          utils.EnumPtr(native.UploadTaskType_UPLOAD),
		OverwriteId:       utils.StringPtr(""),
		TargetProviderIds: utils.StringPtr("[1]"),
	}
	if err := db.GetDB().Create(ur).Error; err != nil {
		t.Fatalf("create upload record failed: %v", err)
	}
	return ur
}
