package server

import (
	"net/http"

	"connectrpc.com/connect"
	"connectrpc.com/validate"
	"connectrpc.com/vanguard"
	"jenken827.com/jenken827/pfm/api"
	"jenken827.com/jenken827/pfm/driver/telegram"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/proto/gen/native/nativeconnect"
	"jenken827.com/jenken827/pfm/server/middleware"
	"jenken827.com/jenken827/pfm/server/webdav"
)

var vanguardServices []*vanguard.Service = make([]*vanguard.Service, 0)

func route(mux *http.ServeMux) {
	healthRoute(mux)
	telegramWebhookRoute(mux)
	webdavRoute(mux)
	fileViewerRoute(mux)

	fileRoute(mux)
	uploadRoute(mux)
	storageProviderRoute(mux)
	backupRoute(mux)           // 备份服务路由
	permissionRoute(mux)       // 权限服务路由
	fileStreamRoute(mux)       // 文件流式传输路由
	publicDownloadRoute(mux)   // 公共下载路由

	// Then create transcoder with all registered services
	transcoder, err := vanguard.NewTranscoder(vanguardServices)
	if err != nil {
		panic(err)
	}
	// Register transcoder to handle both Connect and RESTful requests
	mux.Handle("/", transcoder)
}

// healthRoute 健康检查路由
func healthRoute(mux *http.ServeMux) {
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
}

// fileStreamRoute 文件流式传输路由（预览/下载）
func fileStreamRoute(mux *http.ServeMux) {
	// 文件预览（直接在浏览器中打开，支持缩略图）
	mux.HandleFunc("/api/file/stream/preview", api.PreviewFileHandler)

	// 文件下载（强制下载）
	mux.HandleFunc("/api/file/stream/download", api.DownloadFileHandler)

	// 文件历史版本预览
	mux.HandleFunc("/api/file/history/preview", api.PreviewFileHistoryHandler)

	// 文件历史版本下载
	mux.HandleFunc("/api/file/history/download", api.DownloadFileHistoryHandler)

	// 原始文件预览（用于 Flyfish Viewer）
	// 支持两种路径：
	//   /api/file/preview/raw?file_id=xxx&token=yyy
	//   /api/file/preview/raw/filename.pdf?file_id=xxx&token=yyy（带文件名，便于 viewer 识别格式）
	mux.HandleFunc("/api/file/preview/raw", api.RawFilePreviewHandler)
	mux.HandleFunc("/api/file/preview/raw/", api.RawFilePreviewHandler)
}

// publicDownloadRoute 公共下载路由（无需认证，签名验证）
func publicDownloadRoute(mux *http.ServeMux) {
	mux.HandleFunc("/s/", api.PublicDownloadHandler)
}

// fileViewerRoute Flyfish Viewer 静态文件路由
func fileViewerRoute(mux *http.ServeMux) {
	// 从配置或默认路径加载 file-viewer 静态文件
	viewerDir := "file-viewer"
	fs := http.FileServer(http.Dir(viewerDir))
	mux.Handle("/file-viewer/", http.StripPrefix("/file-viewer/", fs))
}

// FileRoute 文件服务
func fileRoute(mux *http.ServeMux) {
	file := &api.FileServer{}
	path, handler := nativeconnect.NewFileServiceHandler(
		file,
		// Validation via Protovalidate is almost always recommended
		connect.WithInterceptors(middleware.GInterceptor(), middleware.AuthInterceptor(), validate.NewInterceptor()),
	)

	// 1. 注册 Connect RPC handler
	mux.Handle(path, handler)

	// 2. 创建 Vanguard Service 用于 RESTful 支持
	service := vanguard.NewService(path, handler)
	vanguardServices = append(vanguardServices, service)
}

// StorageProviderRoute 存储提供者服务
func storageProviderRoute(mux *http.ServeMux) {
	provider := &api.StorageProviderServer{}
	path, handler := nativeconnect.NewStorageProviderServiceHandler(
		provider,
		connect.WithInterceptors(middleware.GInterceptor(), middleware.AuthInterceptor(), validate.NewInterceptor()),
	)

	mux.Handle(path, handler)

	service := vanguard.NewService(path, handler)
	vanguardServices = append(vanguardServices, service)
}

// 上传服务路由
func uploadRoute(mux *http.ServeMux) {

	upload := &api.UploadServer{}
	path, handler := nativeconnect.NewUploadServiceHandler(
		upload,
		connect.WithInterceptors(middleware.GInterceptor(), middleware.AuthInterceptor(), validate.NewInterceptor()),
	)

	mux.Handle(path, handler)

	service := vanguard.NewService(path, handler)
	vanguardServices = append(vanguardServices, service)
}

// 备份服务路由
func backupRoute(mux *http.ServeMux) {
	backup := &api.BackupServer{}
	path, handler := nativeconnect.NewBackupServiceHandler(
		backup,
		connect.WithInterceptors(middleware.GInterceptor(), middleware.AuthInterceptor(), validate.NewInterceptor()),
	)

	mux.Handle(path, handler)

	service := vanguard.NewService(path, handler)
	vanguardServices = append(vanguardServices, service)
}

// 电报回调路由
func telegramWebhookRoute(mux *http.ServeMux) {
	mux.HandleFunc(globals.TelegramWebhookPath, func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
		sts := storage.GetStorages()
		llog.L().Info("Received Telegram webhook request", r.Header, r.PostForm)
		sts.Range(func(k any, v any) bool {
			st := storage.GetStorage(k.(uint32))
			if st.GetType() != native.StorageProviderType_TELEGRAM {
				return true
			}
			d := st.(*telegram.TelegramDriver)

			if *d.Config.WebhookSecretToken == token {
				d.Bot.WebhookHandler().ServeHTTP(w, r)
				return false
			}
			return true
		})
	})
}

// webdavRoute WebDAV 服务路由
func webdavRoute(mux *http.ServeMux) {
	webdavHandler := webdav.NewHandler("")
	mux.Handle("/dav/", http.StripPrefix("/dav", webdavHandler))
}

// permissionRoute 权限服务路由
func permissionRoute(mux *http.ServeMux) {
	perm := &api.PermissionServer{}
	path, handler := nativeconnect.NewPermissionServiceHandler(
		perm,
		connect.WithInterceptors(middleware.GInterceptor(), middleware.AuthInterceptor(), validate.NewInterceptor()),
	)

	mux.Handle(path, handler)

	service := vanguard.NewService(path, handler)
	vanguardServices = append(vanguardServices, service)
}
