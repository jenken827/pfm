package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/pkg/sharelink"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/server/middleware"
	"gorm.io/gorm"
)

const (
	// defaultExpireSeconds 默认过期时间一周
	defaultExpireSeconds = 604800
	// shareCodeMaxRetry 分享码生成最大重试次数
	shareCodeMaxRetry = 10
)

// CreateShareLinkHandler 创建分享链接（需认证）
// GET /api/file/share?file_id=xxx&expire=604800&max_access=0&remark=
func CreateShareLinkHandler(w http.ResponseWriter, r *http.Request) {
	// 1. 验证认证
	claims, err := middleware.AuthHTTPMiddleware(w, r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	llog.L().Infof("create share link request from user: %s", claims.User.Name)

	// 2. 获取参数
	fileID := r.URL.Query().Get("file_id")
	if fileID == "" {
		http.Error(w, "file_id is required", http.StatusBadRequest)
		return
	}

	// 验证文件是否存在
	_, err = db.GetFileByID(fileID)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	expireSeconds := int64(defaultExpireSeconds)
	if expireStr := r.URL.Query().Get("expire"); expireStr != "" {
		if expire, err := strconv.ParseInt(expireStr, 10, 64); err == nil && expire > 0 {
			expireSeconds = expire
		}
	}

	neverExpire := false
	if expireStr := r.URL.Query().Get("never_expire"); expireStr == "true" {
		neverExpire = true
	}

	maxAccessCount := uint32(0)
	if maxStr := r.URL.Query().Get("max_access"); maxStr != "" {
		if max, err := strconv.ParseUint(maxStr, 10, 32); err == nil {
			maxAccessCount = uint32(max)
		}
	}

	remark := r.URL.Query().Get("remark")

	// 3. 获取签名密钥
	signToken := config.GetConfig().SignToken
	if signToken == "" {
		http.Error(w, "share link not configured", http.StatusServiceUnavailable)
		return
	}

	// 4. 生成唯一分享码
	var shareCode string
	for i := 0; i < shareCodeMaxRetry; i++ {
		code := sharelink.GenerateShareCode()
		_, err := db.GetFileShareByCode(code)
		if err == gorm.ErrRecordNotFound {
			shareCode = code
			break
		}
		if err != nil {
			llog.L().Errorf("check share code failed: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	if shareCode == "" {
		http.Error(w, "failed to generate share code", http.StatusInternalServerError)
		return
	}

	// 5. 计算过期时间和签名
	var expireAt time.Time
	if !neverExpire {
		expireAt = time.Now().Add(time.Duration(expireSeconds) * time.Second)
	}

	expireAtUnix := int64(0)
	if !neverExpire {
		expireAtUnix = expireAt.Unix()
	}

	signParam := sharelink.BuildSignParam(shareCode, fileID, signToken, expireAtUnix)

	// 6. 写入数据库
	status := int32(native.ShareStatus_ACTIVE)
	fs := &native.FileShareORM{
		ShareCode:      &shareCode,
		FileId:         &fileID,
		Sign:           &signParam,
		NeverExpire:    &neverExpire,
		Status:         &status,
		MaxAccessCount: &maxAccessCount,
		Remark:         &remark,
	}
	if !neverExpire {
		fs.ExpireAt = &expireAt
	}

	if err := db.CreateFileShare(fs); err != nil {
		llog.L().Errorf("create file share failed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// 7. 构建分享链接
	serverURL := config.GetConfig().ServerURL
	if serverURL == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		serverURL = scheme + "://" + r.Host
	}
	shareURL := serverURL + "/s/" + shareCode

	// 8. 返回
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	respData := map[string]interface{}{
		"url":       shareURL,
		"share_code": shareCode,
	}
	if !neverExpire {
		respData["expire_at"] = expireAtUnix
	}
	json.NewEncoder(w).Encode(respData)
}

// CancelShareLinkHandler 取消分享链接（需认证）
// POST /api/file/share/cancel?share_code=xxx
func CancelShareLinkHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := middleware.AuthHTTPMiddleware(w, r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	llog.L().Infof("cancel share link request from user: %s", claims.User.Name)

	shareCode := r.URL.Query().Get("share_code")
	if shareCode == "" {
		http.Error(w, "share_code is required", http.StatusBadRequest)
		return
	}

	fs, err := db.GetFileShareByCode(shareCode)
	if err != nil {
		http.Error(w, "share not found", http.StatusNotFound)
		return
	}

	if fs.Status != nil && *fs.Status == int32(native.ShareStatus_CANCELLED) {
		http.Error(w, "share already cancelled", http.StatusBadRequest)
		return
	}

	if err := db.CancelFileShare(*fs.Id); err != nil {
		http.Error(w, "cancel failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// PublicDownloadHandler 公共下载接口（无需认证，签名验证）
// GET /s/{shareCode}?sign=expireAt:signature
func PublicDownloadHandler(w http.ResponseWriter, r *http.Request) {
	// 1. 从路径中提取 shareCode
	// 路径格式: /s/{shareCode}
	path := r.URL.Path
	shareCode := path[3:] // len("/s/") = 3
	if shareCode == "" {
		http.Error(w, "share_code is required", http.StatusBadRequest)
		return
	}

	// 2. 查询分享记录
	fs, err := db.GetFileShareByCode(shareCode)
	if err != nil {
		http.Error(w, "share not found", http.StatusNotFound)
		return
	}

	// 3. 检查状态
	if fs.Status == nil || *fs.Status != int32(native.ShareStatus_ACTIVE) {
		statusText := "share unavailable"
		if fs.Status != nil {
			switch native.ShareStatus(*fs.Status) {
			case native.ShareStatus_CANCELLED:
				statusText = "share cancelled"
			case native.ShareStatus_EXPIRED:
				statusText = "share expired"
			}
		}
		http.Error(w, statusText, http.StatusGone)
		return
	}

	// 4. 检查过期时间
	if fs.NeverExpire == nil || !*fs.NeverExpire {
		if fs.ExpireAt != nil && fs.ExpireAt.Before(time.Now()) {
			// 异步更新状态为过期
			go func() {
				expiredStatus := int32(native.ShareStatus_EXPIRED)
				db.UpdateFileShare(&native.FileShareORM{Id: fs.Id, Status: &expiredStatus}, []string{"status"})
			}()
			http.Error(w, "share expired", http.StatusGone)
			return
		}
	}

	// 5. 检查访问次数
	if fs.MaxAccessCount != nil && *fs.MaxAccessCount > 0 {
		if fs.AccessCount != nil && *fs.AccessCount >= *fs.MaxAccessCount {
			http.Error(w, "max access count reached", http.StatusGone)
			return
		}
	}

	// 6. 验证签名
	sign := r.URL.Query().Get("sign")
	if sign == "" {
		http.Error(w, "sign is required", http.StatusForbidden)
		return
	}

	signToken := config.GetConfig().SignToken
	if signToken == "" {
		http.Error(w, "share link not configured", http.StatusServiceUnavailable)
		return
	}

	expireAt, signature, err := sharelink.ParseSignURL(sign)
	if err != nil {
		http.Error(w, "invalid sign format", http.StatusForbidden)
		return
	}

	// 检查签名中的过期时间是否与记录一致
	if !*fs.NeverExpire && fs.ExpireAt != nil {
		if expireAt != fs.ExpireAt.Unix() {
			http.Error(w, "invalid sign", http.StatusForbidden)
			return
		}
	}

	if !sharelink.VerifySign(shareCode, *fs.FileId, signToken, signature, expireAt) {
		http.Error(w, "invalid sign", http.StatusForbidden)
		return
	}

	llog.L().Infof("public download request: share_code=%s, file_id=%s", shareCode, *fs.FileId)

	// 7. 增加访问次数
	go db.IncrementShareAccessCount(*fs.Id)

	// 8. 流式传输文件（无需认证，不指定 provider）
	streamFile(r.Context(), w, r, *fs.FileId, "attachment", nil)
}
