package service

import (
	"context"
	"fmt"
	"io"
	"os"

	"jenken827.com/jenken827/pfm/driver/telegram"
	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/internal/thumbnail"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// FileStreamResult 文件流式传输结果
type FileStreamResult struct {
	Reader        types.ReadSeekCloser
	FileName      string
	ContentType   string
	FileSize      int64
	IsPreDownload bool   // 是否为 Telegram 大文件预下载状态
	PreDownloadData map[string]any // 预下载状态数据
}

// GetFileStream 获取文件流（用于预览/下载）
// fileID: 文件ID
// providerID: 指定存储提供者ID（可选，为空则自动选择）
func GetFileStream(ctx context.Context, fileID string, providerID *uint32) (*FileStreamResult, error) {
	// 1. 获取文件信息
	file, err := db.GetFileByID(fileID)
	if err != nil {
		return nil, fmt.Errorf("file not found: %w", err)
	}

	if file.Hash == nil || *file.Hash == "" {
		return nil, fmt.Errorf("file hash is empty")
	}

	// 2. 查询文件位置
	locations, err := db.GetFileLocationsByBlake3Hash(*file.Hash)
	if err != nil {
		return nil, fmt.Errorf("file location not found: %w", err)
	}

	if len(locations) == 0 {
		return nil, fmt.Errorf("no file location found")
	}

	// 3. 选择源提供者
	var sourceProviderID uint32
	var sourceLocations []*native.FileLocationORM
	if providerID != nil {
		sourceProviderID = *providerID
		sourceLocations, err = storage.GetCompleteProviderLocations(locations, sourceProviderID)
		if err != nil {
			return nil, fmt.Errorf("provider %d location not found: %w", sourceProviderID, err)
		}
	} else {
		sourceProviderID, sourceLocations, err = storage.SelectBestSourceProvider(locations)
		if err != nil {
			return nil, fmt.Errorf("no available provider: %w", err)
		}
	}

	llog.L().Infof("stream file: %s, provider: %d, chunks: %d", *file.Name, sourceProviderID, len(sourceLocations))

	// 4. 获取存储实例
	sourceStorage := storage.GetStorage(sourceProviderID)
	if sourceStorage == nil {
		return nil, fmt.Errorf("storage not found for provider %d", sourceProviderID)
	}

	// 5. Telegram 大文件预下载检查
	if preDownloadResult := checkTelegramPreDownload(sourceStorage, sourceLocations, fileID, sourceProviderID); preDownloadResult != nil {
		return &FileStreamResult{
			IsPreDownload:   true,
			PreDownloadData: preDownloadResult,
		}, nil
	}

	// 6. 创建加密器（用于解密，如果需要）
	encryptor, err := createEncryptorIfNeeded(sourceStorage, sourceLocations)
	if err != nil {
		return nil, fmt.Errorf("create encryptor failed: %w", err)
	}

	// 7. 打开文件（支持多分片合并、Seek）
	reader, err := sourceStorage.Open(ctx, sourceLocations, encryptor)
	if err != nil {
		return nil, fmt.Errorf("open file failed: %w", err)
	}

	// 8. 准备响应数据
	fileName := "download"
	if file.Name != nil {
		fileName = *file.Name
	}

	contentType := "application/octet-stream"
	if file.Mime != nil && *file.Mime != "" {
		contentType = *file.Mime
	}

	fileSize := int64(0)
	if file.Size != nil {
		fileSize = int64(*file.Size)
	}

	return &FileStreamResult{
		Reader:      reader,
		FileName:    fileName,
		ContentType: contentType,
		FileSize:    fileSize,
	}, nil
}

// GetFileHistoryStream 获取文件历史版本流
func GetFileHistoryStream(ctx context.Context, historyID uint64, isPreview bool) (*FileStreamResult, error) {
	// 1. 获取历史记录
	history, err := db.GetFileHistoryByID(historyID)
	if err != nil {
		return nil, fmt.Errorf("history not found: %w", err)
	}

	if history.Hash == nil || *history.Hash == "" {
		return nil, fmt.Errorf("history hash is empty")
	}

	// 2. 获取关联的文件信息
	var fileName string
	var mimeType string
	if history.FileId != nil {
		file, err := db.GetFileByID(*history.FileId)
		if err == nil && file.Name != nil {
			fileName = *file.Name
		}
		if err == nil && file.Mime != nil {
			mimeType = *file.Mime
		}
	}
	if fileName == "" {
		fileName = "history_file"
	}

	// 下载时使用通用 MIME 类型，预览时使用原始 MIME 类型
	if !isPreview || mimeType == "" {
		mimeType = "application/octet-stream"
	}

	// 3. 查询文件位置
	locations, err := db.GetFileLocationsByBlake3Hash(*history.Hash)
	if err != nil {
		return nil, fmt.Errorf("file location not found: %w", err)
	}

	if len(locations) == 0 {
		return nil, fmt.Errorf("no file location found")
	}

	// 4. 选择源提供者
	sourceProviderID, sourceLocations, err := storage.SelectBestSourceProvider(locations)
	if err != nil {
		return nil, fmt.Errorf("no available provider: %w", err)
	}

	// 5. 获取存储实例
	sourceStorage := storage.GetStorage(sourceProviderID)
	if sourceStorage == nil {
		return nil, fmt.Errorf("storage not found")
	}

	// 6. 处理加密
	encryptor, err := createEncryptorIfNeeded(sourceStorage, sourceLocations)
	if err != nil {
		return nil, fmt.Errorf("create encryptor failed: %w", err)
	}

	// 7. 打开文件
	reader, err := sourceStorage.Open(ctx, sourceLocations, encryptor)
	if err != nil {
		return nil, fmt.Errorf("open file failed: %w", err)
	}

	// 8. 准备响应数据
	fileSize := int64(0)
	if history.Size != nil {
		fileSize = int64(*history.Size)
	}

	return &FileStreamResult{
		Reader:      reader,
		FileName:    fileName,
		ContentType: mimeType,
		FileSize:    fileSize,
	}, nil
}

// GetThumbnailPath 获取缩略图路径
// 返回缩略图路径和是否需要异步生成
func GetThumbnailPath(fileID string) (thumbnailPath string, needGenerate bool, file *native.FileORM) {
	// 1. 获取文件信息
	file, err := db.GetFileByID(fileID)
	if err != nil {
		return "", false, nil
	}

	// 2. 检查文件是否是图片
	if file.Name == nil || !thumbnail.IsImageFile(*file.Name) {
		return "", false, nil
	}

	// 3. 检查 hash 是否存在
	if file.Hash == nil || *file.Hash == "" {
		return "", false, nil
	}

	// 4. 根据 hash 计算缩略图路径，检查是否存在
	cacheDir := getThumbnailCacheDir()
	generator := thumbnail.NewGenerator(cacheDir)
	expectedPath := generator.GetCachePath(*file.Hash)

	if _, err := os.Stat(expectedPath); err == nil {
		// 缩略图存在
		return expectedPath, false, file
	}

	// 5. 缩略图不存在，需要生成
	return "", true, file
}

// RegenerateThumbnail 重新生成缩略图（异步调用）
func RegenerateThumbnail(ctx context.Context, file *native.FileORM) {
	if file.Hash == nil || *file.Hash == "" {
		return
	}

	// 1. 查找文件位置
	locations, err := db.GetFileLocationsByBlake3Hash(*file.Hash)
	if err != nil || len(locations) == 0 {
		llog.L().Warnf("no file locations found for thumbnail regeneration: %s", *file.Hash)
		return
	}

	// 2. 选择一个可用的存储提供者
	sourceProviderID, sourceLocations, err := storage.SelectBestSourceProvider(locations)
	if err != nil {
		llog.L().Warnf("select source provider failed for thumbnail: %v", err)
		return
	}

	// 3. 获取存储实例
	sourceStorage := storage.GetStorage(sourceProviderID)
	if sourceStorage == nil {
		return
	}

	// 4. 创建加密器（如果需要）
	encryptor, err := createEncryptorIfNeeded(sourceStorage, sourceLocations)
	if err != nil {
		llog.L().Errorf("create encryptor for thumbnail failed: %v", err)
		return
	}

	// 5. 下载文件到临时目录
	tempFile, err := os.CreateTemp("", "thumbnail-*")
	if err != nil {
		llog.L().Errorf("create temp file for thumbnail failed: %v", err)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	// 6. 打开文件
	reader, err := sourceStorage.Open(ctx, sourceLocations, encryptor)
	if err != nil {
		llog.L().Errorf("open file for thumbnail failed: %v", err)
		return
	}
	defer reader.Close()

	// 7. 复制到临时文件
	if _, err := io.Copy(tempFile, reader); err != nil {
		llog.L().Errorf("copy file for thumbnail failed: %v", err)
		return
	}
	tempFile.Close()

	// 8. 获取图片信息
	imgInfo, err := thumbnail.GetImageInfo(tempFile.Name())
	if err != nil {
		llog.L().Warnf("get image info for thumbnail failed: %v", err)
		return
	}

	// 9. 检查是否需要生成缩略图
	if !thumbnail.NeedThumbnail(imgInfo, *file.Name) {
		llog.L().Debugf("thumbnail not needed for regenerated file: %s", *file.Name)
		return
	}

	// 10. 生成缩略图
	cacheDir := getThumbnailCacheDir()
	generator := thumbnail.NewGenerator(cacheDir)
	thumbnailPath, err := generator.Generate(tempFile.Name(), *file.Hash)
	if err != nil {
		llog.L().Errorf("regenerate thumbnail failed: %v", err)
		return
	}

	llog.L().Infof("thumbnail regenerated for file: %s, path: %s", *file.Name, thumbnailPath)
}

// CheckFileIsImage 检查文件是否是图片
func CheckFileIsImage(fileID string) bool {
	file, err := db.GetFileByID(fileID)
	if err != nil {
		return false
	}
	return file.Name != nil && thumbnail.IsImageFile(*file.Name)
}

// checkTelegramPreDownload 检查 Telegram 大文件预下载状态
func checkTelegramPreDownload(sourceStorage types.StorageDriver, sourceLocations []*native.FileLocationORM, fileID string, sourceProviderID uint32) map[string]any {
	if sourceStorage.GetType() != native.StorageProviderType_TELEGRAM {
		return nil
	}

	locSize := sourceLocations[0].Size
	if locSize == nil || int64(*locSize) <= telegram.LargeFileSizeThreshold {
		return nil
	}

	// 检查第一个分片的缓存状态（整体状态）
	cacheStatus := sourceLocations[0].CacheStatus

	// 只有未缓存或缓存失败时才触发预下载（避免重复触发）
	shouldTriggerPreDownload := cacheStatus == nil ||
		*cacheStatus == int32(native.CacheStatus_CACHE_STATUS_UNSPECIFIED) ||
		*cacheStatus == int32(native.CacheStatus_UNCACHED)

	if !shouldTriggerPreDownload {
		return nil
	}

	actualStatus := native.CacheStatus_CACHE_STATUS_UNSPECIFIED
	if cacheStatus != nil {
		actualStatus = native.CacheStatus(*cacheStatus)
	}

	llog.L().Infof("telegram large file pre-download: file_id=%s, provider=%d, size=%d, cache_status=%v, triggering async download",
		fileID, sourceProviderID, *locSize, actualStatus)

	// 检查是否实现了 PreDownloader 接口
	if preDownloader, ok := sourceStorage.(types.PreDownloader); ok {
		// 异步触发预下载（不阻塞当前请求）
		go func() {
			bgCtx := context.Background()
			if err := preDownloader.PreDownload(bgCtx, sourceLocations); err != nil {
				llog.L().Errorf("async pre-download failed: file_id=%s, provider=%d, err=%v",
					fileID, sourceProviderID, err)
			} else {
				llog.L().Infof("async pre-download completed: file_id=%s, provider=%d",
					fileID, sourceProviderID)
			}
		}()
	}

	return map[string]any{
		"status":       "pre_download",
		"provider_id":  sourceProviderID,
		"cache_status": actualStatus,
		"file_id":      fileID,
		"message":      "Large file is being cached, please retry later",
	}
}

// createEncryptorIfNeeded 创建加密器（如果需要）
func createEncryptorIfNeeded(sourceStorage types.StorageDriver, sourceLocations []*native.FileLocationORM) (types.Encryptor, error) {
	if len(sourceLocations) == 0 {
		return &types.NullEncryptor{}, nil
	}

	if sourceLocations[0].Encrypted == nil || !*sourceLocations[0].Encrypted {
		return &types.NullEncryptor{}, nil
	}

	providerOrm := sourceStorage.GetOrm()
	if providerOrm == nil || providerOrm.EncryptionKey == nil {
		return &types.NullEncryptor{}, nil
	}

	salt := ""
	if sourceLocations[0].EncryptionSalt != nil {
		salt = *sourceLocations[0].EncryptionSalt
	}

	return storage.NewCryptEncryptor(*providerOrm.EncryptionKey, salt)
}

// getThumbnailCacheDir 获取缩略图缓存目录
func getThumbnailCacheDir() string {
	return "data/thumbnails"
}
