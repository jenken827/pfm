package storage

import (
	"context"

	"jenken827.com/jenken827/pfm/internal/thumbnail"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/proto/gen/entity"
)

// generateThumbnailForUpload 为上传的文件生成缩略图
func generateThumbnailForUpload(ctx context.Context, ur *entity.UploadRecordORM) {
	// 检查是否是图片文件
	if ur.FileName == nil || ur.TempPath == nil || ur.FileHash == nil {
		return
	}

	fileName := *ur.FileName
	tempPath := *ur.TempPath
	fileHash := *ur.FileHash

	// 检查是否是图片文件
	if !thumbnail.IsImageFile(fileName) {
		llog.C(ctx).Debugf("not an image file, skip thumbnail: %s", fileName)
		return
	}

	// 检查是否应该跳过（SVG等）
	if thumbnail.ShouldSkipThumbnail(fileName) {
		llog.C(ctx).Debugf("skip thumbnail for file: %s", fileName)
		return
	}

	// 获取图片信息
	imgInfo, err := thumbnail.GetImageInfo(tempPath)
	if err != nil {
		llog.C(ctx).Warnf("get image info failed, skip thumbnail: %s, err: %v", fileName, err)
		return
	}

	// 判断是否需要生成缩略图
	if !thumbnail.NeedThumbnail(imgInfo, fileName) {
		llog.C(ctx).Debugf("thumbnail not needed for file: %s, size: %dx%d", fileName, imgInfo.Width, imgInfo.Height)
		return
	}

	// 获取缩略图缓存目录
	cacheDir := getThumbnailCacheDir()
	generator := thumbnail.NewGenerator(cacheDir)

	// 生成所有尺寸的缩略图（200x200 和 400x400）
	thumbnailPaths, err := generator.GenerateAll(tempPath, fileHash)
	if err != nil {
		llog.C(ctx).Warnf("generate thumbnail failed: %s, err: %v", fileName, err)
		return
	}

	llog.C(ctx).Infof("thumbnails ready for file: %s, paths: %v", fileName, thumbnailPaths)
}

// getThumbnailCacheDir 获取缩略图缓存目录

// getThumbnailCacheDir 获取缩略图缓存目录
func getThumbnailCacheDir() string {
	// 使用 data/thumbnails 目录
	return "data/thumbnails"
}
