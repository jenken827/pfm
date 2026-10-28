package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm/internal/db"
)

// ============================================================
// 文件版本发号器（并发上传仲裁）
//
// 背景：同一 key（owner+parent+name）下的文件可能被并发 PUT 覆盖，
// 后到达的 PUT 应当拥有最终内容归属权。仅靠 DB CAS（读旧值写新值）
// 无法表达"到达顺序"——两个并发请求可能读到同一旧版本，谁先落库谁赢，
// 而非"后到者赢"。因此版本号必须在请求头到达时（body 读取前）分配，
// 分配顺序即到达顺序，后到者必然拿到更大的版本号。
//
// 实现：内存 per-key 单调递增计数器，首次访问某 key 时从 DB 校准
// （取该 key 所有文件记录的 max(version)，含软删，避免重建同名文件后
// 版本回退）。单实例部署有效；多实例部署需替换为 DB 序列/Redis。
// ============================================================

// fileVersionAllocator 文件版本发号器
type fileVersionAllocator struct {
	mu     sync.Mutex
	seqs   map[string]uint64 // key -> 已分配的最大版本号
	loaded map[string]bool   // key -> 是否已从 DB 校准
}

var versionAllocator = &fileVersionAllocator{
	seqs:   make(map[string]uint64),
	loaded: make(map[string]bool),
}

// versionKey 构造发号器 key
func versionKey(ownerID, parentID, fileName string) string {
	return ownerID + "|" + parentID + "|" + fileName
}

// loadMaxVersion 从 DB 加载该 key 下所有文件记录（含软删）的 max(version)
func loadMaxVersion(ownerID, parentID, fileName string) (uint64, error) {
	var maxVer int64
	err := db.GetDB().WithContext(context.Background()).
		Model(&entity.FileORM{}).
		Unscoped().
		Where("owner_id = ? AND parent_id = ? AND name = ?", ownerID, parentID, fileName).
		Select("COALESCE(MAX(version), 0)").
		Scan(&maxVer).Error
	if err != nil {
		return 0, fmt.Errorf("load max file version failed: %w", err)
	}
	return uint64(maxVer), nil
}

// AllocateFileVersion 为一次上传分配版本号（必须在请求头到达时调用）。
// 返回单调递增的版本号；同 key 下后调用者必然拿到更大的值。
func AllocateFileVersion(ownerID, parentID, fileName string) uint64 {
	key := versionKey(ownerID, parentID, fileName)

	versionAllocator.mu.Lock()
	defer versionAllocator.mu.Unlock()

	if !versionAllocator.loaded[key] {
		maxVer, err := loadMaxVersion(ownerID, parentID, fileName)
		if err != nil {
			// 校准失败时保守地从 0 起步（DB 查询异常属罕见场景，
			// 且后续 CAS 落库仍会兜底保证不会错误覆盖更高版本）
			maxVer = 0
		}
		versionAllocator.seqs[key] = maxVer
		versionAllocator.loaded[key] = true
	}

	versionAllocator.seqs[key]++
	return versionAllocator.seqs[key]
}

type uploadVersionCtxKey struct{}

// WithUploadVersion 将本次上传的版本号放入 context（供 webdav 等入口层在
// 请求头到达、body 读取前发号后注入，贯穿整个上传处理链路）
func WithUploadVersion(ctx context.Context, version uint64) context.Context {
	return context.WithValue(ctx, uploadVersionCtxKey{}, version)
}

// withUploadVersion 将本次上传的版本号放入 context
func withUploadVersion(ctx context.Context, version uint64) context.Context {
	return WithUploadVersion(ctx, version)
}

// getUploadVersion 从 context 读取本次上传的版本号；未设置（非上传链路调用）返回 0
func getUploadVersion(ctx context.Context) uint64 {
	if v, ok := ctx.Value(uploadVersionCtxKey{}).(uint64); ok {
		return v
	}
	return 0
}
