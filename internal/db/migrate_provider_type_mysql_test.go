package db

import (
	"fmt"
	"os"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"jenken827.com/jenken827/pfm/proto/gen/entity"
)

// MySQL 迁移用例：本地/CI 有可用 MySQL 时用 PFM_TEST_MYSQL_DSN 打开，
// 没有就跳过（默认套件不依赖数据库）。每个用例建独立临时库，结束后删掉。
func TestMigrateProviderTypeToStringMySQL(t *testing.T) {
	dsn := os.Getenv("PFM_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("未设置 PFM_TEST_MYSQL_DSN，跳过 MySQL 迁移用例")
	}
	base, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("解析 PFM_TEST_MYSQL_DSN 失败: %v", err)
	}

	// 先连一个不带库的连接，建临时库（用例之间互不影响）
	dbName := fmt.Sprintf("pfm_migtest_%d", os.Getpid())
	adminCfg := *base
	adminCfg.DBName = ""
	admin, err := gorm.Open(gormmysql.Open(adminCfg.FormatDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("连接 MySQL 失败: %v", err)
	}
	if err := admin.Exec("CREATE DATABASE IF NOT EXISTS " + dbName + " CHARACTER SET utf8mb4").Error; err != nil {
		t.Fatalf("创建临时库失败: %v", err)
	}
	t.Cleanup(func() {
		_ = admin.Exec("DROP DATABASE IF EXISTS " + dbName).Error
		if sqlDB, dbErr := admin.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	cfg := *base
	cfg.DBName = dbName
	gdb, err := gorm.Open(gormmysql.Open(cfg.FormatDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("连接临时库失败: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("取 sql.DB 失败: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	// 旧库形态：整型 provider_type（NOT NULL、无默认值）
	if err := gdb.Exec(`CREATE TABLE storage_providers (
		id bigint unsigned PRIMARY KEY AUTO_INCREMENT,
		name varchar(191) NOT NULL,
		provider_type bigint NOT NULL)`).Error; err != nil {
		t.Fatalf("建旧表失败: %v", err)
	}
	legacy := []struct {
		value int
		want  string
	}{
		{1, "local"}, {2, "telegram"}, {6, "baidu"}, {9, "cloud189pc"},
		{0, "unknown_0"}, {99, "unknown_99"},
	}
	for _, row := range legacy {
		if err := gdb.Exec(`INSERT INTO storage_providers (name, provider_type) VALUES (?, ?)`,
			fmt.Sprintf("p%d", row.value), row.value).Error; err != nil {
			t.Fatalf("插入旧数据失败: %v", err)
		}
	}

	if err := migrateProviderTypeToString(gdb); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	for _, row := range legacy {
		var got string
		if err := gdb.Raw(`SELECT provider_type FROM storage_providers WHERE provider_type = ?`,
			row.want).Row().Scan(&got); err != nil {
			t.Fatalf("迁移后缺少 %q（原值 %d）: %v", row.want, row.value, err)
		}
	}

	// 万一没转成字符串，AutoMigrate 会自己再 ALTER 一次并留下数字字符串
	columnType, exists, err := providerTypeColumnType(gdb)
	if err != nil || !exists || !isStringColumnType(columnType) {
		t.Fatalf("迁移后列类型异常: type=%q exists=%v err=%v", columnType, exists, err)
	}

	// 迁移后紧接 AutoMigrate（真实启动顺序）不能再改列或报错
	if err := gdb.AutoMigrate(&entity.StorageProviderORM{}); err != nil {
		t.Fatalf("迁移后 AutoMigrate 失败: %v", err)
	}

	// 幂等：再跑一次不应报错、数据不变
	if err := migrateProviderTypeToString(gdb); err != nil {
		t.Fatalf("重复迁移应为 no-op，实际 %v", err)
	}
	var count int64
	if err := gdb.Raw(`SELECT COUNT(*) FROM storage_providers WHERE provider_type = 'local'`).
		Row().Scan(&count); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("重复迁移后数据被改写: local 行数 %d", count)
	}
}
