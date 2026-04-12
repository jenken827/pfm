package api

import (
	"net/http"
	"time"

	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/pkg/sharelink"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

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
