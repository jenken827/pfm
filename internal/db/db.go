package db

import (
	"fmt"
	"path"

	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

var db *gorm.DB

// Init 初始化数据库
func Init() {
	cdb := config.GetConfig().DB

	gormConfig := &gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			TablePrefix: config.GetConfig().DB.TablePrefix,
		},
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

// 迁移数据库
func autoMigrate() error {
	if err := db.AutoMigrate(
		&native.FileORM{},
		&native.FileTagORM{},
		&native.FileShareORM{},
		&native.FileHistoryORM{},
		&native.FileLocationORM{},
		&native.StorageProviderORM{},
		&native.UploadRecordORM{},
		&native.UploadChunkRecordORM{},
		&native.DownloadRecordORM{},
		&native.BackupTaskORM{},
		&native.BackupFileRecordORM{},
		&native.DirectoryPermissionORM{},
		&native.AdminUserORM{},
		&native.PermissionAuditLogORM{},
		&native.UserSpaceORM{},
	); err != nil {
		return err
	}

	// 修复 files 表唯一索引：将普通 unique index 改为 partial unique index
	// 这样软删除的记录不参与唯一约束，同目录下可以创建与回收站文件同名的新文件
	return migrateFilesUniqueIndex()
}

// migrateFilesUniqueIndex 将 files 表的 idx_f_npi 从普通唯一索引改为 partial 唯一索引
func migrateFilesUniqueIndex() error {
	// 删除旧的普通索引（如果存在）
	// SQLite 不支持 DROP INDEX IF EXISTS 的 IF EXISTS 语法，先尝试删除
	if err := db.Exec(`DROP INDEX IF EXISTS idx_f_npi`).Error; err != nil {
		llog.L().Warnf("drop old index idx_f_npi failed (may not exist): %v", err)
	}

	// 创建 partial unique index：只对未删除的记录生效
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_f_npi ON files (name, parent_id) WHERE deleted_at IS NULL`).Error; err != nil {
		return fmt.Errorf("create partial unique index idx_f_npi failed: %w", err)
	}

	llog.L().Info("migrated files unique index to partial index (WHERE deleted_at IS NULL)")
	return nil
}
