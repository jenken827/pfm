package webdav

import (
	"testing"

	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// WebDAV 的 href 必须是「客户端可见路径」。本 handler 挂在 /dav/ 且被
// http.StripPrefix("/dav") 处理，后端内部路径是剥掉前缀后的 /webvav/…，
// 因此 FileInfo.Path 必须把 mountPrefix 加回去，否则按路径匹配子项的客户端
// （ReadAny 等）会把 PROPFIND 的条目全部丢掉，进而判定「远端为空」并重传全部文件。
func TestToFileInfoPrefixesClientVisiblePath(t *testing.T) {
	backend := NewBackend("root-id", "owner-id", "/dav")

	folderName := "readany"
	folderType := int32(native.FileType_FOLDER)
	folderSize := uint64(0)
	folder := &native.FileORM{
		Id:   webdavTestStrPtr("file-1"),
		Name: &folderName,
		Type: &folderType,
		Size: &folderSize,
	}

	info := backend.toFileInfo(folder, "/webvav/readany")
	if info.Path != "/dav/webvav/readany" {
		t.Fatalf("folder href = %q, want %q", info.Path, "/dav/webvav/readany")
	}
	if !info.IsDir {
		t.Fatalf("folder must be reported as a collection")
	}

	fileName := "book.epub"
	fileType := int32(native.FileType_DOCUMENT)
	fileSize := uint64(1024)
	file := &native.FileORM{
		Id:   webdavTestStrPtr("file-2"),
		Name: &fileName,
		Type: &fileType,
		Size: &fileSize,
	}

	fileInfo := backend.toFileInfo(file, "/webvav/readany/data/books/b/book.epub")
	if fileInfo.Path != "/dav/webvav/readany/data/books/b/book.epub" {
		t.Fatalf("file href = %q", fileInfo.Path)
	}
	if fileInfo.IsDir {
		t.Fatalf("file must not be reported as a collection")
	}
}

func webdavTestStrPtr(value string) *string { return &value }
