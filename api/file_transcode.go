package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"

	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/internal/transcode"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

// TranscodeFileHandler 视频低码率转码（「流畅播放」）HTTP handler
//
// 高码率视频（手机 1080p 常在 15~30Mbps）在带宽受限的链路上没法实时播放，
// 这里按需转出一份 720p / 1.5Mbps 的副本缓存到数据目录，客户端改播这份。
//
// 响应约定：
//   - 200/206：转码文件就绪，直接按 Range 返回 MP4（浏览器 <video> 可 seek）
//   - 202：还在转（或排队），body 是 {"state":"running|queued","progress":0-100}，
//     客户端带 Retry-After 轮询同一个 URL
//   - 501：服务端没有 ffmpeg，功能未启用
//
// 鉴权同其它文件流：?ticket= 或 Authorization 头。
func TranscodeFileHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := authenticateFileRequest(w, r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	fileID := r.URL.Query().Get("file_id")
	if fileID == "" {
		http.Error(w, "file_id is required", http.StatusBadRequest)
		return
	}

	manager := transcode.Default()
	if manager == nil {
		writeTranscodeError(w, http.StatusNotImplemented, "转码未启用：服务端缺少 ffmpeg")
		return
	}
	if !service.CheckOwnership(r.Context(), fileID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	file, err := db.GetFileByID(fileID)
	if err != nil || file == nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	if file.Type == nil || *file.Type != int32(native.FileType_VIDEO) {
		writeTranscodeError(w, http.StatusBadRequest, "只有视频支持流畅播放")
		return
	}
	if file.Hash == nil || *file.Hash == "" {
		http.Error(w, "file has no hash", http.StatusInternalServerError)
		return
	}
	hash := *file.Hash

	// 1. 已转好：直接发文件（Range / seek 走 http.ServeFile）
	if path, ok := manager.Lookup(hash); ok {
		llog.L().Infof("serve transcoded video: %s (user: %s)", stringValue(file.Name), claims.User.Name)
		utils.Go(func() { service.UpdateFileAccess(fileID) })
		// download=1 强制附件：客户端可以用同一个 URL 测下载速率
		serveTranscodedFile(w, r, path, stringValue(file.Name), r.URL.Query().Get("download") == "1")
		return
	}

	// 2. 没转好：入队 / 加入已有任务，回 202 让客户端轮询
	source, err := transcodeSourceURL(fileID, claims.User.Id)
	if err != nil {
		llog.L().Errorf("build transcode source url failed: %v", err)
		writeTranscodeError(w, http.StatusInternalServerError, "构造转码源地址失败")
		return
	}
	status := manager.Request(hash, source)
	writeTranscodeStatus(w, status)
}

// serveTranscodedFile 输出转码好的 MP4
func serveTranscodedFile(w http.ResponseWriter, r *http.Request, path, name string, attachment bool) {
	disposition := "inline"
	if attachment {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", contentDisposition(disposition, name))
	// 带凭证的响应不进缓存（同 RawFilePreviewHandler）
	w.Header().Set("Cache-Control", "no-store")
	// 用 pacedResponseWriter 记录发送节奏（弱网排查：服务端被反压 vs 客户端不收）
	info, err := os.Stat(path)
	var expected int64
	if err == nil {
		expected = info.Size()
	}
	pace := newPacedWriter(w, name, expected)
	defer pace.stop()
	http.ServeFile(&pacedResponseWriter{ResponseWriter: w, pace: pace}, r, path)
}

// pacedResponseWriter 给 http.ServeFile 套一层发送节奏统计
// （ServeFile 自己 copy，只能在 ResponseWriter 上做手脚）
type pacedResponseWriter struct {
	http.ResponseWriter
	pace *pacedWriter
}

func (w *pacedResponseWriter) Write(b []byte) (int, error) {
	return w.pace.Write(b)
}

// transcodeSourceURL 给 ffmpeg 用的源地址
//
// 走自己的流式接口（带 12 小时下载票），这样本地 / 云端 provider 一视同仁：
// 云端文件由服务端自己按 Range 拉取，ffmpeg 需要 seek 时也能正常工作。
// 回环地址避免绕出去，端口取服务监听端口。
func transcodeSourceURL(fileID, userID string) (string, error) {
	ticket, err := service.SignDownloadTicket(fileID, userID)
	if err != nil {
		return "", err
	}
	port := config.GetConfig().Schema.Port
	query := url.Values{}
	query.Set("file_id", fileID)
	query.Set("ticket", ticket)
	query.Set("original", "true") // 不要缩略图，直接要原文件
	return fmt.Sprintf("http://127.0.0.1:%d/api/file/stream/preview?%s", port, query.Encode()), nil
}

// stringValue 空指针安全的字符串取值
func stringValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// writeTranscodeStatus 转码中的 202（带进度，客户端据此显示百分比）
func writeTranscodeStatus(w http.ResponseWriter, status transcode.Status) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Retry-After", "3")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(status); err != nil {
		llog.L().Errorf("write transcode status failed: %v", err)
	}
}

// writeTranscodeError 统一错误响应
func writeTranscodeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		llog.L().Errorf("write transcode error failed: %v", err)
	}
}
