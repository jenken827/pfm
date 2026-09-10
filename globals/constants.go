package globals

const (
	// TelegramWebhookPath  telegram 回调路径
	TelegramWebhookPath = "/telegram/webhook"
	// ChunksDirName 分片文件夹名称
	ChunksDirName = "chunks"
	// UsersTempDirName 临时目录下按 owner 隔离的顶层目录名
	// 布局：<TempDir>/users/<ownerID>/<filehash>/{chunks/,merged_<name>}
	UsersTempDirName = "users"

	// ContextUserKey 上下文用户键
	ContextUserKey = "user"
	// ContextUserSpaceKey 上下文用户空间键（含根目录、绑定的存储提供者等）
	ContextUserSpaceKey = "user_space"
)
