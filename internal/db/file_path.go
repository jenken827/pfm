package db

import (
	"errors"
	"strings"

	"gorm.io/gorm"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// FilePathResolver 在单次请求内缓存文件行，供「目录路径回溯」复用。
//
// 用途：把 file_id 渲染成可读路径（如 /readany/abc）用于展示。回溯是逐级查询，
// 同一祖先目录可能被多个条目共享（例如同一用户的多个 WebDAV 账号指向同一分支），
// 缓存后 N 个条目 × 深度次查询会去重成「唯一祖先数」次查询。
// 注意：不要跨请求复用（缓存不失效、不带过期语义）。
type FilePathResolver struct {
	cache map[string]*native.FileORM
}

// NewFilePathResolver 创建请求级路径解析器
func NewFilePathResolver() *FilePathResolver {
	return &FilePathResolver{cache: make(map[string]*native.FileORM)}
}

// get 取文件行；不存在/已软删（gorm.ErrRecordNotFound）缓存为 nil，避免重复查询
func (r *FilePathResolver) get(fileID string) (*native.FileORM, error) {
	if file, ok := r.cache[fileID]; ok {
		return file, nil
	}
	file, err := GetFileByID(fileID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			r.cache[fileID] = nil
			return nil, nil
		}
		return nil, err
	}
	r.cache[fileID] = file
	return file, nil
}

// DirPath 由 fileID 向上回溯，拼出以 stopAtID 为根的可读路径。
//
//   - fileID == stopAtID → "/"
//   - stopAtID 为空 → 一直回到顶层（parent_id 为空）再拼，形如 "/a/b"
//   - 中途遇到不存在的节点（软删/脏数据）→ 返回已拼出的部分，不报错
//   - 深度上限 maxAncestorDepth，防止脏数据造成的父子环
func (r *FilePathResolver) DirPath(fileID, stopAtID string) (string, error) {
	if fileID == "" {
		return "", nil
	}
	if stopAtID != "" && fileID == stopAtID {
		return "/", nil
	}

	names := make([]string, 0, 4)
	current := fileID
	for depth := 0; depth < maxAncestorDepth; depth++ {
		if current == "" || current == "0" {
			break
		}
		file, err := r.get(current)
		if err != nil {
			return "", err
		}
		if file == nil {
			// 节点缺失（已软删）：只可能发生在起始节点，返回空串让调用方标注「已失效」
			if current == fileID {
				return "", nil
			}
			break
		}
		if file.Name != nil && *file.Name != "" {
			names = append(names, *file.Name)
		}
		if file.ParentId == nil || *file.ParentId == "" || *file.ParentId == "0" {
			break
		}
		if stopAtID != "" && *file.ParentId == stopAtID {
			break // 到达指定根，不再把根自身的名字（用户根目录名是 UUID）拼进路径
		}
		current = *file.ParentId
	}

	// names 自下而上收集，反转后拼接
	for i, j := 0, len(names)-1; i < j; i, j = i+1, j-1 {
		names[i], names[j] = names[j], names[i]
	}
	return "/" + strings.Join(names, "/"), nil
}
