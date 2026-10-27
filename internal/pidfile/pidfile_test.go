package pidfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReadRemove(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	if err := Write(configPath, 4321); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	pid, err := Read(configPath)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if pid != 4321 {
		t.Fatalf("want 4321, got %d", pid)
	}
	// 覆盖写：进程重启后 pid 会变
	if err := Write(configPath, 4322); err != nil {
		t.Fatalf("覆盖写入失败: %v", err)
	}
	if pid, _ = Read(configPath); pid != 4322 {
		t.Fatalf("覆盖写入后应读到新 pid, got %d", pid)
	}
	if err := Remove(configPath); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := Read(configPath); err == nil {
		t.Fatal("删除后读取应报错")
	}
	// 重复删除不应报错（服务重复退出 / 文件已被手工删掉）
	if err := Remove(configPath); err != nil {
		t.Fatalf("重复删除应忽略: %v", err)
	}
}

func TestReadRejectsBadContent(t *testing.T) {
	cases := map[string]string{
		"空文件":   "",
		"非数字":   "pfm\n",
		"零":     "0\n",
		"负数":    "-7\n",
		"带多余内容": "12 34\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(Path(configPath), []byte(content), 0o644); err != nil {
				t.Fatalf("准备文件失败: %v", err)
			}
			pid, err := Read(configPath)
			if err == nil {
				t.Fatalf("应报错，实际读到 %d", pid)
			}
			if pid != 0 {
				t.Fatalf("出错时应返回 0, got %d", pid)
			}
		})
	}
}

func TestPathEmptyConfig(t *testing.T) {
	if got := Path(""); got != "" {
		t.Fatalf("空配置路径应返回空串, got %q", got)
	}
	if err := Write("", os.Getpid()); err == nil {
		t.Fatal("空配置路径写入应报错")
	}
	if _, err := Read(""); err == nil {
		t.Fatal("空配置路径读取应报错")
	}
	if err := Remove(""); err != nil {
		t.Fatalf("空配置路径删除应静默: %v", err)
	}
}

func TestPathNextToConfig(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	want := filepath.Join(dir, "pfm.pid")
	if got := Path(filepath.Join(dir, "config.yaml")); got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}
