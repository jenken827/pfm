package service

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/globals"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/dbtest"
	"github.com/jenken827/pfm/utils"
)

// ============================================================
// 并发上传同一目标文件：串行化 + 共享临时文件的清理保护
//
// 事故形态：同一文件被客户端并发投递两次，A 插入 files 记录成功、B 撞
// idx_f_npi 唯一约束；B 的错误清理删掉按 hash 共享的临时文件，A 随后在
// Dispatch 里报 temp file not found，整单判失败。
// ============================================================

// TestLockUploadTargetSerializesSameTarget 同一 (owner, 父目录, 文件名) 必须互斥
func TestLockUploadTargetSerializesSameTarget(t *testing.T) {
	var running, peak int32
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := lockUploadTarget("owner-1", "parent-1", "_s.png")
			defer unlock()

			cur := atomic.AddInt32(&running, 1)
			for {
				old := atomic.LoadInt32(&peak)
				if cur <= old || atomic.CompareAndSwapInt32(&peak, old, cur) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			atomic.AddInt32(&running, -1)
		}()
	}
	wg.Wait()

	if peak != 1 {
		t.Fatalf("same target must be serialized, peak concurrency = %d", peak)
	}
}

// TestLockUploadTargetDoesNotBlockOtherTargets 不同目标文件之间不能互相阻塞
func TestLockUploadTargetDoesNotBlockOtherTargets(t *testing.T) {
	unlockA := lockUploadTarget("owner-2", "parent-2", "a.png")
	defer unlockA()

	done := make(chan struct{})
	go func() {
		unlockB := lockUploadTarget("owner-2", "parent-2", "b.png")
		unlockB()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("different target files must not block each other")
	}
}

// TestLockUploadTargetReleasesEntry 全部释放后锁表要回收，避免按文件名的 map 无限增长
func TestLockUploadTargetReleasesEntry(t *testing.T) {
	key := uploadTargetKey{ownerID: "owner-3", parentID: "parent-3", fileName: "c.png"}
	unlock := lockUploadTarget(key.ownerID, key.parentID, key.fileName)
	unlock()

	uploadTargetLocks.Lock()
	_, exists := uploadTargetLocks.m[key]
	uploadTargetLocks.Unlock()
	if exists {
		t.Fatal("lock entry should be reclaimed after the last unlock")
	}
}

func newActiveUploadRecord(t *testing.T, hash, tempPath string) *entity.UploadRecordORM {
	t.Helper()
	ownerID := uuid.New().String()
	parentID := uuid.New().String()
	ur := &entity.UploadRecordORM{
		FileHash:          utils.StringPtr(hash),
		FileName:          utils.StringPtr("_s.png"),
		FileSize:          utils.Uint64Ptr(1024),
		ParentId:          utils.StringPtr(parentID),
		TempPath:          utils.StringPtr(tempPath),
		OwnerId:           utils.StringPtr(ownerID),
		Status:            utils.EnumPtr(native.LocationStatus_PPENDING),
		TaskStatus:        utils.EnumPtr(native.UploadStatus_PENDING),
		TaskType:          utils.EnumPtr(native.UploadTaskType_UPLOAD),
		OverwriteId:       utils.StringPtr(""),
		TargetProviderIds: utils.StringPtr("[1]"),
	}
	if err := db.GetDB().Create(ur).Error; err != nil {
		t.Fatalf("create upload record failed: %v", err)
	}
	return ur
}

// TestCleanupUploadTempIfOrphanKeepsSharedTemp 同 hash 还有在途上传时不许删临时文件，
// 只有在途记录全部结束后才删。
func TestCleanupUploadTempIfOrphanKeepsSharedTemp(t *testing.T) {
	dbtest.Setup(t)

	hash := "aaaabbbbccccddddeeeeffff00001111aaaabbbbccccddddeeeeffff00001112"
	tempDir := filepath.Join(globals.TempDir, hash)
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		t.Fatalf("mkdir temp dir failed: %v", err)
	}
	tempPath := filepath.Join(tempDir, "_s.png")
	if err := os.WriteFile(tempPath, []byte("thumbnail"), 0o600); err != nil {
		t.Fatalf("write temp file failed: %v", err)
	}

	ur := newActiveUploadRecord(t, hash, tempPath)

	// 仍在途（PENDING）：另一个请求的错误清理不能把它删掉
	cleanupUploadTempIfOrphan(tempPath, hash)
	if _, err := os.Stat(tempPath); err != nil {
		t.Fatalf("temp file shared with an active upload must be kept: %v", err)
	}

	// 记录进入终态：确认没有在途上传后才允许清理
	if err := db.TransitionUploadRecordTaskStatus(*ur.Id,
		[]native.UploadStatus{native.UploadStatus_PENDING, native.UploadStatus_UPLOADING},
		native.UploadStatus_FAILED, utils.StringPtr("test")); err != nil {
		t.Fatalf("transition upload record failed: %v", err)
	}
	cleanupUploadTempIfOrphan(tempPath, hash)
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Fatalf("temp file should be removed when no active upload uses the hash, err = %v", err)
	}
}
