package models

// UserContext 通过中间件注入 context 的用户空间信息
// 包含用户 ID、管理员状态、根目录 ID 和绑定的存储提供者
type UserContext struct {
	UserID           string
	IsAdmin          bool
	RootFileID       string   // 用户根目录 ID（空表示未配置）
	BoundProviderIds []uint32 // 绑定的存储提供者（空表示未配置）
}
