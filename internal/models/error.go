package models

import "jenken827.com/jenken827/pfm/proto/gen/native"

// Err 错误类型
type Err struct {
	E    error
	Code native.RespCode
}
