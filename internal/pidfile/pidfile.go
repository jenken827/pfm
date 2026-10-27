// Package pidfile 记录服务进程号。
//
// 用途：`pfm plugin install` / `pfm plugin reload` 是独立进程，要通知已经在跑的
// 服务重新扫描插件目录（SIGHUP），得先知道它是哪个 pid —— 靠这个文件。
//
// 约定：文件与配置文件同目录，名为 pfm.pid。服务监听成功后写入，正常退出时删除。
//
// 文件内容只当线索用：进程可能被 kill -9 或随机器重启消失，pid 也会被系统复用。
// 所以调用方读到 pid 后还要确认「这个 pid 还活着」且「命令行里确实是 pfm」，
// 确认不过就只提示、不发信号。
package pidfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// fileName 文件名（与配置文件同目录）
const fileName = "pfm.pid"

// Path 返回配置文件对应的 pid 文件路径；配置文件路径为空时返回空串。
func Path(configPath string) string {
	if strings.TrimSpace(configPath) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), fileName)
}

// Write 写入当前进程 pid（先写临时文件再 rename，避免 CLI 读到半截内容）。
func Write(configPath string, pid int) error {
	path := Path(configPath)
	if path == "" {
		return fmt.Errorf("配置文件路径为空，无法确定 pid 文件位置")
	}
	if pid <= 0 {
		return fmt.Errorf("pid 必须为正数: %d", pid)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		return fmt.Errorf("写 pid 文件失败: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("替换 pid 文件失败: %w", err)
	}
	return nil
}

// Read 读取 pid 文件；不存在或内容非法时返回错误。
func Read(configPath string) (int, error) {
	path := Path(configPath)
	if path == "" {
		return 0, fmt.Errorf("配置文件路径为空，无法确定 pid 文件位置")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("pid 文件内容非法: %q", strings.TrimSpace(string(raw)))
	}
	return pid, nil
}

// Remove 删除 pid 文件；文件不存在不算错误。
func Remove(configPath string) error {
	path := Path(configPath)
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 pid 文件失败: %w", err)
	}
	return nil
}
