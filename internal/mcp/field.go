package mcp

import (
	"time"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
)

// ORM 实体是生成物，字段全是指针（optional 标量生成指针）。
// 这里集中做 nil 安全取值，免得每个工具里重复写 `if f.Id != nil`。
//
// 传 nil 一律返回零值：调用方（路径解析 / 工具）已经保证非空，多一层兜底
// 是为了让"偶发的空记录"退化成可读错误，而不是 panic。

func fieldID(file *entity.FileORM) string {
	if file == nil || file.Id == nil {
		return ""
	}
	return *file.Id
}

func fieldName(file *entity.FileORM) string {
	if file == nil || file.Name == nil {
		return ""
	}
	return *file.Name
}

func fieldParentID(file *entity.FileORM) string {
	if file == nil || file.ParentId == nil {
		return ""
	}
	return *file.ParentId
}

func fieldType(file *entity.FileORM) int32 {
	if file == nil || file.Type == nil {
		return 0
	}
	return *file.Type
}

func fieldOwnerID(file *entity.FileORM) string {
	if file == nil || file.OwnerId == nil {
		return ""
	}
	return *file.OwnerId
}

func fieldSize(file *entity.FileORM) uint64 {
	if file == nil || file.Size == nil {
		return 0
	}
	return *file.Size
}

func fieldMime(file *entity.FileORM) string {
	if file == nil || file.Mime == nil {
		return ""
	}
	return *file.Mime
}

func fieldHash(file *entity.FileORM) string {
	if file == nil || file.Hash == nil {
		return ""
	}
	return *file.Hash
}

func fieldReady(file *entity.FileORM) bool {
	if file == nil || file.IsReady == nil {
		return false
	}
	return *file.IsReady
}

func fieldEncrypted(file *entity.FileORM) bool {
	if file == nil || file.EnableEncryption == nil {
		return false
	}
	return *file.EnableEncryption
}

func fieldLastAccessedAt(file *entity.FileORM) *time.Time {
	if file == nil {
		return nil
	}
	return file.LastAccessedAt
}
