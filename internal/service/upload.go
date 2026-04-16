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
	"strings"
	"sync"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/gorm"
	"lukechampine.com/blake3"
)

const (
	// DefaultChunkSize default 10MB
	DefaultChunkSize = 10 * 1024 * 1024
	// SmallFileThreshold 小文件阈值 10MB
	SmallFileThreshold = 10 * 1024 * 1024
)

// chunkUploadKey 分片上传并发控制键
type chunkUploadKey struct {
	uploadID   uint32
	chunkIndex uint32
}

// chunkUploadLocks 分片上传锁管理
var chunkUploadLocks sync.Map // map[chunkUploadKey]chan struct{}

// acquireChunkLock 获取分片上传锁（非阻塞）
func acquireChunkLock(key chunkUploadKey) bool {
	ch := make(chan struct{})
	_, loaded := chunkUploadLocks.LoadOrStore(key, ch)
	if loaded {
		// 已经有任务在处理
		return false
	}
	return true
}

// releaseChunkLock 释放分片上传锁
func releaseChunkLock(key chunkUploadKey) {
	if ch, ok := chunkUploadLocks.Load(key); ok {
		close(ch.(chan struct{}))
		chunkUploadLocks.Delete(key)
	}
}

// parseAndCreateTags 解析标签字符串并创建文件标签
func parseAndCreateTags(fileID string, tagsStr *string) {
	if tagsStr == nil || *tagsStr == "" {
		return
	}
	// 按逗号分隔标签
	tags := strings.Split(*tagsStr, ",")
	// 去除空白
	var cleanTags []string
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			cleanTags = append(cleanTags, tag)
		}
	}
	if len(cleanTags) == 0 {
		return
	}
	if err := db.CreateFileTags(fileID, cleanTags); err != nil {
		llog.L().Errorf("create file tags failed: %v", err)
	} else {
		llog.L().Infof("created %d tags for file: %s", len(cleanTags), fileID)
	}
}

// validateAndGetParent 验证父文件夹并获取存储提供者列表
func validateAndGetParent(parentID string) (*native.FileORM, []uint32, *models.Err) {
	parentFile, err := db.GetFileByID(parentID)
	if err != nil {
		llog.L().Errorf("get parent folder failed: %v", err)
		return nil, nil, &models.Err{
			E: errors.New("parent folder not found"),
			C: native.RespCode_DB_ERROR,
		}
	}

	if parentFile.BoundProviderIds == nil || *parentFile.BoundProviderIds == "" {
		return nil, nil, &models.Err{
			E: errors.New("parent folder has no bound storage providers"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	var providerIds []uint32
	if err := json.Unmarshal([]byte(*parentFile.BoundProviderIds), &providerIds); err != nil {
		llog.L().Errorf("invalid bound_provider_ids format: %v", err)
		return nil, nil, &models.Err{
			E: errors.New("invalid bound_provider_ids format"),
			C: native.RespCode_INTERNAL_ERROR,
		}
	}

	if len(providerIds) == 0 {
		return nil, nil, &models.Err{
			E: errors.New("parent folder has no bound storage providers"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	return parentFile, providerIds, nil
}

// checkFileExists 检查文件是否已存在（通过hash和同名）
func checkFileExists(parentID, fileName, fileHash string) (*native.FileORM, *native.FileORM, *models.Err) {
	existingFile, err := db.GetFileByHash(fileHash)
	if err != nil && err != gorm.ErrRecordNotFound {
		llog.L().Errorf("check file exists by hash failed: %v", err)
		return nil, nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	sameNameFile, err := db.GetFileByParentAndName(parentID, fileName)
	if err != nil && err != gorm.ErrRecordNotFound {
		llog.L().Errorf("check same name file failed: %v", err)
		return nil, nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	return existingFile, sameNameFile, nil
}

// createSyncTask 创建同步任务
func createSyncTask(fileName *string, fileSize *uint64, parentID *string, fileHash string, providerIds []uint32) {
	providerIdsJSON, _ := json.Marshal(providerIds)
	targetProviderIds := string(providerIdsJSON)
	emptyStr := ""

	syncRecord := &native.UploadRecordORM{
		FileName:          fileName,
		FileSize:          fileSize,
		ParentId:          parentID,
		FileHash:          &fileHash,
		TargetProviderIds: &targetProviderIds,
		TempPath:          &emptyStr,
		Status:            utils.EnumPtr(native.LocationStatus_PPENDING),
		TaskType:          utils.EnumPtr(native.UploadTaskType_SYNC),
	}

	if err := db.GetDB().Create(syncRecord).Error; err != nil {
		llog.L().Errorf("create sync record failed: %v", err)
	} else {
		go storage.Dispatch(context.Background(), syncRecord)
	}
}

// handleInstantUpload 处理秒传逻辑
// 返回值：handled 表示是否已处理，需要直接返回；fileToUse 表示应该使用的文件记录
func handleInstantUpload(
	ctx context.Context,
	fileHash string,
	fileName *string,
	fileSize *uint64,
	parentID *string,
	providerIds []uint32,
	existingFile *native.FileORM,
	sameNameFile *native.FileORM,
	overwrite bool,
	tags *string,
) (handled bool, fileToUse *native.FileORM, err *models.Err) {
	if existingFile == nil {
		return false, nil, nil
	}

	llog.L().Infof("file already exists by hash: %s", fileHash)

	// 检查文件已经在哪些存储提供者上
	existingLocations, dbErr := db.GetFileLocationsByBlake3Hash(fileHash)
	if dbErr != nil {
		llog.L().Errorf("get existing file locations failed: %v", dbErr)
		return true, nil, &models.Err{E: dbErr, C: native.RespCode_DB_ERROR}
	}

	// ✅ 使用公共方法构建已存在的完整提供者映射（支持分片）
	completeProviderMap := storage.BuildCompleteProviderMap(existingLocations)

	// 计算需要同步到哪些新的存储提供者
	var needSyncProviders []uint32
	for _, pid := range providerIds {
		if !completeProviderMap[pid] {
			needSyncProviders = append(needSyncProviders, pid)
		}
	}

	// 如果需要同步，创建同步任务
	if len(needSyncProviders) > 0 {
		llog.L().Infof("file exists but needs sync to %d new providers, hash: %s", len(needSyncProviders), fileHash)
		createSyncTask(fileName, fileSize, parentID, fileHash, providerIds)
	}

	// 处理同名文件
	if sameNameFile != nil {
		if *sameNameFile.Hash == fileHash {
			// 同名且同 hash，直接返回秒传成功
			return true, existingFile, nil
		}
		// 同名但 hash 不同，检查覆盖标志
		if !overwrite {
			llog.L().Infof("same name file exists, overwrite=false: %s", *fileName)
			return true, nil, &models.Err{
				E: fmt.Errorf("file with same name already exists: %s", *fileName),
				C: native.RespCode_ALREADY_EXISTS,
			}
		}
		// 需要覆盖，更新文件记录
		f := &native.FileORM{
			Hash: &fileHash,
			Size: fileSize,
		}
		if updateErr := db.UpdateFileWithHistory(ctx, f, sameNameFile); updateErr != nil {
			llog.L().Errorf("update file with history failed: %v", updateErr)
			return true, nil, &models.Err{E: updateErr, C: native.RespCode_DB_ERROR}
		}
		sameNameFile.Hash = &fileHash
		sameNameFile.Size = fileSize
		return true, sameNameFile, nil
	}

	// 没有同名文件，创建新的文件记录
	newFileRecord := &native.FileORM{
		Name:     fileName,
		ParentId: parentID,
		Size:     existingFile.Size,
		Hash:     existingFile.Hash,
		Type:     existingFile.Type,
		Mime:     existingFile.Mime,
	}
	if createErr := db.CreateFile(newFileRecord); createErr != nil {
		llog.L().Errorf("create file record failed: %v", createErr)
		return true, nil, &models.Err{E: createErr, C: native.RespCode_DB_ERROR}
	}
	// 创建文件标签
	if newFileRecord.Id != nil {
		parseAndCreateTags(*newFileRecord.Id, tags)
	}
	return true, newFileRecord, nil
}

// handleOverwriteFile 处理覆盖上传或新建文件记录
// 返回创建的文件记录和错误
func handleOverwriteFile(
	ctx context.Context,
	fileRecord *native.FileORM,
	overwriteID string,
	cleanupPath string,
) (*native.FileORM, *models.Err) {
	if overwriteID == "" {
		// 新上传：创建新的文件记录
		if err := db.CreateFile(fileRecord); err != nil {
			if cleanupPath != "" {
				os.Remove(cleanupPath)
			}
			llog.L().Errorf("create file record failed: %v", err)
			return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
		}
		llog.L().Infof("new file created, id: %s, hash: %s", *fileRecord.Id, *fileRecord.Hash)
		return fileRecord, nil
	}

	// 覆盖上传
	fileRecord.Id = &overwriteID
	oldFile, err := db.GetFileByID(overwriteID)
	if err != nil && err != gorm.ErrRecordNotFound {
		if cleanupPath != "" {
			os.Remove(cleanupPath)
		}
		llog.L().Errorf("get old file record failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	if oldFile != nil {
		if err := db.UpdateFileWithHistory(ctx, fileRecord, oldFile); err != nil {
			if cleanupPath != "" {
				os.Remove(cleanupPath)
			}
			llog.L().Errorf("update file with history failed: %v", err)
			return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}
		llog.L().Infof("file updated with history, id: %s, old_hash: %s, new_hash: %s", *fileRecord.Id, *oldFile.Hash, *fileRecord.Hash)
		return fileRecord, nil
	}

	// 旧文件不存在（可能被删除了），创建新记录
	if err := db.CreateFile(fileRecord); err != nil {
		if cleanupPath != "" {
			os.Remove(cleanupPath)
		}
		llog.L().Errorf("create file record failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	llog.L().Warnf("overwrite file not found, created new record, id: %s", *fileRecord.Id)
	return fileRecord, nil
}

// checkSameNameFile 检查同名文件并返回 overwriteID
func checkSameNameFile(sameNameFile *native.FileORM, fileName string, overwrite bool) (string, *models.Err) {
	if sameNameFile == nil {
		return "", nil
	}
	if !overwrite {
		llog.L().Infof("same name file exists, overwrite=false: %s", fileName)
		return "", &models.Err{
			E: fmt.Errorf("file with same name already exists: %s", fileName),
			C: native.RespCode_ALREADY_EXISTS,
		}
	}
	return *sameNameFile.Id, nil
}

// InitUpload init upload flow
func InitUpload(ctx context.Context, req *native.InitUploadReq) (*native.InitUploadRespData, *models.Err) {
	if *req.ParentId == "" {
		return nil, &models.Err{
			E: errors.New("cannot upload file to root directory, please create a directory first"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	blake3Hash := *req.Hash

	// 检查是否有未完成的上传任务（断点续传）
	record, chunks, err := db.GetUploadRecordByHash(blake3Hash, true)
	if err != nil && err != gorm.ErrRecordNotFound {
		llog.L().Errorf("get uncomplete uploads failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}

	// 如果存在未完成的上传任务
	if record != nil {
		// 判断是否是同一个上传记录
		if *record.ParentId != *req.ParentId || *record.FileName != *req.Name {
			llog.L().Errorf("A same hash file is uploading, but file name or parent id not match,uploading file path: %d", *record.Id)
			return nil, &models.Err{E: errors.New("A same hash file is uploading, but file name or parent id not match"), C: native.RespCode_INTERNAL_ERROR}
		}

		// 根据 task_type 区分任务类型
		if record.TaskType != nil && *record.TaskType == int32(native.UploadTaskType_SYNC) {
			// 同步任务：文件已存在，直接返回 exists=true
			llog.L().Infof("found sync task for hash: %s, file already exists", blake3Hash)
			exists := true
			return &native.InitUploadRespData{Exists: &exists}, nil
		}

		// 普通上传任务：返回 upload_id 和已上传分片（位图）
		llog.L().Infof("resume upload: %d, chunk_total: %d, chunk_size: %d",
			*record.Id, *record.ChunkTotal, *record.ChunkSize)
		uploadID := *record.Id
		chunkTotal := *record.ChunkTotal
		chunkSize := *record.ChunkSize

		uploadedChunksBitmap := make([]byte, (chunkTotal+7)/8)
		if chunks != nil {
			uploadedChunksBitmap = buildUploadedBitmap(chunks, chunkTotal)
		}

		return &native.InitUploadRespData{
			UploadId:       &uploadID,
			UploadedChunks: uploadedChunksBitmap,
			ChunkTotal:     &chunkTotal,
			ChunkSize:      &chunkSize,
		}, nil
	}

	// 验证父文件夹并获取存储提供者列表
	parentFile, providerIds, mErr := validateAndGetParent(*req.ParentId)
	if mErr != nil {
		return nil, mErr
	}

	// 检查文件是否已存在
	existingFile, sameNameFile, mErr := checkFileExists(*req.ParentId, *req.Name, blake3Hash)
	if mErr != nil {
		return nil, mErr
	}

	// 处理秒传逻辑
	handled, _, mErr := handleInstantUpload(
		ctx, blake3Hash, req.Name, req.Size, req.ParentId,
		providerIds, existingFile, sameNameFile, req.GetOverwrite(),
		req.Tags,
	)
	if mErr != nil {
		return nil, mErr
	}
	if handled {
		exists := true
		return &native.InitUploadRespData{Exists: &exists}, nil
	}

	// 文件不存在，创建新的上传任务
	overwriteID, mErr := checkSameNameFile(sameNameFile, *req.Name, req.GetOverwrite())
	if mErr != nil {
		return nil, mErr
	}

	// 计算分片大小和总数
	chunkSize := uint64(DefaultChunkSize)
	chunkTotal := uint32((*req.Size + chunkSize - 1) / chunkSize)
	if chunkTotal == 0 {
		chunkTotal = 1
	}

	// 创建临时目录
	if err := utils.CreateFileTempDir(blake3Hash, true); err != nil {
		llog.L().Errorf("create temp dir failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}

	tp := utils.GetFileTempPath(blake3Hash, *req.Name)
	ur := &native.UploadRecordORM{
		FileHash:          &blake3Hash,
		FileName:          req.Name,
		FileSize:          req.Size,
		ParentId:          req.ParentId,
		ChunkTotal:        &chunkTotal,
		ChunkSize:         &chunkSize,
		TempPath:          &tp,
		OverwriteId:       &overwriteID,
		Status:            utils.EnumPtr(native.LocationStatus_PPENDING),
		TargetProviderIds: parentFile.BoundProviderIds,
		TaskType:          utils.EnumPtr(native.UploadTaskType_UPLOAD),
		Tags:              req.Tags,
	}

	if err := db.GetDB().Create(ur).Error; err != nil {
		llog.L().Errorf("create upload record failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	llog.L().Infof("init upload: %d, chunk_total: %d, chunk_size: %d",
		*ur.Id, chunkTotal, chunkSize)

	return &native.InitUploadRespData{
		UploadId:       ur.Id,
		UploadedChunks: make([]byte, (chunkTotal+7)/8),
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

	// 并发控制：防止同时上传同一个分片
	key := chunkUploadKey{
		uploadID:   uint32(uploadID),
		chunkIndex: chunkIndex,
	}
	if !acquireChunkLock(key) {
		llog.L().Warnf("chunk upload already in progress: upload_id=%d, chunk_index=%d", uploadID, chunkIndex)
		return &models.Err{
			E: errors.New("chunk upload already in progress"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}
	defer releaseChunkLock(key)

	// 获取上传记录
	uploadRecord, _, err := db.GetUploadRecordByID(uploadID, false)
	if err != nil {
		llog.L().Errorf("get upload record failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 检查分片记录是否已存在（幂等性）
	existingChunk, err := db.GetChunkRecord(uploadID, chunkIndex)
	if err == nil && existingChunk != nil {
		llog.L().Infof("chunk record already exists, skip: upload_id=%d, chunk_index=%d", uploadID, chunkIndex)
		return nil
	}

	chunkPath := utils.GetChunkTempPath(*uploadRecord.FileHash, int(chunkIndex))

	// 写入分片文件
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

	llog.L().Infof("chunk uploaded successfully: upload_id=%d, chunk_index=%d, size=%d", uploadID, chunkIndex, totalSize)
	return nil
}

// CompleteUpload 完成上传
func CompleteUpload(ctx context.Context, req *native.CompleteUploadReq) *models.Err {
	uploadID := *req.UploadId

	llog.L().Infof("complete upload: %d", uploadID)

	uploadRecord, chunks, err := db.GetUploadRecordByID(uploadID, true)
	if err != nil {
		llog.L().Errorf("get upload record failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

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

	// ✅ 异步处理：立即返回，后台合并文件
	go processCompleteUpload(context.Background(), uploadRecord, chunks)

	return nil
}

// processCompleteUpload 后台处理文件合并和上传
func processCompleteUpload(ctx context.Context, uploadRecord *native.UploadRecordORM, chunks []*native.UploadChunkRecordORM) {
	llog.L().Infof("start processing complete upload: %d", *uploadRecord.Id)

	// 1. 合并所有分片
	outputFile, err := os.Create(*uploadRecord.TempPath)
	if err != nil {
		llog.L().Errorf("create output file failed: %v", err)
		return
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
			return
		}

		n, err := io.Copy(io.MultiWriter(outputFile, hasher), chunkFile)
		chunkFile.Close()
		if err != nil {
			llog.L().Errorf("copy chunk to output failed: %v", err)
			return
		}
		totalSize += n
	}

	finalHash := hex.EncodeToString(hasher.Sum(nil))

	if finalHash != *uploadRecord.FileHash {
		llog.L().Errorf("file hash mismatch: expected %s, got %s", *uploadRecord.FileHash, finalHash)
		return
	}

	llog.L().Infof("file merged successfully: %d bytes", totalSize)

	// 2. 获取文件类型
	filetype, mime := getFileType(utils.GetChunkTempPath(*uploadRecord.FileHash, 0), *uploadRecord.FileName)

	// 3. 创建文件记录
	fileRecord := &native.FileORM{
		Name:     uploadRecord.FileName,
		ParentId: uploadRecord.ParentId,
		Size:     func() *uint64 { v := uint64(totalSize); return &v }(),
		Hash:     &finalHash,
		Type:     &filetype,
		Mime:     &mime,
		IsReady:  utils.BoolPtr(false), // 文件创建时标记为未就绪
	}

	// 处理文件记录（新建或覆盖）
	overwriteID := ""
	if uploadRecord.OverwriteId != nil {
		overwriteID = *uploadRecord.OverwriteId
	}
	createdFile, mErr := handleOverwriteFile(ctx, fileRecord, overwriteID, "")
	if mErr != nil {
		llog.L().Errorf("handle file record failed: %v", mErr)
		return
	}

	// 创建文件标签
	if createdFile.Id != nil {
		parseAndCreateTags(*createdFile.Id, uploadRecord.Tags)
	}

	llog.L().Infof("file record created: %s", *createdFile.Id)

	// 4. 移除分片临时文件
	if err := os.RemoveAll(utils.GetChunkTempDir(*uploadRecord.FileHash)); err != nil {
		llog.L().Warnf("remove temp path failed: %v", err)
	}

	// 5. 上传到存储（异步，在独立协程中执行）
	// 注意：必须在独立协程中执行，否则 outputFile 的 defer Close() 无法及时释放文件句柄
	go storage.Dispatch(context.Background(), uploadRecord)
}

// CancelUpload 取消上传
func CancelUpload(ctx context.Context, req *native.CancelUploadReq) *models.Err {
	uploadID := *req.UploadId

	llog.L().Infof("cancel upload: %d", uploadID)

	uploadRecord, _, err := db.GetUploadRecordByID(uploadID, true)
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
	if err := db.ClearUploadRecords(uploadID); err != nil {
		llog.L().Errorf("delete upload record failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	return nil
}

// UploadSmall 小文件上传（< 10MB）
func UploadSmall(ctx context.Context, req *native.UploadSmallReq) (*native.UploadSmallRespData, *models.Err) {
	// 验证文件大小（必须小于 10MB）
	if *req.Size > SmallFileThreshold {
		llog.L().Errorf("file too large for small file upload: %d > %d", *req.Size, SmallFileThreshold)
		return nil, &models.Err{
			E: fmt.Errorf("file too large for small file upload (max %dMB), use chunk upload instead", SmallFileThreshold/1024/1024),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	blake3Hash := *req.Hash
	llog.L().Infof("small file upload started: name=%s, size=%d, hash=%s", *req.Name, *req.Size, blake3Hash)

	// 验证父文件夹并获取存储提供者列表
	parentFile, providerIds, mErr := validateAndGetParent(*req.ParentId)
	if mErr != nil {
		return nil, mErr
	}

	// 检查文件是否已存在
	existingFile, sameNameFile, mErr := checkFileExists(*req.ParentId, *req.Name, blake3Hash)
	if mErr != nil {
		return nil, mErr
	}

	// 处理秒传逻辑
	handled, fileRecord, mErr := handleInstantUpload(
		ctx, blake3Hash, req.Name, req.Size, req.ParentId,
		providerIds, existingFile, sameNameFile, req.GetOverwrite(),
		req.Tags,
	)
	if mErr != nil {
		return nil, mErr
	}
	if handled {
		exists := true
		return &native.UploadSmallRespData{
			FileId: fileRecord.Id,
			Name:   fileRecord.Name,
			Size:   fileRecord.Size,
			Hash:   fileRecord.Hash,
			Exists: &exists,
		}, nil
	}

	// 文件不存在，需要实际上传
	// 验证文件内容 hash
	hasher := blake3.New(32, nil)
	hasher.Write(req.Content)
	actualHash := hex.EncodeToString(hasher.Sum(nil))

	if actualHash != blake3Hash {
		llog.L().Errorf("file hash mismatch: expected %s, got %s", blake3Hash, actualHash)
		return nil, &models.Err{
			E: errors.New("file hash mismatch"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 验证文件大小
	if uint64(len(req.Content)) != *req.Size {
		llog.L().Errorf("file size mismatch: expected %d, got %d", *req.Size, len(req.Content))
		return nil, &models.Err{
			E: fmt.Errorf("file size mismatch: expected %d, got %d", *req.Size, len(req.Content)),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 处理同名文件
	overwriteID, mErr := checkSameNameFile(sameNameFile, *req.Name, req.GetOverwrite())
	if mErr != nil {
		return nil, mErr
	}

	// 创建临时目录和文件
	if err := utils.CreateFileTempDir(blake3Hash, true); err != nil {
		llog.L().Errorf("create temp dir failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}

	tempPath := utils.GetFileTempPath(blake3Hash, *req.Name)
	if err := os.WriteFile(tempPath, req.Content, 0644); err != nil {
		llog.L().Errorf("write temp file failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}

	// 获取文件类型
	filetype, mime := getFileType(tempPath, *req.Name)

	// 创建文件记录
	newFileRecord := &native.FileORM{
		Name:     req.Name,
		ParentId: req.ParentId,
		Size:     req.Size,
		Hash:     &blake3Hash,
		Type:     &filetype,
		Mime:     &mime,
		IsReady:  utils.BoolPtr(false), // 文件创建时标记为未就绪
	}

	// 处理文件记录（新建或覆盖）
	newFileRecord, mErr = handleOverwriteFile(ctx, newFileRecord, overwriteID, tempPath)
	if mErr != nil {
		return nil, mErr
	}

	// 创建文件标签
	if newFileRecord.Id != nil {
		parseAndCreateTags(*newFileRecord.Id, req.Tags)
	}

	// 创建上传记录并触发后台存储上传
	uploadRecord := &native.UploadRecordORM{
		FileHash:          &blake3Hash,
		FileName:          req.Name,
		FileSize:          req.Size,
		ParentId:          req.ParentId,
		TempPath:          &tempPath,
		OverwriteId:       &overwriteID,
		Status:            utils.EnumPtr(native.LocationStatus_PPENDING),
		TargetProviderIds: parentFile.BoundProviderIds,
		TaskType:          utils.EnumPtr(native.UploadTaskType_UPLOAD),
	}

	if err := db.GetDB().Create(uploadRecord).Error; err != nil {
		llog.L().Errorf("create upload record failed: %v", err)
	} else {
		go storage.Dispatch(context.Background(), uploadRecord)
	}

	exists := false
	return &native.UploadSmallRespData{
		FileId: newFileRecord.Id,
		Name:   newFileRecord.Name,
		Size:   newFileRecord.Size,
		Hash:   newFileRecord.Hash,
		Exists: &exists,
	}, nil
}

// ListUploadRecords 获取上传记录列表
func ListUploadRecords(ctx context.Context, req *native.ListUploadRecordsReq) (*native.ListUploadRecordsResp, *models.Err) {
	// 默认参数
	page := int32(1)
	if req.Page != nil {
		page = int32(*req.Page)
	}
	pageSize := int32(20)
	if req.PageSize != nil {
		pageSize = int32(*req.PageSize)
	}

	// 查询数据库
	records, total, err := db.ListUploadRecords(page, pageSize, (*int32)(req.Status))
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 转换为 Proto
	var uploadRecords []*native.UploadRecordInfo
	for _, record := range records {
		var createdAt *timestamppb.Timestamp
		if record.CreatedAt != nil {
			createdAt = timestamppb.New(*record.CreatedAt)
		}
		var updatedAt *timestamppb.Timestamp
		if record.UpdatedAt != nil {
			updatedAt = timestamppb.New(*record.UpdatedAt)
		}

		info := &native.UploadRecordInfo{
			Id:                record.Id,
			FileName:          record.FileName,
			FileSize:          record.FileSize,
			FileHash:          record.FileHash,
			ParentId:          record.ParentId,
			Status:            (*native.LocationStatus)(record.Status),
			TargetProviderIds: record.TargetProviderIds,
			TaskType:          (*native.UploadTaskType)(record.TaskType),
			CreatedAt:         createdAt,
			UpdatedAt:         updatedAt,
			Message:           record.Message,
		}
		uploadRecords = append(uploadRecords, info)
	}

	totalUint32 := uint32(total)
	return &native.ListUploadRecordsResp{
		Records: uploadRecords,
		Total:   &totalUint32,
	}, nil
}

// GetUploadRecordDetail 获取上传记录详情
func GetUploadRecordDetail(ctx context.Context, req *native.GetUploadRecordDetailReq) (*native.GetUploadRecordDetailResp, *models.Err) {
	// 查询上传记录
	record, _, err := db.GetUploadRecordByID(*req.Id, false)
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 查询文件位置记录
	locations, err := db.GetFileLocationsByFileHash(*record.FileHash)
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 查询存储提供者名称
	providerIDs := []uint32{}
	for _, loc := range locations {
		providerIDs = append(providerIDs, *loc.ProviderId)
	}

	providers, err := db.GetStorageProvidersByIDs(providerIDs)
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	providerMap := make(map[uint32]string)
	for _, p := range providers {
		providerMap[*p.Id] = *p.Name
	}

	// 转换为 Proto
	var locationInfos []*native.FileLocationInfo
	for _, loc := range locations {
		providerName := providerMap[*loc.ProviderId]

		var expiredAt *timestamppb.Timestamp
		if loc.ExpiredAt != nil {
			expiredAt = timestamppb.New(*loc.ExpiredAt)
		}
		var createdAt *timestamppb.Timestamp
		if loc.CreatedAt != nil {
			createdAt = timestamppb.New(*loc.CreatedAt)
		}
		var updatedAt *timestamppb.Timestamp
		if loc.UpdatedAt != nil {
			updatedAt = timestamppb.New(*loc.UpdatedAt)
		}

		info := &native.FileLocationInfo{
			Id:             loc.Id,
			ProviderId:     loc.ProviderId,
			ProviderName:   &providerName,
			Status:         (*native.LocationStatus)(loc.Status),
			Message:        loc.Message,
			Progress:       loc.Progress,
			Encrypted:      loc.Encrypted,
			Size:           loc.Size,
			ProviderFileId: loc.ProviderFileId,
			CacheStatus:    (*native.CacheStatus)(loc.CacheStatus),
			ExpiredAt:      expiredAt,
			CreatedAt:      createdAt,
			UpdatedAt:      updatedAt,
		}
		locationInfos = append(locationInfos, info)
	}

	// 构建上传记录信息
	var recordCreatedAt *timestamppb.Timestamp
	if record.CreatedAt != nil {
		recordCreatedAt = timestamppb.New(*record.CreatedAt)
	}
	var recordUpdatedAt *timestamppb.Timestamp
	if record.UpdatedAt != nil {
		recordUpdatedAt = timestamppb.New(*record.UpdatedAt)
	}

	recordInfo := &native.UploadRecordInfo{
		Id:                record.Id,
		FileName:          record.FileName,
		FileSize:          record.FileSize,
		FileHash:          record.FileHash,
		ParentId:          record.ParentId,
		Status:            (*native.LocationStatus)(record.Status),
		TargetProviderIds: record.TargetProviderIds,
		TaskType:          (*native.UploadTaskType)(record.TaskType),
		CreatedAt:         recordCreatedAt,
		UpdatedAt:         recordUpdatedAt,
		Message:           record.Message,
	}

	return &native.GetUploadRecordDetailResp{
		Record:    recordInfo,
		Locations: locationInfos,
	}, nil
}

// DeleteUploadRecord 删除上传记录
func DeleteUploadRecord(ctx context.Context, req *native.DeleteUploadRecordReq) *models.Err {
	// 查询上传记录
	record, _, err := db.GetUploadRecordByID(*req.Id, false)
	if err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 删除失败的 FileLocation 记录
	if record.FileHash != nil && *record.FileHash != "" {
		// 查询该文件的所有位置记录
		locations, err := db.GetFileLocationsByFileHash(*record.FileHash)
		if err != nil {
			return &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}

		// 删除失败的记录
		for _, loc := range locations {
			if loc.Status != nil && *loc.Status == int32(native.LocationStatus_PFAILED) {
				if err := db.DeleteFileLocation(*loc.Id); err != nil {
					llog.L().Errorf("delete file location failed: %v", err)
					// 继续删除其他记录
				}
			}
		}
	}

	// 删除上传记录
	if err := db.ClearUploadRecords(*req.Id); err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	return nil
}

// RetryUploadRecord 重试上传记录
func RetryUploadRecord(ctx context.Context, req *native.RetryUploadRecordReq) *models.Err {
	// 查询上传记录
	record, _, err := db.GetUploadRecordByID(*req.Id, false)
	if err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 重置状态为等待中
	pendingStatus := int32(native.LocationStatus_PPENDING)
	record.Status = &pendingStatus

	// 更新数据库
	if err := db.UpdateUploadRecord(record, []string{"status"}); err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 重新分发上传任务（使用独立的 context，避免 HTTP context 在请求结束后被取消）
	go storage.Dispatch(context.Background(), record)

	return nil
}
