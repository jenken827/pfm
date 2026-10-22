package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/dbtest"
)

// TestGetFileHistoryStreamBindsTicketToHostFile 历史版本的票据宿主校验：
// 票据只对该文件有效，历史版本也必须属于这个文件——否则一张 A 文件的票可以读到
// 该用户全部文件的历史版本（票据就退化成账户级读权限）。
func TestGetFileHistoryStreamBindsTicketToHostFile(t *testing.T) {
	dbtest.Setup(t)
	ownerID := uuid.New().String()
	rootID := createTestFolder(t, ownerID, "root-"+ownerID, "")
	fileA := createTestFile(t, ownerID, rootID, "a.txt")
	fileB := createTestFile(t, ownerID, rootID, "b.txt")

	oldHash := "history-hash-" + uuid.New().String()[:8]
	if err := db.ArchiveHashAsHistoryIfAbsent(fileA, oldHash, 12); err != nil {
		t.Fatalf("archive history failed: %v", err)
	}
	history, err := db.GetFileHistoryByFileIDAndHash(fileA, oldHash)
	if err != nil {
		t.Fatalf("load history failed: %v", err)
	}
	if history.Id == nil {
		t.Fatal("history id is nil")
	}
	historyID := *history.Id

	// 1) 票据绑定的是 B，却去取 A 的历史版本 → 必须拒绝
	_, err = GetFileHistoryStream(webdavTestCtx(ownerID), historyID, false, fileB)
	if err == nil {
		t.Fatal("want error when ticket file does not match history host file")
	}
	if !errors.Is(err, ErrTicketFileMismatch) {
		t.Fatalf("unexpected error: %v", err)
	}

	// 2) 票据绑定 A（正确宿主）→ 通过绑定校验，不会报这条错
	if _, err := GetFileHistoryStream(webdavTestCtx(ownerID), historyID, false, fileA); err != nil &&
		strings.Contains(err.Error(), "does not match history file") {
		t.Fatalf("ticket for host file rejected: %v", err)
	}

	// 3) 无票据（Authorization 头）时 expectFileID 为空 → 绑定校验不生效，
	//    不能因为空串比较把所有请求都拒掉
	if _, err := GetFileHistoryStream(webdavTestCtx(ownerID), historyID, false, ""); err != nil &&
		strings.Contains(err.Error(), "does not match history file") {
		t.Fatalf("bearer request rejected by ticket binding check: %v", err)
	}
}

// TestGetFileHistoryStreamRejectsNonOwner 历史版本只归宿主文件所有者，管理员不能越权
func TestGetFileHistoryStreamRejectsNonOwner(t *testing.T) {
	dbtest.Setup(t)
	ownerID := uuid.New().String()
	otherID := uuid.New().String()
	rootID := createTestFolder(t, ownerID, "root-"+ownerID, "")
	fileID := createTestFile(t, ownerID, rootID, "c.txt")

	oldHash := "history-hash-" + uuid.New().String()[:8]
	if err := db.ArchiveHashAsHistoryIfAbsent(fileID, oldHash, 3); err != nil {
		t.Fatalf("archive history failed: %v", err)
	}
	history, err := db.GetFileHistoryByFileIDAndHash(fileID, oldHash)
	if err != nil || history.Id == nil {
		t.Fatalf("load history failed: %v", err)
	}

	_, err = GetFileHistoryStream(webdavTestCtx(otherID), *history.Id, false, "")
	if err == nil {
		t.Fatal("want permission error for non-owner")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("unexpected error: %v", err)
	}
}
