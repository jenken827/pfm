package llog

import (
	"github.com/jenken827/pfm-plugin-sdk/driver/types"
)

// driverLog 把驱动层的日志出口接到宿主日志
//
// 驱动层（driver/types 与各驱动实现）只声明最小日志接口，实现由运行环境注入：
// 内置驱动跑在宿主进程里，接这里；插件跑在独立进程里，由插件 SDK 接到 stderr。
// 这样 driver/types 不必 import 宿主日志实现，插件二进制也就不会被拖进宿主的
// config / globals。
type driverLog struct{}

func (driverLog) Debugf(format string, args ...any) { L().Debugf(format, args...) }
func (driverLog) Infof(format string, args ...any)  { L().Infof(format, args...) }
func (driverLog) Warnf(format string, args ...any)  { L().Warnf(format, args...) }
func (driverLog) Errorf(format string, args ...any) { L().Errorf(format, args...) }

// initDriverLog 把驱动层日志接到当前宿主 logger（Init 之后调用）
func initDriverLog() {
	types.SetLogger(driverLog{})
}
