package service

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/downloader"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
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
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https" &&
		parsed.Scheme != "ftp" && parsed.Scheme != "magnet" && parsed.Scheme != "ed2k") {
		return nil, &models.Err{
			E: fmt.Errorf("不支持的 URL 协议（支持 http/https/ftp/magnet/ed2k）"),
			C: native.RespCode_PARAMS_ERROR,
		}
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

	// 3. 检查 aria2 是否可用
	if downloader.Aria2Downloader == nil {
		return nil, &models.Err{
			E: fmt.Errorf("离线下载服务不可用（aria2 未初始化）"),
			C: native.RespCode_INTERNAL_ERROR,
		}
	}

	// 4. 推算文件名（URL path 最后一段，去 query）
	sourceName := inferFileName(parsed, req.FileName)

	// 5. 调 aria2.addUri，dir 指向 .offline/ 独立目录（避免与 .temp janitor 冲突）
	downloadDir := filepath.Join(globals.DataDir, ".offline")
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}

	gid, err := downloader.Aria2Downloader.AddURI(
		[]string{rawURL},
		map[string]any{"dir": downloadDir},
	)
	if err != nil {
		llog.L().Errorf("aria2 addUri failed, url: %s, err: %v", rawURL, err)
		return nil, &models.Err{
			E: fmt.Errorf("aria2 添加下载失败: %v", err),
			C: native.RespCode_INTERNAL_ERROR,
		}
	}

	// 6. 记录 DownloadRecord（URL 任务：无源 location / 上传记录，
	//    blake3_hash 下载完成前未知 → 空串占位，source_location_id=0）
	emptyHash := ""
	zeroLoc := uint64(0)
	state := int32(native.DownloadRecordState_DOWNLOAD_PENDING)
	dr := &native.DownloadRecordORM{
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
		// 记录失败则撤销 aria2 下载，避免孤儿任务
		_, _ = downloader.Aria2Downloader.ForceRemove(gid)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	llog.L().Infof("offline download created, id: %d, gid: %s, url: %s, parent: %s, user: %s",
		*dr.Id, gid, rawURL, parentID, userID)

	pb, _ := dr.ToPB(ctx)
	return &native.CreateOfflineDownloadResp{
		Record: &pb,
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
		records = append(records, &pb)
	}

	return &native.ListOfflineDownloadsResp{
		Records: records,
		Total:   utils.Uint32Ptr(uint32(total)),
	}, nil
}

// DeleteOfflineDownload 删除离线下载任务：停止 aria2 下载并移除记录
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

	// 停止 aria2 下载（仅非终态）
	if dr.State != nil && *dr.State != int32(native.DownloadRecordState_DOWNLOAD_COMPLETED) &&
		*dr.State != int32(native.DownloadRecordState_DOWNLOAD_FAILED) {
		if downloader.Aria2Downloader != nil && dr.Gid != nil {
			_, _ = downloader.Aria2Downloader.ForceRemove(*dr.Gid)
		}
	}

	if err := db.DeleteDownloadRecord(*req.Id); err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	return nil
}

// RetryOfflineDownload 重试失败的离线下载任务
// 复用原 URL 重新创建 aria2 任务并更新 gid
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
	if dr.Url == nil || downloader.Aria2Downloader == nil {
		return nil, &models.Err{
			E: fmt.Errorf("离线下载服务不可用或缺少 URL"),
			C: native.RespCode_INTERNAL_ERROR,
		}
	}

	downloadDir := filepath.Join(globals.DataDir, ".offline")
	_ = os.MkdirAll(downloadDir, 0o755)

	gid, aErr := downloader.Aria2Downloader.AddURI(
		[]string{*dr.Url},
		map[string]any{"dir": downloadDir},
	)
	if aErr != nil {
		llog.L().Errorf("aria2 addUri retry failed, id: %d, err: %v", *req.Id, aErr)
		return nil, &models.Err{E: fmt.Errorf("aria2 添加下载失败: %v", aErr), C: native.RespCode_INTERNAL_ERROR}
	}

	// 更新记录：新 gid + 重置状态
	state := int32(native.DownloadRecordState_DOWNLOAD_PENDING)
	dr.Gid = &gid
	dr.State = &state
	dr.ErrorMessage = utils.StringPtr("")
	_ = db.UpdateDownloadRecord(dr, []string{"gid", "state", "error_message"})

	llog.L().Infof("offline download retried, id: %d, new gid: %s", *req.Id, gid)
	pb, _ := dr.ToPB(ctx)
	return &native.CreateOfflineDownloadResp{Record: &pb}, nil
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
