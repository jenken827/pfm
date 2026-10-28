package llog

import (
	"context"
	"io"
	"path"
	"strings"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/config"
)

var logger *logrus.Logger

// discardLogger 未初始化时的兜底 logger（测试/未初始化场景防 nil panic）
var discardLogger = func() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}()

// Init 初始化日志
// 使用logrus和lumberjack
// lumberjack控制日志文件的大小和数量
func Init() {
	lc := config.GetConfig().Log

	if lc.Filename == "" {
		lc.Filename = path.Join(globals.DataDir, "logs", "log.log")
	} else if !path.IsAbs(lc.Filename) {
		lc.Filename = path.Join(globals.DataDir, lc.Filename)
	}

	lv := getLogLevel(lc.Level)

	lio := &lumberjack.Logger{
		Filename:   lc.Filename,
		MaxSize:    lc.MaxSize,
		MaxAge:     lc.MaxAge,
		MaxBackups: lc.MaxBackups,
		Compress:   lc.Compress,
	}
	logger = logrus.New()
	formatter := logrus.TextFormatter{
		DisableColors:   true,
		TimestampFormat: "2006-01-02 15:04:05",
		FullTimestamp:   true,
		DisableQuote:    true,
	}
	logger.SetFormatter(&formatter)
	logger.SetReportCaller(true)
	logger.SetOutput(lio)
	logger.SetLevel(lv)

	logger.Info("log init success")

	// 把驱动层的日志出口接到宿主日志（此前只有驱动的直接调用者才会用 llog）
	initDriverLog()
}

// 根据字面值获取日志级别
func getLogLevel(l string) logrus.Level {
	lm := map[string]logrus.Level{
		"debug": logrus.DebugLevel,
		"info":  logrus.InfoLevel,
		"warn":  logrus.WarnLevel,
		"error": logrus.ErrorLevel,
	}

	lv, ok := lm[strings.ToLower(l)]
	if !ok {
		lv = logrus.ErrorLevel
	}

	return lv
}

// SetLogLevel 设置日志级别
func SetLogLevel(l string) {
	lv := getLogLevel(l)
	logger.SetLevel(lv)
}

// L 获取日志实例
func L() *logrus.Logger {
	if logger == nil {
		return discardLogger
	}
	return logger
}

// C 获取绑定请求上下文的 logger 入口。
//
// 若 ctx 中带有 request_id（由 HTTPLogMiddleware 注入），返回的 Entry 会自动携带
// request_id 字段，使该请求内的所有日志可通过 request_id 关联检索；否则返回
// 原始 logger 的 Entry（行为与直接调用 L() 一致）。
//
// 用法：
//
//	llog.C(ctx).Info("something happened")
//	llog.C(ctx).Errorf("failed: %v", err)
func C(ctx context.Context) *logrus.Entry {
	l := L()
	if ctx == nil {
		return logrus.NewEntry(l)
	}
	if id, ok := ctx.Value(globals.ContextRequestIDKey).(string); ok && id != "" {
		return l.WithField("request_id", id)
	}
	return logrus.NewEntry(l)
}

// CtxRequestID 从 ctx 取请求 ID（不存在时返回空串）。
func CtxRequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(globals.ContextRequestIDKey).(string); ok {
		return id
	}
	return ""
}
