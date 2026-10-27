package server

import (
	"context"
	"fmt"
	"jenken827.com/jenken827/pfm/bootstrap"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/cors"
	"golang.org/x/net/http2"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/pidfile"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/server/middleware"
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

	// 限频参数来自配置（ratelimit 段，未配置的字段用内置默认值）。
	// 缩略图走独立配额，见 middleware.isThumbnailRequest。
	rl := config.GetConfig().RateLimit
	middleware.InitRateLimit(middleware.RateLimitOptions{
		EndpointBurst:   rl.EndpointBurst,
		EndpointRefill:  time.Duration(rl.EndpointRefillMS) * time.Millisecond,
		GlobalBurst:     rl.GlobalBurst,
		GlobalRefill:    time.Duration(rl.GlobalRefillMS) * time.Millisecond,
		ThumbnailBurst:  rl.ThumbnailBurst,
		ThumbnailRefill: time.Duration(rl.ThumbnailRefillMS) * time.Millisecond,
	})

	// Apply rate limiting middleware
	handlerWithRateLimit := middleware.RateLimitHTTPMiddleware(handlerWithCors)

	// Apply request log middleware (请求日志 + request_id + panic recovery)
	handlerWithLog := middleware.HTTPLogMiddleware(handlerWithRateLimit)

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
		Handler:   handlerWithLog,
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

	// 插件热加载信号（SIGHUP）：重新扫描插件目录并重建受影响的 provider 实例
	reload := make(chan os.Signal, 1)
	if sigs := reloadSignals(); len(sigs) > 0 {
		signal.Notify(reload, sigs...)
	}

	fmt.Printf("Started server on %s:%d\n", addr, port)

	// pid 文件：供 `pfm plugin install` / `pfm plugin reload` 找到本进程发 SIGHUP
	if err := pidfile.Write(globals.ConfigPath, os.Getpid()); err != nil {
		// 写不了只是热加载不方便，不该影响服务本身
		llog.L().Warnf("写 pid 文件失败（插件热加载需要手工发信号）: %v", err)
	}

	for {
		select {
		case err := <-serverErr:
			// 服务启动失败
			llog.L().Errorf("Server failed to start: %v", err)
			// 关闭所有存储驱动与插件进程
			storage.CloseAll()
			bootstrap.ClosePlugins()
			if rmErr := pidfile.Remove(globals.ConfigPath); rmErr != nil {
				llog.L().Warnf("删除 pid 文件失败: %v", rmErr)
			}
			os.Exit(1)
		case <-reload:
			// SIGHUP：重新加载插件（装完插件不用重启服务）
			llog.L().Info("收到 SIGHUP，重新加载插件...")
			if err := bootstrap.ReloadPlugins(); err != nil {
				llog.L().Errorf("插件热加载失败: %v", err)
			}
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

			// 关闭所有存储驱动与插件进程
			storage.CloseAll()
			bootstrap.ClosePlugins()

			if err := pidfile.Remove(globals.ConfigPath); err != nil {
				llog.L().Warnf("删除 pid 文件失败: %v", err)
			}

			llog.L().Info("Server exited")
			return
		}
	}
}
