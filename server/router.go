package server

import (
	"net/http"

	"connectrpc.com/connect"
	"connectrpc.com/validate"
	"connectrpc.com/vanguard"
	"jenken827.com/jenken827/pfm/api"
	"jenken827.com/jenken827/pfm/proto/gen/native/nativeconnect"
	"jenken827.com/jenken827/pfm/server/middleware"
)

var vanguardServices []*vanguard.Service = make([]*vanguard.Service, 0)

func route(mux *http.ServeMux) {

	fileRoute(mux)
	storageGroupRoute(mux)
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

// StorageGroupRoute 存储组服务
func storageGroupRoute(mux *http.ServeMux) {
	group := &api.StorageGroupServer{}
	path, handler := nativeconnect.NewStorageGroupServiceHandler(
		group,
		connect.WithInterceptors(middleware.GInterceptor(), validate.NewInterceptor()),
	)

	mux.Handle(path, handler)

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
