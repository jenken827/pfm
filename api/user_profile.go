package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/server/middleware"
	"jenken827.com/jenken827/pfm/server/response"
	"google.golang.org/protobuf/encoding/protojson"
)

// maxAvatarUploadBytes 单个头像上传的请求体上限（含 multipart 开销）。
const maxAvatarUploadBytes = 9 << 20 // 9MB

// UpdateProfile 更新当前用户资料（用户名 / 邮箱，仅本人）
func (s *UserSpaceServer) UpdateProfile(ctx context.Context, req *native.UpdateProfileReq) (*native.BaseResp, error) {
	resp, err := service.UpdateProfile(ctx, req)
	if err != nil {
		return response.ErrResp(err)
	}
	return response.SuccessResp(resp)
}

// UploadAvatarHandler 上传当前用户头像（multipart/form-data，字段名 file）。
//
// 说明：走裸 HTTP 而非统一分发器，因为 multipart 二进制体不适合 proto3 JSON。
// 鉴权与其他 REST 业务接口一致（AuthHTTPMiddleware）；用户身份取自 token，不接受指定他人。
//
// POST /api/user/profile/avatar
// 请求：multipart/form-data，字段 file=<图片>
// 响应 200：BaseResp{data: UploadAvatarResp{avatar: "/api/user/avatar/<id>?v=<ts>"}}
func UploadAvatarHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAvatarError(w, http.StatusMethodNotAllowed, native.RespCode_PARAMS_ERROR, "method not allowed")
		return
	}

	claims, err := middleware.AuthHTTPMiddleware(w, r)
	if err != nil {
		writeAvatarError(w, http.StatusUnauthorized, native.RespCode_TOKEN_INVALID, err.Error())
		return
	}
	ctx := r.Context()

	// 限制请求体大小，防止超大上传打爆内存
	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarUploadBytes)
	if err := r.ParseMultipartForm(maxAvatarUploadBytes); err != nil {
		llog.C(ctx).Errorf("parse multipart failed: %v", err)
		writeAvatarError(w, http.StatusBadRequest, native.RespCode_PARAMS_ERROR, "invalid multipart form")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeAvatarError(w, http.StatusBadRequest, native.RespCode_PARAMS_ERROR, "field 'file' is required")
		return
	}
	defer file.Close()

	raw, err := io.ReadAll(io.LimitReader(file, maxAvatarUploadBytes))
	if err != nil {
		llog.C(ctx).Errorf("read avatar upload failed: %v", err)
		writeAvatarError(w, http.StatusBadRequest, native.RespCode_PARAMS_ERROR, "read upload failed")
		return
	}

	resp, mErr := service.SaveAvatar(ctx, claims.User.Id, raw)
	if mErr != nil {
		// 业务错误：HTTP 200 + BaseResp.code，与统一分发器语义一致
		writeAvatarError(w, http.StatusOK, mErr.C, mErr.E.Error())
		return
	}
	writeAvatarSuccess(w, resp)
}

// AvatarHandler 公开读取用户头像。
//
// 头像为低敏感度资源，且需在 <img>/Image.network 中直接引用（无法带 Authorization
// 头），故设计为公开读。URL 形如 /api/user/avatar/<userID>。
//
// GET /api/user/avatar/{userID}
func AvatarHandler(w http.ResponseWriter, r *http.Request) {
	userID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/user/avatar/"), "/")
	if userID == "" {
		http.NotFound(w, r)
		return
	}

	path := service.AvatarFilePath(userID)
	f, err := os.Open(path)
	if err != nil {
		// 未设置头像：返回 404，前端回退到默认占位图
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	// 头像可能被跨域前端引用
	w.Header().Set("Access-Control-Allow-Origin", "*")
	http.ServeContent(w, r, userID+".jpg", info.ModTime(), f)
}

// writeAvatarResp 将 BaseResp 以 proto3 JSON 写出。
func writeAvatarResp(w http.ResponseWriter, status int, resp *native.BaseResp) {
	data, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(resp)
	if err != nil {
		http.Error(w, "marshal response failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

// writeAvatarSuccess 以 proto3 JSON 输出成功响应（BaseResp 包装）。
func writeAvatarSuccess(w http.ResponseWriter, resp *native.UploadAvatarResp) {
	wrapped, err := response.SuccessResp(resp)
	if err != nil {
		writeAvatarError(w, http.StatusInternalServerError, native.RespCode_INTERNAL_ERROR, "marshal failed")
		return
	}
	writeAvatarResp(w, http.StatusOK, wrapped)
}

// writeAvatarError 以统一 BaseResp 结构输出错误。
func writeAvatarError(w http.ResponseWriter, status int, code native.RespCode, message string) {
	wrapped, err := response.ErrResp(&models.Err{C: code, E: errors.New(message)})
	if err != nil {
		http.Error(w, message, status)
		return
	}
	writeAvatarResp(w, status, wrapped)
}
