package response

import (
	"connectrpc.com/connect"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// SuccessResp 成功响应
func SuccessResp(data proto.Message) (*connect.Response[native.BaseResp], error) {
	var anyData *anypb.Any
	if data != nil {
		var err error
		anyData, err = anypb.New(data)
		if err != nil {
			llog.GetLogger().Error("convert data to Anypb failed: ", "data", data, "error", err)
			// 处理错误
			return nil, err
		}
	} else {
		anyData = nil
	}

	res := &native.BaseResp{
		Code:    func() *native.RespCode { code := native.RespCode_SUCCESS; return &code }(),
		Message: func() *string { message := "success"; return &message }(),
		Data:    anyData,
	}
	return connect.NewResponse(res), nil
}

// ErrResp 错误响应
func ErrResp(code native.RespCode, err error) *connect.Response[native.BaseResp] {
	msg := err.Error()
	res := &native.BaseResp{
		Code:    &code,
		Message: &msg,
	}
	return connect.NewResponse(res)
}
