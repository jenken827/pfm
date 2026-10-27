package service

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"testing"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/dbtest"
	"jenken827.com/jenken827/pfm/proto/gen/entity"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

// setupLocalProvider 建一个 LOCAL provider（根目录 = root），返回 provider id
func setupLocalProvider(t *testing.T, root string) uint32 {
	t.Helper()
	const providerID uint32 = 4242 // 与其他用例隔离的固定 id
	if err := db.CreateStorageProvider(&entity.StorageProviderORM{
		Id:       utils.Uint32Ptr(providerID),
		Name:     utils.StringPtr("test-local-orphan"),
		DriverId: utils.StringPtr(types.DriverLocal),
		Config:   utils.StringPtr(fmt.Sprintf(`{"root_path":%q}`, root)),
		Enabled:  utils.BoolPtr(true),
	}); err != nil {
		t.Fatalf("创建 provider 失败: %v", err)
	}
	return providerID
}

func writeTestFile(t *testing.T, fullPath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte("x"), 0644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
}

// TestLocalRemoteOrphanEndToEnd 本地 provider 走真实文件系统：
// 扫描只把「远端有、file_locations 无记录」的文件判成孤儿，--remove 后物理文件消失、复扫为零。
func TestLocalRemoteOrphanEndToEnd(t *testing.T) {
	dbtest.Setup(t)
	root := t.TempDir()
	providerID := setupLocalProvider(t, root)

	yearDir := path.Join(root, "2026")
	kept := path.Join(yearDir, "01", "kept.jpg")
	orphan := path.Join(yearDir, "01", "orphan.jpg")
	writeTestFile(t, kept)
	writeTestFile(t, orphan)

	if err := db.CreateFileLocation(&entity.FileLocationORM{
		Blake3Hash: utils.StringPtr("hash-kept"),
		ProviderId: utils.Uint32Ptr(providerID),
		Path:       utils.StringPtr(kept), // 与 LocalDriver 写入 file_locations.path 的形态一致
		Status:     utils.EnumPtr(native.LocationStatus_PCOMPLETED),
		Size:       utils.Uint64Ptr(1),
	}); err != nil {
		t.Fatalf("创建位置行失败: %v", err)
	}

	scan, err := ScanRemoteOrphans(context.Background(), providerID, "test-local-orphan", yearDir, nil)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if scan.Scanned != 2 || scan.Recorded != 1 {
		t.Fatalf("扫描统计不符: scanned=%d recorded=%d", scan.Scanned, scan.Recorded)
	}
	if len(scan.Orphans) != 1 || scan.Orphans[0].Path != orphan {
		t.Fatalf("孤儿识别不符: %+v", scan.Orphans)
	}

	deleted, failed, err := RemoveRemoteOrphans(context.Background(), providerID, scan.Orphans)
	if err != nil || deleted != 1 || failed != 0 {
		t.Fatalf("删除孤儿失败: deleted=%d failed=%d err=%v", deleted, failed, err)
	}
	if _, statErr := os.Stat(orphan); !os.IsNotExist(statErr) {
		t.Fatalf("孤儿文件应已删除: %v", statErr)
	}
	if _, statErr := os.Stat(kept); statErr != nil {
		t.Fatalf("有记录的文件不能被删: %v", statErr)
	}

	rescan, err := ScanRemoteOrphans(context.Background(), providerID, "test-local-orphan", yearDir, nil)
	if err != nil {
		t.Fatalf("复扫失败: %v", err)
	}
	if len(rescan.Orphans) != 0 {
		t.Fatalf("复扫应无孤儿: %+v", rescan.Orphans)
	}
}

// TestScanRemoteOrphansUnsupportedProvider Telegram 无目录枚举能力，必须明确报错而不是静默空结果。
func TestScanRemoteOrphansUnsupportedProvider(t *testing.T) {
	dbtest.Setup(t)
	const providerID uint32 = 4243
	if err := db.CreateStorageProvider(&entity.StorageProviderORM{
		Id:       utils.Uint32Ptr(providerID),
		Name:     utils.StringPtr("test-telegram-orphan"),
		DriverId: utils.StringPtr(types.DriverTelegram),
		Config:   utils.StringPtr(`{"bot_token":"x","chat_id":1}`),
		Enabled:  utils.BoolPtr(true),
	}); err != nil {
		t.Fatalf("创建 provider 失败: %v", err)
	}
	_, err := ScanRemoteOrphans(context.Background(), providerID, "test-telegram-orphan", "/", nil)
	if err == nil {
		t.Fatalf("不支持的 provider 应返回错误")
	}
}
