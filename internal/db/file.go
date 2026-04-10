package db

import (
	"context"
	"fmt"
	"strings"

	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GetFileByID 获取文件
func GetFileByID(id string) (*native.FileORM, error) {
	file, err := gorm.G[*native.FileORM](db).Where("id=?", id).First(context.Background())
	if err != nil {
		return nil, err
	}
	return file, nil
}

// GetFileByHash 获取文件
func GetFileByHash(hash string) (*native.FileORM, error) {
	file, err := gorm.G[*native.FileORM](db).Where("hash=?", hash).First(context.Background())
	if err != nil {
		return nil, err
	}
	return file, nil
}

// GetFileByParentAndName 获取文件
func GetFileByParentAndName(parentID string, name string) (*native.FileORM, error) {
	file, err := gorm.G[*native.FileORM](db).Where("parent_id=? AND name=?", parentID, name).First(context.Background())
	if err != nil {
		return nil, err
	}
	return file, nil
}

// CreateFile 创建文件
func CreateFile(file *native.FileORM) error {
	if file.Id == nil {
		uuid, _ := uuid.NewV7()
		file.Id = utils.StringPtr(uuid.String())
	}
	return db.Create(file).Error
}

// DeleteFiles 批量删除文件
func DeleteFiles(ids []string) error {
	_, err := gorm.G[native.FileORM](db).Where("id IN ?", ids).Delete(context.Background())
	return err

}

// GetFileList 获取文件列表
// GetFileList 获取文件列表（支持分页）
// parentID: 父目录ID
// offset: 偏移量
// pageSize: 每页数量，如果为0则使用默认值500
func GetFileList(parentID string, offset, pageSize uint32) ([]*native.FileORM, int64, bool, error) {
	var files []*native.FileORM
	var total int64
	var err error
	ctx := context.Background()

	// 默认每页500条
	if pageSize == 0 {
		pageSize = 500
	}

	// 获取总数
	gdb := gorm.G[*native.FileORM](db).Where("parent_id = ?", parentID)
	if total, err = gdb.Count(ctx, "*"); err != nil {
		return nil, 0, false, err
	}

	// 分页查询
	if files, err = gorm.G[*native.FileORM](db).
		Where("parent_id = ?", parentID).
		Offset(int(offset)).
		Limit(int(pageSize)).
		Find(ctx); err != nil {
		return nil, 0, false, err
	}

	// 判断是否有更多数据
	hasMore := int64(offset)+int64(len(files)) < total

	return files, total, hasMore, nil
}

// GetFolderList 获取指定目录下的所有文件夹
func GetFolderList(parentID string) ([]*native.FileORM, error) {
	var folders []*native.FileORM
	var err error
	ctx := context.Background()

	// 查询文件夹类型（FileType_FOLDER = 2）
	folderType := int32(native.FileType_FOLDER)
	if folders, err = gorm.G[*native.FileORM](db).
		Where("parent_id = ?", parentID).
		Where("type = ?", folderType).
		Order("name ASC").
		Find(ctx); err != nil {
		return nil, err
	}

	return folders, nil
}

// UpdateDir 更新文件夹配置
func UpdateDir(orm *native.FileORM) error {
	ctx := context.Background()
	fields := []string{}
	if orm.Name != nil {
		fields = append(fields, "name")
	}
	if orm.EnableEncryption != nil {
		fields = append(fields, "enable_encryption")
	}
	if orm.EncryptFilename != nil {
		fields = append(fields, "encrypt_filename")
	}
	if orm.BoundProviderIds != nil {
		fields = append(fields, "bound_provider_ids")
	}

	// GORM 泛型 API 的 Select 签名为 Select(query string, args ...interface{})
	// 需要正确展开字段列表
	if len(fields) == 0 {
		_, err := gorm.G[*native.FileORM](db).Where("id = ?", *orm.Id).Updates(ctx, orm)
		return err
	}
	// 将剩余字段转换为 []interface{}
	args := make([]any, len(fields)-1)
	for i, f := range fields[1:] {
		args[i] = f
	}
	_, err := gorm.G[*native.FileORM](db).Where("id = ?", *orm.Id).Select(fields[0], args...).Updates(ctx, orm)
	return err
}

// RenameFiles 批量重命名文件
func RenameFiles(renames []*native.RenameObj) error {
	if len(renames) == 0 {
		return nil
	}
	// 过滤有效数据
	var validRenames []*native.RenameObj
	for _, r := range renames {
		if r.GetId() != "" && r.GetNewName() != "" {
			validRenames = append(validRenames, r)
		}
	}
	if len(validRenames) == 0 {
		return nil
	}

	// 使用参数化查询构建 CASE WHEN 语句
	// 构建 CASE WHEN 部分的占位符: WHEN ? THEN ?
	var caseWhenParts []string
	var args []interface{}

	for _, r := range validRenames {
		caseWhenParts = append(caseWhenParts, "WHEN ? THEN ?")
		args = append(args, r.GetId(), r.GetNewName())
	}

	// 构建 WHERE IN 部分的占位符
	var wherePlaceholders []string
	for _, r := range validRenames {
		wherePlaceholders = append(wherePlaceholders, "?")
		args = append(args, r.GetId())
	}

	query := fmt.Sprintf(
		"UPDATE files SET name = CASE id %s END WHERE id IN (%s)",
		strings.Join(caseWhenParts, " "),
		strings.Join(wherePlaceholders, ","),
	)

	if err := db.Exec(query, args...).Error; err != nil {
		return err
	}
	return nil
}

// UpdateFile 更新文件记录
func UpdateFile(file *native.FileORM, updateFields []string) error {
	ctx := context.Background()
	if err := GetDB().WithContext(ctx).Model(file).Select(updateFields).Updates(file).Error; err != nil {
		return err
	}
	return nil
}

// UpdateFileWithHistory 更新文件记录并记录历史
func UpdateFileWithHistory(ctx context.Context, file *native.FileORM, oldFile *native.FileORM) error {

	err := db.Transaction(func(tx *gorm.DB) error {
		// Use Generics API inside the transaction
		if _, err := gorm.G[*native.FileORM](tx).Where("id=?", *file.Id).Select("*").Updates(ctx, file); err != nil {
			return err
		}
		history := &native.FileHistoryORM{
			FileId:        file.Id,
			Hash:          oldFile.Hash,
			Size:          oldFile.Size,
			VersionNumber: oldFile.UpdatedAt,
		}

		if err := gorm.G[native.FileHistoryORM](tx).Create(ctx, history); err != nil {
			return err
		}

		return nil
	})
	return err
}

// UpdateFileIsReady 更新文件is_ready状态
// 通过parent_id和file_name定位文件，设置is_ready为true
func UpdateFileIsReady(parentID, fileName string) error {
	ctx := context.Background()
	isReady := true
	_, err := gorm.G[*native.FileORM](db).
		Where("parent_id = ? AND name = ?", parentID, fileName).
		Update(ctx, "is_ready", isReady)
	return err
}
