package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/server/middleware"
)

// PreviewFileHandler 文件预览HTTP handler
// 默认返回缩略图（如果存在），添加 ?original=true 参数预览原图
func PreviewFileHandler(w http.ResponseWriter, r *http.Request) {
	// 1. 验证认证
	claims, err := middleware.AuthHTTPMiddleware(w, r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	llog.L().Infof("preview file request from user: %s", claims.User.Name)

	// 2. 获取参数
	fileID := r.URL.Query().Get("file_id")
	if fileID == "" {
		http.Error(w, "file_id is required", http.StatusBadRequest)
		return
	}
	original := r.URL.Query().Get("original") == "true"

	// 3. 如果不是请求原图，尝试返回缩略图
	if !original {
		if serveThumbnail(w, r, fileID) {
			return
		}
	}

	// 4. 获取指定的 provider_id（可选）
	providerID := parseProviderID(r)

	// 5. 获取文件流
	result, err := service.GetFileStream(ctx, fileID, providerID)
	if err != nil {
		llog.L().Errorf("get file stream failed: %v", err)
		http.Error(w, err.Error(), httpStatusFromError(err))
		return
	}

	// 6. 处理 Telegram 大文件预下载状态
	if result.IsPreDownload {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(result.PreDownloadData)
		return
	}
	defer result.Reader.Close()

	// 7. 流式传输文件
	streamResponse(w, r, result, "inline")
}

// DownloadFileHandler 文件下载HTTP handler（强制下载，支持断点续传）
func DownloadFileHandler(w http.ResponseWriter, r *http.Request) {
	// 1. 验证认证
	claims, err := middleware.AuthHTTPMiddleware(w, r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	llog.L().Infof("download file request from user: %s", claims.User.Name)

	// 2. 获取参数
	fileID := r.URL.Query().Get("file_id")
	if fileID == "" {
		http.Error(w, "file_id is required", http.StatusBadRequest)
		return
	}
	providerID := parseProviderID(r)

	// 3. 获取文件流
	result, err := service.GetFileStream(ctx, fileID, providerID)
	if err != nil {
		llog.L().Errorf("get file stream failed: %v", err)
		http.Error(w, err.Error(), httpStatusFromError(err))
		return
	}
	defer result.Reader.Close()

	// 4. 流式传输文件
	streamResponse(w, r, result, "attachment")
}

// PreviewFileHistoryHandler 预览文件历史版本HTTP handler
func PreviewFileHistoryHandler(w http.ResponseWriter, r *http.Request) {
	streamFileHistory(w, r, true)
}

// DownloadFileHistoryHandler 下载文件历史版本HTTP handler
func DownloadFileHistoryHandler(w http.ResponseWriter, r *http.Request) {
	streamFileHistory(w, r, false)
}

// serveThumbnail 提供缩略图服务，返回是否成功
func serveThumbnail(w http.ResponseWriter, r *http.Request, fileID string) bool {
	thumbnailPath, needGenerate, file := service.GetThumbnailPath(fileID)

	if thumbnailPath != "" {
		// 缩略图存在，直接返回
		llog.L().Debugf("serving thumbnail: %s", thumbnailPath)
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, r, thumbnailPath)
		return true
	}

	// 缩略图不存在，触发异步生成
	if needGenerate && file != nil {
		llog.L().Infof("thumbnail not found, triggering async generation: %s", fileID)
		go service.RegenerateThumbnail(r.Context(), file)
	}

	return false
}

// streamFileHistory 流式传输文件历史版本
func streamFileHistory(w http.ResponseWriter, r *http.Request, isPreview bool) {
	// 1. 验证认证
	claims, err := middleware.AuthHTTPMiddleware(w, r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()

	actionType := "download"
	if isPreview {
		actionType = "preview"
	}
	llog.L().Infof("%s file history request from user: %s", actionType, claims.User.Name)

	// 2. 获取历史版本 ID
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	// 3. 获取文件历史流
	result, err := service.GetFileHistoryStream(ctx, id, isPreview)
	if err != nil {
		llog.L().Errorf("get file history stream failed: %v", err)
		http.Error(w, err.Error(), httpStatusFromError(err))
		return
	}
	defer result.Reader.Close()

	// 4. 流式传输
	disposition := "attachment"
	if isPreview {
		disposition = "inline"
	}
	streamResponse(w, r, result, disposition)

	llog.L().Infof("file history %s completed: %s", actionType, result.FileName)
}

// streamResponse 流式传输响应
func streamResponse(w http.ResponseWriter, r *http.Request, result *service.FileStreamResult, disposition string) {
	w.Header().Set("Content-Type", result.ContentType)
	w.Header().Set("Accept-Ranges", "bytes")

	// 处理断点续传（Range 请求）
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		streamRangeResponse(w, r, result, disposition, rangeHeader)
	} else {
		streamFullResponse(w, r, result, disposition)
	}
}

// streamRangeResponse 处理 Range 请求（断点续传）
func streamRangeResponse(w http.ResponseWriter, r *http.Request, result *service.FileStreamResult, disposition string, rangeHeader string) {
	// 解析 Range 头
	start, end, err := parseRangeHeader(rangeHeader, result.FileSize)
	if err != nil {
		http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	// Seek 到起始位置
	_, err = result.Reader.Seek(start, io.SeekStart)
	if err != nil {
		llog.L().Errorf("seek failed: %v", err)
		http.Error(w, "seek failed", http.StatusInternalServerError)
		return
	}

	// 设置响应头（206 Partial Content）
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, result.FileName))
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, result.FileSize))
	w.WriteHeader(http.StatusPartialContent) // 206

	// 传输指定范围的数据
	_, err = io.CopyN(w, result.Reader, end-start+1)
	if err != nil && err != io.EOF {
		llog.L().Errorf("stream range failed: %v", err)
		return
	}

	llog.L().Infof("range stream completed: %s, bytes %d-%d/%d", result.FileName, start, end, result.FileSize)
}

// streamFullResponse 处理完整文件传输
func streamFullResponse(w http.ResponseWriter, r *http.Request, result *service.FileStreamResult, disposition string) {
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, result.FileName))
	if result.FileSize > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(result.FileSize, 10))
	}
	w.WriteHeader(http.StatusOK) // 200

	// 流式传输文件内容
	written, err := io.Copy(w, result.Reader)
	if err != nil {
		llog.L().Errorf("stream file failed: %v", err)
		return
	}

	llog.L().Infof("file stream completed: %s, %d bytes sent, disposition: %s", result.FileName, written, disposition)
}

// parseProviderID 解析 provider_id 参数
func parseProviderID(r *http.Request) *uint32 {
	if pidStr := r.URL.Query().Get("provider_id"); pidStr != "" {
		if pid, err := strconv.ParseUint(pidStr, 10, 32); err == nil {
			id := uint32(pid)
			return &id
		}
	}
	return nil
}

// parseRangeHeader 解析 HTTP Range 头
// 示例: "bytes=1024000-2048000" 或 "bytes=1024000-"
func parseRangeHeader(rangeHeader string, fileSize int64) (start, end int64, err error) {
	// 移除 "bytes=" 前缀
	rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
	if rangeSpec == rangeHeader {
		return 0, 0, fmt.Errorf("invalid range header: %s", rangeHeader)
	}

	// 分割起始和结束位置
	parts := strings.Split(rangeSpec, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range format: %s", rangeSpec)
	}

	// 解析起始位置
	start, err = strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid start: %s", parts[0])
	}

	// 解析结束位置
	if parts[1] == "" {
		// 未指定结束位置，表示到文件末尾
		end = fileSize - 1
	} else {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid end: %s", parts[1])
		}
	}

	// 验证范围
	if start < 0 || end < start || start >= fileSize {
		return 0, 0, fmt.Errorf("range out of bounds: %d-%d/%d", start, end, fileSize)
	}

	// 限制结束位置不超过文件大小
	if end >= fileSize {
		end = fileSize - 1
	}

	return start, end, nil
}

// httpStatusFromError 根据错误类型返回 HTTP 状态码
func httpStatusFromError(err error) int {
	errMsg := err.Error()
	switch {
	case strings.Contains(errMsg, "not found"):
		return http.StatusNotFound
	case strings.Contains(errMsg, "no available provider"):
		return http.StatusServiceUnavailable
	case strings.Contains(errMsg, "hash is empty"):
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
