package thumbnail

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"

	"jenken827.com/jenken827/pfm/internal/llog"
	"golang.org/x/image/draw"
)

const (
	// DefaultWidth 默认缩略图宽度
	DefaultWidth = 200
	// DefaultHeight 默认缩略图高度
	DefaultHeight = 200
	// DefaultQuality JPEG 质量
	DefaultQuality = 75
)

// Generator 缩略图生成器
type Generator struct {
	cacheDir string
	width    int
	height   int
	quality  int
}

// NewGenerator 创建缩略图生成器
func NewGenerator(cacheDir string) *Generator {
	return &Generator{
		cacheDir: cacheDir,
		width:    DefaultWidth,
		height:   DefaultHeight,
		quality:  DefaultQuality,
	}
}

// GetCachePath 获取缩略图缓存路径
func (g *Generator) GetCachePath(fileHash string) string {
	filename := fmt.Sprintf("%s_%dx%d.jpg", fileHash, g.width, g.height)
	return filepath.Join(g.cacheDir, filename)
}

// Generate 生成缩略图
// filePath: 原图路径
// fileHash: 文件 hash
// 返回缩略图缓存路径
func (g *Generator) Generate(filePath string, fileHash string) (string, error) {
	// 确保缓存目录存在
	if err := os.MkdirAll(g.cacheDir, 0755); err != nil {
		return "", fmt.Errorf("create cache dir failed: %w", err)
	}

	cachePath := g.GetCachePath(fileHash)

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
	thumbImg := g.resize(srcImg)

	// 缩码为 JPEG
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

// resize 缩放图片（简单居中裁剪）
func (g *Generator) resize(src image.Image) image.Image {
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	// 计算缩放比例
	scaleX := float64(g.width) / float64(srcW)
	scaleY := float64(g.height) / float64(srcH)
	scale := scaleX
	if scaleY > scale {
		scale = scaleY
	}

	// 计算新尺寸
	newW := int(float64(srcW) * scale)
	newH := int(float64(srcH) * scale)

	// 创建缩放后的图片
	scaled := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.BiLinear.Scale(scaled, scaled.Bounds(), src, src.Bounds(), draw.Over, nil)

	// 居中裁剪
	cropX := (newW - g.width) / 2
	cropY := (newH - g.height) / 2
	cropped := image.NewRGBA(image.Rect(0, 0, g.width, g.height))

	draw.Draw(cropped, cropped.Bounds(), scaled, image.Point{X: cropX, Y: cropY}, draw.Src)

	// 如果目标尺寸大于源图片，填充白色背景
	if g.width > newW || g.height > newH {
		bg := image.NewRGBA(image.Rect(0, 0, g.width, g.height))
		draw.Draw(bg, bg.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		draw.Draw(bg, cropped.Bounds(), cropped, image.Point{}, draw.Src)
		return bg
	}

	return cropped
}

// DeleteThumbnail 删除缩略图缓存
func (g *Generator) DeleteThumbnail(fileHash string) error {
	cachePath := g.GetCachePath(fileHash)
	if err := os.Remove(cachePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ThumbnailExists 检查缩略图是否存在
func (g *Generator) ThumbnailExists(fileHash string) bool {
	cachePath := g.GetCachePath(fileHash)
	_, err := os.Stat(cachePath)
	return err == nil
}
