package service

import (
	"context"
	"errors"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// 票据有效期（两种票据读的是同一份字节，只差时间窗）。
const (
	// previewTicketTTL 预览票据有效期：覆盖"打开预览页 → viewer 把文件拉完"这一段。
	// viewer 拿到 URL 后立刻 GET，不存在复用长连接的情况；过期后重新打开预览会重新签发。
	previewTicketTTL = 10 * time.Minute

	// downloadTicketTTL 下载票据有效期。
	//
	// 票据只在请求入口校验一次，正在传输的连接不会因过期中断；真正需要时间窗的是
	// **续传**：浏览器下载器暂停 / 断网重连 / 从下载历史重开都会发一次新的 HTTP
	// 请求（通常带 Range），要重新校验票据。12 小时覆盖慢速链路上的超大文件与
	// 断网过夜，同时把"URL 被留在下载历史 / 反代日志"的暴露面限制在单文件一天内。
	downloadTicketTTL = 12 * time.Hour
)

// ticketTTLForPurpose 按用途取有效期；未知用途按预览处理（保守）
func ticketTTLForPurpose(purpose string) time.Duration {
	if purpose == auth.PurposeDownload {
		return downloadTicketTTL
	}
	return previewTicketTTL
}

// CreatePreviewTicket 为单个文件签发单文件票据（仅文件所有者）。
//
// 票据用于替代"把 access token 拼进 URL"：URL 会进 WebView / 浏览器历史 / 下载历史
// 与 Referer，access token 一旦泄露等于交出账户级读权限，票据则只对这一个文件有效。
// purpose 决定有效期（预览 10 分钟 / 下载 12 小时），不影响权限。
func CreatePreviewTicket(ctx context.Context, req *native.CreatePreviewTicketReq) (*native.CreatePreviewTicketRespData, *models.Err) {
	fileID := req.GetFileId()
	if fileID == "" {
		return nil, &models.Err{E: errors.New("file_id is required"), C: native.RespCode_PARAMS_ERROR}
	}

	// 只有文件所有者能给自己签票据；管理员不享有越权预览
	if !CheckOwnership(ctx, fileID) {
		return nil, &models.Err{E: errors.New("permission denied: not owner of file"), C: native.RespCode_PERMISSION_DENIED}
	}

	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return nil, &models.Err{E: errors.New("user not found in context"), C: native.RespCode_TOKEN_INVALID}
	}

	purpose := ticketPurposeFromProto(req.GetPurpose())
	ticket, expireAt, err := auth.GetLocalAuth().SignFileTicket(fileID, userID, purpose, ticketTTLForPurpose(purpose))
	if err != nil {
		// ⚠ 票据是临时凭证，错误信息里不留它
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	// 只记"给谁、对哪个文件、什么时候过期、什么用途"，不记票据本身
	llog.C(ctx).Infof("%s ticket issued, user: %s, file_id: %s, expires_at: %s",
		purpose, userID, fileID, expireAt.Format(time.RFC3339))

	return &native.CreatePreviewTicketRespData{
		Ticket:    &ticket,
		ExpiresAt: timestamppb.New(expireAt),
	}, nil
}

// ticketPurposeFromProto 把 proto 枚举翻译成票据负载里的用途字符串
func ticketPurposeFromProto(p native.TicketPurpose) string {
	if p == native.TicketPurpose_DOWNLOAD {
		return auth.PurposeDownload
	}
	return auth.PurposePreview
}

// SignDownloadTicket 服务端内部签发下载票据（用于直链 302 回退：浏览器跟随重定向时
// 带不了 Authorization 头，Location 里必须自带凭证）。
//
// 调用方必须已经完成归属校验（本函数不再查库）。
func SignDownloadTicket(fileID, userID string) (string, error) {
	ticket, _, err := auth.GetLocalAuth().SignFileTicket(fileID, userID, auth.PurposeDownload, downloadTicketTTL)
	if err != nil {
		return "", err
	}
	return ticket, nil
}
