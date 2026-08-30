package models

// Config system config
type Config struct {
	Schema Schema `json:"schema" yaml:"schema"`
	// Redis   Redis   `json:"redis" yaml:"redis"`
	Cors    Cors    `json:"cors" yaml:"cors"`
	Log     Log     `json:"log" yaml:"log"`
	DB      DB      `json:"db" yaml:"db"`
	Casdoor Casdoor `json:"casdoor" yaml:"casdoor"`
	// 认证配置（casdoor / local 双模式）
	Auth AuthConfig `json:"auth" yaml:"auth"`
	//服务对外地址，用于telegram回调，i.e. https://example.com
	ServerURL string      `json:"server_url" yaml:"server_url"`
	Aria2     Aria2Config `json:"aria2" yaml:"aria2"`
	// 分享链接签名密钥，用于生成和验证分享链接的签名
	SignToken string `json:"sign_token" yaml:"sign_token"`
	// 上传配置
	Upload UploadConfig `json:"upload" yaml:"upload"`
	// 传输服务配置（高带宽上传下载地址）
	Transfer TransferConfig `json:"transfer" yaml:"transfer"`
}

// AuthConfig 认证配置
type AuthConfig struct {
	// 认证模式：casdoor（默认，OAuth2 + Casdoor JWT）或 local（本地用户表 + 自签 JWT）
	Mode string `json:"mode" yaml:"mode"`
	// 本地认证配置（仅 mode=local 时生效）
	Local LocalAuthConfig `json:"local" yaml:"local"`
}

// LocalAuthConfig 本地认证配置
type LocalAuthConfig struct {
	// JWT 签名密钥（HS256）。留空则首次启动时自动生成 32 字节随机密钥，
	// 持久化到数据目录 jwt_secret 文件（0600 权限），重启后 token 仍有效
	JWTSecret string `json:"jwt_secret" yaml:"jwt_secret"`
	// Token 有效期（小时），默认 720（30 天）
	TokenTTLHours int `json:"token_ttl_hours" yaml:"token_ttl_hours"`
}

// UploadConfig 上传配置
type UploadConfig struct {
	// 全局最大并发上传数（所有存储提供者共享），默认 10
	MaxConcurrentUploads int `json:"max_concurrent_uploads" yaml:"max_concurrent_uploads"`
}

// TransferConfig 传输服务配置（高带宽上传下载地址）
type TransferConfig struct {
	// 传输服务地址，为空时使用主服务地址
	ServerURL string `json:"server_url" yaml:"server_url"`
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
