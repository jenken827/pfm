package service

import (
	"context"
	"errors"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GetFileDetail 获取文件详情
func GetFileDetail(ctx context.Context, req *native.FileDetailReq) (*native.FileDetailRespData, *models.Err) {
	fileID := req.GetFileId()
	if fileID == "" {
		return nil, &models.Err{E: errors.New("file_id is required"), C: native.RespCode_PARAMS_ERROR}
	}

	// 1. 获取文件信息
	file, err := db.GetFileByID(fileID)
	if err != nil {
		llog.L().Errorf("get file by id failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 2. 查询文件位置
	var providerLocations []*native.ProviderLocationInfo
	if file.Hash != nil && *file.Hash != "" {
		locations, err := db.GetFileLocationsByBlake3Hash(*file.Hash)
		if err != nil {
			llog.L().Errorf("get file locations failed: %v", err)
			return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}

		// 按提供者分组
		providerLocations = groupLocationsByProvider(locations)
	}

	// 3. 查询文件历史
	histories, err := db.GetFileHistoriesByFileID(fileID)
	if err != nil {
		llog.L().Errorf("get file histories failed: %v", err)
		// 历史记录查询失败不影响整体返回
		histories = nil
	}

	// 4. 转换历史记录为PB格式
	var historyPBs []*native.FileHistory
	for _, h := range histories {
		pb, err := h.ToPB(ctx)
		if err != nil {
			llog.L().Warnf("convert file history to pb failed: %v", err)
			continue
		}
		historyPBs = append(historyPBs, &pb)
	}

	// 5. 转换文件为PB格式
	filePB, err := file.ToPB(ctx)
	if err != nil {
		llog.L().Errorf("convert file to pb failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}

	return &native.FileDetailRespData{
		File:      &filePB,
		Locations: providerLocations,
		Histories: historyPBs,
	}, nil
}

// groupLocationsByProvider 按存储提供者分组文件位置信息
func groupLocationsByProvider(locations []*native.FileLocationORM) []*native.ProviderLocationInfo {
	type providerGroup struct {
		providerID   uint32
		encrypted    bool
		chunkTotal   uint32
		status       native.UploadProviderStatus
		chunkCount   int
		size         uint64
		cacheStatus  native.CacheStatus
	}

	groupMap := make(map[uint32]*providerGroup)

	for _, loc := range locations {
		if loc.ProviderId == nil {
			continue
		}

		providerID := *loc.ProviderId
		group, exists := groupMap[providerID]
		if !exists {
			encrypted := loc.Encrypted != nil && *loc.Encrypted
			chunkTotal := uint32(1)
			if loc.ChunkTotal != nil {
				chunkTotal = *loc.ChunkTotal
			}
			status := native.UploadProviderStatus_PPENDING
			if loc.Status != nil {
				status = native.UploadProviderStatus(*loc.Status)
			}
			cacheStatus := native.CacheStatus_CACHE_STATUS_UNSPECIFIED
			if loc.CacheStatus != nil {
				cacheStatus = native.CacheStatus(*loc.CacheStatus)
			}

			group = &providerGroup{
				providerID:  providerID,
				encrypted:   encrypted,
				chunkTotal:  chunkTotal,
				status:      status,
				cacheStatus: cacheStatus,
			}
			groupMap[providerID] = group
		}
		group.chunkCount++

		// size 取第一个分片（chunk_number=0）的值，即文件总大小
		if loc.ChunkNumber != nil && *loc.ChunkNumber == 0 && loc.Size != nil {
			group.size = *loc.Size
		}
	}

	var result []*native.ProviderLocationInfo
	for _, group := range groupMap {
		// 获取提供者信息
		st := storage.GetStorage(group.providerID)
		providerName := ""
		var providerType native.StorageProviderType
		if st != nil {
			providerName = st.GetName()
			providerType = st.GetType()
		}

		isChunked := group.chunkTotal > 1

		info := &native.ProviderLocationInfo{
			ProviderId:   &group.providerID,
			ProviderName: &providerName,
			ProviderType: &providerType,
			Encrypted:    &group.encrypted,
			IsChunked:    &isChunked,
			ChunkTotal:   &group.chunkTotal,
			Status:       &group.status,
			Size:         &group.size,
			CacheStatus:  &group.cacheStatus,
		}
		result = append(result, info)
	}

	return result
}

// GetFileCacheStatus 查询文件缓存状态
func GetFileCacheStatus(ctx context.Context, req *native.GetFileCacheStatusReq) (*native.GetFileCacheStatusResp, *models.Err) {
	// 查询文件位置记录
	location, err := db.GetFileLocationByFileHashAndProvider(*req.FileHash, *req.ProviderId)
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	var expiredAt *timestamppb.Timestamp
	if location.ExpiredAt != nil {
		expiredAt = timestamppb.New(*location.ExpiredAt)
	}

	return &native.GetFileCacheStatusResp{
		CacheStatus:    (*native.CacheStatus)(location.CacheStatus),
		Message:        location.Message,
		ExpiredAt:      expiredAt,
		UploadStatus:   (*native.UploadProviderStatus)(location.Status),
		UploadProgress: location.Progress,
	}, nil
}