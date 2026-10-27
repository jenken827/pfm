package db

import (
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/proto/gen/entity"
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

// Init 初始化数据库
func Init() {
	cdb := config.GetConfig().DB

	gormConfig := &gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			TablePrefix: config.GetConfig().DB.TablePrefix,
		},
		Logger: gormLogger(),
	}
	var err error

	switch cdb.Type {
	// case "mysql":
	// 	{
	// 		// refer https://github.com/go-sql-driver/mysql#dsn-data-source-name
	// 		//[username[:password]@][protocol[(address)]]/dbname[?param1=value1&...&paramN=valueN]
	// 		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
	// 			cdb.User, cdb.Password, cdb.Addr, cdb.Port, cdb.Name)

	// 		db, err = gorm.Open(mysql.Open(dsn), gormConfig)
	// 	}
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
//   - upload_records：旧的全局唯一索引 idx_ur_fh（只看 file_hash）必须删除，否则不同
//     用户上传同一 hash 会互相撞约束；复合唯一索引 idx_ur_fh_owner 由模型 tag 建。
func ensureSchemaIndexes() error {
	if err := ensureFilesUniqueIndex(); err != nil {
		return err
	}
	if err := db.Exec(`DROP INDEX IF EXISTS idx_ur_fh`).Error; err != nil {
		llog.L().Warnf("drop old index idx_ur_fh failed (may not exist): %v", err)
	}
	return nil
}

// ensureFilesUniqueIndex 建 files 的 partial 唯一索引。
//
// 老库可能已有同名的「非 partial」索引：只有现有定义里没有 WHERE 时才重建，
// 避免每次启动都重建索引（大库启动开销明显）。
func ensureFilesUniqueIndex() error {
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
