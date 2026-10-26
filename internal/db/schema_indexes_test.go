package db_test

import (
	"strings"
	"testing"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/dbtest"
)

// TestAutoMigrateCreatesSchemaIndexes 全新库必须由 autoMigrate 建出两个索引不变式：
// files 的 partial 唯一索引（gorm tag 无法表达 WHERE）与 upload_records 的复合唯一索引。
// 历史数据回填清理后，这两个索引是"新库也正确"的唯一保证，不能被误删。
func TestAutoMigrateCreatesSchemaIndexes(t *testing.T) {
	dbtest.Setup(t) // 内部已跑 db.Init → autoMigrate
	gdb := db.GetDB()

	indexSQL := func(name string) string {
		t.Helper()
		var def string
		if err := gdb.Raw(
			`SELECT COALESCE((SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?), '')`,
			name).Row().Scan(&def); err != nil {
			t.Fatalf("query index %s: %v", name, err)
		}
		return strings.ToUpper(def)
	}

	filesIdx := indexSQL("idx_f_npi")
	if !strings.Contains(filesIdx, "UNIQUE") || !strings.Contains(filesIdx, "WHERE") ||
		!strings.Contains(filesIdx, "DELETED_AT IS NULL") {
		t.Fatalf("idx_f_npi 应为 partial unique index, got %q", filesIdx)
	}

	uploadIdx := indexSQL("idx_ur_fh_owner")
	if !strings.Contains(uploadIdx, "UNIQUE") {
		t.Fatalf("idx_ur_fh_owner 应为 unique index, got %q", uploadIdx)
	}

	if oldIdx := indexSQL("idx_ur_fh"); oldIdx != "" {
		t.Fatalf("旧的全局唯一索引 idx_ur_fh 不应存在: %q", oldIdx)
	}
}
