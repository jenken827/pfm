package mcp

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/service"
	"google.golang.org/protobuf/proto"
	"lukechampine.com/blake3"
)

// 写工具（P2）。
//
// 全部是 service 层的薄壳：归属校验、同名冲突消解、落盘分发、回收站语义都在
// service / storage 里，MCP 层**不自己建 FileORM、不自己写 file_location**。
// 有效写权限 = mcp.allow_write 且 令牌 allow_write，两个都开才会被 tools/list 列出。

// writeTools 写工具定义
func writeTools() []*Tool {
	return []*Tool{
		toolCreateFolder(),
		toolWriteTextFile(),
		toolRenameFile(),
		toolMoveFile(),
		toolDeleteFile(),
		toolRestoreFile(),
		toolUpdateTags(),
		toolCreateShareLink(),
		toolCancelShareLink(),
		toolCreateOfflineDownload(),
		toolSyncFileProviders(),
	}
}

// ===== create_folder =====

func toolCreateFolder() *Tool {
	return &Tool{
		Name:     "create_folder",
		Title:    "新建目录",
		ReadOnly: false,
		Source:   "core",
		Description: "按路径新建目录（默认逐级创建父目录，mkdir -p 语义）。目录已存在时直接返回它，" +
			"不报错（幂等）。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "目录路径，以 / 开头，如 /照片/2026"},
				"create_parents": {"type": "boolean", "description": "父目录不存在时是否一并创建，默认 true"}
			},
			"required": ["path"],
			"additionalProperties": false
		}`),
		Handler: func(ctx context.Context, call *Call) (*Result, error) {
			res, err := newResolver(call.Principal)
			if err != nil {
				return nil, err
			}
			segments, err := splitPath(call.StrOr("path", ""))
			if err != nil {
				return nil, err
			}
			if len(segments) == 0 {
				return nil, fmt.Errorf("path 不能是根目录")
			}
			createParents := call.BoolOr("create_parents", true)
			folderID, err := ensureFolders(ctx, res, segments, createParents)
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{
				"path":      "/" + strings.Join(segments, "/"),
				"file_id":   folderID,
				"is_folder": true,
			}), nil
		},
	}
}

// ensureFolders 逐段确保目录存在，返回叶子目录 id。
//
// createParents=false 时要求父目录已存在（只建最后一级）。
func ensureFolders(ctx context.Context, res *resolver, segments []string, createParents bool) (string, error) {
	currentID := res.rootID
	limit := len(segments)
	if !createParents && limit > 1 {
		parent, err := res.node("/" + strings.Join(segments[:limit-1], "/"))
		if err != nil {
			return "", err
		}
		currentID = fieldID(parent)
		return createChildFolder(ctx, currentID, segments[limit-1])
	}
	for index, segment := range segments {
		existing, err := db.FindFileByParentAndName(currentID, segment, res.ownerID)
		if err != nil {
			return "", fmt.Errorf("查询目录 %q 失败: %w", segment, err)
		}
		if existing != nil {
			if dirType(existing) != "folder" {
				return "", fmt.Errorf("%s 已存在且不是目录", "/"+strings.Join(segments[:index+1], "/"))
			}
			currentID = fieldID(existing)
			continue
		}
		currentID, err = createChildFolder(ctx, currentID, segment)
		if err != nil {
			return "", err
		}
	}
	return currentID, nil
}

// createChildFolder 在指定父目录下建一级目录
func createChildFolder(ctx context.Context, parentID, name string) (string, error) {
	created, merr := service.CreateDir(ctx, &native.CreateDirReq{
		Name:     proto.String(name),
		ParentId: proto.String(parentID),
	})
	if merr != nil {
		return "", fmt.Errorf("创建目录 %q 失败: %w", name, fromServiceErr(merr))
	}
	return created, nil
}

// ===== write_text_file =====

func toolWriteTextFile() *Tool {
	return &Tool{
		Name:     "write_text_file",
		Title:    "写入文本文件",
		ReadOnly: false,
		Source:   "core",
		Description: "把文本内容写成文件（父目录必须已存在；同名文件默认不覆盖）。" +
			"内容以 UTF-8 编码，服务端算 blake3 并按目录绑定分发到存储提供者。" +
			"只适合小文本（几百 KB 级）；大文件 / 二进制请走客户端上传。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "文件路径，以 / 开头，父目录必须已存在"},
				"content": {"type": "string", "description": "文件内容（UTF-8 文本）"},
				"overwrite": {"type": "boolean", "description": "同名文件是否覆盖，默认 false"}
			},
			"required": ["path", "content"],
			"additionalProperties": false
		}`),
		Handler: func(ctx context.Context, call *Call) (*Result, error) {
			res, err := newResolver(call.Principal)
			if err != nil {
				return nil, err
			}
			path := call.StrOr("path", "")
			dir, name, err := res.parent(path)
			if err != nil {
				return nil, err
			}
			content, _ := call.Str("content")
			body := []byte(content)
			hash := blake3.Sum256(body)
			result, merr := service.UploadSmall(ctx, &native.UploadSmallReq{
				Name:      proto.String(name),
				ParentId:  proto.String(fieldID(dir)),
				Size:      proto.Uint64(uint64(len(body))),
				Hash:      proto.String(hex.EncodeToString(hash[:])),
				Overwrite: proto.Bool(call.BoolOr("overwrite", false)),
				Content:   body,
			})
			if merr != nil {
				return nil, fromServiceErr(merr)
			}
			return jsonResult(map[string]any{
				"path":    res.DisplayPath(dir) + "/" + name,
				"file_id": result.GetFileId(),
				"size":    result.GetSize(),
				"exists":  result.GetExists(),
			}), nil
		},
	}
}

// ===== rename_file =====

func toolRenameFile() *Tool {
	return &Tool{
		Name:     "rename_file",
		Title:    "重命名",
		ReadOnly: false,
		Source:   "core",
		Description: "重命名文件或目录（只改元数据，不动存储提供者里的字节）。" +
			"同层重名会被拒绝，需要先改名或移走。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "原路径，以 / 开头"},
				"file_id": {"type": "string", "description": "原文件 ID（与 path 二选一，优先 file_id）"},
				"new_name": {"type": "string", "description": "新名字（不含路径分隔符）"}
			},
			"required": ["new_name"],
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
			if fieldID(file) == res.rootID {
				return nil, fmt.Errorf("不能重命名可见根目录")
			}
			newName := strings.TrimSpace(call.StrOr("new_name", ""))
			if newName == "" || strings.ContainsAny(newName, "/\\") {
				return nil, fmt.Errorf("new_name 不能为空，也不能包含路径分隔符")
			}
			if merr := service.RenameFile(ctx, &native.RenameFileReq{
				RenameObjs: []*native.RenameObj{{
					Id:      proto.String(fieldID(file)),
					NewName: proto.String(newName),
				}},
			}); merr != nil {
				return nil, fromServiceErr(merr)
			}
			parentPath := "/"
			if parent, parentErr := res.load(fieldParentID(file)); parentErr == nil {
				parentPath = res.DisplayPath(parent)
			}
			return jsonResult(map[string]any{
				"file_id":  fieldID(file),
				"new_name": newName,
				"path":     joinPath(parentPath, newName),
			}), nil
		},
	}
}

// ===== move_file =====

func toolMoveFile() *Tool {
	return &Tool{
		Name:     "move_file",
		Title:    "移动",
		ReadOnly: false,
		Source:   "core",
		Description: "把文件或目录移到另一个目录下（只改 parent_id，不改目录绑定、不补副本）。" +
			"目标目录必须已存在；目标下重名会被拒绝。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "源路径，以 / 开头"},
				"file_id": {"type": "string", "description": "源文件 ID（与 path 二选一，优先 file_id）"},
				"target_path": {"type": "string", "description": "目标目录路径，以 / 开头"}
			},
			"required": ["target_path"],
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
			if fieldID(file) == res.rootID {
				return nil, fmt.Errorf("不能移动可见根目录")
			}
			target, err := res.node(call.StrOr("target_path", ""))
			if err != nil {
				return nil, err
			}
			if dirType(target) != "folder" {
				return nil, fmt.Errorf("%s 不是目录", res.DisplayPath(target))
			}
			success, failed, messages, merr := service.MoveFile(ctx, &native.MoveFileReq{
				FileIds:        []string{fieldID(file)},
				TargetParentId: proto.String(fieldID(target)),
			})
			if merr != nil {
				return nil, fromServiceErr(merr)
			}
			if len(failed) > 0 {
				return nil, fmt.Errorf("移动失败: %s", firstMessage(messages))
			}
			payload := map[string]any{
				"moved": success,
				"path":  joinPath(res.DisplayPath(target), fieldName(file)),
			}
			return jsonResult(payload), nil
		},
	}
}

// ===== delete_file =====

func toolDeleteFile() *Tool {
	return &Tool{
		Name:        "delete_file",
		Title:       "删除（进回收站）",
		ReadOnly:    false,
		Destructive: true,
		Source:      "core",
		Description: "把文件或目录移入回收站（软删除，可恢复）。不会物理删除字节；回收站里的清理需要人工在客户端操作。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "路径，以 / 开头"},
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
			if fieldID(file) == res.rootID {
				return nil, fmt.Errorf("不能删除可见根目录")
			}
			displayPath := res.DisplayPath(file)
			if merr := service.DeleteFile(ctx, &native.DeleteFileReq{
				Ids: []string{fieldID(file)},
			}); merr != nil {
				return nil, fromServiceErr(merr)
			}
			return jsonResult(map[string]any{
				"path":        displayPath,
				"in_trash":    true,
				"recoverable": true,
			}), nil
		},
	}
}

// ===== restore_file =====

func toolRestoreFile() *Tool {
	return &Tool{
		Name:        "restore_file",
		ReadOnly:    false,
		Source:      "core",
		Title:       "从回收站恢复",
		Description: "把回收站里的文件恢复回原位置（原位置已被占用时会被拒绝）。file_id 取 trash_list 返回的 id。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {
				"file_id": {"type": "string", "description": "回收站里的文件 ID"}
			},
			"required": ["file_id"],
			"additionalProperties": false
		}`),
		Handler: func(ctx context.Context, call *Call) (*Result, error) {
			if _, err := newResolver(call.Principal); err != nil {
				return nil, err
			}
			fileID := strings.TrimSpace(call.StrOr("file_id", ""))
			if fileID == "" {
				return nil, fmt.Errorf("file_id 不能为空")
			}
			if merr := service.RestoreFromTrash(ctx, &native.RestoreFromTrashReq{
				Ids: []string{fileID},
			}); merr != nil {
				return nil, fromServiceErr(merr)
			}
			return jsonResult(map[string]any{"file_id": fileID, "restored": true}), nil
		},
	}
}

// ===== tags_update =====

func toolUpdateTags() *Tool {
	return &Tool{
		Name:        "update_tags",
		ReadOnly:    false,
		Source:      "core",
		Title:       "修改标签",
		Description: "整体替换文件的标签列表（传空数组等于清空）。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "路径，以 / 开头"},
				"file_id": {"type": "string", "description": "文件 ID（与 path 二选一，优先 file_id）"},
				"tags": {"type": "array", "items": {"type": "string"}, "description": "新的标签列表"}
			},
			"required": ["tags"],
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
			tags := call.StringSlice("tags")
			if merr := service.UpdateFileTags(ctx, &native.UpdateFileTagsReq{
				FileId: proto.String(fieldID(file)),
				Tags:   tags,
			}); merr != nil {
				return nil, fromServiceErr(merr)
			}
			return jsonResult(map[string]any{
				"file_id": fieldID(file),
				"tags":    tags,
			}), nil
		},
	}
}

// ===== share_link_create =====

func toolCreateShareLink() *Tool {
	return &Tool{
		Name:     "create_share_link",
		Title:    "创建分享链接",
		ReadOnly: false,
		Source:   "core",
		Description: "为文件 / 目录创建公开分享链接（默认 7 天有效）。链接是带签名的凭证：" +
			"只发给需要的人，不要写进日志或长期保存。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "路径，以 / 开头"},
				"file_id": {"type": "string", "description": "文件 ID（与 path 二选一，优先 file_id）"},
				"expire_seconds": {"type": "integer", "description": "有效秒数，默认 604800（7 天）"},
				"never_expire": {"type": "boolean", "description": "是否永久有效，默认 false"},
				"remark": {"type": "string", "description": "备注"}
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
			req := &native.CreateShareLinkReq{
				FileId:      proto.String(fieldID(file)),
				NeverExpire: proto.Bool(call.BoolOr("never_expire", false)),
				Remark:      proto.String(call.StrOr("remark", "")),
			}
			if expireSeconds := call.IntOr("expire_seconds", 0, 0); expireSeconds > 0 {
				req.ExpireSeconds = proto.Int64(int64(expireSeconds))
			}
			data, merr := service.CreateShareLink(ctx, req)
			if merr != nil {
				return nil, fromServiceErr(merr)
			}
			return jsonResult(map[string]any{
				"path":       res.DisplayPath(file),
				"share_code": data.GetShareCode(),
				"share_url":  data.GetShareUrl(),
				"expire_at":  data.GetExpireAt(),
			}), nil
		},
	}
}

// ===== share_link_cancel =====

func toolCancelShareLink() *Tool {
	return &Tool{
		Name:        "cancel_share_link",
		ReadOnly:    false,
		Destructive: true,
		Source:      "core",
		Title:       "取消分享",
		Description: "按分享码取消分享链接（立刻失效）。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {
				"share_code": {"type": "string", "description": "分享码（share_links_list 里的 share_code）"}
			},
			"required": ["share_code"],
			"additionalProperties": false
		}`),
		Handler: func(ctx context.Context, call *Call) (*Result, error) {
			if _, err := newResolver(call.Principal); err != nil {
				return nil, err
			}
			shareCode := strings.TrimSpace(call.StrOr("share_code", ""))
			if shareCode == "" {
				return nil, fmt.Errorf("share_code 不能为空")
			}
			if merr := service.CancelShareLink(ctx, &native.CancelShareLinkReq{
				ShareCode: proto.String(shareCode),
			}); merr != nil {
				return nil, fromServiceErr(merr)
			}
			return jsonResult(map[string]any{"share_code": shareCode, "canceled": true}), nil
		},
	}
}

// ===== offline_download_create =====

func toolCreateOfflineDownload() *Tool {
	return &Tool{
		Name:     "create_offline_download",
		Title:    "离线下载",
		ReadOnly: false,
		Source:   "core",
		Description: "让服务端下载一个 URL（http/https/ftp/magnet）到指定目录，产物由服务端入库。" +
			"需要管理员配置好离线下载后端；内网 / 回环地址会被服务端的 SSRF 校验拒绝。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {
				"url": {"type": "string", "description": "下载源 URL"},
				"target_path": {"type": "string", "description": "落库目录路径，默认根目录"},
				"file_name": {"type": "string", "description": "可选：指定文件名"}
			},
			"required": ["url"],
			"additionalProperties": false
		}`),
		Handler: func(ctx context.Context, call *Call) (*Result, error) {
			res, err := newResolver(call.Principal)
			if err != nil {
				return nil, err
			}
			target, err := res.node(call.StrOr("target_path", "/"))
			if err != nil {
				return nil, err
			}
			if dirType(target) != "folder" {
				return nil, fmt.Errorf("%s 不是目录", res.DisplayPath(target))
			}
			data, merr := service.CreateOfflineDownload(ctx, &native.CreateOfflineDownloadReq{
				Url:      proto.String(strings.TrimSpace(call.StrOr("url", ""))),
				ParentId: proto.String(fieldID(target)),
				FileName: proto.String(call.StrOr("file_name", "")),
			})
			if merr != nil {
				return nil, fromServiceErr(merr)
			}
			record := data.GetRecord()
			return jsonResult(map[string]any{
				"id":    record.GetId(),
				"state": record.GetState().String(),
				"url":   record.GetUrl(),
			}), nil
		},
	}
}

// ===== file_sync_providers =====

func toolSyncFileProviders() *Tool {
	return &Tool{
		Name:     "sync_file_providers",
		Title:    "补齐存储副本",
		ReadOnly: false,
		Source:   "core",
		Description: "把文件内容同步到指定存储提供者（补齐副本，用于容灾 / 加速）。" +
			"provider_ids 取 providers_list 返回的 id。",
		Schema: mustSchema(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "文件路径，以 / 开头"},
				"file_id": {"type": "string", "description": "文件 ID（与 path 二选一，优先 file_id）"},
				"provider_ids": {"type": "array", "items": {"type": "integer"}, "description": "目标存储提供者 id 列表"}
			},
			"required": ["provider_ids"],
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
				return nil, fmt.Errorf("%s 是目录", res.DisplayPath(file))
			}
			providerIDs := call.Uint32Slice("provider_ids")
			if len(providerIDs) == 0 {
				return nil, fmt.Errorf("provider_ids 不能为空")
			}
			if merr := service.SyncFileToProviders(ctx, &native.SyncFileToProvidersReq{
				FileId:      proto.String(fieldID(file)),
				ProviderIds: providerIDs,
			}); merr != nil {
				return nil, fromServiceErr(merr)
			}
			return jsonResult(map[string]any{
				"file_id":      fieldID(file),
				"provider_ids": providerIDs,
				"synced":       true,
			}), nil
		},
	}
}

// ===== 辅助 =====

// joinPath 拼路径（父路径为 "/" 时不重复斜杠）
func joinPath(parentPath, name string) string {
	if parentPath == "" || parentPath == "/" {
		return "/" + name
	}
	return parentPath + "/" + name
}

// firstMessage 取 map 里的第一条消息（失败原因给模型看）
func firstMessage(messages map[string]string) string {
	for _, message := range messages {
		return message
	}
	return "未知原因"
}
