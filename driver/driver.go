package driver

import (
	// 核心只内置 local；其余存储提供者都是独立进程插件（见 DESIGN §11.4）
	_ "jenken827.com/jenken827/pfm/driver/local"
)
