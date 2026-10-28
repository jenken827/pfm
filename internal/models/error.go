package models

import "github.com/jenken827/pfm-proto/proto/gen/native"

// Err 错误类型
type Err struct {
	E error
	C native.RespCode
}
