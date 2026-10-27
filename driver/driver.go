package driver

import (
	// 隐式加载所有驱动
	_ "jenken827.com/jenken827/pfm/driver/aliyundrive_open"
	_ "jenken827.com/jenken827/pfm/driver/cloud189pc"
	_ "jenken827.com/jenken827/pfm/driver/local"
	_ "jenken827.com/jenken827/pfm/driver/pan123"
	_ "jenken827.com/jenken827/pfm/driver/telegram"
	_ "jenken827.com/jenken827/pfm/driver/yun139"
)
