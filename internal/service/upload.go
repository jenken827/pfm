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
	"path/filepath"
	"strconv"

	"jenken827.com/jenken827/pfm/flags"
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
)

// InitUpload 初始化上传任务
func InitUpload(ctx context.Context, req *native.InitUploadReq) (*native.InitUploadRespData, *models.Err) {
	// 根据hash查询文件记录
	existingFile, err := db.GetFile("hash = ?", *req.Hash)
	if err != nil && err != gorm.ErrRecordNotFound {
		llog.GetLogger().Errorf("check file exists by hash failed: %v", err)
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}
	// 检查同目录下是否有同名文件
	sameNameFile, err := db.GetFile("parent_id = ? and name = ?", *req.ParentId, *req.Name)
	if err != nil && err != gorm.ErrRecordNotFound {
		llog.GetLogger().Errorf("check same name file failed: %v", err)
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}
	// 文件已存在（通过hash判断）
	if existingFile != nil {
		llog.GetLogger().Infof("file already exists by hash: %s, name: %s, parent_id: %s",
			*req.Hash, *existingFile.Name, *existingFile.ParentId)
		if sameNameFile != nil {
			// 同名文件存在，检查哈希是否匹配
			if *sameNameFile.Hash == *req.Hash {
				llog.GetLogger().Infof("same file exists, fast upload: %s", *req.Hash)
				a := true
				return &native.InitUploadRespData{
					Exists: &a,
				}, nil
			}
			//hash不匹配
			if !*req.Overwrite { //不允许覆盖，直接返回错误
				llog.GetLogger().Infof("same name file exists, overwrite=false: %s", *req.Name)
				return nil, &models.Err{
					E:    fmt.Errorf("file with same name already exists: %s", *req.Name),
					Code: native.RespCode_ITEM_ALREADY_EXISTS,
				}
			}
		} else { // 不存在同名文件，文件已经上传了，只需要添加新的记录
			newFileID := uuid.New().String()
			dir := "/"
			if *req.ParentId != "" {
				//获取父目录信息
				parentDir, err := db.GetFile("id = ?", *req.ParentId)
				if err != nil {
					llog.GetLogger().Errorf("get parent dir failed: %v", err)
					return nil, &models.Err{E: err, Code: native.RespCode_CUSTOM_ERROR}
				}
				dir = path.Join(*parentDir.Dir, *parentDir.Name)
			}

			// 创建新的文件记录（复用已存在的文件内容）
			newFileRecord := &native.FileORM{
				Id:       &newFileID,
				Name:     req.Name,
				ParentId: req.ParentId,
				Dir:      &dir,
				Size:     existingFile.Size,
				Hash:     existingFile.Hash,
				Type:     existingFile.Type,
				Mime:     existingFile.Mime,
			}
			if err := db.CreateFile(newFileRecord); err != nil {
				llog.GetLogger().Errorf("create file record failed: %v", err)
				return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
			}
			a := true
			return &native.InitUploadRespData{
				Exists: &a,
			}, nil
		}
	} else {
		// 文件未上传过，存在同名文件，如果覆盖标志为false，返回存在同名文件错误
		if sameNameFile != nil && !*req.Overwrite {
			llog.GetLogger().Infof("same name file exists, overwrite=false: %s", *req.Name)
			return nil, &models.Err{
				E:    fmt.Errorf("file with same name already exists: %s", *req.Name),
				Code: native.RespCode_ITEM_ALREADY_EXISTS,
			}
		}
	}

	// 检查是否有未完成的上传任务（断点续传）
	record, chunks, err := db.GetUploadRecord("file_hash = ?", true, *req.Hash)
	if err != nil && err != gorm.ErrRecordNotFound {
		llog.GetLogger().Errorf("get uncomplete uploads failed: %v", err)
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
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
		llog.GetLogger().Infof("resume upload: %s, chunk_total: %d, chunk_size: %d",
			uploadID, chunkTotal, chunkSize)
		return &native.InitUploadRespData{
			UploadId:       &uploadID,
			UploadedChunks: uploadedChunksBitmap,
			ChunkTotal:     &chunkTotal,
			ChunkSize:      &chunkSize,
		}, nil
	}

	// 4. 创建新的上传任务
	uploadID := uuid.New().String()

	// 计算分片大小和总数
	chunkSize := uint64(DefaultChunkSize)
	chunkTotal := uint32((*req.Size + chunkSize - 1) / chunkSize)
	if chunkTotal == 0 {
		chunkTotal = 1
	}

	// 创建临时目录
	tempDir := filepath.Join(flags.TempDir, uploadID)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		llog.GetLogger().Errorf("create temp dir failed: %v", err)
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	ur := &native.UploadRecordORM{
		Id:         &uploadID,
		FileHash:   req.Hash,
		FileName:   req.Name,
		FileSize:   req.Size,
		ParentId:   req.ParentId,
		ChunkTotal: &chunkTotal,
		ChunkSize:  &chunkSize,
		TempPath:   &tempDir,
	}

	if err := db.GetDB().Create(ur).Error; err != nil {
		llog.GetLogger().Errorf("create upload record failed: %v", err)
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	llog.GetLogger().Infof("init upload: %s, chunk_total: %d, chunk_size: %d",
		uploadID, chunkTotal, chunkSize)

	return &native.InitUploadRespData{
		UploadId:       &uploadID,
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

	llog.GetLogger().Infof("upload chunk: upload_id=%s, chunk_index=%d", uploadID, chunkIndex)

	// 获取上传记录
	uploadRecord, _, err := db.GetUploadRecord("id = ?", false, uploadID)
	if err != nil {
		llog.GetLogger().Errorf("get upload record failed: %v", err)
		return &models.Err{E: err, Code: native.RespCode_CUSTOM_ERROR}
	}

	// 写入分片文件
	chunkPath := filepath.Join(*uploadRecord.TempPath, fmt.Sprintf("chunk_%d", chunkIndex))
	chunkFile, err := os.Create(chunkPath)
	if err != nil {
		llog.GetLogger().Errorf("create chunk file failed: %v", err)
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}
	defer chunkFile.Close()

	// 计算 hash 并写入
	hasher := blake3.New(32, nil)
	var totalSize int64

	// 写入分片数据
	if len(req.Chunk) > 0 {
		n, err := chunkFile.Write(req.Chunk)
		if err != nil {
			return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
		}
		hasher.Write(req.Chunk)
		totalSize += int64(n)
	}

	// 验证分片 hash（如果提供了）
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if chunkHash != "" && chunkHash != actualHash {
		os.Remove(chunkPath)
		llog.GetLogger().Errorf("chunk hash mismatch: expected=%s, actual=%s", chunkHash, actualHash)
		return &models.Err{E: errors.New("chunk hash mismatch"), Code: native.RespCode_PARAMS_ERROR}
	}

	// 创建新的分片记录
	newRecord := &native.UploadChunkRecordORM{
		UploadId: &uploadID,
		Hash:     &actualHash,
		Index:    &chunkIndex,
	}
	if err := db.GetDB().Create(newRecord).Error; err != nil {
		llog.GetLogger().Errorf("create chunk record failed: %v", err)
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	return nil
}

// CompleteUpload 完成上传
func CompleteUpload(ctx context.Context, req *native.CompleteUploadReq) *models.Err {
	uploadID := *req.UploadId

	llog.GetLogger().Infof("complete upload: %s", uploadID)

	uploadRecord, chunks, err := db.GetUploadRecord("id=?", true, uploadID)

	if len(chunks) == 0 {
		llog.GetLogger().Errorf("no chunk records found")
		return &models.Err{E: errors.New("no upload records found"), Code: native.RespCode_PARTIAL_UPLOAD_ERROR}
	}

	// 检查所有分片是否都已上传
	if len(chunks) < int(*uploadRecord.ChunkTotal) {
		llog.GetLogger().Errorf("not all chunks uploaded: %d/%d", len(chunks), *uploadRecord.ChunkTotal)
		return &models.Err{
			E:    fmt.Errorf("not all chunks uploaded: %d/%d", len(chunks), *uploadRecord.ChunkTotal),
			Code: native.RespCode_PARTIAL_UPLOAD_ERROR,
		}
	}

	// 存储逻辑-----------------------------------------

	// 获取存储介质配置
	provider, err := db.GetStorageProvider("")
	if err != nil {
		llog.GetLogger().Errorf("get storage provider failed: %v", err)
		return &models.Err{E: fmt.Errorf("storage provider not found: %v", err), Code: native.RespCode_INTERNAL_ERROR}
	}

	config, err := parseProviderConfig(*provider.Config)
	if err != nil {
		llog.GetLogger().Errorf("parse provider config failed: %v", err)
		return &models.Err{E: fmt.Errorf("parse provider config failed: %v", err), Code: native.RespCode_INTERNAL_ERROR}
	}
	rootPath := config.RootPath

	filetype := getFileType(path.Join(*uploadRecord.TempPath, "chunk_0"), *uploadRecord.FileName)

	// 生成文件存储路径
	fileID := uuid.New().String()
	filePath := filepath.Join(rootPath, "files", fileID[:2], fileID[2:4], fileID)

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		llog.GetLogger().Errorf("create file dir failed: %v", err)
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	// 合并所有分片
	outputFile, err := os.Create(filePath)
	if err != nil {
		llog.GetLogger().Errorf("create output file failed: %v", err)
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}
	defer outputFile.Close()

	// 使用 blake3 计算文件哈希（与前端一致）
	hasher := blake3.New(32, nil)
	var totalSize int64

	for i := range chunks {
		f := path.Join(*uploadRecord.TempPath, "chunk_"+strconv.Itoa(i))
		chunkFile, err := os.Open(f)
		if err != nil {
			llog.GetLogger().Errorf("open chunk file failed: %v", err)
			return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
		}

		n, err := io.Copy(io.MultiWriter(outputFile, hasher), chunkFile)
		chunkFile.Close()
		if err != nil {
			llog.GetLogger().Errorf("copy chunk to output failed: %v", err)
			return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
		}
		totalSize += n
	}

	finalHash := hex.EncodeToString(hasher.Sum(nil))
	if finalHash != *uploadRecord.FileHash {
		llog.GetLogger().Errorf("file hash mismatch: expected %s, got %s", *uploadRecord.FileHash, finalHash)
		return &models.Err{E: errors.New("file hash mismatch"), Code: native.RespCode_CUSTOM_ERROR}
	}
	// 创建文件记录
	dir := "/"
	parentID := *uploadRecord.ParentId
	if parentID != "" {
		parentDir, err := db.GetFile("id=?", parentID)
		if err == nil {
			dir = path.Join(*parentDir.Dir, *parentDir.Name)
		}
	}

	fileRecord := &native.FileORM{
		Id:       &fileID,
		Name:     uploadRecord.FileName,
		ParentId: &parentID,
		Dir:      &dir,
		Size:     func() *uint64 { v := uint64(totalSize); return &v }(),
		Hash:     &finalHash,
		Type:     &filetype,
	}

	if err := db.CreateFile(fileRecord); err != nil {
		llog.GetLogger().Errorf("create file record failed: %v", err)
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	// 创建文件位置记录
	ct := uint32(1)
	providerType := *provider.ProviderType
	locationRecord := &native.FileLocationORM{
		Blake3Hash:   &finalHash,
		Path:         &filePath,
		ProviderType: &providerType,
		ProviderId:   provider.Id,
		ChunkTotal:   &ct,
	}

	if err := db.GetDB().Create(locationRecord).Error; err != nil {
		llog.GetLogger().Errorf("create file location failed: %v", err)
	}

	// 清理临时文件
	tempDir := filepath.Join(rootPath, ".temp", uploadID)
	os.RemoveAll(tempDir)

	return nil
}

// GetUploadProgress 获取上传进度
func GetUploadProgress(ctx context.Context, req *native.UploadProgressReq) (*native.UploadProgressRespData, *models.Err) {
	uploadID := *req.UploadId

	uploadRecord, chunks, err := db.GetUploadRecord("id=?", true, uploadID)
	if err != nil {
		llog.GetLogger().Errorf("get upload record failed: %v", err)
		return nil, &models.Err{E: err, Code: native.RespCode_CUSTOM_ERROR}
	}
	uploadedChunks := buildUploadedBitmap(chunks, *uploadRecord.ChunkTotal)

	return &native.UploadProgressRespData{
		UploadId:       &uploadID,
		ChunkTotal:     uploadRecord.ChunkTotal,
		UploadedChunks: uploadedChunks,
	}, nil
}

// CancelUpload 取消上传
func CancelUpload(ctx context.Context, req *native.CancelUploadReq) *models.Err {
	uploadID := *req.UploadId

	llog.GetLogger().Infof("cancel upload: %s", uploadID)

	uploadRecord, _, err := db.GetUploadRecord("id=?", true, uploadID)
	if err != nil && err != gorm.ErrRecordNotFound {
		llog.GetLogger().Errorf("get upload record failed: %v", err)
		return &models.Err{E: err, Code: native.RespCode_CUSTOM_ERROR}
	}
	if err == gorm.ErrRecordNotFound {
		return nil
	}

	// 清理临时文件
	tempDir := filepath.Join(*uploadRecord.TempPath, uploadID)
	os.RemoveAll(tempDir)

	// 删除数据库记录
	if err := db.DelUploadRecord(uploadID); err != nil {
		llog.GetLogger().Errorf("delete upload record failed: %v", err)
		return &models.Err{E: err, Code: native.RespCode_CUSTOM_ERROR}
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
