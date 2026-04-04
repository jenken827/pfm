package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/internal/utils"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"gorm.io/gorm"
	"lukechampine.com/blake3"
)

const (
	// DefaultChunkSize default 10MB
	DefaultChunkSize = 10 * 1024 * 1024
)

// InitUpload init upload flow
func InitUpload(ctx context.Context, req *native.InitUploadReq) (*native.InitUploadRespData, *models.Err) {
	if *req.ParentId == "" {
		return nil, &models.Err{
			E: errors.New("cannot upload file to root directory, please create a directory first"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 获取父文件夹，检查是否绑定了存储提供者
	parentFiles, err := db.GetFiles("id = ?", req.GetParentId())
	if err != nil || len(parentFiles) == 0 {
		llog.L().Errorf("get parent folder failed: %v", err)
		return nil, &models.Err{
			E: errors.New("parent folder not found"),
			C: native.RespCode_DB_ERROR,
		}
	}
	parentFile := parentFiles[0]

	// 检查父文件夹是否绑定了存储提供者
	if parentFile.BoundProviderIds == nil || *parentFile.BoundProviderIds == "" {
		return nil, &models.Err{
			E: errors.New("parent folder has no bound storage providers, please update folder settings"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 验证bound_provider_ids是否为有效的JSON数组
	var providerIds []uint32
	if err := json.Unmarshal([]byte(*parentFile.BoundProviderIds), &providerIds); err != nil {
		llog.L().Errorf("invalid bound_provider_ids format in parent folder: %v", err)
		return nil, &models.Err{
			E: errors.New("invalid bound_provider_ids format in parent folder"),
			C: native.RespCode_INTERNAL_ERROR,
		}
	}

	if len(providerIds) == 0 {
		return nil, &models.Err{
			E: errors.New("parent folder has no bound storage providers"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	blake3 := *req.Hash

	// 检查是否有未完成的上传任务（断点续传）
	record, chunks, err := db.GetUploadRecord("file_hash = ?", true, blake3)
	if err != nil && err != gorm.ErrRecordNotFound {
		llog.L().Errorf("get uncomplete uploads failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	// 如果存在未完成的上传任务，返回 upload_id 和已上传分片（位图）
	if record != nil {
		uploadID := *record.Id
		chunkTotal := *record.ChunkTotal
		chunkSize := *record.ChunkSize

		// 构建已上传分片位图
		uploadedChunksBitmap := make([]byte, (chunkTotal+7)/8)
		if chunks != nil {
			uploadedChunksBitmap = buildUploadedBitmap(chunks, chunkTotal)
		}
		llog.L().Infof("resume upload: %d, chunk_total: %d, chunk_size: %d",
			uploadID, chunkTotal, chunkSize)
		if *record.ParentId != *req.ParentId || *record.FileName != *req.Name {
			llog.L().Errorf("A same hash file is uploading, but file name or parent id not match,uploading file path: %d", *record.Id)

			return nil, &models.Err{E: errors.New("A same hash file is uploading, but file name or parent id not match"), C: native.RespCode_INTERNAL_ERROR}
		}
		return &native.InitUploadRespData{
			UploadId:       &uploadID,
			UploadedChunks: uploadedChunksBitmap,
			ChunkTotal:     &chunkTotal,
			ChunkSize:      &chunkSize,
		}, nil
	}

	// 检查是否已上传过
	existingFiles, err := db.GetFiles("hash = ?", blake3)
	if err != nil {
		llog.L().Errorf("check file exists by hash failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	// 检查是否有同名文件
	sameNameFiles, err := db.GetFiles("parent_id = ? and name = ?", req.GetParentId(), req.GetName())
	if err != nil {
		llog.L().Errorf("check same name file failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 相同文件hash的文件已存在
	if len(existingFiles) > 0 {
		llog.L().Infof("file already exists by hash: %s", blake3)
		existingFile := existingFiles[0]
		exists := true
		rd := &native.InitUploadRespData{
			Exists: &exists,
		}

		// 检查文件已经在哪些存储提供者上
		existingLocations, err := db.GetFileLocations(&db.DBQuery{
			Where: "blake3_hash = ?",
			Args:  []any{blake3},
		})
		if err != nil {
			llog.L().Errorf("get existing file locations failed: %v", err)
			return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}

		// 构建已存在的提供者映射
		existingProviderMap := make(map[uint32]bool)
		for _, loc := range existingLocations {
			if *loc.Status == int32(native.UploadProviderStatus_PCOMPLETED) {
				existingProviderMap[*loc.ProviderId] = true
			}
		}

		// 计算需要同步到哪些新的存储提供者
		var needSyncProviders []uint32
		for _, pid := range providerIds {
			if !existingProviderMap[pid] {
				needSyncProviders = append(needSyncProviders, pid)
			}
		}

		// 如果需要同步，创建真实的同步任务记录
		if len(needSyncProviders) > 0 {
			llog.L().Infof("file exists but needs sync to %d new providers, hash: %s", len(needSyncProviders), blake3)

			// 转换为JSON字符串
			providerIdsJSON, _ := json.Marshal(providerIds)
			targetProviderIds := string(providerIdsJSON)

			// 创建真实的同步任务记录（TempPath 为空表示同步任务）
			emptyStr := ""
			syncStatus := int32(native.GroupUploadStatus_GPENDING)
			syncRecord := &native.UploadRecordORM{
				FileHash:          &blake3,
				TargetProviderIds: &targetProviderIds,
				TempPath:          &emptyStr, // 空路径表示这是同步任务
				Status:            &syncStatus,
			}

			// 保存到数据库，确保可以恢复
			if err := db.GetDB().Create(syncRecord).Error; err != nil {
				llog.L().Errorf("create sync record failed: %v", err)
				// 创建失败不影响秒传响应，只是后台同步可能无法执行
			} else {
				// 后台异步同步
				go storage.Dispatch(context.Background(), syncRecord)
			}
		}

		if len(sameNameFiles) > 0 {
			//存在同名文件
			snf := sameNameFiles[0]
			if *snf.Hash == blake3 {
				return rd, nil
			}
			// hash not same, check overwrite flag
			if !*req.Overwrite {
				llog.L().Infof("same name file exists, overwrite=false: %s", *req.Name)
				return nil, &models.Err{
					E: fmt.Errorf("file with same name already exists: %s", *req.Name),
					C: native.RespCode_ALREADY_EXISTS,
				}
			}
			// 需要覆盖上传，但是已经上传过了，所以直接更新数据表就可以
			f := &native.FileORM{
				Hash: &blake3,
				Size: req.Size,
			}
			err := db.UpdateFileWithHistory(ctx, f, snf)
			if err != nil {
				llog.L().Errorf("update file with history failed: %v", err)
				return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
			}
			return rd, nil
		}
		// 如果没有同名文件，直接新增一条新的记录
		newFileRecord := &native.FileORM{
			Name:     req.Name,
			ParentId: req.ParentId,
			Size:     existingFile.Size,
			Hash:     existingFile.Hash,
			Type:     existingFile.Type,
			Mime:     existingFile.Mime,
		}
		if err := db.CreateFile(newFileRecord); err != nil {
			llog.L().Errorf("create file record failed: %v", err)
			return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}
		return rd, nil
	}

	// 文件未上传过，存在同名文件，如果覆盖标志为false，返回存在同名文件错误
	overwriteID := ""
	if len(sameNameFiles) > 0 {
		if !*req.Overwrite {
			llog.L().Infof("same name file exists, overwrite=false: %s", *req.Name)
			return nil, &models.Err{
				E: fmt.Errorf("file with same name already exists: %s", *req.Name),
				C: native.RespCode_ALREADY_EXISTS,
			}
		}
		sameNameFile := sameNameFiles[0]
		overwriteID = *sameNameFile.Id
	}

	// 计算分片大小和总数
	chunkSize := uint64(DefaultChunkSize)
	chunkTotal := uint32((*req.Size + chunkSize - 1) / chunkSize)
	if chunkTotal == 0 {
		chunkTotal = 1
	}

	// 创建临时目录
	err = utils.CreateFileTempDir(blake3)
	if err != nil {
		llog.L().Errorf("create temp dir failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}

	tp := utils.GetFileTempPath(blake3, *req.Name)
	status := int32(native.GroupUploadStatus_GPENDING)
	ur := &native.UploadRecordORM{
		FileHash:          &blake3,
		FileName:          req.Name,
		FileSize:          req.Size,
		ParentId:          req.ParentId,
		ChunkTotal:        &chunkTotal,
		ChunkSize:         &chunkSize,
		TempPath:          &tp,
		OverwriteId:       &overwriteID,
		Status:            &status,
		TargetProviderIds: parentFile.BoundProviderIds,
	}

	if err := db.GetDB().Create(ur).Error; err != nil {
		llog.L().Errorf("create upload record failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	llog.L().Infof("init upload: %d, chunk_total: %d, chunk_size: %d",
		*ur.Id, chunkTotal, chunkSize)

	return &native.InitUploadRespData{
		UploadId:       ur.Id,
		UploadedChunks: make([]byte, (chunkTotal+7)/8), // 空位图
		ChunkTotal:     &chunkTotal,
		ChunkSize:      &chunkSize,
	}, nil
}

func buildUploadedBitmap(chunks []*native.UploadChunkRecordORM, chunkTotal uint32) []byte {
	// 构建已上传分片位图
	uploadedChunksBitmap := make([]byte, (chunkTotal+7)/8)
	for _, r := range chunks {
		chunkIdx := *r.Index // 位图索引从0开始
		uploadedChunksBitmap[chunkIdx/8] |= (1 << (chunkIdx % 8))
	}
	return uploadedChunksBitmap

}

// UploadChunkSingle 上传单个分片（非流式）
func UploadChunkSingle(ctx context.Context, req *native.UploadChunkReq) *models.Err {
	uploadID := *req.UploadId
	chunkIndex := *req.ChunkIndex
	chunkHash := *req.ChunkHash

	llog.L().Infof("upload chunk: upload_id=%d, chunk_index=%d", uploadID, chunkIndex)

	// 获取上传记录
	uploadRecord, _, err := db.GetUploadRecord("id = ?", false, uploadID)
	if err != nil {
		llog.L().Errorf("get upload record failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 写入分片文件
	chunkPath := utils.GetChunkTempPath(*uploadRecord.FileHash, int(chunkIndex))
	chunkFile, err := os.Create(chunkPath)
	if err != nil {
		llog.L().Errorf("create chunk file failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	defer chunkFile.Close()

	// 计算 hash 并写入
	hasher := blake3.New(32, nil)
	var totalSize int64

	// 写入分片数据
	if len(req.Chunk) > 0 {
		n, err := chunkFile.Write(req.Chunk)
		if err != nil {
			return &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
		}
		hasher.Write(req.Chunk)
		totalSize += int64(n)
	}

	// 验证分片 hash（如果提供了）
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if chunkHash != "" && chunkHash != actualHash {
		os.Remove(chunkPath)
		llog.L().Errorf("chunk hash mismatch: expected=%s, actual=%s", chunkHash, actualHash)
		return &models.Err{E: errors.New("chunk hash mismatch"), C: native.RespCode_PARAMS_ERROR}
	}

	// 创建新的分片记录
	newRecord := &native.UploadChunkRecordORM{
		UploadId: &uploadID,
		Hash:     &actualHash,
		Index:    &chunkIndex,
	}
	if err := db.GetDB().Create(newRecord).Error; err != nil {
		llog.L().Errorf("create chunk record failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}

	return nil
}

// CompleteUpload 完成上传
func CompleteUpload(ctx context.Context, req *native.CompleteUploadReq) *models.Err {
	uploadID := *req.UploadId

	llog.L().Infof("complete upload: %d", uploadID)

	uploadRecord, chunks, err := db.GetUploadRecord("id=?", true, uploadID)

	if len(chunks) == 0 {
		llog.L().Errorf("no chunk records found")
		return &models.Err{E: errors.New("no upload records found"), C: native.RespCode_PARTIAL_UPLOAD_ERROR}
	}

	// 检查所有分片是否都已上传
	if len(chunks) < int(*uploadRecord.ChunkTotal) {
		llog.L().Errorf("not all chunks uploaded: %d/%d", len(chunks), *uploadRecord.ChunkTotal)
		return &models.Err{
			E: fmt.Errorf("not all chunks uploaded: %d/%d", len(chunks), *uploadRecord.ChunkTotal),
			C: native.RespCode_PARTIAL_UPLOAD_ERROR,
		}
	}

	// 合并所有分片
	outputFile, err := os.Create(*uploadRecord.TempPath)
	if err != nil {
		llog.L().Errorf("create output file failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	defer outputFile.Close()

	// 使用 blake3 计算文件哈希（与前端一致）
	hasher := blake3.New(32, nil)
	var totalSize int64

	for i := range chunks {
		f := utils.GetChunkTempPath(*uploadRecord.FileHash, i)
		chunkFile, err := os.Open(f)
		if err != nil {
			llog.L().Errorf("open chunk file failed: %v", err)
			return &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
		}

		n, err := io.Copy(io.MultiWriter(outputFile, hasher), chunkFile)
		chunkFile.Close()
		if err != nil {
			llog.L().Errorf("copy chunk to output failed: %v", err)
			return &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
		}
		totalSize += n
	}

	finalHash := hex.EncodeToString(hasher.Sum(nil))
	if finalHash != *uploadRecord.FileHash {
		llog.L().Errorf("file hash mismatch: expected %s, got %s", *uploadRecord.FileHash, finalHash)
		return &models.Err{E: errors.New("file hash mismatch"), C: native.RespCode_INTERNAL_ERROR}
	}
	filetype, mime := getFileType(utils.GetChunkTempPath(*uploadRecord.FileHash, 0), *uploadRecord.FileName)

	fileRecord := &native.FileORM{
		Name:     uploadRecord.FileName,
		ParentId: uploadRecord.ParentId,
		Size:     func() *uint64 { v := uint64(totalSize); return &v }(),
		Hash:     &finalHash,
		Type:     &filetype,
		Mime:     &mime,
	}

	// 检查是否是覆盖上传
	if uploadRecord.OverwriteId != nil && *uploadRecord.OverwriteId != "" {
		// 覆盖上传：更新现有文件记录并创建历史
		fileRecord.Id = uploadRecord.OverwriteId
		oldFiles, err := db.GetFiles("id = ?", *uploadRecord.OverwriteId)
		if err != nil {
			llog.L().Errorf("get old file record failed: %v", err)
			return &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}
		if len(oldFiles) > 0 {
			oldFile := oldFiles[0]
			if err := db.UpdateFileWithHistory(ctx, fileRecord, oldFile); err != nil {
				llog.L().Errorf("update file with history failed: %v", err)
				return &models.Err{E: err, C: native.RespCode_DB_ERROR}
			}
			llog.L().Infof("file updated with history, id: %s, old_hash: %s, new_hash: %s", *fileRecord.Id, *oldFile.Hash, finalHash)
		} else {
			// 旧文件不存在（可能被删除了），创建新记录
			if err := db.CreateFile(fileRecord); err != nil {
				llog.L().Errorf("create file record failed: %v", err)
				return &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
			}
			llog.L().Warnf("overwrite file not found, created new record, id: %s", *fileRecord.Id)
		}
	} else {
		// 新上传：创建新的文件记录
		if err := db.CreateFile(fileRecord); err != nil {
			llog.L().Errorf("create file record failed: %v", err)
			return &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
		}
		llog.L().Infof("new file created, id: %s, hash: %s", *fileRecord.Id, finalHash)
	}
	// 移除分片
	if err := os.RemoveAll(utils.GetChunkTempDir(*uploadRecord.FileHash)); err != nil {
		llog.L().Warnf("remove temp path failed: %v", err)
	}
	// 上传到存储
	go storage.Dispatch(ctx, uploadRecord)
	return nil
}

// CancelUpload 取消上传
func CancelUpload(ctx context.Context, req *native.CancelUploadReq) *models.Err {
	uploadID := *req.UploadId

	llog.L().Infof("cancel upload: %d", uploadID)

	uploadRecord, _, err := db.GetUploadRecord("id=?", true, uploadID)
	if err != nil && err != gorm.ErrRecordNotFound {
		llog.L().Errorf("get upload record failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	if err == gorm.ErrRecordNotFound {
		return nil
	}

	// 清理临时文件
	os.RemoveAll(path.Dir(*uploadRecord.TempPath))

	// 删除数据库记录
	if err := db.DelUploadRecord(uploadID); err != nil {
		llog.L().Errorf("delete upload record failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	return nil
}
