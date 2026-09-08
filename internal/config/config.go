package config

import (
	"fmt"

	"os"

	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/models"
	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

var cfg *models.Config

// Init 配置初始化
func Init() {
	initViper()
	cfg = defaultConfig()
	// 使用 yaml 标签进行解析
	if err := viper.Unmarshal(cfg, func(dc *mapstructure.DecoderConfig) {
		dc.TagName = "yaml"
	}); err != nil {
		panic(fmt.Errorf("fatal error config file: %v", err))
	}

}

// GetConfig 获取配置
func GetConfig() *models.Config {
	return cfg
}

func initViper() {
	checkConfigFile()

	viper.SetConfigFile(globals.ConfigPath)
	viper.SetConfigType("yaml")

	if err := viper.ReadInConfig(); err != nil {
		panic(fmt.Errorf("read config file: %v, err: %v ", globals.ConfigPath, err))
	}

}

func checkConfigFile() {
	fmt.Printf("config path: %s\n", globals.ConfigPath)
	stat, err := os.Stat(globals.ConfigPath)
	if err != nil {
		panic(fmt.Errorf("parse config file: %v err: %v", globals.ConfigPath, err))
	}
	if stat.IsDir() {
		panic(fmt.Sprintf("config file: %v is a directory", globals.ConfigPath))
	}

}

func defaultConfig() *models.Config {
	return &models.Config{
		// 监听地址与端口
		Schema: models.Schema{
			Addr: "127.0.0.1",
			Port: 10999,
		},
		DB: models.DB{
			Type:     "sqlite3",
			Addr:     "",
			Port:     0,
			User:     "",
			Password: "",
			Name:     "ga.db",
		},

		Log: models.Log{
			Level:      "warning",
			Filename:   "logs/log.log",
			LocalTime:  true,
			MaxSize:    10,
			MaxAge:     7,
			MaxBackups: 10,
			Compress:   false,
		},

		Cors: models.Cors{
			AllowOrigins: []string{"*"},
			AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "HEAD"},
			AllowHeaders: []string{"*"},
		},

		// 本地认证：users 表 + HS256 JWT（默认 token 有效期 30 天）
		Auth: models.AuthConfig{
			Local: models.LocalAuthConfig{
				TokenTTLHours: 720,
			},
		},
	}

}
