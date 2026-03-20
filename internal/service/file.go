package service

import (
	"context"
	"errors"
	"path"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GetFileList 获取文件列表
func GetFileList(ctx context.Context, req *native.FileListReq) ([]*native.File, int64, *models.Err) {
	files, total, err := db.GetFileList(*req.ParentId)
	if err != nil {
		return nil, 0, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	var result []*native.File
	for _, f := range files {
		pb, err := f.ToPB(ctx)
		if err != nil {
			llog.GetLogger().Errorf("convert file to pb failed, err: %v", err)
			continue
		}
		result = append(result, &pb)
	}

	return result, total, nil
}

// CreateDir 创建文件夹
func CreateDir(ctx context.Context, req *native.CreateDirReq) (*native.File, *models.Err) {
	dir := "/"
	if *req.ParentId != "" {
		parentDir, err := db.GetFileByID(*req.ParentId)
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, &models.Err{E: errors.New("parent dir not exists"), Code: native.RespCode_PARAMS_ERROR}
			}
			return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
		}
		dir = path.Join(*parentDir.Dir, *parentDir.Name)
	}
	uuid, err := uuid.NewV7()
	if err != nil {
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}
	id := uuid.String()
	t := int32(native.FileType_FOLDER.Number())
	f := &native.FileORM{
		Name:     req.Name,
		ParentId: req.ParentId,
		Dir:      &dir,
		Id:       &id,
		Type:     &t,
	}
	dp := path.Join(dir, *req.Name)
	fc := &native.DirConfigORM{
		EnableEncryption: req.EnableEncryption,
		EncryptName:      req.EncryptName,
		EncryptionKey:    req.EncryptionKey,
		EncryptionSalt:   req.EncryptionSalt,
		EncryptionSuffix: req.EncryptionSuffix,
		Path:             &dp,
		FileId:           &id,
	}
	e := db.CreateDir(f, fc)
	if e != nil {
		return nil, &models.Err{E: e, Code: native.RespCode_INTERNAL_ERROR}
	}
	ff, err := f.ToPB(context.Background())
	if err != nil {
		llog.GetLogger().Errorf("convert file to pb failed, err: %v", err)
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}
	return &ff, nil
}

// UpdateDir 更新文件夹
func UpdateDir(ctx context.Context, req *native.UpdateDirReq) *models.Err {
	if err := db.UpdateDir(*req.Id, req); err != nil {
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}
	return nil
}

// DeleteFile 删除文件
func DeleteFile(ctx context.Context, req *native.DeleteFileReq) *models.Err {
	if err := db.DeleteFiles(req.Ids); err != nil {
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}
	return nil
}

// RenameFile 重命名文件
func RenameFile(ctx context.Context, req *native.RenameFileReq) *models.Err {
	renames := make([]map[string]string, 0, len(req.RenameObjs))
	for _, obj := range req.RenameObjs {
		renames = append(renames, map[string]string{
			"id":       *obj.Id,
			"new_name": *obj.NewName,
		})
	}

	if err := db.RenameFiles(renames); err != nil {
		return &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}
	return nil
}

// UploadFile 文件上传
func UploadFile(ctx context.Context, metadata *native.UploadMetadata, chunks [][]byte) (*native.UploadRespData, *models.Err) {
	// 检查文件是否已存在（通过哈希）
	if metadata.Hash != nil && *metadata.Hash != "" {
		exists, fileID, err := db.CheckFileExistsByHash(*metadata.Hash)
		if err != nil {
			return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
		}
		if exists {
			return &native.UploadRespData{
				FileId: &fileID,
				Exists: func() *bool { v := true; return &v }(),
			}, nil
		}
	}

	// 生成文件ID
	id, err := uuid.NewV7()
	if err != nil {
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	// 合并文件内容
	var totalSize uint64
	var content []byte
	for _, chunk := range chunks {
		content = append(content, chunk...)
		totalSize += uint64(len(chunk))
	}

	// 构建文件对象
	dir := "/"
	if metadata.ParentId != nil && *metadata.ParentId != "" {
		parentDir, err := db.GetFileByID(*metadata.ParentId)
		if err != nil {
			if err != gorm.ErrRecordNotFound {
				return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
			}
		} else {
			dir = path.Join(*parentDir.Dir, *parentDir.Name)
		}
	}

	t := int32(native.FileType_TYPE_UNSPECIFIED.Number())
	f := &native.FileORM{
		Id:       func() *string { v := id.String(); return &v }(),
		Name:     metadata.Name,
		ParentId: metadata.ParentId,
		Dir:      &dir,
		Size:     &totalSize,
		Hash:     metadata.Hash,
		Type:     &t,
		Mime:     metadata.Mime,
	}

	// 保存文件到数据库
	if err := db.CreateFile(f); err != nil {
		return nil, &models.Err{E: err, Code: native.RespCode_INTERNAL_ERROR}
	}

	// TODO: 实际保存文件内容到存储介质

	result := &native.UploadRespData{
		FileId: func() *string { v := id.String(); return &v }(),
		Exists: func() *bool { v := false; return &v }(),
	}

	return result, nil
}
