package thumbnail

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
	"jenken827.com/jenken827/pfm/internal/llog"
)

const (
	// ThumbnailSizeSmall 小尺寸缩略图（网格视图）
	ThumbnailSizeSmall = 200
	// ThumbnailSizeLarge 大尺寸缩略图（预览）
	ThumbnailSizeLarge = 400
	// DefaultQuality JPEG 质量
	DefaultQuality = 75
)

// ThumbnailSize 缩略图尺寸
type ThumbnailSize struct {
	Width  int
	Height int
}

// 预定义的缩略图尺寸
var ThumbnailSizes = []ThumbnailSize{
	{Width: ThumbnailSizeSmall, Height: ThumbnailSizeSmall},
	{Width: ThumbnailSizeLarge, Height: ThumbnailSizeLarge},
}

// Generator 缩略图生成器
type Generator struct {
	cacheDir string
	quality  int
}

// NewGenerator 创建缩略图生成器
func NewGenerator(cacheDir string) *Generator {
	return &Generator{
		cacheDir: cacheDir,
		quality:  DefaultQuality,
	}
}

// GetCachePath 获取缩略图缓存路径
func (g *Generator) GetCachePath(fileHash string, size ThumbnailSize) string {
	filename := fmt.Sprintf("%s_%d.jpg", fileHash, size.Width)
	return filepath.Join(g.cacheDir, filename)
}

// Generate 生成指定尺寸的缩略图
// filePath: 原图路径
// fileHash: 文件 hash
// size: 缩略图尺寸
// 返回缩略图缓存路径
func (g *Generator) Generate(filePath string, fileHash string, size ThumbnailSize) (string, error) {
	// 确保缓存目录存在
	if err := os.MkdirAll(g.cacheDir, 0755); err != nil {
		return "", fmt.Errorf("create cache dir failed: %w", err)
	}

	cachePath := g.GetCachePath(fileHash, size)

	// 检查是否已存在
	if _, err := os.Stat(cachePath); err == nil {
		llog.L().Debugf("thumbnail already exists: %s", cachePath)
		return cachePath, nil
	}

	// 打开原图
	srcFile, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open source file failed: %w", err)
	}
	defer srcFile.Close()

	// 解码图片
	srcImg, _, err := image.Decode(srcFile)
	if err != nil {
		return "", fmt.Errorf("decode image failed: %w", err)
	}

	// 生成缩略图
	thumbImg := resize(srcImg, size.Width, size.Height)

	// 编码为 JPEG
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, thumbImg, &jpeg.Options{Quality: g.quality}); err != nil {
		return "", fmt.Errorf("encode thumbnail failed: %w", err)
	}

	// 写入缓存文件
	if err := os.WriteFile(cachePath, buf.Bytes(), 0644); err != nil {
		return "", fmt.Errorf("write thumbnail failed: %w", err)
	}

	llog.L().Infof("thumbnail generated: %s", cachePath)
	return cachePath, nil
}

// GenerateAll 生成所有尺寸的缩略图
// filePath: 原图路径
// fileHash: 文件 hash
// 返回所有缩略图路径
func (g *Generator) GenerateAll(filePath string, fileHash string) (map[ThumbnailSize]string, error) {
	result := make(map[ThumbnailSize]string)

	for _, size := range ThumbnailSizes {
		path, err := g.Generate(filePath, fileHash, size)
		if err != nil {
			return result, err
		}
		result[size] = path
	}

	return result, nil
}

// resize 按比例缩放图片，保持原始宽高比
func resize(src image.Image, maxWidth, maxHeight int) image.Image {
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	// 如果图片已经小于目标尺寸，直接返回
	if srcW <= maxWidth && srcH <= maxHeight {
		return src
	}

	// 计算缩放比例，保持宽高比
	scaleX := float64(maxWidth) / float64(srcW)
	scaleY := float64(maxHeight) / float64(srcH)
	scale := scaleX
	if scaleY < scale {
		scale = scaleY
	}

	// 计算新尺寸
	newW := int(float64(srcW) * scale)
	newH := int(float64(srcH) * scale)

	// 确保最小尺寸为 1
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}

	// 创建缩放后的图片
	scaled := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.BiLinear.Scale(scaled, scaled.Bounds(), src, src.Bounds(), draw.Over, nil)

	return scaled
}

// DeleteThumbnail 删除指定尺寸的缩略图缓存
func (g *Generator) DeleteThumbnail(fileHash string, size ThumbnailSize) error {
	cachePath := g.GetCachePath(fileHash, size)
	if err := os.Remove(cachePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// DeleteAllThumbnails 删除所有尺寸的缩略图缓存
func (g *Generator) DeleteAllThumbnails(fileHash string) error {
	for _, size := range ThumbnailSizes {
		if err := g.DeleteThumbnail(fileHash, size); err != nil {
			return err
		}
	}
	return nil
}

// ThumbnailExists 检查指定尺寸的缩略图是否存在
func (g *Generator) ThumbnailExists(fileHash string, size ThumbnailSize) bool {
	cachePath := g.GetCachePath(fileHash, size)
	_, err := os.Stat(cachePath)
	return err == nil
}
