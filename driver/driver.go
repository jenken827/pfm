package driver

import (
	// 核心只内置 local；其余存储提供者都是独立进程插件（打包成插件的方式见 README「如何开发驱动」第 5 条）
	_ "github.com/jenken827/pfm/driver/local"
)
