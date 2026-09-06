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
	fs := &native.FileShareORM{
		ShareCode:      &shareCode,
		FileId:         &fileID,
		Sign:           &signParam,
		NeverExpire:    &neverExpire,
		Status:         &status,
		MaxAccessCount: &maxAccessCount,
		Remark:         &remark,
	}
	if !neverExpire {
		fs.ExpireAt = &expireAt
	}

	if err := db.CreateFileShare(fs); err != nil {
		llog.L().Errorf("create file share failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 7. 构建分享链接（sign 参数使 /s/{shareCode} 免登录公共下载可通过签名校验）
	serverURL := strings.TrimRight(config.GetConfig().ServerURL, "/")
	shareURL := serverURL + "/s/" + shareCode + "?sign=" + url.QueryEscape(signParam)

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

	if fs.Status != nil && *fs.Status == int32(native.ShareStatus_CANCELLED) {
		return &models.Err{E: errors.New("share already cancelled"), C: native.RespCode_PARAMS_ERROR}
	}

	if err := db.CancelFileShare(*fs.Id); err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	return nil
}
