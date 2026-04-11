package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"jenken827.com/jenken827/pfm/driver/telegram"
	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/server/middleware"
)

// PreviewFileHandler 文件预览HTTP handler（服务器代理流量，支持多分片、断点续传）
func PreviewFileHandler(w http.ResponseWriter, r *http.Request) {
	// 1. 验证认证
	claims, err := middleware.AuthHTTPMiddleware(w, r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	llog.L().Infof("preview file request from user: %s", claims.User.Name)

	// 2. 获取文件ID
	fileID := r.URL.Query().Get("file_id")
	if fileID == "" {
		http.Error(w, "file_id is required", http.StatusBadRequest)
		return
	}

	// 3. 获取指定的 provider_id（可选）
	var providerID *uint32
	if pidStr := r.URL.Query().Get("provider_id"); pidStr != "" {
		if pid, err := strconv.ParseUint(pidStr, 10, 32); err == nil {
			id := uint32(pid)
			providerID = &id
		}
	}

	// 4. 流式传输文件（支持断点续传）
	streamFile(ctx, w, r, fileID, "inline", providerID)
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

	// 2. 获取文件ID
	fileID := r.URL.Query().Get("file_id")
	if fileID == "" {
		http.Error(w, "file_id is required", http.StatusBadRequest)
		return
	}

	// 3. 获取指定的 provider_id（可选）
	var providerID *uint32
	if pidStr := r.URL.Query().Get("provider_id"); pidStr != "" {
		if pid, err := strconv.ParseUint(pidStr, 10, 32); err == nil {
			id := uint32(pid)
			providerID = &id
		}
	}

	// 4. 流式传输文件（支持断点续传）
	streamFile(ctx, w, r, fileID, "attachment", providerID)
}

// streamFile 流式传输文件（支持多分片自动合并、断点续传）
// 若 providerID 不为空，则强制使用该提供者；否则自动选择最优提供者
func streamFile(ctx context.Context, w http.ResponseWriter, r *http.Request, fileID string, disposition string, providerID *uint32) {
	// 1. 获取文件信息
	file, err := db.GetFileByID(fileID)
	if err != nil {
		llog.L().Errorf("get file by id failed: %v", err)
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	if file.Hash == nil || *file.Hash == "" {
		http.Error(w, "file hash is empty", http.StatusInternalServerError)
		return
	}

	// 2. 查询文件位置
	locations, err := db.GetFileLocationsByBlake3Hash(*file.Hash)
	if err != nil {
		llog.L().Errorf("get file locations failed: %v", err)
		http.Error(w, "file location not found", http.StatusNotFound)
		return
	}

	if len(locations) == 0 {
		http.Error(w, "no file location found", http.StatusNotFound)
		return
	}

	// 3. 选择源提供者（指定 provider_id 则强制使用，否则自动选择最优）
	var sourceProviderID uint32
	var sourceLocations []*native.FileLocationORM
	if providerID != nil {
		sourceProviderID = *providerID
		sourceLocations, err = storage.GetCompleteProviderLocations(locations, sourceProviderID)
		if err != nil {
			llog.L().Errorf("get provider %d locations failed: %v", sourceProviderID, err)
			http.Error(w, "provider location not found", http.StatusNotFound)
			return
		}
	} else {
		sourceProviderID, sourceLocations, err = storage.SelectBestSourceProvider(locations)
		if err != nil {
			llog.L().Errorf("select best source provider failed: %v", err)
			http.Error(w, "no available provider", http.StatusServiceUnavailable)
			return
		}
	}

	llog.L().Infof("stream file: %s, provider: %d, chunks: %d", *file.Name, sourceProviderID, len(sourceLocations))

	// 4. 获取存储实例
	sourceStorage := storage.GetStorage(sourceProviderID)
	if sourceStorage == nil {
		http.Error(w, "storage not found", http.StatusInternalServerError)
		return
	}

	// 5. Telegram 大文件预下载检查
	// 对于大文件（>20MB），如果第一个分片（整体状态）未缓存或缓存失败，触发异步预下载并返回状态
	if sourceStorage.GetType() == native.StorageProviderType_TELEGRAM {
		locSize := sourceLocations[0].Size
		if locSize != nil && int64(*locSize) > telegram.LargeFileSizeThreshold {
			// 检查第一个分片的缓存状态（整体状态）
			cacheStatus := sourceLocations[0].CacheStatus

			// 只有未缓存或缓存失败时才触发预下载（避免重复触发）
			shouldTriggerPreDownload := cacheStatus == nil ||
				*cacheStatus == int32(native.CacheStatus_CACHE_STATUS_UNSPECIFIED) ||
				*cacheStatus == int32(native.CacheStatus_UNCACHED)

			if shouldTriggerPreDownload {
				actualStatus := native.CacheStatus_CACHE_STATUS_UNSPECIFIED
				if cacheStatus != nil {
					actualStatus = native.CacheStatus(*cacheStatus)
				}

				llog.L().Infof("telegram large file pre-download: file_id=%s, provider=%d, size=%d, cache_status=%v, triggering async download",
					fileID, sourceProviderID, *locSize, actualStatus)

				// 检查是否实现了 PreDownloader 接口
				if preDownloader, ok := sourceStorage.(types.PreDownloader); ok {
					// 异步触发预下载（不阻塞当前请求）
					go func() {
						bgCtx := context.Background()
						if err := preDownloader.PreDownload(bgCtx, sourceLocations); err != nil {
							llog.L().Errorf("async pre-download failed: file_id=%s, provider=%d, err=%v",
								fileID, sourceProviderID, err)
						} else {
							llog.L().Infof("async pre-download completed: file_id=%s, provider=%d",
								fileID, sourceProviderID)
						}
					}()
				}

				// 返回预下载状态给前端
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusAccepted)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"status":       "pre_download",
					"provider_id":  sourceProviderID,
					"cache_status": actualStatus,
					"file_id":      fileID,
					"message":      "Large file is being cached, please retry later",
				})
				return
			}
		}
	}

	// 6. 创建加密器（用于解密，如果需要）
	var encryptor types.Encryptor
	if sourceLocations[0].Encrypted != nil && *sourceLocations[0].Encrypted {
		providerOrm := sourceStorage.GetOrm()
		if providerOrm != nil && providerOrm.EncryptionKey != nil {
			salt := ""
			if sourceLocations[0].EncryptionSalt != nil {
				salt = *sourceLocations[0].EncryptionSalt
			}
			encryptor, err = storage.NewCryptEncryptor(*providerOrm.EncryptionKey, salt)
			if err != nil {
				llog.L().Errorf("create encryptor failed: %v", err)
				http.Error(w, "decrypt failed", http.StatusInternalServerError)
				return
			}
		}
	}

	// 7. 打开文件（支持多分片合并、Seek）
	reader, err := sourceStorage.Open(ctx, sourceLocations, encryptor)
	if err != nil {
		llog.L().Errorf("open file failed: %v", err)
		http.Error(w, "open file failed", http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	// 7. 设置响应头
	fileName := "download"
	if file.Name != nil {
		fileName = *file.Name
	}

	contentType := "application/octet-stream"
	if file.Mime != nil && *file.Mime != "" {
		contentType = *file.Mime
	}

	// 文件总大小
	fileSize := int64(0)
	if file.Size != nil {
		fileSize = int64(*file.Size)
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Accept-Ranges", "bytes") // 支持断点续传

	// 8. 处理断点续传（Range 请求）
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		// 解析 Range 头
		start, end, err := parseRangeHeader(rangeHeader, fileSize)
		if err != nil {
			http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}

		// Seek 到起始位置
		_, err = reader.Seek(start, io.SeekStart)
		if err != nil {
			llog.L().Errorf("seek failed: %v", err)
			http.Error(w, "seek failed", http.StatusInternalServerError)
			return
		}

		// 设置响应头（206 Partial Content）
		w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, fileName))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
		w.WriteHeader(http.StatusPartialContent) // 206

		// 传输指定范围的数据
		_, err = io.CopyN(w, reader, end-start+1)
		if err != nil && err != io.EOF {
			llog.L().Errorf("stream range failed: %v", err)
			return
		}

		llog.L().Infof("range stream completed: %s, bytes %d-%d/%d", fileName, start, end, fileSize)
	} else {
		// 完整文件传输
		w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, fileName))
		if fileSize > 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(fileSize, 10))
		}
		w.WriteHeader(http.StatusOK) // 200

		// 流式传输文件内容
		written, err := io.Copy(w, reader)
		if err != nil {
			llog.L().Errorf("stream file failed: %v", err)
			// 无法修改HTTP状态码（已发送部分数据）
			return
		}

		llog.L().Infof("file stream completed: %s, %d bytes sent, disposition: %s", fileName, written, disposition)
	}
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
