package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"lukechampine.com/blake3"
)

const (
	// DefaultChunkSize 默认分片大小 10MB
	DefaultChunkSize = 10 * 1024 * 1024
	// LargeFileThreshold 大文件阈值 100MB
	LargeFileThreshold = 100 * 1024 * 1024
	// UploadStatusPending 待上传
	UploadStatusPending = "pending"
	// UploadStatusUploading 上传中
	UploadStatusUploading = "uploading"
	// UploadStatusCompleted 已完成
	UploadStatusCompleted = "completed"
	// UploadStatusFailed 失败
	UploadStatusFailed = "failed"
)

// InitUpload 初始化上传任务
func InitUpload(ctx context.Context, req *native.InitUploadReq) (*native.InitUploadRespData, *models.Err) {
	// 1. 检查文件是否已存在（秒传）
	if req.Hash != nil && *req.Hash != "" {
		exists, fileID, err := db.CheckFileExistsByHash(*req.Hash)
		if err != nil {
			llog.GetLogger().Errorf("check file exists failed: %v", err)
			return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
		}
		if exists {
			llog.GetLogger().Infof("file already exists, fast upload: %s", *req.Hash)
			return &native.InitUploadRespData{
				Exists: func() *bool { v := true; return &v }(),
				FileId: &fileID,
			}, nil
		}
	}

	// 2. 检查是否有未完成的上传任务（断点续传）
	var existingRecords []native.UploadChunkRecordORM
	if req.Hash != nil && *req.Hash != "" {
		if err := db.GetDB().Where("file_hash = ? AND status != ?", *req.Hash, UploadStatusCompleted).
			Order("chunk_index asc").
			Find(&existingRecords).Error; err != nil {
			llog.GetLogger().Errorf("query existing upload records failed: %v", err)
		}
	}

	// 如果存在未完成的上传任务，返回 upload_id 和已上传分片
	if len(existingRecords) > 0 {
		uploadID := *existingRecords[0].UploadId
		var uploadedChunks []uint32
		for _, r := range existingRecords {
			// 只包含状态为 uploading 且 chunk_index > 0 的分片
			if r.ChunkIndex != nil && *r.ChunkIndex > 0 &&
				r.Status != nil && *r.Status == UploadStatusUploading {
				uploadedChunks = append(uploadedChunks, *r.ChunkIndex)
			}
		}
		chunkTotal := *existingRecords[0].ChunkTotal
		chunkSize := *existingRecords[0].ChunkSize

		llog.GetLogger().Infof("resume upload: %s, uploaded chunks: %v", uploadID, uploadedChunks)
		return &native.InitUploadRespData{
			UploadId:       &uploadID,
			UploadedChunks: uploadedChunks,
			ChunkTotal:     &chunkTotal,
			ChunkSize:      &chunkSize,
		}, nil
	}

	// 3. 创建新的上传任务
	uploadID := uuid.New().String()

	// 计算分片大小和总数
	chunkSize := DefaultChunkSize
	if req.ChunkSize != nil && *req.ChunkSize > 0 {
		chunkSize = int(*req.ChunkSize)
	}
	fileSize := int64(0)
	if req.Size != nil {
		fileSize = int64(*req.Size)
	}
	chunkTotal := uint32((fileSize + int64(chunkSize) - 1) / int64(chunkSize))
	if chunkTotal == 0 {
		chunkTotal = 1
	}

	// 4. 获取第一个启用的存储介质
	provider, group, err := getFirstEnabledProvider()
	if err != nil {
		llog.GetLogger().Errorf("get storage provider failed: %v", err)
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	// 5. 创建分片上传记录
	status := UploadStatusPending
	parentID := ""
	if req.ParentId != nil {
		parentID = *req.ParentId
	}
	fileName := ""
	if req.Name != nil {
		fileName = *req.Name
	}
	fileHash := ""
	if req.Hash != nil {
		fileHash = *req.Hash
	}
	mime := ""
	if req.Mime != nil {
		mime = *req.Mime
	}
	fileType := int32(0)
	if req.Type != nil {
		fileType = int32(req.Type.Number())
	}

	record := &native.UploadChunkRecordORM{
		UploadId:   &uploadID,
		FileHash:   &fileHash,
		FileName:   &fileName,
		FileSize:   req.Size,
		ParentId:   &parentID,
		Mime:       &mime,
		FileType:   &fileType,
		ChunkTotal: &chunkTotal,
		ChunkSize:  func() *uint32 { v := uint32(chunkSize); return &v }(),
		ChunkIndex: func() *uint32 { v := uint32(0); return &v }(),
		Status:     &status,
		ProviderId: provider.Id,
		GroupId:    group.Id,
	}

	if err := db.GetDB().Create(record).Error; err != nil {
		llog.GetLogger().Errorf("create upload record failed: %v", err)
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	llog.GetLogger().Infof("init upload: %s, chunk_total: %d, chunk_size: %d", uploadID, chunkTotal, chunkSize)

	return &native.InitUploadRespData{
		UploadId:       &uploadID,
		UploadedChunks: []uint32{},
		ChunkTotal:     &chunkTotal,
		ChunkSize:      func() *uint32 { v := uint32(chunkSize); return &v }(),
	}, nil
}

// UploadChunkSingle 上传单个分片（非流式）
func UploadChunkSingle(ctx context.Context, req *native.UploadChunkReq) (*native.UploadChunkRespData, *models.Err) {
	if req.UploadId == nil || *req.UploadId == "" {
		return nil, &models.Err{E: errors.New("upload_id is required"), Code: native.RespCode_PARAMS_ERROR}
	}
	if req.ChunkIndex == nil {
		return nil, &models.Err{E: errors.New("chunk_index is required"), Code: native.RespCode_PARAMS_ERROR}
	}

	uploadID := *req.UploadId
	chunkIndex := *req.ChunkIndex
	chunkHash := ""
	if req.ChunkHash != nil {
		chunkHash = *req.ChunkHash
	}

	llog.GetLogger().Infof("upload chunk: upload_id=%s, chunk_index=%d", uploadID, chunkIndex)

	// 获取上传记录
	var record native.UploadChunkRecordORM
	if err := db.GetDB().Where("upload_id = ?", uploadID).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &models.Err{E: errors.New("upload task not found"), Code: native.RespCode_PARAMS_ERROR}
		}
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	// 获取存储介质配置
	var provider native.StorageProviderORM
	if err := db.GetDB().Where("id = ?", *record.ProviderId).First(&provider).Error; err != nil {
		return nil, &models.Err{E: fmt.Errorf("storage provider not found: %v", err), Code: native.RespCode_INTERNAL_ERROR}
	}

	// 解析配置获取存储路径
	config, err := parseProviderConfig(*provider.Config)
	if err != nil {
		return nil, &models.Err{E: fmt.Errorf("parse provider config failed: %v", err), Code: native.RespCode_INTERNAL_ERROR}
	}
	rootPath := config.RootPath

	// 创建临时目录
	tempDir := filepath.Join(rootPath, ".temp", uploadID)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		llog.GetLogger().Errorf("create temp dir failed: %v", err)
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	// 写入分片文件
	chunkPath := filepath.Join(tempDir, fmt.Sprintf("chunk_%d", chunkIndex))
	chunkFile, err := os.Create(chunkPath)
	if err != nil {
		llog.GetLogger().Errorf("create chunk file failed: %v", err)
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}
	defer chunkFile.Close()

	// 计算 hash 并写入
	hasher := blake3.New(32, nil)
	var totalSize int64

	// 写入分片数据
	if len(req.Chunk) > 0 {
		n, err := chunkFile.Write(req.Chunk)
		if err != nil {
			return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
		}
		hasher.Write(req.Chunk)
		totalSize += int64(n)
	}

	// 验证分片 hash（如果提供了）
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if chunkHash != "" && chunkHash != actualHash {
		os.Remove(chunkPath)
		return nil, &models.Err{E: errors.New("chunk hash mismatch"), Code: native.RespCode_PARAMS_ERROR}
	}

	// 更新上传状态
	status := UploadStatusUploading
	now := time.Now()
	tempPath := chunkPath

	// 检查该分片是否已存在记录
	var existingRecord native.UploadChunkRecordORM
	err = db.GetDB().Where("upload_id = ? AND chunk_index = ?", uploadID, chunkIndex).First(&existingRecord).Error

	if err == gorm.ErrRecordNotFound {
		// 创建新的分片记录
		newRecord := &native.UploadChunkRecordORM{
			UploadId:   &uploadID,
			FileHash:   record.FileHash,
			FileName:   record.FileName,
			FileSize:   record.FileSize,
			ParentId:   record.ParentId,
			Mime:       record.Mime,
			FileType:   record.FileType,
			ChunkTotal: record.ChunkTotal,
			ChunkSize:  record.ChunkSize,
			ChunkIndex: &chunkIndex,
			ChunkHash:  &actualHash,
			TempPath:   &tempPath,
			Status:     &status,
			ProviderId: record.ProviderId,
			GroupId:    record.GroupId,
			UpdatedAt:  &now,
		}
		if err := db.GetDB().Create(newRecord).Error; err != nil {
			llog.GetLogger().Errorf("create chunk record failed: %v", err)
			return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
		}
	} else if err != nil {
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	} else {
		// 更新已有记录
		updates := map[string]interface{}{
			"chunk_hash": actualHash,
			"temp_path":  tempPath,
			"status":     status,
			"updated_at": now,
		}
		if err := db.GetDB().Model(&existingRecord).Updates(updates).Error; err != nil {
			llog.GetLogger().Errorf("update chunk record failed: %v", err)
			return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
		}
	}

	// 统计已上传分片数量
	var uploadedCount int64
	db.GetDB().Model(&native.UploadChunkRecordORM{}).
		Where("upload_id = ? AND status = ?", uploadID, status).
		Count(&uploadedCount)

	chunkTotal := *record.ChunkTotal

	llog.GetLogger().Infof("chunk uploaded: upload_id=%s, chunk_index=%d, progress=%d/%d",
		uploadID, chunkIndex, uploadedCount, chunkTotal)

	return &native.UploadChunkRespData{
		UploadId:      &uploadID,
		ChunkIndex:    &chunkIndex,
		UploadedCount: func() *uint32 { v := uint32(uploadedCount); return &v }(),
		ChunkTotal:    &chunkTotal,
	}, nil
}

// CompleteUpload 完成上传
func CompleteUpload(ctx context.Context, req *native.CompleteUploadReq) (*native.CompleteUploadRespData, *models.Err) {
	uploadID := *req.UploadId

	llog.GetLogger().Infof("complete upload: %s", uploadID)

	// 获取所有分片记录
	var records []native.UploadChunkRecordORM
	if err := db.GetDB().Where("upload_id = ?", uploadID).
		Order("chunk_index asc").
		Find(&records).Error; err != nil {
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	if len(records) == 0 {
		return nil, &models.Err{E: errors.New("no upload records found"), Code: native.RespCode_PARAMS_ERROR}
	}

	// 检查所有分片是否都已上传
	chunkTotal := *records[0].ChunkTotal
	var uploadedCount int
	for _, r := range records {
		if r.Status != nil && *r.Status == UploadStatusUploading {
			uploadedCount++
		}
	}

	if uploadedCount < int(chunkTotal) {
		return nil, &models.Err{
			E:    fmt.Errorf("not all chunks uploaded: %d/%d", uploadedCount, chunkTotal),
			Code: native.RespCode_PARAMS_ERROR,
		}
	}

	// 获取存储介质配置
	var provider native.StorageProviderORM
	if err := db.GetDB().Where("id = ?", *records[0].ProviderId).First(&provider).Error; err != nil {
		return nil, &models.Err{E: fmt.Errorf("storage provider not found: %v", err), Code: native.RespCode_INTERNAL_ERROR}
	}

	config, err := parseProviderConfig(*provider.Config)
	if err != nil {
		return nil, &models.Err{E: fmt.Errorf("parse provider config failed: %v", err), Code: native.RespCode_INTERNAL_ERROR}
	}
	rootPath := config.RootPath

	// 生成文件存储路径
	fileID := uuid.New().String()
	ext := filepath.Ext(*records[0].FileName)
	filePath := filepath.Join(rootPath, "files", fileID[:2], fileID[2:4], fileID+ext)

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		llog.GetLogger().Errorf("create file dir failed: %v", err)
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	// 合并所有分片
	outputFile, err := os.Create(filePath)
	if err != nil {
		llog.GetLogger().Errorf("create output file failed: %v", err)
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}
	defer outputFile.Close()

	hasher := sha256.New()
	var totalSize int64

	for _, record := range records {
		if record.TempPath == nil || *record.TempPath == "" {
			continue
		}
		chunkFile, err := os.Open(*record.TempPath)
		if err != nil {
			llog.GetLogger().Errorf("open chunk file failed: %v", err)
			return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
		}

		n, err := io.Copy(io.MultiWriter(outputFile, hasher), chunkFile)
		chunkFile.Close()
		if err != nil {
			llog.GetLogger().Errorf("copy chunk to output failed: %v", err)
			return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
		}
		totalSize += n
	}

	finalHash := hex.EncodeToString(hasher.Sum(nil))

	// 创建文件记录
	dir := "/"
	parentID := *records[0].ParentId
	if parentID != "" {
		parentDir, err := db.GetFileByID(parentID)
		if err == nil {
			dir = path.Join(*parentDir.Dir, *parentDir.Name)
		}
	}

	fileRecord := &native.FileORM{
		Id:       &fileID,
		Name:     records[0].FileName,
		ParentId: &parentID,
		Dir:      &dir,
		Size:     func() *uint64 { v := uint64(totalSize); return &v }(),
		Hash:     &finalHash,
		Type:     records[0].FileType,
		Mime:     records[0].Mime,
	}

	if err := db.CreateFile(fileRecord); err != nil {
		llog.GetLogger().Errorf("create file record failed: %v", err)
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	// 创建文件位置记录
	providerType := *provider.ProviderType
	locationRecord := &native.FileLocationORM{
		Blake3Hash:   &finalHash,
		Path:         &filePath,
		ProviderType: &providerType,
		ProviderId:   records[0].ProviderId,
		ChunkTotal:   records[0].ChunkTotal,
	}

	if err := db.GetDB().Create(locationRecord).Error; err != nil {
		llog.GetLogger().Errorf("create file location failed: %v", err)
	}

	// 清理临时文件
	tempDir := filepath.Join(rootPath, ".temp", uploadID)
	os.RemoveAll(tempDir)

	// 更新所有分片记录状态为已完成
	completedStatus := UploadStatusCompleted
	db.GetDB().Model(&native.UploadChunkRecordORM{}).
		Where("upload_id = ?", uploadID).
		Update("status", completedStatus)

	fileName := *records[0].FileName
	llog.GetLogger().Infof("upload completed: file_id=%s, name=%s, size=%d", fileID, fileName, totalSize)

	return &native.CompleteUploadRespData{
		FileId: &fileID,
		Name:   &fileName,
		Size:   func() *uint64 { v := uint64(totalSize); return &v }(),
	}, nil
}

// GetUploadProgress 获取上传进度
func GetUploadProgress(ctx context.Context, req *native.UploadProgressReq) (*native.UploadProgressRespData, *models.Err) {
	uploadID := *req.UploadId

	var records []native.UploadChunkRecordORM
	if err := db.GetDB().Where("upload_id = ?", uploadID).
		Order("chunk_index asc").
		Find(&records).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &models.Err{E: errors.New("upload task not found"), Code: native.RespCode_PARAMS_ERROR}
		}
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	if len(records) == 0 {
		return nil, &models.Err{E: errors.New("upload task not found"), Code: native.RespCode_PARAMS_ERROR}
	}

	var uploadedChunks []uint32
	var status string
	for _, r := range records {
		if r.Status != nil && (*r.Status == UploadStatusUploading || *r.Status == UploadStatusCompleted) {
			if r.ChunkIndex != nil && *r.ChunkIndex > 0 {
				uploadedChunks = append(uploadedChunks, *r.ChunkIndex)
			}
		}
		status = *r.Status
	}

	chunkTotal := *records[0].ChunkTotal
	uploadedCount := uint32(len(uploadedChunks))

	return &native.UploadProgressRespData{
		UploadId:       &uploadID,
		ChunkTotal:     &chunkTotal,
		UploadedChunks: uploadedChunks,
		UploadedCount:  &uploadedCount,
		Status:         &status,
	}, nil
}

// CancelUpload 取消上传
func CancelUpload(ctx context.Context, req *native.CancelUploadReq) *models.Err {
	uploadID := *req.UploadId

	llog.GetLogger().Infof("cancel upload: %s", uploadID)

	// 获取上传记录
	var records []native.UploadChunkRecordORM
	if err := db.GetDB().Where("upload_id = ?", uploadID).Find(&records).Error; err != nil {
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	if len(records) == 0 {
		return nil
	}

	// 获取存储介质配置
	var provider native.StorageProviderORM
	if err := db.GetDB().Where("id = ?", *records[0].ProviderId).First(&provider).Error; err != nil {
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	config, err := parseProviderConfig(*provider.Config)
	if err != nil {
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}
	rootPath := config.RootPath

	// 清理临时文件
	tempDir := filepath.Join(rootPath, ".temp", uploadID)
	os.RemoveAll(tempDir)

	// 删除数据库记录
	if err := db.GetDB().Where("upload_id = ?", uploadID).Delete(&native.UploadChunkRecordORM{}).Error; err != nil {
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	return nil
}

// getFirstEnabledProvider 获取第一个启用的存储介质
func getFirstEnabledProvider() (*native.StorageProviderORM, *native.StorageGroupORM, error) {
	// 获取第一个存储组
	var group native.StorageGroupORM
	if err := db.GetDB().Order("id asc").First(&group).Error; err != nil {
		return nil, nil, fmt.Errorf("no storage group found: %v", err)
	}

	// 获取该组第一个启用的存储介质
	var provider native.StorageProviderORM
	if err := db.GetDB().Where("group_id = ? AND enabled = ?", group.Id, true).
		Order("`order` asc, id asc").
		First(&provider).Error; err != nil {
		return nil, nil, fmt.Errorf("no enabled storage provider found in group: %v", err)
	}

	return &provider, &group, nil
}

// ProviderConfig 存储介质配置
type ProviderConfig struct {
	RootPath        string `json:"root_path"`
	Capacity        int64  `json:"capacity"`
	ShowHidden      bool   `json:"show_hidden"`
	MkdirPermission string `json:"mkdir_permission"`
}

// parseProviderConfig 解析存储介质配置
func parseProviderConfig(configStr string) (*ProviderConfig, error) {
	var config ProviderConfig
	if err := json.Unmarshal([]byte(configStr), &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// GetFileByHash 根据哈希获取文件
func GetFileByHash(hash string) (*native.FileORM, error) {
	var file native.FileORM
	if err := db.GetDB().Where("hash = ?", hash).First(&file).Error; err != nil {
		return nil, err
	}
	return &file, nil
}

// ParseInt helper function to parse string to int
func ParseInt(s string, defaultValue int) int {
	if s == "" {
		return defaultValue
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultValue
	}
	return v
}
