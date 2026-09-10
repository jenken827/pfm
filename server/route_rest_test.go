package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jenken827.com/jenken827/pfm/proto/gen/native"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"
)

// 测试注册表：直接注册假业务函数，不触碰 service/db 层。

// okHandler 使用无必填约束的请求类型（FileListReq.parent_id 带 buf.validate required，
// 不适合用来测「空 body」场景）。
func okHandler(_ context.Context, req *native.GetPhotoDateConfigReq) (*native.BaseResp, error) {
	_ = req
	data, err := anypb.New(&native.PhotoDateConfig{})
	if err != nil {
		return nil, err
	}
	code := native.RespCode_SUCCESS
	msg := "success"
	return &native.BaseResp{Code: &code, Message: &msg, Data: data}, nil
}

func decodeBaseResp(t *testing.T, body []byte) *native.BaseResp {
	t.Helper()
	resp := &native.BaseResp{}
	if err := protojson.Unmarshal(body, resp); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, body)
	}
	return resp
}

func TestRegisterPostJSON(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, http.MethodPost, "/api/test/photo-date", okHandler)

	req := httptest.NewRequest(http.MethodPost, "/api/test/photo-date", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeBaseResp(t, rec.Body.Bytes())
	if resp.GetCode() != native.RespCode_SUCCESS {
		t.Fatalf("code = %v, body=%s", resp.GetCode(), rec.Body.String())
	}
	// Any 必须以带 @type 的完整结构展开（前端 unpackInto 依赖）。
	if !strings.Contains(rec.Body.String(), `"@type":"type.googleapis.com/native.PhotoDateConfig"`) {
		t.Fatalf("Any type url missing; body=%s", rec.Body.String())
	}
}

func TestRegisterEmptyBody(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, http.MethodPost, "/api/test/photo-date", okHandler)

	req := httptest.NewRequest(http.MethodPost, "/api/test/photo-date", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200（空 body 视为空消息）; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRegisterValidationEnforced(t *testing.T) {
	mux := http.NewServeMux()
	// FileListReq.parent_id 声明了 buf.validate required，缺失必须被拦截。
	Register(mux, http.MethodPost, "/api/test/list",
		func(context.Context, *native.FileListReq) (*native.BaseResp, error) {
			t.Error("handler 不应被调用：校验应先失败")
			return nil, nil
		})

	req := httptest.NewRequest(http.MethodPost, "/api/test/list", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400（校验失败）; body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeBaseResp(t, rec.Body.Bytes())
	if resp.GetCode() != native.RespCode_PARAMS_ERROR {
		t.Fatalf("code = %v, want PARAMS_ERROR", resp.GetCode())
	}
}

func TestRegisterMethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, http.MethodPost, "/api/test/photo-date", okHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/test/photo-date", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRegisterInvalidJSON(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, http.MethodPost, "/api/test/photo-date", okHandler)

	req := httptest.NewRequest(http.MethodPost, "/api/test/photo-date", strings.NewReader("{not json"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRegisterPanicRecovered(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, http.MethodPost, "/api/test/panic",
		func(context.Context, *native.GetPhotoDateConfigReq) (*native.BaseResp, error) {
			panic("boom")
		})

	req := httptest.NewRequest(http.MethodPost, "/api/test/panic", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// panic 被兜底转为业务错误，HTTP 仍为 200。
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeBaseResp(t, rec.Body.Bytes())
	if resp.GetCode() != native.RespCode_INTERNAL_ERROR {
		t.Fatalf("code = %v, want INTERNAL_ERROR", resp.GetCode())
	}
}
