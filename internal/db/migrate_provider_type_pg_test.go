package db

import (
	"fmt"
	"os"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// Postgres 迁移用例：本地/CI 有可用 PG 时用 PFM_TEST_POSTGRES_DSN 打开，
// 没有就跳过（默认套件不依赖数据库）。
//
// 每个用例在独立 schema 里跑，结束后 CASCADE 删除，不影响库里的真实数据。
func TestMigrateProviderTypeToStringPostgres(t *testing.T) {
	dsn := os.Getenv("PFM_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("未设置 PFM_TEST_POSTGRES_DSN，跳过 Postgres 迁移用例")
	}
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open postgres failed: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("get sql.DB failed: %v", err)
	}
	// 只留一条连接：SET search_path 是会话级的，连接池换连接就丢了
	sqlDB.SetMaxOpenConns(1)

	schema := fmt.Sprintf("pfm_migtest_%d", os.Getpid())
	if err := gdb.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error; err != nil {
		t.Fatalf("drop old schema failed: %v", err)
	}
	if err := gdb.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("create schema failed: %v", err)
	}
	t.Cleanup(func() {
		_ = gdb.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error
		_ = sqlDB.Close()
	})
	if err := gdb.Exec("SET search_path TO " + schema).Error; err != nil {
		t.Fatalf("set search_path failed: %v", err)
	}

	// 先按当前模型建全表，再退回旧形态（整型 provider_type + NOT NULL），
	// 这样后续 AutoMigrate 检查的是"真实老库"而不是缺列的简化表。
	if err := gdb.AutoMigrate(&native.StorageProviderORM{}); err != nil {
		t.Fatalf("create current table failed: %v", err)
	}
	for _, sql := range []string{
		"ALTER TABLE storage_providers ALTER COLUMN provider_type DROP NOT NULL",
		"ALTER TABLE storage_providers ALTER COLUMN provider_type TYPE bigint USING provider_type::bigint",
		"ALTER TABLE storage_providers ALTER COLUMN provider_type SET NOT NULL",
	} {
		if err := gdb.Exec(sql).Error; err != nil {
			t.Fatalf("roll back to legacy column failed (%s): %v", sql, err)
		}
	}
	legacy := []struct {
		value int
		want  string
	}{
		{1, "local"}, {2, "telegram"}, {3, "discord"}, {4, "huggingface"},
		{5, "pan123"}, {6, "baidu"}, {7, "aliyundrive_open"}, {8, "yun139"},
		{9, "cloud189pc"}, {0, "unknown_0"}, {99, "unknown_99"},
	}
	// 老库里这些行是完整记录：按 NOT NULL 且无默认值的列补齐，避免插入时报空值
	notNull := requiredColumns(t, gdb)
	for _, row := range legacy {
		columns := "provider_type"
		placeArgs := "?"
		values := []any{row.value}
		for _, column := range notNull {
			columns += ", " + column.name
			placeArgs += ", ?"
			// name 上有唯一索引，占位值要每行不同
			if column.name == "name" {
				values = append(values, fmt.Sprintf("p%d", row.value))
				continue
			}
			values = append(values, column.zeroValue())
		}
		if err := gdb.Exec("INSERT INTO storage_providers ("+columns+") VALUES ("+placeArgs+")",
			values...).Error; err != nil {
			t.Fatalf("insert legacy row failed: %v", err)
		}
	}

	if err := migrateProviderTypeToString(gdb); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	for _, row := range legacy {
		var got string
		if err := gdb.Raw(`SELECT provider_type FROM storage_providers WHERE provider_type = ?`,
			row.want).Row().Scan(&got); err != nil {
			t.Fatalf("迁移后缺少 %q（原值 %d）: %v", row.want, row.value, err)
		}
	}

	// 迁移后紧接 AutoMigrate（真实启动顺序）不能再改列或报错
	if err := gdb.Exec("SET search_path TO " + schema).Error; err != nil {
		t.Fatalf("reset search_path failed: %v", err)
	}
	if err := gdb.AutoMigrate(&native.StorageProviderORM{}); err != nil {
		t.Fatalf("AutoMigrate after migration failed: %v", err)
	}
	columnType, exists, err := providerTypeColumnType(gdb)
	if err != nil || !exists || !isStringColumnType(columnType) {
		t.Fatalf("迁移后列类型异常: type=%q exists=%v err=%v", columnType, exists, err)
	}
}

// notNullColumn 一个 NOT NULL 且无默认值的列（插入时必须显式给值）
type notNullColumn struct {
	name     string
	dataType string
}

// zeroValue 按列类型给一个合法占位值
func (c notNullColumn) zeroValue() any {
	switch c.dataType {
	case "bigint", "integer", "smallint", "numeric", "double precision", "real":
		return 0
	case "boolean":
		return false
	case "timestamp with time zone", "timestamp without time zone":
		return time.Now()
	default:
		return "p"
	}
}

// requiredColumns 查出 id / provider_type 之外、NOT NULL 且无默认值的列
func requiredColumns(t *testing.T, gdb *gorm.DB) []notNullColumn {
	t.Helper()
	rows, err := gdb.Raw(`SELECT column_name, data_type FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'storage_providers'
		AND is_nullable = 'NO' AND column_default IS NULL AND column_name NOT IN ('id', 'provider_type')`).Rows()
	if err != nil {
		t.Fatalf("query not null columns failed: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var columns []notNullColumn
	for rows.Next() {
		var column notNullColumn
		if scanErr := rows.Scan(&column.name, &column.dataType); scanErr != nil {
			t.Fatalf("scan column failed: %v", scanErr)
		}
		columns = append(columns, column)
	}
	return columns
}
