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
)

var vanguardServices []*vanguard.Service = make([]*vanguard.Service, 0)

func route(mux *http.ServeMux) {
	telegramWebhookRoute(mux)

	fileRoute(mux)
	uploadRoute(mux)
	storageProviderRoute(mux)

	// Then create transcoder with all registered services
	transcoder, err := vanguard.NewTranscoder(vanguardServices)
	if err != nil {
		panic(err)
	}
	// Register transcoder to handle both Connect and RESTful requests
	mux.Handle("/", transcoder)
}

// FileRoute 文件服务
func fileRoute(mux *http.ServeMux) {
	file := &api.FileServer{}
	path, handler := nativeconnect.NewFileServiceHandler(
		file,
		// Validation via Protovalidate is almost always recommended
		connect.WithInterceptors(middleware.GInterceptor(), validate.NewInterceptor()),
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
		connect.WithInterceptors(middleware.GInterceptor(), validate.NewInterceptor()),
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
		connect.WithInterceptors(middleware.GInterceptor(), validate.NewInterceptor()),
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
