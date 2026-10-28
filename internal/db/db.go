package db

import (
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/pkg/llog"
)

var db *gorm.DB

// gormLogger 走 stderr 且不带 ANSI 颜色
//
// GORM 默认 logger 写 stdout 且带颜色：会污染 CLI 的 --json 输出（慢 SQL 警告直接插进 JSON），
// 日志文件里也只剩一堆转义码。
func gormLogger() logger.Interface {
	return logger.New(log.New(os.Stderr, "", log.LstdFlags), logger.Config{
		SlowThreshold:             200 * time.Millisecond,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: false,
		Colorful:                  false,
	})
}

// mysqlDSN 组装 MySQL 的 DSN。
//
// 格式见 https://github.com/go-sql-driver/mysql#dsn-data-source-name：
// [username[:password]@][protocol[(address)]]/dbname[?param1=value1&...&paramN=valueN]
// addr / port 留空时回落到 127.0.0.1:3306，避免拼出 tcp(:0) 这种连不上的 DSN。
// parseTime=True 让 DATETIME 直接扫进 time.Time（实体里的时间列依赖它）。
func mysqlDSN(addr string, port int, user, password, name string) string {
	if addr == "" {
		addr = "127.0.0.1"
	}
	if port == 0 {
		port = 3306
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		user, password, addr, port, name)
}

// Init 初始化数据库
func Init() {
	cdb := config.GetConfig().DB

	gormConfig := &gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			TablePrefix: config.GetConfig().DB.TablePrefix,
		},
		Logger: gormLogger(),
		// 把各方言的错误码统一成 gorm.ErrDuplicatedKey 等哨兵错误：
		// 唯一键冲突的文案在 SQLite / PostgreSQL / MySQL 里完全不同，
		// 靠字符串匹配迟早漏（MySQL 是 1062 "Duplicate entry"）。
		TranslateError: true,
	}
	var err error

	switch cdb.Type {
	case "mysql":
		{
			db, err = gorm.Open(mysql.Open(mysqlDSN(cdb.Addr, cdb.Port, cdb.User, cdb.Password, cdb.Name)), gormConfig)
		}
	case "postgres":
		{
			dsn := ""

			if cdb.Password != "" {
				dsn = fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d TimeZone=Asia/Shanghai",
					cdb.Addr, cdb.User, cdb.Password, cdb.Name, cdb.Port)
			} else {
				dsn = fmt.Sprintf("host=%s user=%s dbname=%s port=%d TimeZone=Asia/Shanghai",
					cdb.Addr, cdb.User, cdb.Name, cdb.Port)
			}

			db, err = gorm.Open(postgres.Open(dsn), gormConfig)
		}
	case "sqlite3":
		{
			db, err = gorm.Open(sqlite.Open(fmt.Sprintf("%s?_journal=WAL&_vacuum=incremental&_time_format=sqlite&_busy_timeout=5000",
				path.Join(globals.DataDir, cdb.Name))), gormConfig)
		}
	default:
		panic(fmt.Sprintf("not supported database type: %s", cdb.Type))
	}

	if err != nil {
		panic(fmt.Sprintf("failed to connect database:%s", err.Error()))
	}

	if err := autoMigrate(); err != nil {
		panic(fmt.Sprintf("failed to migrate database:%s", err.Error()))

	}

	llog.L().Info("db init success")

}

// GetDB 获取数据库
func GetDB() *gorm.DB {
	return db
}

// Transaction 执行数据库事务
func Transaction(fc func(tx *gorm.DB) error) error {
	return db.Transaction(fc)
}

// 迁移数据库（AutoMigrate + 模型 tag 无法表达的索引不变式）
//
// 历史数据回填已全部在这台部署上执行完并清理（task_status 回填、file_shares.owner_id
// 回填、is_ready 脏数据修复）。新增需要回填的字段时，按同样的方式写幂等迁移函数挂到
// 这里，确认线上执行完后再删。
func autoMigrate() error {
	// 驱动身份（provider_type）字符串化必须在 AutoMigrate 之前：模型已是 string，
	// GORM 对 int -> varchar 只会生成没有 USING 的 ALTER COLUMN。
	if err := migrateProviderTypeToString(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(
		&entity.FileORM{},
		&entity.FileTagORM{},
		&entity.FileShareORM{},
		&entity.FileHistoryORM{},
		&entity.FileLocationORM{},
		&entity.StorageProviderORM{},
		&entity.UploadRecordORM{},
		&entity.UploadChunkRecordORM{},
		&entity.DownloadRecordORM{},
		&entity.BackupTaskORM{},
		&entity.BackupFileRecordORM{},
		&entity.UserSpaceORM{},
		&entity.UserORM{},
		&entity.WebdavAccountORM{},
	); err != nil {
		return err
	}

	return ensureSchemaIndexes()
}

// ensureSchemaIndexes 保证两个「模型 tag 无法完整表达」的索引存在（全新库、老库都要）。
//
//   - files：partial unique index (owner_id, name, parent_id) WHERE deleted_at IS NULL。
//     带 WHERE 条件的索引无法用 gorm tag 表达；缺了它全新库会失去「同目录同用户不许
//     重名」的唯一约束（软删除的记录不参与），并发上传同名会写出重复记录。
//     MySQL 不支持 partial index，这一条在 MySQL 上跳过（见 ensureFilesUniqueIndex）。
//   - upload_records：旧的全局唯一索引 idx_ur_fh（只看 file_hash）必须删除，否则不同
//     用户上传同一 hash 会互相撞约束；复合唯一索引 idx_ur_fh_owner 由模型 tag 建。
func ensureSchemaIndexes() error {
	if err := ensureFilesUniqueIndex(); err != nil {
		return err
	}
	if err := dropLegacyUploadRecordIndex(); err != nil {
		llog.L().Warnf("drop old index idx_ur_fh failed (may not exist): %v", err)
	}
	return nil
}

// dropLegacyUploadRecordIndex 删除旧的全局唯一索引 idx_ur_fh。
//
// MySQL 的 DROP INDEX 不支持 IF EXISTS，先查 information_schema 再删，
// 免得每次启动都刷一条失败告警（老库才可能有这个索引）。
func dropLegacyUploadRecordIndex() error {
	if config.GetConfig().DB.Type == "mysql" {
		exists, err := mysqlIndexExists("upload_records", "idx_ur_fh")
		if err != nil || !exists {
			return err
		}
		return db.Exec("ALTER TABLE upload_records DROP INDEX idx_ur_fh").Error
	}
	return db.Exec(`DROP INDEX IF EXISTS idx_ur_fh`).Error
}

// mysqlIndexExists 查索引是否存在（MySQL 侧没有 pg_indexes / sqlite_master 那样的入口）
func mysqlIndexExists(table, index string) (bool, error) {
	var count int64
	if err := db.Raw(`SELECT COUNT(*) FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`, table, index).
		Row().Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// ensureFilesUniqueIndex 建 files 的 partial 唯一索引。
//
// 老库可能已有同名的「非 partial」索引：只有现有定义里没有 WHERE 时才重建，
// 避免每次启动都重建索引（大库启动开销明显）。
func ensureFilesUniqueIndex() error {
	if config.GetConfig().DB.Type == "mysql" {
		// MySQL 没有 partial index（CREATE INDEX ... WHERE 是语法错误），也没有等价替代：
		// name / owner_id 是不限长的文本列，带前缀的唯一索引会按前 N 个字符误判重名。
		// 所以这里放弃库级约束，同目录重名只由服务层校验兜底（并发同名可能漏网）。
		llog.L().Info("mysql: partial unique index idx_f_npi unsupported, " +
			"duplicate-name check falls back to service layer")
		return nil
	}
	def, err := currentIndexDefinition("idx_f_npi")
	if err != nil {
		return fmt.Errorf("query index idx_f_npi failed: %w", err)
	}
	if strings.Contains(strings.ToUpper(def), "WHERE") {
		return nil
	}
	if def != "" {
		if err := db.Exec(`DROP INDEX IF EXISTS idx_f_npi`).Error; err != nil {
			return fmt.Errorf("drop old index idx_f_npi failed: %w", err)
		}
	}
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_f_npi ON files (owner_id, name, parent_id) WHERE deleted_at IS NULL`).Error; err != nil {
		return fmt.Errorf("create partial unique index idx_f_npi failed: %w", err)
	}
	llog.L().Info("ensured partial unique index idx_f_npi on files")
	return nil
}

// currentIndexDefinition 返回索引定义（不存在返回空串）；只支持 postgres / sqlite3
func currentIndexDefinition(indexName string) (string, error) {
	var query string
	switch config.GetConfig().DB.Type {
	case "postgres":
		query = `SELECT COALESCE((SELECT indexdef FROM pg_indexes WHERE schemaname = current_schema() AND indexname = ?), '')`
	case "sqlite3":
		query = `SELECT COALESCE((SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?), '')`
	default:
		return "", nil
	}
	var def string
	if err := db.Raw(query, indexName).Row().Scan(&def); err != nil {
		return "", err
	}
	return def, nil
}
