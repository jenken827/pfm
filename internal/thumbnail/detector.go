package thumbnail

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

// ImageInfo 图片信息
type ImageInfo struct {
	Width    int
	Height   int
	Format   string // jpeg, png, gif, webp, bmp
	FileSize int64
}

// 支持的图片格式
var supportedFormats = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".webp": true,
	".bmp":  true,
}

// 不需要生成缩略图的格式
var skipFormats = map[string]bool{
	".svg": true,
}

// IsImageFile 判断文件是否为图片
func IsImageFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return supportedFormats[ext]
}

// ShouldSkipThumbnail 判断是否跳过缩略图生成
func ShouldSkipThumbnail(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return skipFormats[ext]
}

// GetImageInfo 获取图片信息（尺寸、格式）
func GetImageInfo(filePath string) (*ImageInfo, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// 读取图片头部获取尺寸
	config, format, err := image.DecodeConfig(f)
	if err != nil {
		return nil, err
	}

	// 获取文件大小
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}

	return &ImageInfo{
		Width:    config.Width,
		Height:   config.Height,
		Format:   format,
		FileSize: stat.Size(),
	}, nil
}

// GetImageInfoFromReader 从 Reader 获取图片信息
func GetImageInfoFromReader(reader io.Reader) (*ImageInfo, error) {
	// 读取部分数据用于检测
	buf := make([]byte, 512*1024) // 512KB 应该足够读取图片头部
	n, err := reader.Read(buf)
	if err != nil && err != io.EOF {
		return nil, err
	}

	config, format, err := image.DecodeConfig(bytes.NewReader(buf[:n]))
	if err != nil {
		return nil, err
	}

	return &ImageInfo{
		Width:  config.Width,
		Height: config.Height,
		Format: format,
	}, nil
}

// NeedThumbnail 判断是否需要生成缩略图
// 规则：
// - SVG 格式：不需要
// - max dimension < 300px：不需要
// - 300px ≤ max dimension < 1024px 且文件 < 64KB：不需要
// - 其他情况：需要
func NeedThumbnail(info *ImageInfo, filename string) bool {
	// SVG 格式跳过
	if ShouldSkipThumbnail(filename) {
		return false
	}

	maxDim := info.Width
	if info.Height > maxDim {
		maxDim = info.Height
	}

	// 小于 300px 不需要
	if maxDim < 300 {
		return false
	}

	// 300-1024px 且文件小于 64KB 不需要
	if maxDim < 1024 && info.FileSize < 64*1024 {
		return false
	}

	return true
}
