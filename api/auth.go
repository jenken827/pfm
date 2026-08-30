package api

import (
	"encoding/json"
	"net/http"

	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
)

// LoginHandler 本地认证登录
// POST /api/auth/login
// Body: {"username":"xxx", "password":"xxx"}
// Response 200: {"token":"<JWT>", "user":{"id":"<uuid>","name":"xxx","email":"","is_admin":false}}
// Response 401: 用户名或密码错误
// 仅 auth.mode=local 时可用；casdoor 模式下返回 400。
// 暴力破解防护：全局 RateLimitHTTPMiddleware（10 req/s/endpoint/IP）+ bcrypt 计算成本。
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !auth.IsLocalMode() {
		http.Error(w, "local auth is not enabled", http.StatusBadRequest)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Username == "" || req.Password == "" {
		http.Error(w, "username and password are required", http.StatusBadRequest)
		return
	}

	la := auth.GetLocalAuth()
	userID, err := la.Authenticate(req.Username, req.Password)
	if err != nil {
		llog.L().Warnf("login failed: user=%s err=%v", req.Username, err)
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

	llog.L().Infof("local login success: user=%s, user_id=%s", req.Username, userID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":       derefString(user.UserId),
			"name":     derefString(user.Username),
			"email":    derefString(user.Email),
			"is_admin": db.IsAdmin(userID),
		},
	})
}

// AuthConfigHandler 查询当前认证模式（前端登录页据此渲染登录 UI）
// GET /api/auth/config
// Response: {"mode":"casdoor"|"local"}
func AuthConfigHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mode := "casdoor"
	if auth.IsLocalMode() {
		mode = "local"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"mode": mode})
}

// derefString 安全解引用 string 指针
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
