//go:build !windows

package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"syscall"
)

// signalReload 给指定进程发 SIGHUP，让运行中的 pfm 重载插件
func signalReload(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("非法 pid: %d", pid)
	}
	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		return fmt.Errorf("发送 SIGHUP 失败: %w", err)
	}
	return nil
}

// processAlive 判断 pid 是否还活着（信号 0 只做权限/存在性检查）
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	// EPERM 说明进程存在、只是不归我们管
	return err == nil || errors.Is(err, syscall.EPERM)
}

// processLooksLikePfm 读 /proc/<pid>/cmdline 确认这个 pid 确实是 pfm，
// 避免 pid 被系统复用后把 SIGHUP 发给无关进程（比如另一个服务的重启）。
func processLooksLikePfm(pid int) bool {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		// 没有 /proc（非 Linux）时无法确认，按「相信 pid 文件」处理
		return true
	}
	return bytes.Contains(raw, []byte("pfm"))
}
