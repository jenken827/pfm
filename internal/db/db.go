package db

import (
	"fmt"
	"path"

	"jenken827.com/jenken827/pfm/flags"
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
			db, err = gorm.Open(sqlite.Open(fmt.Sprintf("%s?_journal=WAL&_vacuum=incremental",
				path.Join(flags.DataDir, cdb.Name))), gormConfig)
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
	// if err := initData(); err != nil {
	// 	panic(fmt.Sprintf("failed to init data:%s", err.Error()))
	// }
	llog.GetLogger().Info("db init success")

}

// GetDB 获取数据库
func GetDB() *gorm.DB {
	return db
}

// 迁移数据库
func autoMigrate() error {
	return db.AutoMigrate(
		&native.FileORM{},
		&native.FileHistoryORM{},
		&native.DirConfigORM{},
		&native.FileTagORM{},
		&native.FileLocationORM{},
		&native.StorageGroupORM{},
		&native.StorageProviderORM{},
		&native.UploadChunkRecordORM{},
	)
}
