package db

import (
	"fmt"
	"log"
	"os"
	"path"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm/globals"
	"github.com/jenken827/pfm/internal/config"
	"github.com/jenken827/pfm/pkg/llog"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
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
// 这里只做「每次启动都必须成立」的事：建表/加列，以及 tag 表达不了的索引不变式。
// 一次性数据迁移不进启动路径——历史回填都已在本机执行完并删除，provider_type 的
// 枚举→身份转换也改成了手工 SQL。以后新增回填同样办理：写成文档仓库里的一次性 SQL，
// 跑完就完，不让宿主长期背着一份只对某个老库有意义的代码。
func autoMigrate() error {
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

// ensureSchemaIndexes 保证模型 tag 表达不了的索引不变式（全新库也要成立，所以每次启动都跑）。
//
//   - files：(owner_id, name, parent_id) WHERE deleted_at IS NULL 的 partial unique index。
//     带 WHERE 的索引没法用 gorm tag 表达；缺了它软删除的记录会参与唯一约束，
//     用户删掉文件后无法再建同名文件。MySQL 不支持 partial index，跳过（见下）。
//   - upload_records：复合唯一索引 idx_ur_fh_owner (file_hash, owner_id) 由模型 tag 建，
//     不同用户上传同一 hash 各有一行。
func ensureSchemaIndexes() error {
	return ensureFilesUniqueIndex()
}

// ensureFilesUniqueIndex 建 files 的 partial 唯一索引（幂等）。
//
// MySQL 没有 partial index（CREATE INDEX ... WHERE 是语法错误），也没有等价替代：
// name / owner_id 是不限长的文本列，带前缀的唯一索引会按前 N 个字符误判重名。
// 所以 MySQL 上放弃库级约束，同目录重名只由服务层校验兜底（并发同名可能漏网）。
func ensureFilesUniqueIndex() error {
	if config.GetConfig().DB.Type == "mysql" {
		llog.L().Info("mysql: partial unique index idx_f_npi unsupported, " +
			"duplicate-name check falls back to service layer")
		return nil
	}
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_f_npi ON files (owner_id, name, parent_id) WHERE deleted_at IS NULL`).Error; err != nil {
		return fmt.Errorf("create partial unique index idx_f_npi failed: %w", err)
	}
	return nil
}
