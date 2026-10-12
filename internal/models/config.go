package models

// Config system config
type Config struct {
	Schema Schema `json:"schema" yaml:"schema"`
	// Redis   Redis   `json:"redis" yaml:"redis"`
	Cors Cors `json:"cors" yaml:"cors"`
	Log  Log  `json:"log" yaml:"log"`
	DB   DB   `json:"db" yaml:"db"`
	// 认证配置（本地认证：users 表 + HS256 JWT）
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
	// 照片日期解析配置（相册备份按"年/月"归档；不配置时使用内置默认规则）
	PhotoDate PhotoDateConfig `json:"photo_date" yaml:"photo_date"`
}

// PhotoDateFilenameRule 照片日期文件名解析规则（单条）
// Type 取值（未知类型解析时跳过，保证前后端向前兼容）：
//
//	prefix_segment_date: 前缀+分隔符取 SegmentIndex 段，段内按 YYYYMMDD 或 YYYY-MM-DD 解析
//	prefix_compact_date: 前缀后紧跟 8 位 YYYYMMDD
//	prefix_timestamp:    前缀后紧跟 10 位秒级时间戳
//	leading_timestamp:   文件名开头连续数字，前 10 位为秒级时间戳
type PhotoDateFilenameRule struct {
	Type         string   `json:"type" yaml:"type"`
	Prefixes     []string `json:"prefixes" yaml:"prefixes"`
	SegmentIndex uint32   `json:"segment_index" yaml:"segment_index"`
}

// PhotoDateConfig 照片日期解析配置
type PhotoDateConfig struct {
	// EXIF DateTimeOriginal 是否启用（仅前端相册备份有效），默认 true
	ExifEnabled *bool `json:"exif_enabled" yaml:"exif_enabled"`
	// EXIF 读取文件头上限（字节），默认 262144（256KB）
	ExifHeadReadLimit uint32 `json:"exif_head_read_limit" yaml:"exif_head_read_limit"`
	// 文件名解析规则（顺序敏感，先命中先返回；留空使用内置默认规则）
	FilenameRules []PhotoDateFilenameRule `json:"filename_rules" yaml:"filename_rules"`
	// 时间戳防误判：解析年份下界，默认 2000
	TsMinYear uint32 `json:"ts_min_year" yaml:"ts_min_year"`
	// 时间戳防误判：允许的年份上界偏移（当前年份 + 偏移），默认 1
	TsMaxYearOffset uint32 `json:"ts_max_year_offset" yaml:"ts_max_year_offset"`
}

// AuthConfig 认证配置
// 本地认证：users 表（bcrypt）+ 自签 HS256 JWT（无外部认证服务依赖）
type AuthConfig struct {
	// 本地认证配置
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
	// 直链下载配置（客户端绕开服务端中转，见 DESIGN.md §17）
	DirectLink DirectLinkConfig `json:"direct_link" yaml:"direct_link"`
}

// DirectLinkConfig 直链下载配置
//
// 只有"单分片 + 未加密 + 驱动实现 DirectLinker"的文件才会下发直链，其余一律走代理；
// 因此开启本项不会改变任何不满足条件文件的下载行为。
type DirectLinkConfig struct {
	// 是否启用，默认 true（nil = 启用）
	Enabled *bool `json:"enabled" yaml:"enabled"`
	// 允许直链的存储提供者类型白名单（如 pan123 / aliyundrive_open / baidu_netdisk）；
	// 留空 = 所有实现直链的驱动
	Providers []string `json:"providers" yaml:"providers"`
	// 选源时是否优先挑可直链的提供者，默认 false（不改动现有 sort_order 优先级）
	PreferDirectableProvider bool `json:"prefer_directable_provider" yaml:"prefer_directable_provider"`
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

// Aria2Config aria2 downloader config
type Aria2Config struct {
	Endpoint string `json:"endpoint" yaml:"endpoint"`
	Token    string `json:"token" yaml:"token"`
	Timeout  int64  `json:"timeout" yaml:"timeout"`
}
