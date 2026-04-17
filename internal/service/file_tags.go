package service

import (
	"context"
	"errors"
	"strings"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"gorm.io/gorm"
)

// UpdateFileTagsImpl 更新文件标签的内部实现
func UpdateFileTagsImpl(ctx context.Context, req *native.UpdateFileTagsReq) *models.Err {
	if req.FileId == nil || *req.FileId == "" {
		return &models.Err{
			E: errors.New("file_id is required"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 1. 验证文件是否存在
	file, err := db.GetFileByID(*req.FileId)
	if err != nil {
		llog.L().Errorf("get file failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	if file == nil {
		return &models.Err{
			E: errors.New("file not found"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 2. 验证和清理标签：去重、过滤空字符串、限制数量
	cleanTags := validateAndCleanTags(req.Tags)
	if len(cleanTags) > 50 { // 限制最多50个标签
		cleanTags = cleanTags[:50]
	}

	// 3. 使用事务处理标签更新
	dbErr := db.Transaction(func(tx *gorm.DB) error {
		// 3.1 删除旧标签
		if err := db.DeleteFileTagsByFileIDTx(tx, *req.FileId); err != nil {
			return err
		}

		// 3.2 创建新标签
		if len(cleanTags) > 0 {
			if err := db.CreateFileTagsTx(tx, *req.FileId, cleanTags); err != nil {
				return err
			}
		}

		return nil
	})

	if dbErr != nil {
		llog.L().Errorf("update file tags transaction failed: %v", dbErr)
		return &models.Err{E: dbErr, C: native.RespCode_DB_ERROR}
	}

	llog.L().Infof("updated tags for file %s: %v", *req.FileId, cleanTags)
	return nil
}

// validateAndCleanTags 验证并清理标签列表
func validateAndCleanTags(tags []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, tag := range tags {
		// 去除首尾空白字符
		trimmedTag := strings.TrimSpace(tag)
		
		// 过滤空字符串和过长标签
		if trimmedTag != "" && len(trimmedTag) <= 50 { // 限制单个标签长度不超过50字符
			// 去重
			if !seen[trimmedTag] {
				seen[trimmedTag] = true
				result = append(result, trimmedTag)
			}
		}
	}

	return result
}

// GetFileTags 获取文件标签
func GetFileTags(ctx context.Context, req *native.GetFileTagsReq) (*native.GetFileTagsRespData, *models.Err) {
	if req.FileId == nil || *req.FileId == "" {
		return nil, &models.Err{
			E: errors.New("file_id is required"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 1. 验证文件是否存在
	file, err := db.GetFileByID(*req.FileId)
	if err != nil {
		llog.L().Errorf("get file failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	if file == nil {
		return nil, &models.Err{
			E: errors.New("file not found"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 2. 获取文件标签
	tags, err := db.GetFileTagsByFileID(*req.FileId)
	if err != nil {
		llog.L().Errorf("get file tags failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 3. 返回标签列表
	resp := &native.GetFileTagsRespData{}
	resp.Tags = tags
	return resp, nil
}
