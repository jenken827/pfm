package service

import (
	"context"
	"errors"

	"google.golang.org/protobuf/types/known/timestamppb"
	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// GetFileDetail 获取文件详情
func GetFileDetail(ctx context.Context, req *native.FileDetailReq) (*native.FileDetailRespData, *models.Err) {
	fileID := req.GetFileId()
	if fileID == "" {
		return nil, &models.Err{E: errors.New("file_id is required"), C: native.RespCode_PARAMS_ERROR}
	}

	// 归属校验：仅文件所有者可查看详情（管理员不可越权）
	// 注：放在最前，避免对他人文件做任何后续查询
	if !CheckOwnership(ctx, fileID) {
		return nil, &models.Err{
			E: errors.New("permission denied: not owner of file"),
			C: native.RespCode_PERMISSION_DENIED,
		}
	}

	// 1. 获取文件信息
	file, err := db.GetFileByID(fileID)
	if err != nil {
		llog.C(ctx).Errorf("get file by id failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 2. 查询文件位置
	var providerLocations []*native.ProviderLocationInfo
	if file.Hash != nil && *file.Hash != "" {
		locations, err := db.GetFileLocationsByBlake3Hash(*file.Hash)
		if err != nil {
			llog.C(ctx).Errorf("get file locations failed: %v", err)
			return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}

		// 按提供者分组
		providerLocations = groupLocationsByProvider(locations)
	}

	// 3. 查询文件历史
	histories, err := db.GetFileHistoriesByFileID(fileID)
	if err != nil {
		llog.C(ctx).Errorf("get file histories failed: %v", err)
		// 历史记录查询失败不影响整体返回
		histories = nil
	}

	// 4. 查询文件标签
	tags, err := db.GetFileTagsByFileID(fileID)
	if err != nil {
		llog.C(ctx).Errorf("get file tags failed: %v", err)
		// 标签查询失败不影响整体返回
		tags = nil
	}

	// 5. 转换历史记录为PB格式
	var historyPBs []*native.FileHistory
	for _, h := range histories {
		pb, err := h.ToPB(ctx)
		if err != nil {
			llog.C(ctx).Warnf("convert file history to pb failed: %v", err)
			continue
		}
		historyPBs = append(historyPBs, &pb)
	}

	// 6. 转换文件为PB格式
	filePB, err := file.ToPB(ctx)
	if err != nil {
		llog.C(ctx).Errorf("convert file to pb failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}

	return &native.FileDetailRespData{
		File:      &filePB,
		Locations: providerLocations,
		Histories: historyPBs,
		Tags:      tags,
	}, nil
}

// groupLocationsByProvider 按存储提供者分组文件位置信息
func groupLocationsByProvider(locations []*native.FileLocationORM) []*native.ProviderLocationInfo {
	type providerGroup struct {
		providerID  uint32
		encrypted   bool
		chunkTotal  uint32
		status      native.LocationStatus
		chunkCount  int
		size        uint64
		cacheStatus native.CacheStatus
		storagePath string
	}

	groupMap := make(map[uint32]*providerGroup)

	for _, loc := range locations {
		if loc.ProviderId == nil {
			continue
		}

		providerID := *loc.ProviderId
		llog.L().Infof("groupLocationsByProvider: loc_id=%v, provider_id=%d, path=%v, chunk=%v",
			loc.Id, providerID, loc.Path, loc.ChunkNumber)
		group, exists := groupMap[providerID]
		if !exists {
			encrypted := loc.Encrypted != nil && *loc.Encrypted
			chunkTotal := uint32(1)
			if loc.ChunkTotal != nil {
				chunkTotal = *loc.ChunkTotal
			}
			status := native.LocationStatus_PPENDING
			if loc.Status != nil {
				status = native.LocationStatus(*loc.Status)
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

		// storagePath 取第一个分片的 path
		if group.storagePath == "" && loc.Path != nil && *loc.Path != "" && *loc.Path != "/" {
			llog.L().Infof("groupLocationsByProvider: provider_id=%d, storagePath=%q", providerID, *loc.Path)
			group.storagePath = *loc.Path
		}
	}

	var result []*native.ProviderLocationInfo
	// 实例名/类型批量取一次（避免逐 provider 查库）
	//
	// 注意不能用 st.GetName()：各驱动的 GetName() 返回的是**驱动身份**
	// （local / telegram…），同驱动多实例无法区分；实例名在 storage_providers.name。
	// provider 行已被删除（位置行还在）时回退到驱动身份。
	ids := make([]uint32, 0, len(groupMap))
	for id := range groupMap {
		ids = append(ids, id)
	}
	nameByID := make(map[uint32]string, len(ids))
	typeByID := make(map[uint32]string, len(ids))
	if providers, err := db.GetStorageProvidersByIDs(ids); err == nil {
		for _, p := range providers {
			if p == nil || p.Id == nil {
				continue
			}
			if p.Name != nil && *p.Name != "" {
				nameByID[*p.Id] = *p.Name
			}
			if p.DriverId != nil {
				typeByID[*p.Id] = *p.DriverId
			}
		}
	}

	for _, group := range groupMap {
		st := storage.GetStorage(group.providerID)
		providerName := nameByID[group.providerID]
		driverID, hasDriverID := typeByID[group.providerID]
		if st != nil {
			if providerName == "" {
				providerName = st.GetName()
			}
			if !hasDriverID {
				driverID = st.GetType()
			}
		}

		isChunked := group.chunkTotal > 1

		info := &native.ProviderLocationInfo{
			ProviderId:   &group.providerID,
			ProviderName: &providerName,
			DriverId:     &driverID,
			Encrypted:    &group.encrypted,
			IsChunked:    &isChunked,
			ChunkTotal:   &group.chunkTotal,
			Status:       &group.status,
			Size:         &group.size,
			CacheStatus:  &group.cacheStatus,
			StoragePath:  &group.storagePath,
		}
		if metadata := types.GetDriverMetadata(driverID); metadata != nil && metadata.IconName != nil {
			info.DriverIcon = metadata.IconName
		}
		result = append(result, info)
	}

	return result
}

// GetFileInfo 获取文件基本信息（轻量接口，只返回id/name/parent_id/type/size）
func GetFileInfo(ctx context.Context, req *native.GetFileInfoReq) (*native.GetFileInfoRespData, *models.Err) {
	fileID := req.GetFileId()
	if fileID == "" {
		return nil, &models.Err{E: errors.New("file_id is required"), C: native.RespCode_PARAMS_ERROR}
	}

	// 归属校验：仅文件所有者可查看（管理员不可越权）
	if !CheckOwnership(ctx, fileID) {
		return nil, &models.Err{
			E: errors.New("permission denied: not owner of file"),
			C: native.RespCode_PERMISSION_DENIED,
		}
	}

	file, err := db.GetFileByID(fileID)
	if err != nil {
		llog.C(ctx).Errorf("get file by id failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	resp := &native.GetFileInfoRespData{
		Id:               file.Id,
		Name:             file.Name,
		ParentId:         file.ParentId,
		Size:             file.Size,
		BoundProviderIds: file.BoundProviderIds,
	}
	if file.Type != nil {
		ft := native.FileType(*file.Type)
		resp.Type = &ft
	}
	return resp, nil
}

// GetFileCacheStatus 查询文件缓存状态
func GetFileCacheStatus(ctx context.Context, req *native.GetFileCacheStatusReq) (*native.GetFileCacheStatusResp, *models.Err) {
	// 归属校验：入参为 file_hash（全局唯一），必须先确认当前用户拥有引用该 hash 的文件，
	// 否则任何登录用户都可枚举查询他人文件的缓存/上传状态。
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return nil, &models.Err{E: errors.New("unauthorized"), C: native.RespCode_TOKEN_INVALID}
	}
	ownerFile, err := db.GetFileByHashAndOwner(req.GetFileHash(), userID)
	if err != nil {
		return nil, &models.Err{
			E: errors.New("permission denied: no file with this hash owned by you"),
			C: native.RespCode_PERMISSION_DENIED,
		}
	}
	_ = ownerFile

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
		UploadStatus:   (*native.LocationStatus)(location.Status),
		UploadProgress: location.Progress,
	}, nil
}
