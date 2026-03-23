package service

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// getFileType 根据文件内容或扩展名获取文件类型
func getFileType(path string, filename string) int32 {
	// 优先尝试从文件内容检测（如果path有效）
	if path != "" {
		if fileType := detectFileTypeFromContent(path); fileType != native.FileType_TYPE_UNSPECIFIED {
			return int32(fileType)
		}
	}

	// 回退到根据扩展名判断
	return detectFileTypeFromExt(filename)
}

// detectFileTypeFromContent 从文件内容检测文件类型
func detectFileTypeFromContent(path string) native.FileType {
	// 读取文件前512字节用于检测
	file, err := os.Open(path)
	if err != nil {
		return native.FileType_TYPE_UNSPECIFIED
	}
	defer file.Close()

	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return native.FileType_TYPE_UNSPECIFIED
	}
	if n == 0 {
		return native.FileType_TYPE_UNSPECIFIED
	}

	// 使用http.DetectContentType检测MIME类型
	mimeType := http.DetectContentType(buffer[:n])

	// 从MIME类型映射到FileType
	return mimeTypeToFileType(mimeType)
}

// mimeTypeToFileType 将MIME类型转换为FileType枚举
func mimeTypeToFileType(mimeType string) native.FileType {
	// 只检查主要类型
	if len(mimeType) >= 5 && mimeType[:5] == "image" {
		return native.FileType_IMAGE
	}
	if len(mimeType) >= 5 && mimeType[:5] == "video" {
		return native.FileType_VIDEO
	}
	if len(mimeType) >= 5 && mimeType[:5] == "audio" {
		return native.FileType_AUDIO
	}
	if len(mimeType) >= 11 && mimeType[:11] == "application" {
		// 检查是否是PDF
		if len(mimeType) >= 15 && mimeType[:15] == "application/pdf" {
			return native.FileType_DOCUMENT
		}
		// 检查是否是压缩文件
		if bytes.Contains([]byte(mimeType), []byte("zip")) ||
			bytes.Contains([]byte(mimeType), []byte("rar")) ||
			bytes.Contains([]byte(mimeType), []byte("x-7z")) {
			return native.FileType_ARCHIVE
		}
	}
	if len(mimeType) >= 4 && mimeType[:4] == "text" {
		return native.FileType_DOCUMENT
	}
	if len(mimeType) >= 9 && mimeType[:9] == "font" {
		return native.FileType_FONT
	}

	return native.FileType_TYPE_UNSPECIFIED
}

// detectFileTypeFromExt 根据文件扩展名获取文件类型
func detectFileTypeFromExt(filename string) int32 {
	ext := filepath.Ext(filename)
	if ext == "" {
		return int32(native.FileType_APPLICATION)
	}

	// 转换小写以便比较
	ext = string(bytes.ToLower([]byte(ext)))

	// 使用map快速查找
	fileTypeMap := map[string]native.FileType{
		".jpg":   native.FileType_IMAGE,
		".jpeg":  native.FileType_IMAGE,
		".png":   native.FileType_IMAGE,
		".gif":   native.FileType_IMAGE,
		".webp":  native.FileType_IMAGE,
		".bmp":   native.FileType_IMAGE,
		".ico":   native.FileType_IMAGE,
		".svg":   native.FileType_IMAGE,
		".raw":   native.FileType_IMAGE,
		".tiff":  native.FileType_IMAGE,
		".mp4":   native.FileType_VIDEO,
		".avi":   native.FileType_VIDEO,
		".mkv":   native.FileType_VIDEO,
		".mov":   native.FileType_VIDEO,
		".wmv":   native.FileType_VIDEO,
		".webm":  native.FileType_VIDEO,
		".flv":   native.FileType_VIDEO,
		".m4v":   native.FileType_VIDEO,
		".mp3":   native.FileType_AUDIO,
		".wav":   native.FileType_AUDIO,
		".ogg":   native.FileType_AUDIO,
		".flac":  native.FileType_AUDIO,
		".aac":   native.FileType_AUDIO,
		".m4a":   native.FileType_AUDIO,
		".wma":   native.FileType_AUDIO,
		".ape":   native.FileType_AUDIO,
		".pdf":   native.FileType_DOCUMENT,
		".doc":   native.FileType_DOCUMENT,
		".docx":  native.FileType_DOCUMENT,
		".xls":   native.FileType_DOCUMENT,
		".xlsx":  native.FileType_DOCUMENT,
		".ppt":   native.FileType_DOCUMENT,
		".pptx":  native.FileType_DOCUMENT,
		".txt":   native.FileType_DOCUMENT,
		".md":    native.FileType_DOCUMENT,
		".rtf":   native.FileType_DOCUMENT,
		".odt":   native.FileType_DOCUMENT,
		".ods":   native.FileType_DOCUMENT,
		".odp":   native.FileType_DOCUMENT,
		".zip":   native.FileType_ARCHIVE,
		".rar":   native.FileType_ARCHIVE,
		".7z":    native.FileType_ARCHIVE,
		".tar":   native.FileType_ARCHIVE,
		".gz":    native.FileType_ARCHIVE,
		".bz2":   native.FileType_ARCHIVE,
		".xz":    native.FileType_ARCHIVE,
		".iso":   native.FileType_ARCHIVE,
		".dmg":   native.FileType_ARCHIVE,
		".ttf":   native.FileType_FONT,
		".otf":   native.FileType_FONT,
		".woff":  native.FileType_FONT,
		".woff2": native.FileType_FONT,
		".eot":   native.FileType_FONT,
	}

	if fileType, ok := fileTypeMap[string(ext)]; ok {
		return int32(fileType)
	}
	return int32(native.FileType_APPLICATION)
}
