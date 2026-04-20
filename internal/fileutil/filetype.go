package fileutil

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// GetFileType 根据文件内容或扩展名获取文件类型
func GetFileType(path string, filename string) (int32, string) {
	// 优先尝试从文件内容检测（如果path有效）
	if path != "" {
		if fileType, mime := detectFileTypeFromContent(path); fileType != native.FileType_FILE_TYPE_UNKNOWN {
			return int32(fileType), mime
		}
	}

	// 回退到根据扩展名判断
	return DetectFileTypeFromExt(filename)
}

// DetectFileTypeFromExt 根据文件扩展名获取文件类型
func DetectFileTypeFromExt(filename string) (int32, string) {
	ext := filepath.Ext(filename)
	if ext == "" {
		return int32(native.FileType_APPLICATION), "application/octet-stream"
	}

	// 转换小写以便比较
	ext = string(bytes.ToLower([]byte(ext)))

	// 使用 map 快速查找
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

	// MIME 类型映射
	mimeTypesMap := map[string]string{
		".jpg":   "image/jpeg",
		".jpeg":  "image/jpeg",
		".png":   "image/png",
		".gif":   "image/gif",
		".webp":  "image/webp",
		".bmp":   "image/bmp",
		".ico":   "image/x-icon",
		".svg":   "image/svg+xml",
		".raw":   "image/raw",
		".tiff":  "image/tiff",
		".mp4":   "video/mp4",
		".avi":   "video/x-msvideo",
		".mkv":   "video/x-matroska",
		".mov":   "video/quicktime",
		".wmv":   "video/x-ms-wmv",
		".webm":  "video/webm",
		".flv":   "video/x-flv",
		".m4v":   "video/x-m4v",
		".mp3":   "audio/mpeg",
		".wav":   "audio/wav",
		".ogg":   "audio/ogg",
		".flac":  "audio/flac",
		".aac":   "audio/aac",
		".m4a":   "audio/mp4",
		".wma":   "audio/x-ms-wma",
		".ape":   "audio/ape",
		".pdf":   "application/pdf",
		".doc":   "application/msword",
		".docx":  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		".xls":   "application/vnd.ms-excel",
		".xlsx":  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		".ppt":   "application/vnd.ms-powerpoint",
		".pptx":  "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		".txt":   "text/plain",
		".md":    "text/markdown",
		".rtf":   "application/rtf",
		".odt":   "application/vnd.oasis.opendocument.text",
		".ods":   "application/vnd.oasis.opendocument.spreadsheet",
		".odp":   "application/vnd.oasis.opendocument.presentation",
		".zip":   "application/zip",
		".rar":   "application/vnd.rar",
		".7z":    "application/x-7z-compressed",
		".tar":   "application/x-tar",
		".gz":    "application/gzip",
		".bz2":   "application/x-bzip2",
		".xz":    "application/x-xz",
		".iso":   "application/x-iso9660-image",
		".dmg":   "application/x-apple-diskimage",
		".ttf":   "font/ttf",
		".otf":   "font/otf",
		".woff":  "font/woff",
		".woff2": "font/woff2",
		".eot":   "application/vnd.ms-fontobject",
	}

	if fileType, ok := fileTypeMap[string(ext)]; ok {
		mimeType, exists := mimeTypesMap[string(ext)]
		if !exists {
			mimeType = "application/octet-stream"
		}
		return int32(fileType), mimeType
	}

	return int32(native.FileType_APPLICATION), "application/octet-stream"
}

// detectFileTypeFromContent 从文件内容检测文件类型
func detectFileTypeFromContent(path string) (native.FileType, string) {
	// 读取文件前512字节用于检测
	file, err := os.Open(path)
	if err != nil {
		return native.FileType_FILE_TYPE_UNKNOWN, ""
	}
	defer file.Close()

	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return native.FileType_FILE_TYPE_UNKNOWN, ""
	}
	if n == 0 {
		return native.FileType_FILE_TYPE_UNKNOWN, ""
	}

	// 使用http.DetectContentType检测MIME类型
	mimeType := http.DetectContentType(buffer[:n])

	// 如果是 ZIP 格式，进一步检测具体类型
	if bytes.Contains([]byte(mimeType), []byte("zip")) {
		if fileType, specificMime := detectZipBasedFormat(path); fileType != native.FileType_FILE_TYPE_UNKNOWN {
			return fileType, specificMime
		}
	}

	// 从MIME类型映射到FileType
	return mimeTypeToFileType(mimeType), mimeType
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
		// 检查是否是Office文档
		if bytes.Contains([]byte(mimeType), []byte("wordprocessing")) ||
			bytes.Contains([]byte(mimeType), []byte("spreadsheet")) ||
			bytes.Contains([]byte(mimeType), []byte("presentation")) ||
			bytes.Contains([]byte(mimeType), []byte("opendocument")) {
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

	return native.FileType_FILE_TYPE_UNKNOWN
}

// detectZipBasedFormat 检测基于 ZIP 的文件格式（Office Open XML、OpenDocument、EPUB等）
func detectZipBasedFormat(path string) (native.FileType, string) {
	// 打开 ZIP 文件
	reader, err := zip.OpenReader(path)
	if err != nil {
		return native.FileType_FILE_TYPE_UNKNOWN, ""
	}
	defer reader.Close()

	// 检查是否有 [Content_Types].xml（Office Open XML 标志）
	hasContentTypes := false
	for _, f := range reader.File {
		if f.Name == "[Content_Types].xml" {
			hasContentTypes = true
			break
		}
	}

	// 如果是 Office Open XML 格式
	if hasContentTypes {
		// 检查 [Content_Types].xml 内容判断具体类型
		for _, f := range reader.File {
			if f.Name == "[Content_Types].xml" {
				rc, err := f.Open()
				if err != nil {
					break
				}
				defer rc.Close()

				content, err := io.ReadAll(rc)
				if err != nil {
					break
				}

				contentStr := string(content)

				// Word 文档
				if strings.Contains(contentStr, "word/document.xml") {
					return native.FileType_DOCUMENT, "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
				}
				// Excel 文档
				if strings.Contains(contentStr, "xl/workbook.xml") {
					return native.FileType_DOCUMENT, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
				}
				// PowerPoint 文档
				if strings.Contains(contentStr, "ppt/presentation.xml") {
					return native.FileType_DOCUMENT, "application/vnd.openxmlformats-officedocument.presentationml.presentation"
				}

				break
			}
		}
	}

	// 检查 OpenDocument / EPUB 格式（根目录有 mimetype 文件）
	for _, f := range reader.File {
		if f.Name == "mimetype" {
			rc, err := f.Open()
			if err != nil {
				break
			}
			defer rc.Close()

			content, err := io.ReadAll(rc)
			if err != nil {
				break
			}

			mimetype := string(content)

			// EPUB
			if mimetype == "application/epub+zip" {
				return native.FileType_DOCUMENT, "application/epub+zip"
			}

			// OpenDocument Text
			if mimetype == "application/vnd.oasis.opendocument.text" {
				return native.FileType_DOCUMENT, mimetype
			}

			// OpenDocument Spreadsheet
			if mimetype == "application/vnd.oasis.opendocument.spreadsheet" {
				return native.FileType_DOCUMENT, mimetype
			}

			// OpenDocument Presentation
			if mimetype == "application/vnd.oasis.opendocument.presentation" {
				return native.FileType_DOCUMENT, mimetype
			}

			break
		}
	}

	// 无法确定具体类型，返回 UNKNOWN
	return native.FileType_FILE_TYPE_UNKNOWN, ""
}
