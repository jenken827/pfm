package webdav

import "testing"

// WebDAV 的 href 必须是「客户端可见路径」。本 handler 挂在 /dav/ 且被
// http.StripPrefix("/dav") 处理，后端内部路径是剥掉前缀后的 /webvav/…，
// 因此 FileInfo.Path 必须把 mountPrefix 加回去，否则按路径匹配子项的客户端
// （ReadAny 等）会把 PROPFIND 的条目全部丢掉，进而判定「远端为空」并重传全部文件。
func TestToFileInfoPrefixesClientVisiblePath(t *testing.T) {
	backend := NewBackend("root-id", "owner-id", "/dav")

	folder := &Entry{ID: "file-1", Name: "readany", IsDir: true}

	info := backend.toFileInfo(folder, "/webvav/readany")
	if info.Path != "/dav/webvav/readany" {
		t.Fatalf("folder href = %q, want %q", info.Path, "/dav/webvav/readany")
	}
	if !info.IsDir {
		t.Fatalf("folder must be reported as a collection")
	}

	file := &Entry{ID: "file-2", Name: "book.epub", Size: 1024}

	fileInfo := backend.toFileInfo(file, "/webvav/readany/data/books/b/book.epub")
	if fileInfo.Path != "/dav/webvav/readany/data/books/b/book.epub" {
		t.Fatalf("file href = %q", fileInfo.Path)
	}
	if fileInfo.IsDir {
		t.Fatalf("file must not be reported as a collection")
	}
	if fileInfo.Size != 1024 {
		t.Fatalf("file size = %d, want 1024", fileInfo.Size)
	}
}

// MIME 类型：文件行自带的优先，缺失时按扩展名推断（目录不推断）
func TestToFileInfoMIMEType(t *testing.T) {
	backend := NewBackend("root-id", "owner-id", "/dav")

	withMime := backend.toFileInfo(&Entry{Name: "book.bin", Mime: "application/epub+zip"}, "/x")
	if withMime.MIMEType != "application/epub+zip" {
		t.Errorf("MIMEType = %q, want the one from the file row", withMime.MIMEType)
	}

	byExt := backend.toFileInfo(&Entry{Name: "book.epub"}, "/x")
	if byExt.MIMEType != "application/epub+zip" {
		t.Errorf("MIMEType = %q, want inferred from extension", byExt.MIMEType)
	}

	dir := backend.toFileInfo(&Entry{Name: "books", IsDir: true}, "/x")
	if dir.MIMEType != "" {
		t.Errorf("directory must not carry a MIME type, got %q", dir.MIMEType)
	}
}
