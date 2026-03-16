package server

import (
	"fmt"
	"net/http"

	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/server/middleware"
	"github.com/rs/cors"
	"golang.org/x/net/http2"
)

// Run 启动服务
func Run() {

	mux := http.NewServeMux()
	p := new(http.Protocols)

	p.SetHTTP1(true)

	// Use h2c so we can serve HTTP/2 without TLS.
	p.SetUnencryptedHTTP2(true)

	// 配置 HTTP/2
	http2Server := &http2.Server{
		MaxReadFrameSize: 16 * 1024 * 1024, // 16MB
	}

	cc := config.GetConfig().Cors
	// 创建 CORS 配置
	c := cors.New(cors.Options{
		AllowedOrigins: cc.AllowOrigins,
		AllowedMethods: cc.AllowMethods,
		AllowedHeaders: cc.AllowHeaders,
	})

	// 将 CORS 中间件包装到处理器上
	handlerWithCors := c.Handler(mux)

	// Apply rate limiting middleware
	handlerWithRateLimit := middleware.RateLimitHTTPMiddleware(handlerWithCors)

	route(mux)

	schema := config.GetConfig().Schema
	s := &http.Server{
		Addr:      fmt.Sprintf("%s:%d", schema.Addr, schema.Port),
		Handler:   handlerWithRateLimit,
		Protocols: p,
	}

	// 配置 HTTP/2
	if err := http2.ConfigureServer(s, http2Server); err != nil {
		panic(err)
	}
	fmt.Println("Starting server on localhost:", schema.Port, "...")
	if err := s.ListenAndServe(); err != nil {
		panic(err)
	}
}
