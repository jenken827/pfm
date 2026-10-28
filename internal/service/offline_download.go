package service

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/downloader"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/utils"
)

// ============================================================
// URL 离线下载（任务14）
//
// 用户提交 http/https/ftp/magnet URL → aria2 在服务器下载到
// .offline/<gid>/ 目录 → 完成回调（internal/download）算 blake3、
// 建 File 记录到目标目录、建 UploadRecord 分发到绑定 providers。
// ============================================================

// CreateOfflineDownload 创建 URL 离线下载任务
func CreateOfflineDownload(ctx context.Context, req *native.CreateOfflineDownloadReq) (*native.CreateOfflineDownloadResp, *models.Err) {
	// 1. 参数校验
	if req.Url == nil || strings.TrimSpace(*req.Url) == "" {
		return nil, &models.Err{E: fmt.Errorf("下载 URL 不能为空"), C: native.RespCode_PARAMS_ERROR}
	}
	rawURL := strings.TrimSpace(*req.Url)
	// ed2k 链接含 "|"（url.Parse 直接失败），且 aria2 本身不支持 eD2k：
	// 单独给出明确提示，避免落到"不支持的 URL 协议"这种自相矛盾的报错
	if strings.HasPrefix(strings.ToLower(rawURL), "ed2k:") {
		return nil, &models.Err{
			E: fmt.Errorf("aria2 不支持 ed2k 协议，请改用 http/https/ftp/magnet 链接"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https" &&
		parsed.Scheme != "ftp" && parsed.Scheme != "magnet") {
		return nil, &models.Err{
			E: fmt.Errorf("不支持的 URL 协议（支持 http/https/ftp/magnet）"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}
	// SSRF 防护：拒绝内网 / 回环 / 链路本地 / 云元数据地址（可用 download.allow_private_host 放行）
	if guardErr := validateOfflineDownloadURL(parsed); guardErr != nil {
		return nil, &models.Err{E: guardErr, C: native.RespCode_PARAMS_ERROR}
	}

	userID := getCurrentUserIDFromCtx(ctx)

	// 2. 目标目录归属校验
	parentID := ""
	if req.ParentId != nil && *req.ParentId != "" {
		if !checkOwnership(ctx, *req.ParentId) {
			return nil, &models.Err{
				E: fmt.Errorf("无权下载到该目录"),
				C: native.RespCode_PERMISSION_DENIED,
			}
		}
		parentID = *req.ParentId
	} else {
		// 未指定目录 → 用户根目录
		rootID, rootErr := getRootFileIDFromCtx(ctx)
		if rootErr != nil {
			return nil, rootErr
		}
		parentID = rootID
	}

	// 3. 检查离线下载后端是否可用（kind=downloader 插件是否已挂载）
	backend := downloader.Current()
	if backend == nil {
		return nil, &models.Err{
			E: downloader.ErrUnavailable,
			C: native.RespCode_INTERNAL_ERROR,
		}
	}

	// 4. 推算文件名（URL path 最后一段，去 query）
	sourceName := inferFileName(parsed, req.FileName)

	// 5. 提交给下载器后端，dir 指向 .offline/ 独立目录（避免与 .temp janitor 冲突）
	downloadDir := filepath.Join(globals.DataDir, ".offline")
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}

	gid, err := backend.AddURI(ctx, []string{rawURL}, downloader.Options{Dir: downloadDir})
	if err != nil {
		llog.C(ctx).Errorf("添加离线下载任务失败, url: %s, err: %v", rawURL, err)
		return nil, &models.Err{
			E: fmt.Errorf("添加下载任务失败: %v", err),
			C: native.RespCode_INTERNAL_ERROR,
		}
	}

	// 6. 记录 DownloadRecord（URL 任务：无源 location / 上传记录，
	//    blake3_hash 下载完成前未知 → 空串占位，source_location_id=0）
	emptyHash := ""
	zeroLoc := uint64(0)
	state := int32(native.DownloadRecordState_DOWNLOAD_PENDING)
	dr := &entity.DownloadRecordORM{
		Gid:              &gid,
		Blake3Hash:       &emptyHash,
		SourceLocationId: &zeroLoc,
		State:            &state,
		Url:              &rawURL,
		OwnerId:          &userID,
		ParentId:         &parentID,
		SourceName:       &sourceName,
	}
	if err := db.CreateDownloadRecord(dr); err != nil {
		// 记录失败则撤销下载器任务，避免孤儿任务
		_ = backend.Remove(ctx, gid)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	llog.C(ctx).Infof("offline download created, id: %d, gid: %s, url: %s, parent: %s, user: %s",
		*dr.Id, gid, rawURL, parentID, userID)

	pb, _ := dr.ToPB(ctx)
	return &native.CreateOfflineDownloadResp{
		Record: pb,
	}, nil
}

// ListOfflineDownloads 列出当前用户的离线下载任务
func ListOfflineDownloads(ctx context.Context, req *native.ListOfflineDownloadsReq) (*native.ListOfflineDownloadsResp, *models.Err) {
	userID := getCurrentUserIDFromCtx(ctx)

	page := uint32(1)
	if req.Page != nil && *req.Page > 0 {
		page = *req.Page
	}
	pageSize := uint32(20)
	if req.PageSize != nil && *req.PageSize > 0 && *req.PageSize <= 100 {
		pageSize = *req.PageSize
	}

	offset := int((page - 1) * pageSize)
	drs, total, err := db.ListOfflineDownloadsByOwner(userID, offset, int(pageSize))
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	records := make([]*native.DownloadRecord, 0, len(drs))
	for _, dr := range drs {
		pb, _ := dr.ToPB(ctx)
		records = append(records, pb)
	}

	return &native.ListOfflineDownloadsResp{
		Records: records,
		Total:   utils.Uint32Ptr(uint32(total)),
	}, nil
}

// DeleteOfflineDownload 删除离线下载任务：停止下载器任务并移除记录
// 已完成的下载仅删除记录，不影响已入库文件
func DeleteOfflineDownload(ctx context.Context, req *native.DeleteOfflineDownloadReq) *models.Err {
	dr, err := db.GetDownloadRecordByID(*req.Id)
	if err != nil {
		return &models.Err{E: fmt.Errorf("下载任务不存在"), C: native.RespCode_PARAMS_ERROR}
	}
	// 归属校验：仅任务所有者可删除（管理员不可越权操作他人任务）
	userID := getCurrentUserIDFromCtx(ctx)
	if dr.OwnerId == nil || *dr.OwnerId != userID {
		return &models.Err{E: fmt.Errorf("无权删除该任务"), C: native.RespCode_PERMISSION_DENIED}
	}

	// 停止下载器任务（仅非终态）
	if dr.State != nil && *dr.State != int32(native.DownloadRecordState_DOWNLOAD_COMPLETED) &&
		*dr.State != int32(native.DownloadRecordState_DOWNLOAD_FAILED) {
		if backend := downloader.Current(); backend != nil && dr.Gid != nil {
			_ = backend.Remove(ctx, *dr.Gid)
		}
	}

	if err := db.DeleteDownloadRecord(*req.Id); err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	return nil
}

// RetryOfflineDownload 重试失败的离线下载任务
// 复用原 URL 重新创建下载器任务并更新 gid
func RetryOfflineDownload(ctx context.Context, req *native.RetryOfflineDownloadReq) (*native.CreateOfflineDownloadResp, *models.Err) {
	dr, err := db.GetDownloadRecordByID(*req.Id)
	if err != nil {
		return nil, &models.Err{E: fmt.Errorf("下载任务不存在"), C: native.RespCode_PARAMS_ERROR}
	}
	// 归属校验：仅任务所有者可重试（管理员不可越权操作他人任务）
	userID := getCurrentUserIDFromCtx(ctx)
	if dr.OwnerId == nil || *dr.OwnerId != userID {
		return nil, &models.Err{E: fmt.Errorf("无权重试该任务"), C: native.RespCode_PERMISSION_DENIED}
	}
	// 仅失败/删除状态可重试
	if dr.State == nil || *dr.State != int32(native.DownloadRecordState_DOWNLOAD_FAILED) {
		return nil, &models.Err{
			E: fmt.Errorf("仅失败状态的下载任务可重试"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}
	backend := downloader.Current()
	if dr.Url == nil || backend == nil {
		return nil, &models.Err{
			E: fmt.Errorf("离线下载服务不可用或缺少 URL"),
			C: native.RespCode_INTERNAL_ERROR,
		}
	}
	// 重试也要过一遍校验：历史记录可能是在放开限制时创建的
	if parsedURL, pErr := url.Parse(*dr.Url); pErr == nil {
		if guardErr := validateOfflineDownloadURL(parsedURL); guardErr != nil {
			return nil, &models.Err{E: guardErr, C: native.RespCode_PARAMS_ERROR}
		}
	}

	downloadDir := filepath.Join(globals.DataDir, ".offline")
	_ = os.MkdirAll(downloadDir, 0o755)

	gid, aErr := backend.AddURI(ctx, []string{*dr.Url}, downloader.Options{Dir: downloadDir})
	if aErr != nil {
		llog.C(ctx).Errorf("重试离线下载失败, id: %d, err: %v", *req.Id, aErr)
		return nil, &models.Err{E: fmt.Errorf("添加下载任务失败: %v", aErr), C: native.RespCode_INTERNAL_ERROR}
	}

	// 更新记录：新 gid + 重置状态
	state := int32(native.DownloadRecordState_DOWNLOAD_PENDING)
	dr.Gid = &gid
	dr.State = &state
	dr.ErrorMessage = utils.StringPtr("")
	_ = db.UpdateDownloadRecord(dr, []string{"gid", "state", "error_message"})

	llog.C(ctx).Infof("offline download retried, id: %d, new gid: %s", *req.Id, gid)
	pb, _ := dr.ToPB(ctx)
	return &native.CreateOfflineDownloadResp{Record: pb}, nil
}

// inferFileName 从 URL 推断文件名，req 指定了则优先用指定名
func inferFileName(u *url.URL, override *string) string {
	if override != nil && strings.TrimSpace(*override) != "" {
		name := strings.TrimSpace(*override)
		// 防路径穿越：只保留 basename
		name = filepath.Base(name)
		return name
	}
	pathPart := u.Path
	if pathPart == "" || strings.HasSuffix(pathPart, "/") {
		return ""
	}
	base := filepath.Base(pathPart)
	if base == "." || base == "/" || base == "" {
		return ""
	}
	return base
}
