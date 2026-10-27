//go:build !windows

package server

import (
	"os"
	"syscall"
)

// reloadSignals 触发插件热加载的信号：SIGHUP 是 unix 的「重载配置」惯例，
// 且默认动作是终止进程 —— 必须由我们接管，否则会被当成退出。
func reloadSignals() []os.Signal {
	return []os.Signal{syscall.SIGHUP}
}
