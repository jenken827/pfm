// Package dbtest 提供测试用的临时 SQLite 数据库初始化。
//
// 仓库里此前的 DB 相关测试都是 t.Skip（开发环境没有可用的数据库），
// 这里用「临时目录 + SQLite 文件 + 最小配置文件」把真实 DB 跑起来，
// 让入库逻辑（唯一索引、批量更新、软删过滤等）可以被真正验证。
package dbtest

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
)

var (
	once sync.Once
	dir  string
	err  error
)

// Setup 初始化全局配置与数据库（同一进程只初始化一次：SQLite 单文件 + 全局单例 db），
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

		cfgPath := filepath.Join(dir, "config.yaml")
		content := "db:\n  type: sqlite3\n  name: test.db\nlog:\n  level: error\n"
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

// DataDir 返回测试数据目录（Setup 之后可用）
func DataDir() string {
	return dir
}
