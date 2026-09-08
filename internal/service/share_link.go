package service

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/pkg/sharelink"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/gorm"
)

const (
	// defaultExpireSeconds 默认过期时间一周
	defaultExpireSeconds = 604800
	// shareCodeMaxRetry 分享码生成最大重试次数
	shareCodeMaxRetry = 10
)

// CreateShareLink 创建分享链接
func CreateShareLink(ctx context.Context, req *native.CreateShareLinkReq) (*native.CreateShareLinkRespData, *models.Err) {
	// 1. 验证文件是否存在
	fileID := req.GetFileId()
	if fileID == "" {
		return nil, &models.Err{E: errors.New("file_id is required"), C: native.RespCode_PARAMS_ERROR}
	}

	_, err := db.GetFileByID(fileID)
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 1.1 归属校验：仅文件所有者（或管理员）可创建分享
	if !CheckOwnership(ctx, fileID) {
		return nil, &models.Err{E: errors.New("permission denied: only file owner can share"), C: native.RespCode_PERMISSION_DENIED}
	}

	// 2. 获取参数
	expireSeconds := int64(defaultExpireSeconds)
	if req.GetExpireSeconds() > 0 {
		expireSeconds = req.GetExpireSeconds()
	}

	neverExpire := req.GetNeverExpire()
	maxAccessCount := req.GetMaxAccessCount()
	remark := req.GetRemark()

	// 3. 获取签名密钥
	signToken := config.GetConfig().SignToken
	if signToken == "" {
		return nil, &models.Err{E: errors.New("share link not configured"), C: native.RespCode_INTERNAL_ERROR}
	}

	// 4. 生成唯一分享码
	var shareCode string
	for i := 0; i < shareCodeMaxRetry; i++ {
		code := sharelink.GenerateShareCode()
		_, err := db.GetFileShareByCode(code)
		if err == gorm.ErrRecordNotFound {
			shareCode = code
			break
		}
		if err != nil {
			llog.L().Errorf("check share code failed: %v", err)
			return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}
	}
	if shareCode == "" {
		return nil, &models.Err{E: errors.New("failed to generate share code"), C: native.RespCode_INTERNAL_ERROR}
	}

	// 5. 计算过期时间和签名
	var expireAt time.Time
	if !neverExpire {
		expireAt = time.Now().Add(time.Duration(expireSeconds) * time.Second)
	}

	expireAtUnix := int64(0)
	if !neverExpire {
		expireAtUnix = expireAt.Unix()
	}

	signParam := sharelink.BuildSignParam(shareCode, fileID, signToken, expireAtUnix)

	// 6. 写入数据库
	status := int32(native.ShareStatus_ACTIVE)
	ownerID := getCurrentUserIDFromCtx(ctx)
	fs := &native.FileShareORM{
		ShareCode:      &shareCode,
		FileId:         &fileID,
		Sign:           &signParam,
		NeverExpire:    &neverExpire,
		Status:         &status,
		MaxAccessCount: &maxAccessCount,
		Remark:         &remark,
		OwnerId:        &ownerID,
	}
	if !neverExpire {
		fs.ExpireAt = &expireAt
	}

	if err := db.CreateFileShare(fs); err != nil {
		llog.L().Errorf("create file share failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 7. 构建分享链接（sign 参数使 /s/{shareCode} 免登录公共下载可通过签名校验）
	shareURL := buildShareURL(shareCode, signParam)

	// 8. 返回
	result := &native.CreateShareLinkRespData{
		ShareUrl:  &shareURL,
		ShareCode: &shareCode,
	}
	if !neverExpire {
		result.ExpireAt = &expireAtUnix
	}

	return result, nil
}

// buildShareURL 拼接分享链接（sign 已由调用方按当前有效期生成）
func buildShareURL(shareCode, sign string) string {
	serverURL := strings.TrimRight(config.GetConfig().ServerURL, "/")
	return serverURL + "/s/" + shareCode + "?sign=" + url.QueryEscape(sign)
}

// checkShareOwnership 校验当前用户是否为分享的归属者（分享记录的 owner_id 或管理员）
// 老数据 owner_id 为空时退化为校验对应文件的所有者
func checkShareOwnership(ctx context.Context, fs *native.FileShareORM) bool {
	if isAdminUser(ctx) {
		return true
	}
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return false
	}
	if fs.OwnerId != nil && *fs.OwnerId != "" {
		return *fs.OwnerId == userID
	}
	// owner_id 未回填的老记录：按文件所有者校验
	if fs.FileId != nil {
		return CheckOwnership(ctx, *fs.FileId)
	}
	return false
}

// CancelShareLink 取消分享链接
func CancelShareLink(ctx context.Context, req *native.CancelShareLinkReq) *models.Err {
	shareCode := req.GetShareCode()
	if shareCode == "" {
		return &models.Err{E: errors.New("share_code is required"), C: native.RespCode_PARAMS_ERROR}
	}

	fs, err := db.GetFileShareByCode(shareCode)
	if err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 归属校验：仅分享所有者（或管理员）可取消
	if !checkShareOwnership(ctx, fs) {
		return &models.Err{E: errors.New("permission denied: not share owner"), C: native.RespCode_PERMISSION_DENIED}
	}

	if fs.Status != nil && *fs.Status == int32(native.ShareStatus_CANCELLED) {
		return &models.Err{E: errors.New("share already cancelled"), C: native.RespCode_PARAMS_ERROR}
	}

	if err := db.CancelFileShare(*fs.Id); err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	return nil
}

// ListMyShares 获取当前用户的分享列表（含对应文件的展示信息）
func ListMyShares(ctx context.Context, req *native.ListMySharesReq) (*native.ListMySharesRespData, *models.Err) {
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return nil, &models.Err{E: errors.New("unauthorized"), C: native.RespCode_TOKEN_INVALID}
	}

	pageSize := int(req.GetPageSize())
	if pageSize <= 0 {
		pageSize = 20
	}
	offset := int(req.GetOffset())
	if offset < 0 {
		offset = 0
	}

	shares, total, err := db.ListFileSharesByOwner(userID, offset, pageSize)
	if err != nil {
		llog.L().Errorf("list shares failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 批量收集 file_id 一次查询，避免 N+1
	fileIDs := make([]string, 0, len(shares))
	for _, s := range shares {
		if s.FileId != nil && *s.FileId != "" {
			fileIDs = append(fileIDs, *s.FileId)
		}
	}
	fileMap := make(map[string]*native.FileORM, len(fileIDs))
	if len(fileIDs) > 0 {
		files, fErr := db.GetFilesByIDsUnscoped(fileIDs)
		if fErr != nil {
			llog.L().Errorf("get share files failed: %v", fErr)
		} else {
			for _, f := range files {
				if f.Id != nil {
					fileMap[*f.Id] = f
				}
			}
		}
	}

	now := time.Now()
	items := make([]*native.MyShareItem, 0, len(shares))
	for _, s := range shares {
		item := &native.MyShareItem{
			Id:             s.Id,
			ShareCode:      s.ShareCode,
			FileId:         s.FileId,
			ExpireAt:       nil,
			NeverExpire:    s.NeverExpire,
			Status:         (*native.ShareStatus)(s.Status),
			AccessCount:    s.AccessCount,
			MaxAccessCount: s.MaxAccessCount,
			Remark:         s.Remark,
		}
		if s.CreatedAt != nil {
			item.CreatedAt = timestamppb.New(*s.CreatedAt)
		}
		if s.ExpireAt != nil {
			item.ExpireAt = timestamppb.New(*s.ExpireAt)
		}
		if s.Sign != nil && s.ShareCode != nil {
			url := buildShareURL(*s.ShareCode, *s.Sign)
			item.ShareUrl = &url
		}

		// 对应文件信息（含软删记录；永久删除则标记 missing）
		if s.FileId != nil {
			if f, ok := fileMap[*s.FileId]; ok {
				if f.Name != nil {
					item.FileName = f.Name
				}
				if f.Type != nil {
					ft := native.FileType(*f.Type)
					item.FileType = &ft
				}
				if f.Size != nil {
					item.FileSize = f.Size
				}
				if f.DeletedAt != nil && f.DeletedAt.Valid {
					inTrash := true
					item.FileInTrash = &inTrash
				}
			} else {
				missing := true
				item.FileMissing = &missing
			}
		}

		// 有效期已过的 ACTIVE 分享在列表中呈现为已过期（不落库，读时计算）
		if item.Status != nil && *item.Status == native.ShareStatus_ACTIVE &&
			s.NeverExpire != nil && !*s.NeverExpire &&
			s.ExpireAt != nil && s.ExpireAt.Before(now) {
			expired := native.ShareStatus_EXPIRED
			item.Status = &expired
		}

		items = append(items, item)
	}

	hasMore := offset+len(items) < int(total)
	return &native.ListMySharesRespData{
		Items:   items,
		Total:   func() *uint32 { t := uint32(total); return &t }(),
		HasMore: &hasMore,
	}, nil
}

// UpdateShareLink 更新分享（有效期/永久/访问上限/备注）
// 有效期变化会导致 sign 重签，因此返回新的 share_url
func UpdateShareLink(ctx context.Context, req *native.UpdateShareLinkReq) (*native.UpdateShareLinkRespData, *models.Err) {
	shareCode := req.GetShareCode()
	if shareCode == "" {
		return nil, &models.Err{E: errors.New("share_code is required"), C: native.RespCode_PARAMS_ERROR}
	}

	fs, err := db.GetFileShareByCode(shareCode)
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 归属校验：仅分享所有者（或管理员）可更新
	if !checkShareOwnership(ctx, fs) {
		return nil, &models.Err{E: errors.New("permission denied: not share owner"), C: native.RespCode_PERMISSION_DENIED}
	}

	if fs.Status != nil && *fs.Status == int32(native.ShareStatus_CANCELLED) {
		return nil, &models.Err{E: errors.New("share already cancelled"), C: native.RespCode_PARAMS_ERROR}
	}

	// 计算新的有效期
	neverExpire := fs.NeverExpire != nil && *fs.NeverExpire
	var expireAt *time.Time
	if fs.ExpireAt != nil {
		t := *fs.ExpireAt
		expireAt = &t
	}
	if req.GetNeverExpire() {
		neverExpire = true
		expireAt = nil
	} else if req.GetExpireSeconds() > 0 {
		// 明确传入限时有效期：从当前时间重新计时
		neverExpire = false
		t := time.Now().Add(time.Duration(req.GetExpireSeconds()) * time.Second)
		expireAt = &t
	}

	// 组装更新字段
	update := &native.FileShareORM{Id: fs.Id}
	fields := []string{}

	// 有效期变化需要重签 sign（sign 由 share_code+file_id+expire_at 决定）
	oldNever := fs.NeverExpire != nil && *fs.NeverExpire
	oldExpireUnix := int64(0)
	if fs.ExpireAt != nil {
		oldExpireUnix = fs.ExpireAt.Unix()
	}
	newExpireUnix := int64(0)
	if expireAt != nil {
		newExpireUnix = expireAt.Unix()
	}
	if neverExpire != oldNever || newExpireUnix != oldExpireUnix {
		signToken := config.GetConfig().SignToken
		if signToken == "" {
			return nil, &models.Err{E: errors.New("share link not configured"), C: native.RespCode_INTERNAL_ERROR}
		}
		newSign := sharelink.BuildSignParam(shareCode, *fs.FileId, signToken, newExpireUnix)
		update.Sign = &newSign
		fields = append(fields, "sign")
		update.NeverExpire = &neverExpire
		fields = append(fields, "never_expire")
		update.ExpireAt = expireAt
		fields = append(fields, "expire_at")
	}

	// max_access_count：请求显式传入（含 0=不限）则更新
	if req.MaxAccessCount != nil {
		mac := req.GetMaxAccessCount()
		update.MaxAccessCount = &mac
		fields = append(fields, "max_access_count")
	}

	// remark：请求显式传入（含空串清空）则更新
	if req.Remark != nil {
		rm := req.GetRemark()
		update.Remark = &rm
		fields = append(fields, "remark")
	}

	if len(fields) == 0 {
		return nil, &models.Err{E: errors.New("nothing to update"), C: native.RespCode_PARAMS_ERROR}
	}

	if err := db.UpdateFileShare(update, fields); err != nil {
		llog.L().Errorf("update share failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 重新计算响应（sign 可能已变）
	result := &native.UpdateShareLinkRespData{ShareCode: &shareCode}
	finalExpireUnix := int64(0)
	if expireAt != nil {
		finalExpireUnix = expireAt.Unix()
		result.ExpireAt = &finalExpireUnix
	}
	// sign 未变时沿用旧签名
	signForURL := update.Sign
	if signForURL == nil {
		signForURL = fs.Sign
	}
	if signForURL != nil {
		url := buildShareURL(shareCode, *signForURL)
		result.ShareUrl = &url
	}
	return result, nil
}
