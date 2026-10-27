package response

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// SuccessResp 构造成功响应。
// 返回非 nil error 仅在 data 无法转换为 Any 时（调用方应直接返回该 error）。
func SuccessResp(data proto.Message) (*native.BaseResp, error) {
	var anyData *anypb.Any
	if data != nil {
		var err error
		anyData, err = anypb.New(data)
		if err != nil {
			llog.L().Error("convert data to Anypb failed: ", "data", data, "error", err)
			return nil, err
		}
	}

	code := native.RespCode_SUCCESS
	message := "success"
	return &native.BaseResp{
		Code:    &code,
		Message: &message,
		Data:    anyData,
	}, nil
}

// ErrResp 构造错误响应。业务错误通过 BaseResp.Code 表达，HTTP 状态码始终为 200。
func ErrResp(err *models.Err) (*native.BaseResp, error) {
	msg := err.E.Error()
	return &native.BaseResp{
		Code:    &err.C,
		Message: &msg,
	}, nil
}
