package globals

const (
	// ChunksDirName 分片文件夹名称
	ChunksDirName = "chunks"
	// UsersTempDirName 临时目录下按 owner 隔离的顶层目录名
	// 布局：<TempDir>/users/<ownerID>/<filehash>/chunks/{chunk_*, merged_<name>}
	// （分片与合并明文同处 chunks/，合并明文是异步分发读的源，清理时不可整目录删除）
	UsersTempDirName = "users"

	// ContextUserKey 上下文用户键
	ContextUserKey = "user"
	// ContextUserSpaceKey 上下文用户空间键（含根目录、绑定的存储提供者等）
	ContextUserSpaceKey = "user_space"
	// ContextRequestIDKey 上下文请求 ID 键（HTTPLogMiddleware 注入，用于日志关联）
	ContextRequestIDKey = "request_id"
)
