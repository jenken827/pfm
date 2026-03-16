package llog

import (
	"path"
	"strings"

	"jenken827.com/jenken827/pfm/flags"
	"jenken827.com/jenken827/pfm/internal/config"
	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

var logger *logrus.Logger

// Init 初始化日志
// 使用slog和lumberjack
// slog是官方日志库，lumberjack控制日志文件的大小和数量
func Init() {
	lc := config.GetConfig().Log

	if lc.Filename == "" {
		lc.Filename = path.Join(flags.DataDir, "logs", "log.log")
	} else if !path.IsAbs(lc.Filename) {
		lc.Filename = path.Join(flags.DataDir, lc.Filename)
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

// GetLogger 获取日志实例
func GetLogger() *logrus.Logger {
	return logger
}
