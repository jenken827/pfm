//go:build windows

package server

import "os"

// reloadSignals Windows 没有 SIGHUP，插件热加载只能靠重启服务。
func reloadSignals() []os.Signal {
	return nil
}
