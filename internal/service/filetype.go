package service

import "jenken827.com/jenken827/pfm/internal/fileutil"

// GetFileType 根据文件内容或扩展名获取文件类型
func GetFileType(path string, filename string) (int32, string) {
	return fileutil.GetFileType(path, filename)
}

// DetectFileTypeFromExt 根据文件扩展名获取文件类型
func DetectFileTypeFromExt(filename string) (int32, string) {
	return fileutil.DetectFileTypeFromExt(filename)
}
