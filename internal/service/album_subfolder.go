package service

import (
	"context"

	"fmt"
	"github.com/google/uuid"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
	"strconv"
)

const maxFilesPerSubfolder = 500

// parseSubfolderIndex 判断目录名是否为「编号目录」，并返回其序号。
//
// 识别规则：名称必须全部由 ASCII 数字组成（如 "01"、"001"、"00010"），
// 否则返回 ok=false。这样做的目的是兼容历史遗留的多种命名格式——
// 早期版本曾生成 "01"（两位）和 "00001"（前缀叠加补零）两种格式，
// 现存数据中仍大量存在；统一按「纯数字」识别可保证旧目录被正确复用与续号。
//
// 注意：拍照日期目录（如 "20260715"）虽然也是纯数字，但它由
// ensure-dir-path 按 "年/月" 结构创建，不会作为此处 parentID 的直接子目录
// 参与编号查找；且即便出现，也只会让续号从该数值起跳，不影响正确性。
func parseSubfolderIndex(name string) (int, bool) {
	if name == "" {
		return 0, false
	}
	num, err := strconv.Atoi(name)
	if err != nil {
		return 0, false
	}
	return num, true
}

// GetOrCreateSubfolder 获取有空位的编号子目录，都满则新建一个。
//
// 语义：在 req.ParentId 下查找「纯数字名称」且文件数未达 maxFilesPerSubfolder
// 的第一个目录；若都满或不存在，则按最大编号 +1 新建（格式 %03d，如 001、002）。
//
// 命名兼容：历史版本曾产生 01 / 00001 等格式，此处统一按「纯数字」识别，
// 因此旧目录仍会被正确复用与续号，不会重复创建。新目录一律使用 %03d。
func GetOrCreateSubfolder(ctx context.Context, req *native.GetOrCreateSubfolderReq) (*native.GetOrCreateSubfolderResp, *models.Err) {
	userID := getCurrentUserIDFromCtx(ctx)
	parentID := req.GetParentId()

	folders, err := db.GetSubfoldersByParentID(parentID, userID)
	if err != nil {
		llog.C(ctx).Errorf("get subfolders failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 查找第一个有空位的编号目录（按名称排序）
	for _, folder := range folders {
		if folder.Name == nil || folder.Id == nil {
			continue
		}
		if _, ok := parseSubfolderIndex(*folder.Name); !ok {
			continue
		}
		count, err := db.CountChildrenByParentID(*folder.Id, userID)
		if err != nil {
			llog.C(ctx).Errorf("count children failed: %v", err)
			continue
		}
		if count < maxFilesPerSubfolder {
			return &native.GetOrCreateSubfolderResp{FolderId: folder.Id}, nil
		}
	}

	// 没有合适目录：取最大编号 +1 新建
	maxNum := 0
	for _, folder := range folders {
		if folder.Name == nil {
			continue
		}
		if num, ok := parseSubfolderIndex(*folder.Name); ok && num > maxNum {
			maxNum = num
		}
	}
	newName := fmt.Sprintf("%03d", maxNum+1)

	// 从父目录继承属性
	parent, err := db.GetFileByID(parentID)
	if err != nil {
		llog.C(ctx).Errorf("get parent folder failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	newID := uuid.New().String()
	newFolder := &native.FileORM{
		Id:               &newID,
		Name:             &newName,
		ParentId:         &parentID,
		Type:             utils.EnumPtr(native.FileType_FOLDER),
		IsReady:          utils.BoolPtr(true),
		OwnerId:          utils.StringPtr(userID),
		EnableEncryption: parent.EnableEncryption,
		MaxHistories:     parent.MaxHistories,
	}
	// 继承父目录的存储提供者绑定（父目录未显式绑定时回退用户分配集）
	if binding := inheritBoundProviderIds(userID, parentID); binding != "" {
		newFolder.BoundProviderIds = utils.StringPtr(binding)
	}

	if err := db.CreateFile(newFolder); err != nil {
		llog.C(ctx).Errorf("create subfolder failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	llog.C(ctx).Infof("created subfolder %s (id=%s) under parent %s", newName, newID, parentID)
	return &native.GetOrCreateSubfolderResp{FolderId: &newID}, nil
}
