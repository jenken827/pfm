package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jenken827/pfm-plugin-sdk/driver/cryptostream"
	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	abi "github.com/jenken827/pfm-plugin-sdk/proto/gen/abi"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	ormconv "github.com/jenken827/pfm/driver/orm"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/storage"
	"github.com/jenken827/pfm/internal/thumbnail"
	"github.com/jenken827/pfm/pkg/llog"
	"github.com/jenken827/pfm/utils"
)

// waitForReadyProviderWait 等待首个 provider 完成的最大时长（写后立读场景兜底）
const waitForReadyProviderWait = 10 * time.Second

// waitForReadyProviderWaitInterval 轮询间隔
const waitForReadyProviderWaitInterval = 150 * time.Millisecond

// FileStreamResult 文件流式传输结果
type FileStreamResult struct {
	Reader          types.ReadSeekCloser
	FileName        string
	ContentType     string
	FileSize        int64
	IsPreDownload   bool           // 是否为"预下载中"状态（驱动声明 PREDOWNLOAD 能力时才会出现）
	PreDownloadData map[string]any // 预下载状态数据
}

// GetFileStream 获取文件流（用户场景：内部强制归属校验）
//
// 仅文件所有者可取流。分享场景请用 GetFileStreamForShare（访问者不是 owner）。
// fileID: 文件ID；providerID: 指定存储提供者ID（可选，为空则自动选择）
func GetFileStream(ctx context.Context, fileID string, providerID *uint32) (*FileStreamResult, error) {
	if !CheckOwnership(ctx, fileID) {
		return nil, fmt.Errorf("permission denied: not owner of file")
	}
	return getFileStream(ctx, fileID, providerID)
}

// GetFileStreamForShare 获取文件流（分享场景：跳过归属校验）
//
// 调用方必须已完成分享有效性校验（分享码有效、未过期、未超访问次数），
// 见 api/share_link.go 的 PublicDownload/RawFilePreview 流程。
func GetFileStreamForShare(ctx context.Context, fileID string, providerID *uint32) (*FileStreamResult, error) {
	return getFileStream(ctx, fileID, providerID)
}

// getFileStream 取流的内部实现（不含归属校验，由调用方保证已授权）
func getFileStream(ctx context.Context, fileID string, providerID *uint32) (*FileStreamResult, error) {
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
	var sourceLocations []*entity.FileLocationORM
	if providerID != nil {
		sourceProviderID = *providerID
		sourceLocations, err = storage.GetCompleteProviderLocations(locations, sourceProviderID)
		if err != nil {
			return nil, fmt.Errorf("provider %d location not found: %w", sourceProviderID, err)
		}
	} else {
		sourceProviderID, sourceLocations, err = storage.SelectBestSourceProvider(locations)
		if err != nil {
			// 无可用 provider：文件可能正处于异步分发上传窗口（如 WebDAV PUT 后立即读）。
			// 降级 1：读本地 staging 明文（TempDir/<hash>/<fileName>，上传期必在）；
			// 降级 2：短轮询等待首个 provider 完成后再选源。
			if result, ok := tryServeFromLocalTemp(file); ok {
				return result, nil
			}
			if waitErr := waitForReadyProvider(ctx, *file.Hash); waitErr != nil {
				return nil, fmt.Errorf("no available provider: %w", err)
			}
			// 等待期间 location 状态已变化，重新拉取后选源
			locations, err = db.GetFileLocationsByBlake3Hash(*file.Hash)
			if err != nil {
				return nil, fmt.Errorf("file location not found: %w", err)
			}
			sourceProviderID, sourceLocations, err = storage.SelectBestSourceProvider(locations)
			if err != nil {
				return nil, fmt.Errorf("no available provider: %w", err)
			}
		}
	}

	llog.C(ctx).Infof("stream file: %s, provider: %d, chunks: %d", *file.Name, sourceProviderID, len(sourceLocations))

	// 4. 获取存储实例
	sourceStorage := storage.GetStorage(sourceProviderID)
	if sourceStorage == nil {
		return nil, fmt.Errorf("storage not found for provider %d", sourceProviderID)
	}

	// 5. 大文件预下载检查（驱动声明 PREDOWNLOAD 能力时；使用 file_location.Size）
	var fileSizePtr *int64
	if len(sourceLocations) > 0 && sourceLocations[0].Size != nil {
		s := int64(*sourceLocations[0].Size)
		fileSizePtr = &s
	}
	if preDownloadResult := checkPreDownloadState(sourceStorage, sourceLocations, fileID, sourceProviderID, fileSizePtr); preDownloadResult != nil {
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
	reader, err := sourceStorage.Open(ctx, ormconv.FileRefsFromLocations(sourceLocations), types.OpenOptions{Encryptor: encryptor})
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

// tryServeFromLocalTemp 上传窗口期从本地 staging 明文直读（无 complete provider 时的降级源）
// 合并后的明文位于 owner 隔离目录：
//
//	TempDir/users/<ownerID>/<hash>/chunks/merged_<fileName>
//
// 兼容旧布局：早期版本写在内容寻址层 TempDir/<hash>/<fileName>，这里做回退查找。
// 与远程 provider 解密后的内容字节等价，可直接返回；都找不到时返回 false 走轮询兜底。
func tryServeFromLocalTemp(file *entity.FileORM) (*FileStreamResult, bool) {
	if file.Hash == nil || *file.Hash == "" || file.Name == nil || *file.Name == "" {
		return nil, false
	}

	// 候选路径：owner 隔离目录（当前布局） → 内容寻址层（旧布局兼容）
	var candidates []string
	if ownerID := strPtrVal(file.OwnerId); ownerID != "" {
		candidates = append(candidates,
			filepath.Join(utils.GetChunkTempDir(*file.Hash, ownerID), utils.MergedFileName(*file.Name)))
	}
	candidates = append(candidates, utils.GetFileTempPath(*file.Hash, *file.Name))

	var (
		f        *os.File
		tempPath string
		openErr  error
	)
	for _, p := range candidates {
		f, openErr = os.Open(p)
		if openErr == nil {
			tempPath = p
			break
		}
	}
	if f == nil {
		return nil, false
	}
	contentType := "application/octet-stream"
	if file.Mime != nil && *file.Mime != "" {
		contentType = *file.Mime
	}
	fileSize := int64(0)
	if file.Size != nil {
		fileSize = int64(*file.Size)
	}
	llog.L().Infof("stream file from local staging (upload in progress): %s, path: %s", *file.Name, tempPath)
	return &FileStreamResult{
		Reader:      f,
		FileName:    *file.Name,
		ContentType: contentType,
		FileSize:    fileSize,
	}, true
}

// waitForReadyProvider 轮询等待文件出现任一 complete provider（写后立读兜底）
func waitForReadyProvider(ctx context.Context, fileHash string) error {
	deadline := time.Now().Add(waitForReadyProviderWait)
	for {
		locations, err := db.GetFileLocationsByBlake3Hash(fileHash)
		if err == nil && len(locations) > 0 {
			if providers, perr := storage.GetCompleteProviders(locations); perr == nil && len(providers) > 0 {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("file %s has no complete provider within %v", fileHash, waitForReadyProviderWait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitForReadyProviderWaitInterval):
		}
	}
}

// ErrTicketFileMismatch 票据绑定的文件与历史版本的宿主文件不一致。
// 这是鉴权层面的拒绝（HTTP 层映射 403），不是服务器故障——别让它变成 500。
var ErrTicketFileMismatch = errors.New("ticket does not match history file")

// GetFileHistoryStream 获取文件历史版本流
//
// expectFileID 非空时来自单文件票据（claims.PreviewFileID）：历史版本必须属于该文件，
// 否则一张 A 文件的下载票可以读该用户全部文件的历史版本，票据就退化成账户级读权限。
func GetFileHistoryStream(ctx context.Context, historyID uint64, isPreview bool, expectFileID string) (*FileStreamResult, error) {
	// 1. 获取历史记录
	history, err := db.GetFileHistoryByID(historyID)
	if err != nil {
		return nil, fmt.Errorf("history not found: %w", err)
	}

	// 归属校验：历史版本归属其宿主文件，仅文件所有者可取流（管理员不可越权）
	if history.FileId == nil || !CheckOwnership(ctx, *history.FileId) {
		return nil, fmt.Errorf("permission denied: not owner of file history")
	}

	// 票据的宿主校验：票据绑定的文件必须就是这个历史版本的宿主文件
	if expectFileID != "" && *history.FileId != expectFileID {
		return nil, ErrTicketFileMismatch
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
	reader, err := sourceStorage.Open(ctx, ormconv.FileRefsFromLocations(sourceLocations), types.OpenOptions{Encryptor: encryptor})
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
// size: 缩略图尺寸（200 或 400）
// 返回缩略图路径和是否需要异步生成
func GetThumbnailPath(fileID string, size thumbnail.ThumbnailSize) (thumbnailPath string, needGenerate bool, file *entity.FileORM) {
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
	expectedPath := generator.GetCachePath(*file.Hash, size)

	if _, err := os.Stat(expectedPath); err == nil {
		// 缩略图存在
		return expectedPath, false, file
	}

	// 5. 缩略图不存在，需要生成
	return "", true, file
}

// RegenerateThumbnail 重新生成缩略图（异步调用）
func RegenerateThumbnail(ctx context.Context, file *entity.FileORM) {
	if file.Hash == nil || *file.Hash == "" {
		return
	}

	// 1. 查找文件位置
	locations, err := db.GetFileLocationsByBlake3Hash(*file.Hash)
	if err != nil || len(locations) == 0 {
		llog.C(ctx).Warnf("no file locations found for thumbnail regeneration: %s", *file.Hash)
		return
	}

	// 2. 选择一个可用的存储提供者
	sourceProviderID, sourceLocations, err := storage.SelectBestSourceProvider(locations)
	if err != nil {
		llog.C(ctx).Warnf("select source provider failed for thumbnail: %v", err)
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
		llog.C(ctx).Warnf("create encryptor for thumbnail failed: %v", err)
		return
	}

	// 5. 下载文件到临时目录
	tempFile, err := os.CreateTemp("", "thumbnail-*")
	if err != nil {
		llog.C(ctx).Warnf("create temp file for thumbnail failed: %v", err)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	// 6. 打开文件
	reader, err := sourceStorage.Open(ctx, ormconv.FileRefsFromLocations(sourceLocations), types.OpenOptions{Encryptor: encryptor})
	if err != nil {
		llog.C(ctx).Warnf("open file for thumbnail failed: %v", err)
		return
	}
	defer reader.Close()

	// 7. 复制到临时文件
	if _, err := io.Copy(tempFile, reader); err != nil {
		llog.C(ctx).Warnf("copy file for thumbnail failed: %v", err)
		return
	}
	tempFile.Close()

	// 8. 获取图片信息
	imgInfo, err := thumbnail.GetImageInfo(tempFile.Name())
	if err != nil {
		llog.C(ctx).Warnf("get image info for thumbnail failed: %v", err)
		return
	}

	// 9. 检查是否需要生成缩略图
	if !thumbnail.NeedThumbnail(imgInfo, *file.Name) {
		llog.C(ctx).Debugf("thumbnail not needed for regenerated file: %s", *file.Name)
		return
	}

	// 10. 生成所有尺寸的缩略图
	cacheDir := getThumbnailCacheDir()
	generator := thumbnail.NewGenerator(cacheDir)
	thumbnailPaths, err := generator.GenerateAll(tempFile.Name(), *file.Hash)
	if err != nil {
		llog.C(ctx).Warnf("regenerate thumbnail failed: %v", err)
		return
	}

	llog.C(ctx).Infof("thumbnails regenerated for file: %s, paths: %v", *file.Name, thumbnailPaths)
}

// CheckFileIsImage 检查文件是否是图片
func CheckFileIsImage(fileID string) bool {
	file, err := db.GetFileByID(fileID)
	if err != nil {
		return false
	}
	return file.Name != nil && thumbnail.IsImageFile(*file.Name)
}

// preDownloadSizeThreshold 预下载阈值：超过它的文件才走"先缓存再返回 202"的路径
//
// 这是宿主侧的策略阈值，不绑定具体提供者（各驱动的分片/缓存策略由驱动自己决定）。
const preDownloadSizeThreshold int64 = 20 * 1024 * 1024

// checkPreDownloadState 检查大文件预下载状态
// fileSize: 文件实际大小（来自 files 表），用于判断是否需要预下载。
// 不能使用 sourceLocations[0].Size，因为远端 API 可能返回不可靠的值。
func checkPreDownloadState(sourceStorage types.StorageDriver, sourceLocations []*entity.FileLocationORM, fileID string, sourceProviderID uint32, fileSize *int64) map[string]any {
	llog.L().Infof("checkPreDownloadState ENTER: file_id=%s, storage_type=%v, fileSize=%v, locs=%d", fileID, sourceStorage.GetType(), fileSize, len(sourceLocations))
	// 只有声明了 PREDOWNLOAD 能力的驱动才有预缓存状态
	if !types.HasCapability(types.GetDriverMetadata(sourceStorage.GetType()), abi.DriverCapability_DRIVER_CAPABILITY_PREDOWNLOAD) {
		return nil
	}

	if fileSize == nil || *fileSize <= preDownloadSizeThreshold {
		llog.L().Infof("checkPreDownloadState SKIP: fileSize too small or nil")
		return nil
	}

	// 检查第一个分片的缓存状态（整体状态）
	cacheStatus := sourceLocations[0].CacheStatus

	// DEBUG: log path and cache status
	pathVal := "<nil>"
	if sourceLocations[0].Path != nil {
		pathVal = *sourceLocations[0].Path
	}
	cacheVal := int32(-1)
	if cacheStatus != nil {
		cacheVal = *cacheStatus
	}
	llog.L().Infof("checkPreDownloadState: file_id=%s, path=%q, cache_status=%d, file_size=%d", fileID, pathVal, cacheVal, *fileSize)

	// 只根据 cache_status 判断是否需要预下载
	// 注意：Path 字段是存储提供者的地址，不是本地缓存路径，不能用来判断缓存状态
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

	llog.L().Infof("large file pre-download: file_id=%s, provider=%d, file_size=%d, cache_status=%v, triggering async download",
		fileID, sourceProviderID, *fileSize, actualStatus)

	// 检查是否实现了 PreDownloader 接口
	if preDownloader, ok := sourceStorage.(types.PreDownloader); ok {
		// 异步触发预下载（不阻塞当前请求）
		utils.Go(func() {
			bgCtx := context.Background()
			if err := preDownloader.PreDownload(bgCtx, ormconv.FileRefsFromLocations(sourceLocations)); err != nil {
				llog.L().Errorf("async pre-download failed: file_id=%s, provider=%d, err=%v",
					fileID, sourceProviderID, err)
			} else {
				llog.L().Infof("async pre-download completed: file_id=%s, provider=%d",
					fileID, sourceProviderID)
			}
		})
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
func createEncryptorIfNeeded(sourceStorage types.StorageDriver, sourceLocations []*entity.FileLocationORM) (types.Encryptor, error) {
	if len(sourceLocations) == 0 {
		return &types.NullEncryptor{}, nil
	}

	if sourceLocations[0].Encrypted == nil || !*sourceLocations[0].Encrypted {
		return &types.NullEncryptor{}, nil
	}

	pcfg := sourceStorage.GetProviderConfig()
	if pcfg == nil || pcfg.EncryptionKey == "" {
		return &types.NullEncryptor{}, nil
	}

	salt := ""
	if sourceLocations[0].EncryptionSalt != nil {
		salt = *sourceLocations[0].EncryptionSalt
	}

	return cryptostream.NewCryptEncryptor(pcfg.EncryptionKey, salt)
}

// getThumbnailCacheDir 获取缩略图缓存目录
func getThumbnailCacheDir() string {
	return "data/thumbnails"
}
