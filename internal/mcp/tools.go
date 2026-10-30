package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/internal/fileutil"
	"github.com/jenken827/pfm/internal/models"
	"github.com/jenken827/pfm/internal/service"
	"github.com/jenken827/pfm/internal/thumbnail"
	"google.golang.org/protobuf/proto"
)

// 分页默认值与上限（工具参数里的 limit 都按这个收敛）
const (
	defaultPageSize = 100
	maxPageSize     = 500
	// maxTrashItems 回收站一次最多返回多少条（回收站没有分页接口）
	maxTrashItems = 200
	// downloadLinkTTLText 下载票据有效期的展示文案（与 service 的下载票据一致：12 小时）
	downloadLinkTTLText = "12h"
)

// coreTools 核心工具（P1 只读；写工具随 P2 追加）
func coreTools() []*Tool {
	tools := []*Tool{
		toolListFiles(),
		toolStatFile(),
		toolSearchFiles(),
		toolRecentFiles(),
		toolTrashList(),
		toolReadText(),
		toolReadImage(),
		toolDownloadLink(),
		toolShareLinksList(),
		toolProvidersList(),
	}
	// 写工具在 tools/list 里按权限过滤（见 Registry.List），但注册表里一直存在：
	// `pfm mcp tools` 要能看到全量，排查"工具没出现"才有依据
	return append(tools, writeTools()...)
}

// ===== list_files =====

func toolListFiles() *Tool {
	return &Tool{
		Name:     "list_files",
		Title:    "列出目录内容",
		ReadOnly: true,
		Source:   "core",
		Description: "列出目录下的文件与子目录。path 省略或为 / 时表示令牌可见的根目录；" +
			"目录不存在的报错里会给出同层候选名。结果含 id / name / path / type / size / updated_at，" +
			"type 为 folder 的是目录。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "目录路径，以 / 开头；省略表示根目录"},
				"file_id": {"type": "string", "description": "目录 ID（与 path 二选一，优先 file_id）"},
				"limit": {"type": "integer", "description": "返回条数，默认 100，最大 500"},
				"offset": {"type": "integer", "description": "偏移量，默认 0"}
			},
			"additionalProperties": false
		}`),
		Handler: func(ctx context.Context, call *Call) (*Result, error) {
			res, err := newResolver(call.Principal)
			if err != nil {
				return nil, err
			}
			parent, err := targetNode(res, call)
			if err != nil {
				return nil, err
			}
			if dirType(parent) != "folder" {
				return nil, fmt.Errorf("%s 不是目录", res.DisplayPath(parent))
			}
			limit := call.IntOr("limit", defaultPageSize, maxPageSize)
			offset := call.IntOr("offset", 0, 0)
			files, total, hasMore, merr := service.GetFileList(ctx, &native.FileListReq{
				ParentId: proto.String(fieldID(parent)),
				PageSize: proto.Uint32(uint32(limit)),
				Offset:   proto.Uint32(uint32(offset)),
			})
			if merr != nil {
				return nil, fromServiceErr(merr)
			}
			parentPath := res.DisplayPath(parent)
			items := make([]any, 0, len(files))
			for _, file := range files {
				items = append(items, fileSummary(parentPath, file))
			}
			return jsonResult(map[string]any{
				"path":     parentPath,
				"total":    total,
				"offset":   offset,
				"has_more": hasMore,
				"items":    items,
			}), nil
		},
	}
}

// ===== stat_file =====

func toolStatFile() *Tool {
	return &Tool{
		Name:     "stat_file",
		Title:    "查看文件或目录详情",
		ReadOnly: true,
		Source:   "core",
		Description: "查看单个文件 / 目录的详情：路径、类型、大小、更新时间、标签、md5 之外的 hash 摘要、" +
			"副本落在哪些存储提供者、历史版本数量。搜索结果只给 id，想拿完整路径就用这个工具。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "文件或目录路径，以 / 开头"},
				"file_id": {"type": "string", "description": "文件 ID（与 path 二选一，优先 file_id）"}
			},
			"additionalProperties": false
		}`),
		Handler: func(ctx context.Context, call *Call) (*Result, error) {
			res, err := newResolver(call.Principal)
			if err != nil {
				return nil, err
			}
			file, err := targetNode(res, call)
			if err != nil {
				return nil, err
			}
			detail, merr := service.GetFileDetail(ctx, &native.FileDetailReq{FileId: proto.String(fieldID(file))})
			if merr != nil {
				return nil, fromServiceErr(merr)
			}
			payload := fileSummary("/", file)
			payload["path"] = res.DisplayPath(file)
			payload["parent_id"] = fieldParentID(file)
			payload["hash"] = fieldHash(file)
			payload["ready"] = fieldReady(file)
			payload["encrypted"] = fieldEncrypted(file)
			payload["tags"] = detail.GetTags()
			payload["history_count"] = len(detail.GetHistories())
			locations := make([]any, 0, len(detail.GetLocations()))
			for _, item := range detail.GetLocations() {
				locations = append(locations, map[string]any{
					"provider_id":   item.GetProviderId(),
					"provider_name": item.GetProviderName(),
					"driver_id":     item.GetDriverId(),
					"status":        item.GetStatus().String(),
					"encrypted":     item.GetEncrypted(),
					"chunked":       item.GetIsChunked(),
				})
			}
			payload["locations"] = locations
			return jsonResult(payload), nil
		},
	}
}

// ===== search_files =====

func toolSearchFiles() *Tool {
	return &Tool{
		Name:     "search_files",
		Title:    "按文件名或标签搜索",
		ReadOnly: true,
		Source:   "core",
		Description: "在令牌可见范围内按文件名或标签模糊搜索。结果只给 id / name / parent_id，" +
			"要完整路径请用 stat_file。分页用 offset + limit。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {
				"keyword": {"type": "string", "description": "搜索关键词"},
				"search_type": {"type": "string", "enum": ["name", "tag"], "description": "按文件名（默认）或按标签"},
				"limit": {"type": "integer", "description": "返回条数，默认 100，最大 500"},
				"offset": {"type": "integer", "description": "偏移量，默认 0"}
			},
			"required": ["keyword"],
			"additionalProperties": false
		}`),
		Handler: func(ctx context.Context, call *Call) (*Result, error) {
			res, err := newResolver(call.Principal)
			if err != nil {
				return nil, err
			}
			keyword, _ := call.Str("keyword")
			if strings.TrimSpace(keyword) == "" {
				return nil, fmt.Errorf("keyword 不能为空")
			}
			searchType := native.SearchType_NAME
			if call.StrOr("search_type", "name") == "tag" {
				searchType = native.SearchType_TAG
			}
			limit := call.IntOr("limit", defaultPageSize, maxPageSize)
			offset := call.IntOr("offset", 0, 0)

			files, total, hasMore, merr := service.SearchFiles(ctx, &native.SearchFilesReq{
				Keyword:    proto.String(keyword),
				SearchType: searchType.Enum(),
				PageSize:   proto.Uint32(uint32(limit)),
				Offset:     proto.Uint32(uint32(offset)),
			})
			if merr != nil {
				return nil, fromServiceErr(merr)
			}
			items := make([]any, 0, len(files))
			for _, file := range files {
				if !res.inScope(file) {
					continue
				}
				items = append(items, map[string]any{
					"id":         fieldID(file),
					"name":       fieldName(file),
					"parent_id":  fieldParentID(file),
					"type":       dirType(file),
					"size":       fieldSize(file),
					"updated_at": formatTime(file.UpdatedAt),
				})
			}
			return jsonResult(map[string]any{
				"keyword":  keyword,
				"total":    total,
				"offset":   offset,
				"has_more": hasMore,
				"items":    items,
			}), nil
		},
	}
}

// ===== recent_files =====

func toolRecentFiles() *Tool {
	return &Tool{
		Name:        "recent_files",
		Title:       "最近访问的文件",
		ReadOnly:    true,
		Source:      "core",
		Description: "列出最近访问过的文件（可见范围内）。适合在不知道路径时先看用户最近在动什么。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {
				"limit": {"type": "integer", "description": "返回条数，默认 50，最大 200"},
				"offset": {"type": "integer", "description": "偏移量，默认 0"}
			},
			"additionalProperties": false
		}`),
		Handler: func(ctx context.Context, call *Call) (*Result, error) {
			res, err := newResolver(call.Principal)
			if err != nil {
				return nil, err
			}
			limit := call.IntOr("limit", 50, 200)
			offset := call.IntOr("offset", 0, 0)
			files, total, hasMore, merr := service.GetRecentFiles(ctx, &native.GetRecentFilesReq{
				PageSize: proto.Uint32(uint32(limit)),
				Offset:   proto.Uint32(uint32(offset)),
			})
			if merr != nil {
				return nil, fromServiceErr(merr)
			}
			items := make([]any, 0, len(files))
			for _, file := range files {
				if !res.inScope(file) {
					continue
				}
				items = append(items, map[string]any{
					"id":               fieldID(file),
					"name":             fieldName(file),
					"parent_id":        fieldParentID(file),
					"type":             dirType(file),
					"size":             fieldSize(file),
					"last_accessed_at": formatTime(fieldLastAccessedAt(file)),
				})
			}
			return jsonResult(map[string]any{
				"total":    total,
				"has_more": hasMore,
				"items":    items,
			}), nil
		},
	}
}

// ===== trash_list =====

func toolTrashList() *Tool {
	return &Tool{
		Name:        "trash_list",
		Title:       "回收站列表",
		ReadOnly:    true,
		Source:      "core",
		Description: "列出回收站里的文件（可见范围内，最多 200 条）。删除只进回收站，可用工具恢复。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {},
			"additionalProperties": false
		}`),
		Handler: func(ctx context.Context, call *Call) (*Result, error) {
			// 回收站按 owner 过滤，不按 token 的可见根目录裁剪：软删记录保留了
			// 原 parent_id，按子树过滤会漏掉"根目录之外被移入回收站"的项。
			data, merr := service.GetTrashList(ctx)
			if merr != nil {
				return nil, fromServiceErr(merr)
			}
			items := make([]any, 0, len(data.GetFiles()))
			for _, item := range data.GetFiles() {
				if len(items) >= maxTrashItems {
					break
				}
				if item.GetOwnerId() != call.Principal.UserID {
					continue
				}
				deletedAt := ""
				if item.DeletedAt != nil {
					deletedAt = item.DeletedAt.AsTime().Format(time.RFC3339)
				}
				items = append(items, map[string]any{
					"id":         item.GetId(),
					"name":       item.GetName(),
					"parent_id":  item.GetParentId(),
					"type":       protoDirType(item),
					"size":       item.GetSize(),
					"deleted_at": deletedAt,
				})
			}
			return jsonResult(map[string]any{
				"total": data.GetTotal(),
				"shown": len(items),
				"items": items,
			}), nil
		},
	}
}

// ===== read_text =====

func toolReadText() *Tool {
	return &Tool{
		Name:     "read_text",
		Title:    "读取文本文件内容",
		ReadOnly: true,
		Source:   "core",
		Description: "读取文本类文件（txt / md / json / yaml / csv / 代码等）的 UTF-8 内容。默认最多读 256KB，" +
			"超出部分截断并在结果里标 truncated=true；需要完整原始字节请用 download_link。" +
			"图片请用 read_image，其它二进制格式无法读取。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "文件路径，以 / 开头"},
				"file_id": {"type": "string", "description": "文件 ID（与 path 二选一，优先 file_id）"},
				"max_bytes": {"type": "integer", "description": "本次最多读取多少字节（不超过服务端上限）"}
			},
			"additionalProperties": false
		}`),
		Handler: func(ctx context.Context, call *Call) (*Result, error) {
			res, err := newResolver(call.Principal)
			if err != nil {
				return nil, err
			}
			file, err := targetNode(res, call)
			if err != nil {
				return nil, err
			}
			if dirType(file) == "folder" {
				return nil, fmt.Errorf("%s 是目录，用 list_files 查看内容", res.DisplayPath(file))
			}
			if !isTextFile(file) {
				return nil, fmt.Errorf("%s 不是文本文件（mime=%q），图片用 read_image，其余用 download_link",
					res.DisplayPath(file), fieldMime(file))
			}

			limit := call.Settings.MaxReadBytes
			if asked := call.IntOr("max_bytes", 0, call.Settings.MaxReadBytes); asked > 0 {
				limit = asked
			}

			stream, err := service.GetFileStream(ctx, fieldID(file), nil)
			if err != nil {
				return nil, fmt.Errorf("读取内容失败: %w", err)
			}
			defer func() { _ = stream.Reader.Close() }()
			if stream.IsPreDownload {
				return nil, fmt.Errorf("内容正在从存储提供者预下载，请稍后重试")
			}

			body, err := io.ReadAll(io.LimitReader(stream.Reader, int64(limit)))
			if err != nil {
				return nil, fmt.Errorf("读取内容失败: %w", err)
			}
			if !utf8.Valid(body) {
				return nil, fmt.Errorf("%s 不是 UTF-8 文本，请用 download_link 取原始字节", res.DisplayPath(file))
			}
			return jsonResult(map[string]any{
				"path":      res.DisplayPath(file),
				"size":      stream.FileSize,
				"read":      len(body),
				"truncated": stream.FileSize > int64(len(body)),
				"text":      string(body),
			}), nil
		},
	}
}

// ===== read_image =====

func toolReadImage() *Tool {
	return &Tool{
		Name:     "read_image",
		Title:    "查看图片",
		ReadOnly: true,
		Source:   "core",
		Description: "以图片内容块返回图片。默认用 400px 的预览缩略图（省 token）；" +
			"size=small 是 200px 缩略图。原图请用 download_link。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "图片路径，以 / 开头"},
				"file_id": {"type": "string", "description": "文件 ID（与 path 二选一，优先 file_id）"},
				"size": {"type": "string", "enum": ["small", "large"], "description": "缩略图档位，默认 large"}
			},
			"additionalProperties": false
		}`),
		Handler: func(ctx context.Context, call *Call) (*Result, error) {
			res, err := newResolver(call.Principal)
			if err != nil {
				return nil, err
			}
			file, err := targetNode(res, call)
			if err != nil {
				return nil, err
			}
			if dirType(file) == "folder" {
				return nil, fmt.Errorf("%s 是目录，不是图片", res.DisplayPath(file))
			}
			size := thumbnail.LargeSize()
			if call.StrOr("size", "large") == "small" {
				size = thumbnail.SmallSize()
			}
			thumbnailPath, needGenerate, thumbFile := service.GetThumbnailPath(fieldID(file), size)
			if thumbnailPath == "" && needGenerate && thumbFile != nil {
				// 同步生成：MCP 是一次性请求，异步生成会让本次调用拿不到图
				service.RegenerateThumbnail(ctx, thumbFile)
				thumbnailPath, _, _ = service.GetThumbnailPath(fieldID(file), size)
			}
			if thumbnailPath == "" {
				return nil, fmt.Errorf("%s 生成不了缩略图（不是图片，或存储提供者暂时不可用），请用 download_link",
					res.DisplayPath(file))
			}
			body, err := os.ReadFile(thumbnailPath)
			if err != nil {
				return nil, fmt.Errorf("读取缩略图失败: %w", err)
			}
			return imageResult("image/jpeg", body), nil
		},
	}
}

// ===== download_link =====

func toolDownloadLink() *Tool {
	return &Tool{
		Name:     "download_link",
		Title:    "生成下载链接",
		ReadOnly: true,
		Source:   "core",
		Description: "给文件签一个 12 小时有效的单文件下载链接（URL 自带票据，无需 Authorization 头）。" +
			"链接是临时凭证：只用于本次交付，不要写进日志或长期保存。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "文件路径，以 / 开头"},
				"file_id": {"type": "string", "description": "文件 ID（与 path 二选一，优先 file_id）"}
			},
			"additionalProperties": false
		}`),
		Handler: func(ctx context.Context, call *Call) (*Result, error) {
			res, err := newResolver(call.Principal)
			if err != nil {
				return nil, err
			}
			file, err := targetNode(res, call)
			if err != nil {
				return nil, err
			}
			if dirType(file) == "folder" {
				return nil, fmt.Errorf("%s 是目录，不能下载", res.DisplayPath(file))
			}
			ticket, err := service.SignDownloadTicket(fieldID(file), call.Principal.UserID)
			if err != nil {
				return nil, fmt.Errorf("签发下载票据失败: %w", err)
			}
			base := strings.TrimRight(call.Settings.ServerURL, "/")
			return jsonResult(map[string]any{
				"path":       res.DisplayPath(file),
				"size":       fieldSize(file),
				"expires_in": downloadLinkTTLText,
				"url":        base + "/api/file/stream/download?ticket=" + url.QueryEscape(ticket),
			}), nil
		},
	}
}

// ===== share_links_list =====

func toolShareLinksList() *Tool {
	return &Tool{
		Name:        "share_links_list",
		Title:       "我的分享链接",
		ReadOnly:    true,
		Source:      "core",
		Description: "列出当前用户创建的分享链接（含分享码、过期时间、访问次数）。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {
				"limit": {"type": "integer", "description": "返回条数，默认 20，最大 100"},
				"offset": {"type": "integer", "description": "偏移量，默认 0"}
			},
			"additionalProperties": false
		}`),
		Handler: func(ctx context.Context, call *Call) (*Result, error) {
			limit := call.IntOr("limit", 20, 100)
			offset := call.IntOr("offset", 0, 0)
			data, merr := service.ListMyShares(ctx, &native.ListMySharesReq{
				PageSize: proto.Uint32(uint32(limit)),
				Offset:   proto.Uint32(uint32(offset)),
			})
			if merr != nil {
				return nil, fromServiceErr(merr)
			}
			items := make([]any, 0, len(data.GetItems()))
			for _, item := range data.GetItems() {
				entry := map[string]any{
					"id":            item.GetId(),
					"share_code":    item.GetShareCode(),
					"file_id":       item.GetFileId(),
					"file_name":     item.GetFileName(),
					"file_missing":  item.GetFileMissing(),
					"file_in_trash": item.GetFileInTrash(),
					"access_count":  item.GetAccessCount(),
					"never_expire":  item.GetNeverExpire(),
					"status":        item.GetStatus().String(),
				}
				if item.GetExpireAt() != nil {
					expireAt := item.GetExpireAt().AsTime()
					entry["expire_at"] = formatTime(&expireAt)
				}
				// share_url 含签名参数，属于临时链接：只在本结果里出现，不写日志
				entry["share_url"] = item.GetShareUrl()
				items = append(items, entry)
			}
			return jsonResult(map[string]any{
				"total":    data.GetTotal(),
				"has_more": data.GetHasMore(),
				"items":    items,
			}), nil
		},
	}
}

// ===== providers_list =====

func toolProvidersList() *Tool {
	return &Tool{
		Name:        "providers_list",
		Title:       "可用的存储提供者",
		ReadOnly:    true,
		Source:      "core",
		Description: "列出管理员分配给当前用户的存储提供者（不含任何凭据，只有 id / 名称 / 容量 / 启用状态）。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {},
			"additionalProperties": false
		}`),
		Handler: func(ctx context.Context, call *Call) (*Result, error) {
			data, merr := service.GetMyBoundProviders(ctx)
			if merr != nil {
				return nil, fromServiceErr(merr)
			}
			items := make([]any, 0, len(data.GetProviders()))
			for _, provider := range data.GetProviders() {
				items = append(items, map[string]any{
					"id":          provider.GetId(),
					"name":        provider.GetName(),
					"driver_id":   provider.GetDriverId(),
					"enabled":     provider.GetEnabled(),
					"description": provider.GetDescription(),
				})
			}
			return jsonResult(map[string]any{"items": items}), nil
		},
	}
}

// ===== 公共辅助 =====

// targetNode 按 file_id / path 取目标节点（file_id 优先，并做可见范围校验）
func targetNode(res *resolver, call *Call) (*entity.FileORM, error) {
	if fileID, ok := call.Str("file_id"); ok && fileID != "" {
		file, err := res.load(fileID)
		if err != nil {
			return nil, err
		}
		if !res.inScope(file) {
			return nil, fmt.Errorf("该文件不在令牌可见范围内")
		}
		return file, nil
	}
	path, _ := call.Str("path")
	file, err := res.node(path)
	if err != nil {
		return nil, err
	}
	return file, nil
}

// fromServiceErr 把 service 的 models.Err 转成普通 error
func fromServiceErr(err *models.Err) error {
	if err == nil {
		return nil
	}
	if err.E != nil {
		return err.E
	}
	return fmt.Errorf("操作失败（code=%s）", err.C.String())
}

// fileSummary 文件摘要（列表用；parentPath 已知，直接拼出完整路径）
func fileSummary(parentPath string, file *entity.FileORM) map[string]any {
	path := parentPath
	if path == "" {
		path = "/"
	}
	if path != "/" {
		path += "/"
	}
	item := map[string]any{
		"id":         fieldID(file),
		"name":       fieldName(file),
		"path":       path + fieldName(file),
		"type":       dirType(file),
		"size":       fieldSize(file),
		"updated_at": formatTime(file.UpdatedAt),
	}
	if !fieldReady(file) && dirType(file) == "file" {
		item["ready"] = false
	}
	return item
}

// dirType 目录 / 文件（对模型只用这两个词，别把 8 种 FileType 全铺出去）
func dirType(file *entity.FileORM) string {
	if fieldType(file) == int32(native.FileType_FOLDER) {
		return "folder"
	}
	return "file"
}

// protoDirType native.File 版本的 dirType（回收站列表用）
func protoDirType(file *native.File) string {
	if file.GetType() == native.FileType_FOLDER {
		return "folder"
	}
	return "file"
}

// isTextFile 是否可按文本读取：MIME 是 text/*，或扩展名在文本白名单里
func isTextFile(file *entity.FileORM) bool {
	return fileutil.IsTextFile(fieldName(file), fieldMime(file))
}

// formatTime 统一的时间格式（空值返回空串）
func formatTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339)
}

// encodeBase64 图片内容块用
func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
