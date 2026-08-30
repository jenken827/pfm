package api

import (
	"encoding/json"
	"net/http"

	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/server/middleware"
)

// GetOrCreateSubfolderHandler 获取有空位的子目录，如果都满了则创建新目录
// POST /api/file/get-or-create-subfolder
// Body: {"parent_id": "xxx", "prefix": "001"}
// Response: {"folder_id": "xxx"}
func GetOrCreateSubfolderHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims, err := middleware.AuthHTTPMiddleware(w, r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	_ = claims

	var req struct {
		ParentID string `json:"parent_id"`
		Prefix   string `json:"prefix"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.ParentID == "" {
		http.Error(w, "parent_id is required", http.StatusBadRequest)
		return
	}
	if req.Prefix == "" {
		req.Prefix = "00" // 默认前缀
	}

	folderID, mErr := service.GetOrCreateSubfolder(r.Context(), req.ParentID, req.Prefix)
	if mErr != nil {
		llog.L().Errorf("GetOrCreateSubfolder failed: %v", mErr.E)
		http.Error(w, mErr.E.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"folder_id": folderID})
}
