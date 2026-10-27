package db

import (
	"strings"
	"testing"
)

func TestMySQLDSN(t *testing.T) {
	got := mysqlDSN("db.local", 3307, "pfm", "secret", "pfm_db")
	want := "pfm:secret@tcp(db.local:3307)/pfm_db?charset=utf8mb4&parseTime=True&loc=Local"
	if got != want {
		t.Fatalf("DSN 不符合预期\n got: %s\nwant: %s", got, want)
	}
}

// addr / port 留空要回落默认值：拼成 tcp(:0) 是连不上的
func TestMySQLDSNDefaults(t *testing.T) {
	got := mysqlDSN("", 0, "root", "", "pfm")
	if !strings.Contains(got, "@tcp(127.0.0.1:3306)/pfm") {
		t.Fatalf("默认地址/端口应为 127.0.0.1:3306，实际 %s", got)
	}
	// parseTime 缺了时间列会扫不进 time.Time
	if !strings.Contains(got, "parseTime=True") {
		t.Fatalf("DSN 必须带 parseTime=True，实际 %s", got)
	}
	// 中文字段与 emoji 需要 utf8mb4
	if !strings.Contains(got, "charset=utf8mb4") {
		t.Fatalf("DSN 必须带 charset=utf8mb4，实际 %s", got)
	}
}
