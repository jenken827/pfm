package service

import (
	"encoding/hex"
	"sync"
	"testing"

	"github.com/google/uuid"
	"lukechampine.com/blake3"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/dbtest"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/utils"
)

// TestUploadSmallConcurrentSameFileIsIdempotent 复现事故场景并钉住修复：
//
// 客户端把同一个文件并发投递两次（两个 upload/small 同时在途）。修复前：
// 两边各自"查重 → 建 files 记录"，后到者撞 idx_f_npi 唯一约束，其错误清理
// 又删掉按 hash 共享的临时文件，先到者随后在 Dispatch 里报 temp file not found，
// 整单判失败（用户看到"已上传成功却分发失败"）。
// 修复后：同一 (owner, 父目录, 文件名) 串行化，后到者命中「同名同 hash → 秒传」，
// 直接返回 exists=true。
func TestUploadSmallConcurrentSameFileIsIdempotent(t *testing.T) {
	dbtest.Setup(t)

	userID := uuid.New().String()
	rootID := createTestFolder(t, userID, "root-"+userID, "")
	if err := db.CreateUserSpace(&entity.UserSpaceORM{
		UserId:           utils.StringPtr(userID),
		RootFileId:       utils.StringPtr(rootID),
		BoundProviderIds: utils.StringPtr("[1]"),
		QuotaBytes:       utils.Uint64Ptr(0),
		UsedBytes:        utils.Uint64Ptr(0),
	}); err != nil {
		t.Fatalf("create user space failed: %v", err)
	}

	content := []byte("fake png payload for concurrent upload test")
	hasher := blake3.New(32, nil)
	hasher.Write(content)
	hash := hex.EncodeToString(hasher.Sum(nil))
	name := "_s.png"

	type result struct {
		exists bool
		err    *models.Err
	}
	results := make([]result, 2)
	var wg sync.WaitGroup
	for i := 0; i < len(results); i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resp, mErr := UploadSmall(webdavTestCtx(userID), &native.UploadSmallReq{
				Name:     utils.StringPtr(name),
				Size:     utils.Uint64Ptr(uint64(len(content))),
				Hash:     utils.StringPtr(hash),
				ParentId: utils.StringPtr(rootID),
				Content:  content,
			})
			if mErr != nil {
				results[idx].err = mErr
				return
			}
			results[idx].exists = resp.GetExists()
		}(i)
	}
	wg.Wait()

	existing := 0
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("upload %d must not fail: %v", i, r.err.E)
		}
		if r.exists {
			existing++
		}
	}
	if existing != 1 {
		t.Fatalf("exactly one request should hit instant upload, got %d exists=true", existing)
	}

	file, fErr := db.FindFileByParentAndName(rootID, name, userID)
	if fErr != nil {
		t.Fatalf("find uploaded file failed: %v", fErr)
	}
	if file == nil {
		t.Fatal("uploaded file record missing")
	}
	if file.Hash == nil || *file.Hash != hash {
		t.Fatalf("file hash = %v, want %s", file.Hash, hash)
	}
}
