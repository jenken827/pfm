package api

import (
	"io"
	"net/http"

	"google.golang.org/protobuf/encoding/protojson"
	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// maxLoginBodyBytes 限制登录请求体大小（仅两个短字段，防止超大 body）。
const maxLoginBodyBytes = 4 << 10 // 4KB

// LoginHandler 本地认证登录
//
// POST /api/auth/login
// 请求体：native.LoginReq（proto3 JSON）
// 响应 200：native.LoginResp（proto3 JSON）
// 响应 401：凭据错误（前端依赖该状态码判定登录失败）
//
// 说明：登录是鉴权之前的端点，不参与统一的鉴权路由链（restAuthMiddleware），
// 响应也不使用 BaseResp 包装——前端依赖 HTTP 状态码区分凭据错误与系统错误。
// 请求/响应结构仍取自 proto 契约（native.LoginReq / native.LoginResp），
// 避免前后端各自手写 JSON 结构。
//
// 暴力破解防护：全局 RateLimitHTTPMiddleware（10 req/s/endpoint/IP）+ bcrypt 计算成本。
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	req := &native.LoginReq{}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxLoginBodyBytes))
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(body) > 0 {
		if err := protojson.Unmarshal(body, req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
	}

	username := req.GetUsername()
	password := req.GetPassword()
	if username == "" || password == "" {
		http.Error(w, "username and password are required", http.StatusBadRequest)
		return
	}

	la := auth.GetLocalAuth()
	userID, err := la.Authenticate(username, password)
	if err != nil {
		llog.L().Warnf("login failed: user=%s err=%v", username, err)
		http.Error(w, "invalid username or password", http.StatusUnauthorized)
		return
	}

	user, err := db.GetUserByID(userID)
	if err != nil {
		llog.L().Errorf("login: get user by id %s failed: %v", userID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	token, err := la.IssueToken(user)
	if err != nil {
		llog.L().Errorf("login: issue token failed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := &native.LoginResp{
		Token:   &token,
		UserId:  user.UserId,
		Name:    user.Username,
		Email:   user.Email,
		IsAdmin: boolPtr(db.IsAdmin(userID)),
		Avatar:  user.Avatar,
	}
	data, err := protojson.Marshal(resp)
	if err != nil {
		llog.L().Errorf("login: marshal response failed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	llog.L().Infof("login success: user=%s, user_id=%s", username, userID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// boolPtr 返回 bool 的指针（proto optional 字段需要）。
func boolPtr(b bool) *bool { return &b }
