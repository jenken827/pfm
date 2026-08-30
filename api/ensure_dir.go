package api

import (
	"encoding/json"
	"net/http"

	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/server/middleware"
)

// EnsureDirPathHandler 按相对路径逐级确保目录存在（mkdir -p 语义）
// 供相册备份按"年/月"目录归档使用；后端自动创建缺失目录，并发安全。
// POST /api/file/ensure-dir-path
// Body: {"parent_id": "xxx", "path": "2026/07"}
// Response: {"folder_id": "xxx"}
func EnsureDirPathHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, err := middleware.AuthHTTPMiddleware(w, r); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		ParentID string `json:"parent_id"`
		Path     string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.ParentID == "" {
		http.Error(w, "parent_id is required", http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	folderID, mErr := service.EnsureDirPath(r.Context(), req.ParentID, req.Path)
	if mErr != nil {
		llog.L().Errorf("EnsureDirPath failed: %v", mErr.E)
		status := http.StatusInternalServerError
		if mErr.C == native.RespCode_PARAMS_ERROR {
			status = http.StatusBadRequest
		}
		http.Error(w, mErr.E.Error(), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"folder_id": folderID})
}
