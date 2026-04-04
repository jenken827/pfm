package driver

import (
	// 隐式加载所有驱动
	_ "jenken827.com/jenken827/pfm/driver/local"
	_ "jenken827.com/jenken827/pfm/driver/telegram"
)
