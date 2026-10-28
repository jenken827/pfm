package db

import (
	"database/sql"
	"strings"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// newDryRunMySQLDB 只借 MySQL 方言推演建表 SQL，不连数据库：
// sql.Open 是惰性的，再关掉自动 Ping 和版本探测，就能在没有 MySQL 的机器上跑。
func newDryRunMySQLDB(t *testing.T) *gorm.DB {
	t.Helper()
	conn, err := sql.Open("mysql", "user:pass@tcp(127.0.0.1:3306)/pfm?charset=utf8mb4&parseTime=True")
	if err != nil {
		t.Fatalf("打开 MySQL 连接池失败: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	gdb, err := gorm.Open(gormmysql.New(gormmysql.Config{
		Conn:                      conn,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("初始化 MySQL 方言失败: %v", err)
	}
	return gdb
}

// TestMySQLSchemaIsCreatable 离线校验 MySQL 建表 / 建索引能不能过。
//
// 两条只有 MySQL 才挑的规则，SQLite / Postgres 都无感，所以放在这里兜住：
//   - TEXT / LONGTEXT 不能带默认值（MySQL 8.0.13 起只接受表达式默认值，gorm 生成字面量）
//   - TEXT / LONGTEXT 建索引必须给前缀长度；唯一索引带前缀等于「按前 N 个字符唯一」，
//     会把不同名字误判成重复，正确做法是给字段加 size 落成 varchar
func TestMySQLSchemaIsCreatable(t *testing.T) {
	gdb := newDryRunMySQLDB(t)
	models := []any{
		&entity.FileORM{}, &entity.FileTagORM{}, &entity.FileShareORM{}, &entity.FileHistoryORM{},
		&entity.FileLocationORM{}, &entity.StorageProviderORM{}, &entity.UploadRecordORM{},
		&entity.UploadChunkRecordORM{}, &entity.DownloadRecordORM{}, &entity.BackupTaskORM{},
		&entity.BackupFileRecordORM{}, &entity.UserSpaceORM{}, &entity.UserORM{}, &entity.WebdavAccountORM{},
	}
	for _, model := range models {
		st := &gorm.Statement{DB: gdb}
		if err := st.ParseWithSpecialTableName(model, ""); err != nil {
			t.Fatalf("解析 %T 失败: %v", model, err)
		}
		types := make(map[string]string, len(st.Schema.Fields))
		for _, f := range st.Schema.Fields {
			types[f.DBName] = gdb.Migrator().FullDataTypeOf(f).SQL
		}
		for column, columnType := range types {
			if isTextColumn(columnType) && strings.Contains(strings.ToUpper(columnType), "DEFAULT") {
				t.Errorf("%s.%s: TEXT 列不能带默认值，MySQL 建表会失败（%s）",
					st.Schema.Table, column, columnType)
			}
		}
		for _, index := range st.Schema.ParseIndexes() {
			for _, f := range index.Fields {
				columnType := types[f.DBName]
				if !isTextColumn(columnType) {
					continue
				}
				switch {
				case f.Length == 0:
					t.Errorf("%s 索引 %s: 列 %s 是 %s 且没有前缀长度，MySQL 建索引会失败",
						st.Schema.Table, index.Name, f.DBName, columnType)
				case index.Class == "UNIQUE":
					t.Errorf("%s 唯一索引 %s: 列 %s 是 %s，只能按前 %d 个字符唯一（会误判重复），应给字段加 size",
						st.Schema.Table, index.Name, f.DBName, columnType, f.Length)
				}
			}
		}
	}
}

// isTextColumn 判断列类型是否属于 MySQL 的 TEXT 家族（varchar 不算）
func isTextColumn(columnType string) bool {
	return strings.Contains(strings.ToLower(columnType), "text")
}
