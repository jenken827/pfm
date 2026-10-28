package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"gorm.io/gorm"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/utils"
)

// errInvalidDirPath 目录路径非法（路径穿越/非法字符）
var errInvalidDirPath = errors.New("invalid directory path")

// EnsureDirPath 在 req.ParentId 下按相对路径逐级确保目录存在（mkdir -p 语义）
// req.Path 形如 "2026/07"，按 "/" 分割逐级查找/创建，返回叶子目录 ID。
//
// 并发安全：files 表存在 (owner_id, name, parent_id) partial unique index
// （WHERE deleted_at IS NULL），多个客户端并发创建同名目录时，后到者
// 触发唯一键冲突，捕获后重新查询返回已存在的目录。
func EnsureDirPath(ctx context.Context, req *native.EnsureDirPathReq) (*native.EnsureDirPathResp, *models.Err) {
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return nil, &models.Err{E: errors.New("unauthorized"), C: native.RespCode_TOKEN_INVALID}
	}
	folderID, err := ensureDirPathForOwner(userID, req.GetParentId(), req.GetPath())
	if err == nil {
		return &native.EnsureDirPathResp{FolderId: &folderID}, nil
	}
	if errors.Is(err, errInvalidDirPath) {
		return nil, &models.Err{E: err, C: native.RespCode_PARAMS_ERROR}
	}
	return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
}

// ensureDirPathForOwner 按显式 ownerID 确保目录存在（无 context 场景：CLI 数据修复等）
func ensureDirPathForOwner(userID, parentID, relPath string) (string, error) {
	if userID == "" {
		return "", errors.New("unauthorized")
	}

	// 校验路径：只允许安全字符（字母/数字/下划线/中划线/点），
	// 拒绝空段与 "."、".."（防路径穿越）
	segments := strings.Split(relPath, "/")
	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." || !isSafeDirSegment(seg) {
			return "", fmt.Errorf("%w: %s", errInvalidDirPath, relPath)
		}
	}

	// 取顶层父目录，继承其加密/历史版本属性
	parent, err := db.GetFileByID(parentID)
	if err != nil {
		return "", err
	}

	currentID := parentID
	for _, seg := range segments {
		dir, err := db.FindFileByParentAndName(currentID, seg, userID)
		if err != nil {
			return "", err
		}
		if dir != nil && dir.Id != nil {
			currentID = *dir.Id
			continue
		}

		// 目录不存在，创建（并发冲突时唯一索引兜底）
		newID := uuid.New().String()
		newDir := &entity.FileORM{
			Id:               utils.StringPtr(newID),
			Name:             utils.StringPtr(seg),
			ParentId:         utils.StringPtr(currentID),
			Type:             utils.EnumPtr(native.FileType_FOLDER),
			IsReady:          utils.BoolPtr(true),
			OwnerId:          utils.StringPtr(userID),
			EnableEncryption: parent.EnableEncryption,
			MaxHistories:     parent.MaxHistories,
		}
		// 继承父目录的存储提供者绑定（父目录未显式绑定时回退用户分配集）
		if binding := inheritBoundProviderIds(userID, currentID); binding != "" {
			newDir.BoundProviderIds = utils.StringPtr(binding)
		}
		if err := db.CreateFile(newDir); err != nil {
			// 并发创建同名目录 → 唯一键冲突，重查已存在的目录
			if isUniqueViolation(err) {
				dir, qerr := db.FindFileByParentAndName(currentID, seg, userID)
				if qerr == nil && dir != nil && dir.Id != nil {
					currentID = *dir.Id
					continue
				}
			}
			llog.L().Errorf("ensure dir: create folder %s under %s failed: %v", seg, currentID, err)
			return "", err
		}
		llog.L().Infof("ensure dir: created folder %s (id=%s) under %s", seg, newID, currentID)
		currentID = newID
	}

	return currentID, nil
}

// isSafeDirSegment 校验目录名段：仅字母/数字/下划线/中划线/点
func isSafeDirSegment(seg string) bool {
	for _, r := range seg {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.'
		if !ok {
			return false
		}
	}
	return true
}

// isUniqueViolation 判断错误是否为唯一键冲突（SQLite / PostgreSQL / MySQL）
//
// 优先用 gorm 的错误翻译（db.Init 里开了 TranslateError），文案匹配只作兜底。
func isUniqueViolation(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "duplicate key value violates unique constraint") ||
		strings.Contains(msg, "Duplicate entry")
}
