package db

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// newMigrationTestDB 建一个独立的 SQLite 文件库（不用全局 db，避免影响其它用例）
func newMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "migrate.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	return gdb
}

// 旧库是整型 provider_type：迁移后必须是驱动身份字符串，未知值保留成 unknown_*
func TestMigrateProviderTypeToString(t *testing.T) {
	gdb := newMigrationTestDB(t)
	if err := gdb.Exec(`CREATE TABLE storage_providers (
		id integer PRIMARY KEY, name text, provider_type integer NOT NULL)`).Error; err != nil {
		t.Fatalf("create legacy table failed: %v", err)
	}
	legacy := map[int]string{
		1: "local", 2: "telegram", 3: "discord", 4: "huggingface", 5: "pan123",
		6: "baidu", 7: "aliyundrive_open", 8: "yun139", 9: "cloud189pc",
		0: "unknown_0", 99: "unknown_99",
	}
	for value := range legacy {
		if err := gdb.Exec(`INSERT INTO storage_providers (name, provider_type) VALUES (?, ?)`,
			"p", value).Error; err != nil {
			t.Fatalf("insert legacy row failed: %v", err)
		}
	}

	if err := migrateProviderTypeToString(gdb); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	rows, err := gdb.Raw(`SELECT provider_type FROM storage_providers ORDER BY provider_type`).Rows()
	if err != nil {
		t.Fatalf("query migrated rows failed: %v", err)
	}
	defer func() { _ = rows.Close() }()
	got := make(map[string]int)
	for rows.Next() {
		var value string
		if scanErr := rows.Scan(&value); scanErr != nil {
			t.Fatalf("scan migrated value failed: %v", scanErr)
		}
		got[value]++
	}
	for _, want := range legacy {
		if got[want] != 1 {
			t.Fatalf("迁移结果缺少 %q，实际 %v", want, got)
		}
	}

	// 列类型与 NOT NULL 不能退化
	columnType, exists, err := providerTypeColumnType(gdb)
	if err != nil || !exists {
		t.Fatalf("query column after migrate failed: exists=%v err=%v", exists, err)
	}
	if !isStringColumnType(columnType) {
		t.Fatalf("迁移后列类型应为字符串，实际 %q", columnType)
	}

	// 幂等：再跑一次不应报错、数据不变
	if err := migrateProviderTypeToString(gdb); err != nil {
		t.Fatalf("second migrate should be no-op, got %v", err)
	}
}

// 全新库（表还不存在）必须直接跳过，交给 AutoMigrate 建 varchar 列
func TestMigrateProviderTypeToStringMissingTable(t *testing.T) {
	gdb := newMigrationTestDB(t)
	if err := migrateProviderTypeToString(gdb); err != nil {
		t.Fatalf("missing table should be skipped, got %v", err)
	}
}

// 已经是字符串列的库不能被再改一次（历史上可能跑过一次）
func TestMigrateProviderTypeToStringAlreadyMigrated(t *testing.T) {
	gdb := newMigrationTestDB(t)
	if err := gdb.Exec(`CREATE TABLE storage_providers (
		id integer PRIMARY KEY, name text, provider_type varchar(32) NOT NULL)`).Error; err != nil {
		t.Fatalf("create migrated table failed: %v", err)
	}
	if err := gdb.Exec(`INSERT INTO storage_providers (name, provider_type) VALUES ('p', 'baidu')`).Error; err != nil {
		t.Fatalf("insert migrated row failed: %v", err)
	}
	if err := migrateProviderTypeToString(gdb); err != nil {
		t.Fatalf("already migrated should be no-op, got %v", err)
	}
	var value string
	if err := gdb.Raw(`SELECT provider_type FROM storage_providers`).Row().Scan(&value); err != nil {
		t.Fatalf("read value failed: %v", err)
	}
	if value != "baidu" {
		t.Fatalf("已有字符串值不应被改写，实际 %q", value)
	}
}
