package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/storage"
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

	// Apply recover middleware (panic recovery)
	handlerWithRecover := middleware.HTTPRecoverMiddleware(handlerWithRateLimit)

	route(mux)

	schema := config.GetConfig().Schema
	addr := schema.Addr
	port := schema.Port

	// Debug 模式：使用临时端口
	if globals.Debug {
		if globals.DebugPort > 0 {
			port = uint32(globals.DebugPort)
		} else {
			// 使用随机可用端口
			listener, err := net.Listen("tcp", fmt.Sprintf("%s:0", addr))
			if err != nil {
				llog.L().Errorf("Failed to get random port: %v", err)
				os.Exit(1)
			}
			port = uint32(listener.Addr().(*net.TCPAddr).Port)
			listener.Close()
		}
		llog.L().Infof("Debug mode enabled, using port %d", port)
	}

	s := &http.Server{
		Addr:      fmt.Sprintf("%s:%d", addr, port),
		Handler:   handlerWithRecover,
		Protocols: p,
	}

	// 配置 HTTP/2
	if err := http2.ConfigureServer(s, http2Server); err != nil {
		llog.L().Errorf("Configure HTTP/2 failed: %v", err)
		os.Exit(1)
	}

	// 服务启动错误通道
	serverErr := make(chan error, 1)

	// 使用单独协程启动服务
	go func() {
		defer func() {
			if r := recover(); r != nil {
				llog.L().Errorf("[SERVER PANIC] %v", r)
				serverErr <- fmt.Errorf("server panic: %v", r)
			}
		}()
		llog.L().Infof("Starting server on %s:%d", addr, port)
		if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// 监听结束信号或启动错误
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	fmt.Printf("Started server on %s:%d\n", addr, port)

	select {
	case err := <-serverErr:
		// 服务启动失败
		llog.L().Errorf("Server failed to start: %v", err)
		// 关闭所有存储驱动
		storage.CloseAll()
		os.Exit(1)
	case <-quit:
		// 收到结束信号
		llog.L().Info("Shutting down server...")

		// 创建超时上下文
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// 优雅关闭 HTTP 服务
		if err := s.Shutdown(ctx); err != nil {
			llog.L().Errorf("Server shutdown error: %v", err)
		}

		// 关闭所有存储驱动
		storage.CloseAll()

		llog.L().Info("Server exited")
	}
}
