package models

// Config system config
type Config struct {
	Schema Schema `json:"scheme" yaml:"scheme"`
	// Redis   Redis   `json:"redis" yaml:"redis"`
	Cors    Cors    `json:"cors" yaml:"cors"`
	Log     Log     `json:"log" yaml:"log"`
	DB      DB      `json:"db" yaml:"db"`
	Casdoor Casdoor `json:"casdoor" yaml:"casdoor"`
	//服务对外地址，用于telegram回调，i.e. https://example.com
	ServerURL string      `json:"server_url" yaml:"server_url"`
	Aria2     Aria2Config `json:"aria2" yaml:"aria2"`
}

// Schema http server config
type Schema struct {
	Addr string `json:"addr" yaml:"addr"`
	Port uint32 `json:"port" yaml:"port"`
}

// DB 数据库配置
// 支持mysql,pgsql,sqlite
type DB struct {
	Type        string `json:"type" yaml:"type"`
	Addr        string `json:"addr" yaml:"addr"`
	Port        int    `json:"port" yaml:"port"`
	User        string `json:"user" yaml:"user"`
	Password    string `json:"password" yaml:"password"`
	Name        string `json:"name" yaml:"name"`
	TablePrefix string `json:"table_prefix" yaml:"table_prefix"`
}

// Log 日志配置
type Log struct {
	Filename   string `json:"filename" yaml:"filename"`       // 文件完整路径
	MaxSize    int    `json:"max_size" yaml:"max_size"`       // 日志文件大小，单位M
	MaxAge     int    `json:"max_age" yaml:"max_age"`         // 保留天数
	MaxBackups int    `json:"max_backups" yaml:"max_backups"` // 保留文件个数
	LocalTime  bool   `json:"local_time" yaml:"local_time"`   // 是否使用本地时间
	Compress   bool   `json:"compress" yaml:"compress"`       // 是否压缩
	Level      string `json:"level" yaml:"level"`             //支持四个等级 debug,info,warn,error
}

// Redis redis config
type Redis struct {
	Addr     string `json:"host" yaml:"host"`
	Port     int    `json:"port" yaml:"port"`
	Password string `json:"password" yaml:"password"`
	DB       int    `json:"db" yaml:"db"`
}

// Cors cors
type Cors struct {
	AllowOrigins []string `json:"allow_origins" yaml:"allow_origins"`
	AllowMethods []string `json:"allow_methods" yaml:"allow_methods"`
	AllowHeaders []string `json:"allow_headers" yaml:"allow_headers"`
}

// Casdoor casdoor config
type Casdoor struct {
	Endpoint         string `json:"endpoint" yaml:"endpoint"`
	ClientID         string `json:"client_id" yaml:"client_id"`
	ClientSecret     string `json:"client_secret" yaml:"client_secret"`
	Certificate      string `json:"certificate" yaml:"certificate"`
	OrganizationName string `json:"organization_name" yaml:"organization_name"`
	ApplicationName  string `json:"application_name" yaml:"application_name"`
}

// Aria2Config aria2 downloader config
type Aria2Config struct {
	Endpoint string `json:"endpoint" yaml:"endpoint"`
	Token    string `json:"token" yaml:"token"`
	Timeout  int64  `json:"timeout" yaml:"timeout"`
}
