//go:build windows

package cmd

import (
	"fmt"
	"os"
)

// signalReload Windows 没有 SIGHUP，热加载不可用（只能重启服务）
func signalReload(pid int) error {
	return fmt.Errorf("当前平台不支持 SIGHUP 热加载（pid=%d），请重启服务", pid)
}

// processAlive Windows 下用 FindProcess 近似判断
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = proc.Release()
	return true
}

// processLooksLikePfm Windows 无法可靠读命令行，按「相信 pid 文件」处理
func processLooksLikePfm(pid int) bool { return true }
