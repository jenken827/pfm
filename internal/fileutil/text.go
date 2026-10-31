package fileutil

import (
	"path/filepath"
	"strings"
)

// 可按文本读取的扩展名（小写、不带点）。
//
// 这份白名单是**服务端**的口径（MCP 的 read_text、将来的纯文本预览都按它判断），
// 与客户端预览的白名单同源：改这里要同步 pfm-frontend 的 utils/text_preview.dart
// 与 Web UI 的 js/text_preview.js，否则同一文件在两端一个能读一个不能读。
var textExtensions = map[string]struct{}{
	// 纯文本 / 标记
	"txt": {}, "text": {}, "log": {}, "md": {}, "markdown": {}, "rst": {}, "adoc": {},
	// 结构化数据 / 配置
	"json": {}, "jsonl": {}, "yaml": {}, "yml": {}, "toml": {}, "ini": {}, "conf": {},
	"cfg": {}, "properties": {}, "env": {}, "xml": {}, "csv": {}, "tsv": {},
	// 前端 / 脚本 / 代码
	"html": {}, "htm": {}, "css": {}, "scss": {}, "less": {}, "js": {}, "mjs": {},
	"cjs": {}, "ts": {}, "tsx": {}, "jsx": {}, "vue": {}, "svelte": {},
	"sh": {}, "bash": {}, "zsh": {}, "fish": {}, "bat": {}, "ps1": {},
	"py": {}, "rb": {}, "php": {}, "go": {}, "rs": {}, "java": {}, "kt": {}, "kts": {},
	"c": {}, "h": {}, "cc": {}, "cpp": {}, "hpp": {}, "cs": {}, "swift": {}, "m": {},
	"lua": {}, "pl": {}, "r": {}, "scala": {}, "sql": {}, "proto": {}, "gradle": {},
	"dart": {}, "ex": {}, "exs": {}, "erl": {}, "hs": {}, "clj": {}, "vim": {}, "el": {},
	"dockerfile": {}, "makefile": {}, "gitignore": {}, "editorconfig": {},
	// 少量纯文本型文档
	"svg": {},
}

// 按文件名（无扩展名）识别的一类纯文本文件
var textFileNames = map[string]struct{}{
	"dockerfile": {}, "makefile": {}, "readme": {}, "license": {},
	"changelog": {}, "authors": {}, "notice": {},
}

// IsTextFile 判断文件是否可按 UTF-8 文本读取。
//
// 判定顺序：MIME 是 text/* 或带 +json / +xml 后缀 → 直接算文本；
// 其余只看扩展名白名单（不猜内容、不读文件头：这是"能不能安全塞进上下文"的
// 准入，不是内容嗅探）。明确的应用类 MIME（image/audio/video/application/pdf…）
// 一律拒绝，即便扩展名在白名单里。
func IsTextFile(filename, mimeType string) bool {
	mime := strings.ToLower(strings.TrimSpace(mimeType))
	if strings.HasPrefix(mime, "image/") ||
		strings.HasPrefix(mime, "audio/") ||
		strings.HasPrefix(mime, "video/") ||
		strings.HasPrefix(mime, "font/") {
		return false
	}
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	if strings.HasPrefix(mime, "text/") ||
		strings.HasSuffix(mime, "+json") ||
		strings.HasSuffix(mime, "+xml") {
		return true
	}
	if strings.Contains(mime, "pdf") || strings.Contains(mime, "zip") ||
		strings.Contains(mime, "octet-stream") {
		return false
	}

	name := strings.ToLower(strings.TrimSpace(filename))
	if name == "" {
		return false
	}
	if _, ok := textFileNames[name]; ok {
		return true
	}
	ext := strings.TrimPrefix(filepath.Ext(name), ".")
	if ext == "" {
		return false
	}
	_, ok := textExtensions[ext]
	return ok
}
