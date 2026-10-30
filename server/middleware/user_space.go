package middleware

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jenken827/pfm/globals"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/models"
	"github.com/jenken827/pfm/pkg/llog"
	"gorm.io/gorm"
)

// injectUserSpace 查询用户空间（UserSpace）信息并注入 context。
//
// 为什么查不到 UserSpace 时不阻断请求：
//   - 职责分离：中间件只负责「认证」（你是谁），授权（你能做什么）由 service 层
//     按需判断。并非所有接口都需要 UserSpace——GetMyRoles / ListUsers 等管理接口
//     不依赖它，而 AssignRootDir 本身正是「为没有 UserSpace 的用户分配根目录」的
//     接口，若在中间件阻断将导致用户永远无法被配置（死锁）。
//   - 默认安全：不注入时下游 getUserContext(ctx) 返回 nil，
//     getRootFileIDFromCtx / getBoundProviderIdsFromCtx 会返回
//     「根目录未配置，请联系管理员」，文件类操作该失败的照样失败。
//
// 错误处理区分两类情况（勿混为一谈，否则故障排查会被误导）：
//   - gorm.ErrRecordNotFound：用户确实尚未配置，属正常业务状态，Debug 级日志；
//   - 其他错误：查询故障（连接失败、超时等），Error 级日志以便告警排查。
func injectUserSpace(ctx context.Context, userID string) context.Context {
	space, err := db.GetUserSpaceByUserID(userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			llog.C(ctx).Debugf("user %s has no UserSpace yet: %v", userID, err)
		} else {
			llog.C(ctx).Errorf("query UserSpace failed for user %s: %v", userID, err)
		}
		return ctx
	}

	var providerIds []uint32
	if space.BoundProviderIds != nil && *space.BoundProviderIds != "" {
		if err := json.Unmarshal([]byte(*space.BoundProviderIds), &providerIds); err != nil {
			llog.C(ctx).Warnf("invalid bound_provider_ids for user %s: %v", userID, err)
		}
	}

	rootFileID := ""
	if space.RootFileId != nil {
		rootFileID = *space.RootFileId
	}

	uc := &models.UserContext{
		UserID:           userID,
		IsAdmin:          db.IsAdmin(userID),
		RootFileID:       rootFileID,
		BoundProviderIds: providerIds,
	}
	return context.WithValue(ctx, globals.ContextUserSpaceKey, uc)
}

// InjectUserSpace 查询并注入用户空间（导出给 MCP 这类非 REST 入口复用）。
//
// 语义与 REST 完全一致：查不到时只记日志、不阻断（授权判断交给 service 层）。
func InjectUserSpace(ctx context.Context, userID string) context.Context {
	return injectUserSpace(ctx, userID)
}
