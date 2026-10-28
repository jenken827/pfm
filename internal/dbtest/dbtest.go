// Package dbtest 提供测试用的临时数据库初始化。
//
// 默认用「临时目录 + SQLite 文件 + 最小配置文件」把真实 DB 跑起来，让入库逻辑
// （唯一索引、批量更新、软删过滤等）可以被真正验证。
//
// 设了 PFM_TEST_MYSQL_DSN（格式 "user:pass@tcp(host:port)/任意库?charset=utf8mb4&parseTime=True"）
// 就改跑 MySQL：按进程号建一个临时库（包与包并行跑也互不干扰），跑完不自动删，
// 需要手工清理 pfm_t<pid> 这类库名。
package dbtest

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jenken827/pfm/globals"
	"github.com/jenken827/pfm/internal/config"
	"github.com/jenken827/pfm/internal/db"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var (
	once sync.Once
	dir  string
	err  error
	// Database 实际使用的库名（MySQL 模式；SQLite 模式为 test.db）
	Database string
	// Dialect 实际运行的方言：sqlite3 / mysql
	Dialect string
)

// Setup 初始化全局配置与数据库（同一进程只初始化一次：单库 + 全局单例 db），
// 返回数据目录。同一测试包内的多个测试共享该数据库，因此测试数据要自行隔离
// （用户名/目录名加前缀，或按测试名生成）。
func Setup(t *testing.T) string {
	t.Helper()
	once.Do(func() {
		dir, err = os.MkdirTemp("", "pfm-dbtest-")
		if err != nil {
			return
		}
		globals.DataDir = dir
		globals.TempDir = filepath.Join(dir, "temp")
		if mkErr := os.MkdirAll(globals.TempDir, 0o755); mkErr != nil {
			err = mkErr
			return
		}

		content := "db:\n  type: sqlite3\n  name: test.db\nlog:\n  level: error\n"
		Database = "test.db"
		Dialect = "sqlite3"
		if dsn := os.Getenv("PFM_TEST_MYSQL_DSN"); dsn != "" {
			content, err = mysqlTestConfig(dsn)
			if err != nil {
				return
			}
			Dialect = "mysql"
		}

		cfgPath := filepath.Join(dir, "config.yaml")
		if wErr := os.WriteFile(cfgPath, []byte(content), 0o644); wErr != nil {
			err = wErr
			return
		}
		globals.ConfigPath = cfgPath

		config.Init() // 读取上面的最小配置（config.Init 要求配置文件存在）
		db.Init()     // 建表 + 迁移
	})
	if err != nil {
		t.Fatalf("dbtest setup failed: %v", err)
	}
	return dir
}

// mysqlTestConfig 按 DSN 建一个本进程独占的临时库，并生成对应的最小配置。
func mysqlTestConfig(dsn string) (string, error) {
	base, parseErr := mysqldriver.ParseDSN(dsn)
	if parseErr != nil {
		return "", fmt.Errorf("解析 PFM_TEST_MYSQL_DSN 失败: %w", parseErr)
	}
	Database = fmt.Sprintf("pfm_t%d", os.Getpid())
	if execErr := withAdminConn(base, func(admin *gorm.DB) error {
		return admin.Exec("CREATE DATABASE " + Database + " CHARACTER SET utf8mb4").Error
	}); execErr != nil {
		return "", fmt.Errorf("创建测试库 %s 失败: %w", Database, execErr)
	}
	addr, port := base.Addr, 3306
	if h, p, splitErr := net.SplitHostPort(base.Addr); splitErr == nil {
		addr = h
		if parsed, atoiErr := strconv.Atoi(p); atoiErr == nil {
			port = parsed
		}
	}
	return fmt.Sprintf(`db:
  type: mysql
  addr: %s
  port: %d
  name: %s
  user: %s
  password: %q
log:
  level: error
`, addr, port, Database, base.User, base.Passwd), nil
}

func withAdminConn(base *mysqldriver.Config, fn func(*gorm.DB) error) error {
	cfg := *base
	cfg.DBName = ""
	admin, err := gorm.Open(gormmysql.Open(cfg.FormatDSN()), &gorm.Config{})
	if err != nil {
		return err
	}
	defer func() {
		if sqlDB, dbErr := admin.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	}()
	return fn(admin)
}

// DataDir 返回测试数据目录（Setup 之后可用）
func DataDir() string {
	return dir
}
