package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/service"
	"jenken827.com/jenken827/pfm/internal/thumbnail"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/server/middleware"
	"jenken827.com/jenken827/pfm/utils"
)

// PreviewFileHandler 文件预览HTTP handler
// 默认返回缩略图（如果存在），添加 ?original=true 参数预览原图
func PreviewFileHandler(w http.ResponseWriter, r *http.Request) {
	// 1. 验证认证（票据或请求头）
	claims, err := authenticateFileRequest(w, r)
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

	// 3. 解析缩略图尺寸参数（默认 small，边长由配置 thumbnail.small / large 决定）
	thumbnailSize := thumbnail.SmallSize()
	if sizeParam := r.URL.Query().Get("size"); sizeParam == "large" {
		thumbnailSize = thumbnail.LargeSize()
	}

	// 4. 如果不是请求原图，尝试返回缩略图
	if !original {
		if serveThumbnail(w, r, fileID, thumbnailSize) {
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

	// 5.1 更新文件访问时间（异步，不阻塞响应）
	utils.Go(func() { service.UpdateFileAccess(fileID) })

	// 6. 处理 Telegram 大文件预下载状态
	if result.IsPreDownload {
		respondPreDownload(w, result.PreDownloadData)
		return
	}
	defer result.Reader.Close()

	// 7. 流式传输文件
	streamResponse(w, r, result, "inline")
}

// authenticateFileRequest 文件流的两种凭证，票据优先：
//  1. `?ticket=<单文件票据>`：浏览器 / WebView / 浏览器下载器用（带不了请求头），
//     单文件、短期、不暴露 access token；
//  2. `Authorization: Bearer <JWT>`：App 与其他能带请求头的调用方。
//
// **不再支持 `?token=<JWT>`**：access token 是账户级、长期有效的凭证，写进 URL 会
// 留在浏览器历史、下载历史与 Referer 里，泄露一次等于交出全部文件读权限。
func authenticateFileRequest(w http.ResponseWriter, r *http.Request) (*auth.Claims, error) {
	if r.URL.Query().Get("ticket") != "" {
		return middleware.AuthPreviewTicketHTTP(w, r)
	}
	return middleware.AuthHTTPMiddleware(w, r)
}

// DownloadFileHandler 文件下载HTTP handler（强制下载，支持断点续传）
//
// 鉴权见 authenticateFileRequest：浏览器下载用 ?ticket=（12 小时下载票，覆盖
// 暂停 / 断网续传），App 用 Authorization 头。日志只记录 r.URL.Path，票据不落盘。
func DownloadFileHandler(w http.ResponseWriter, r *http.Request) {
	// 1. 验证认证（票据或请求头）
	claims, err := authenticateFileRequest(w, r)
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

	// 3.1 更新文件访问时间（异步，不阻塞响应）
	utils.Go(func() { service.UpdateFileAccess(fileID) })

	// 3.2 处理 Telegram 大文件预下载状态
	if result.IsPreDownload {
		respondPreDownload(w, result.PreDownloadData)
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
func serveThumbnail(w http.ResponseWriter, r *http.Request, fileID string, size thumbnail.ThumbnailSize) bool {
	thumbnailPath, needGenerate, file := service.GetThumbnailPath(fileID, size)

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
		// 注意：必须用后台 ctx。r.Context() 在 handler 返回时即被取消，
		// 若把请求 ctx 传给异步任务，任务里的下游调用（如 Telegram getFile）
		// 会随 ctx 取消而失败（"context canceled"），缩略图永远无法生成。
		utils.Go(func() {
			service.RegenerateThumbnail(context.Background(), file)
		})
	}

	return false
}

// streamFileHistory 流式传输文件历史版本
//
// 票据场景下 claims.PreviewFileID 是票据绑定的文件，必须与历史版本的宿主文件一致
// （由 service.GetFileHistoryStream 校验），否则一张 A 文件的票能读全部历史版本。
func streamFileHistory(w http.ResponseWriter, r *http.Request, isPreview bool) {
	// 1. 验证认证（票据或请求头）
	claims, err := authenticateFileRequest(w, r)
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

	// 3. 获取文件历史流（票据必须绑定该历史版本的宿主文件）
	result, err := service.GetFileHistoryStream(ctx, id, isPreview, claims.PreviewFileID)
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

// preReadStream 预读 1 字节并回退，验证 lazy reader 可用
// MultiChunkReader 的首次 Read 才真正打开分片（本地文件 open / 远端下载），
// 若失败发生在 header 写出之后，连接会被截断，前端表现为
// "Connection closed before full header received"；预读把错误提前到
// WriteHeader 之前，可返回明确的 500 错误。
func preReadStream(reader interface {
	io.Reader
	io.Seeker
}) error {
	buf := make([]byte, 1)
	n, err := reader.Read(buf)
	if err != nil && err != io.EOF {
		return err
	}
	if n > 0 {
		if _, seekErr := reader.Seek(-int64(n), io.SeekCurrent); seekErr != nil {
			return fmt.Errorf("seek back after pre-read failed: %w", seekErr)
		}
	}
	return nil
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

	// 预读验证（在写出 206 头之前）：lazy reader 的打开失败发生在首次 Read，
	// 提前失败可返回明确错误而非截断连接
	if err := preReadStream(result.Reader); err != nil {
		llog.L().Errorf("pre-read range stream failed: %v", err)
		http.Error(w, "file stream failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 设置响应头（206 Partial Content）
	w.Header().Set("Content-Disposition", contentDisposition(disposition, result.FileName))
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, result.FileSize))
	w.WriteHeader(http.StatusPartialContent) // 206

	// 传输指定范围的数据（pacedWriter 记录发送节奏，用来区分"服务端慢"和"客户端不收"）
	pace := newPacedWriter(w, result.FileName, end-start+1)
	defer pace.stop()
	_, err = io.CopyN(pace, result.Reader, end-start+1)
	if err != nil && err != io.EOF {
		llog.L().Errorf("stream range failed: %v", err)
		return
	}

	llog.L().Infof("range stream completed: %s, bytes %d-%d/%d", result.FileName, start, end, result.FileSize)
}

// streamFullResponse 处理完整文件传输
func streamFullResponse(w http.ResponseWriter, r *http.Request, result *service.FileStreamResult, disposition string) {
	// 预读验证（在写出 200 头之前）：lazy reader 的打开失败发生在首次 Read，
	// 提前失败可返回明确错误而非截断连接（前端报 Connection closed 的根因）
	if err := preReadStream(result.Reader); err != nil {
		llog.L().Errorf("pre-read file stream failed: %v", err)
		http.Error(w, "file stream failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Disposition", contentDisposition(disposition, result.FileName))
	if result.FileSize > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(result.FileSize, 10))
	}
	w.WriteHeader(http.StatusOK) // 200

	// 流式传输文件内容（pacedWriter 记录发送节奏，见 pacedWriter 注释）
	pace := newPacedWriter(w, result.FileName, result.FileSize)
	defer pace.stop()
	written, err := io.Copy(pace, result.Reader)
	if err != nil {
		llog.L().Errorf("stream file failed: %v", err)
		return
	}

	llog.L().Infof("file stream completed: %s, %d bytes sent, disposition: %s", result.FileName, written, disposition)
}

// pacedWriter 给流式响应加"发送节奏"日志。
//
// 排查「视频卡顿」时最容易搞混的两件事：服务端发得慢，还是客户端收得慢?
// io.Copy 的写操作会被 TCP 反压住 —— 客户端不往下读，这里的 Write 就阻塞。
// 所以每 5 秒打一行「已发字节 / 耗时 / 这 5 秒的速率」（写不动时速率会掉到 0）：
//   - 速率长期只有 0.1~0.5MB/s 且耗时与客户端播放时长相当 ⇒ 链路/客户端瓶颈；
//   - 几十毫秒发完几 MB ⇒ 服务端与被测机之间的缓冲就够了，卡顿在客户端那侧；
//   - 中途速率掉到 0 好几秒 ⇒ 客户端在停顿（解码/缓冲/切网）。
//
// 注意：内核 socket 缓冲会"吸收"前几 MB，所以服务端写快 ≠ 客户端收到得快，
// 结论要结合客户端侧上报的「已缓冲秒数」一起看。
type pacedWriter struct {
	w        io.Writer
	name     string
	expected int64

	start  time.Time
	mu     sync.Mutex
	total  int64
	last   int64
	lastAt time.Time
	done   chan struct{}
	once   sync.Once
}

// 每隔这么久打一行节奏日志（写不动也会打，用来发现停顿）
const pacedInterval = 5 * time.Second

// 少于这么多字节的响应（Range 探测、缩略图式的 0-0 试探）不打日志，避免刷屏
const pacedMinReportBytes = 256 << 10

func newPacedWriter(w io.Writer, name string, expected int64) *pacedWriter {
	now := time.Now()
	p := &pacedWriter{w: w, name: name, expected: expected, start: now, lastAt: now, done: make(chan struct{})}
	utils.Go(p.loop)
	return p
}

// stop 结束节奏日志（handler 返回时调用），并把收尾统计打一行
func (p *pacedWriter) stop() {
	p.once.Do(func() {
		close(p.done)
		p.report(time.Now())
	})
}

func (p *pacedWriter) loop() {
	ticker := time.NewTicker(pacedInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.done:
			return
		case now := <-ticker.C:
			p.report(now)
		}
	}
}

func (p *pacedWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.mu.Lock()
	p.total += int64(n)
	p.mu.Unlock()
	return n, err
}

func (p *pacedWriter) report(now time.Time) {
	p.mu.Lock()
	total, last := p.total, p.last
	window := now.Sub(p.lastAt)
	p.last, p.lastAt = total, now
	p.mu.Unlock()

	if total < pacedMinReportBytes {
		return
	}
	rate := float64(total-last) / window.Seconds() / (1024 * 1024)
	llog.L().Infof(
		"stream pacing: %s, total %d/%d bytes, elapsed %s, last %s: %d bytes %.2f MB/s",
		p.name, total, p.expected, now.Sub(p.start).Round(time.Millisecond),
		window.Round(time.Millisecond), total-last, rate,
	)
}

// contentDisposition 组装 RFC 6266 的 Content-Disposition。
//
// 只写 `filename="原始名"` 时中文名在部分浏览器 / 代理下会乱码（header 按
// latin-1 解释）：这里同时给 ASCII 回退名和 RFC 5987 的 `filename*=UTF-8”…`，
// 现代浏览器优先后者，老客户端退回前者。
func contentDisposition(disposition, fileName string) string {
	return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`,
		disposition, asciiFallbackName(fileName), rfc5987Escape(fileName))
}

// asciiFallbackName 把非 ASCII / 控制字符 / 引号替换成 '_'（老客户端只看 filename）
func asciiFallbackName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "unnamed"
	}
	return b.String()
}

// rfc5987Escape 按 RFC 5987 的 attr-char 集合做百分号编码（UTF-8 字节）
func rfc5987Escape(s string) string {
	const attrChar = "!#$&+-.^_`|~"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			strings.IndexByte(attrChar, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
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
	// 鉴权类拒绝：票据绑定的文件与请求不符、非所有者 —— 403，不能报成服务器故障
	case errors.Is(err, service.ErrTicketFileMismatch):
		return http.StatusForbidden
	case strings.Contains(errMsg, "permission denied"):
		return http.StatusForbidden
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

// RawFilePreviewHandler 原始文件预览（用于 Flyfish Viewer 与网页预览）
// 返回完整文件二进制，带鉴权，支持 CORS
// 鉴权：?ticket=（单文件票据）或 Authorization header，见 authenticateFileRequest
func RawFilePreviewHandler(w http.ResponseWriter, r *http.Request) {
	// 1. 验证认证
	claims, err := authenticateFileRequest(w, r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	llog.L().Infof("raw file preview request from user: %s", claims.User.Name)

	// 2. 获取参数
	fileID := r.URL.Query().Get("file_id")
	if fileID == "" {
		http.Error(w, "file_id is required", http.StatusBadRequest)
		return
	}

	// 3. CORS 头（允许 file-viewer iframe 访问）
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	// 响应不允许进浏览器缓存：请求 URL 里带着预览票据，缓存下来等于延长凭证寿命
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// 4. 获取文件流
	result, err := service.GetFileStream(ctx, fileID, nil)
	if err != nil {
		llog.L().Errorf("get file stream failed: %v", err)
		http.Error(w, err.Error(), httpStatusFromError(err))
		return
	}

	// 4.1 更新文件访问时间
	utils.Go(func() { service.UpdateFileAccess(fileID) })

	// 4.2 处理 Telegram 大文件预下载状态
	//
	// 必须和 PreviewFileHandler / DownloadFileHandler 保持一致：此时 result.Reader
	// 是 nil（service 只回状态，后台已异步预下载），继续往下走会在
	// `defer result.Reader.Close()` / 首次 Read 时 nil pointer panic → HTTP 500。
	if result.IsPreDownload {
		respondPreDownload(w, result.PreDownloadData)
		return
	}

	if result.Reader != nil {
		defer result.Reader.Close()
	}

	// 5. 流式传输（inline，浏览器直接显示）
	streamResponse(w, r, result, "inline")
}

// respondPreDownload 回 202 + 预下载状态，提示客户端稍后重试。
//
// 大文件（驱动声明了 PREDOWNLOAD 能力）首次访问时尚未落到本地缓存，
// 服务端不阻塞请求（bot-api 拉 30MB+ 可能几十秒），而是触发异步预下载并立即返回本响应。
// 缓存完成后（cache_status=CACHED）同一个请求会正常流式返回。
//
// 三个流式 handler 都必须走这里，漏一个就是 nil Reader → HTTP PANIC（见 BUGS.md）。
func respondPreDownload(w http.ResponseWriter, data map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", "5")
	// 202 是临时状态，别让浏览器 / 代理缓存下来，否则缓存就绪后仍拿到「正在准备」
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		llog.L().Errorf("write pre-download response failed: %v", err)
	}
}
