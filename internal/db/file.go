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
	return gorm.G[*native.FileORM](db).Where("id=?", id).First(context.Background())
}

// GetFileByHash 获取文件
func GetFileByHash(hash string) (*native.FileORM, error) {
	return gorm.G[*native.FileORM](db).Where("hash=?", hash).First(context.Background())
}

// GetFileByParentAndName 获取文件
func GetFileByParentAndName(parentID string, name string) (*native.FileORM, error) {
	return gorm.G[*native.FileORM](db).Where("parent_id=? AND name=?", parentID, name).First(context.Background())
}

// CreateFile 创建文件
func CreateFile(file *native.FileORM) error {
	if file.Id == nil {
		uuid, _ := uuid.NewV7()
		file.Id = utils.StringPtr(uuid.String())
	}
	return db.FirstOrCreate(file).Error
}

// DeleteFiles 批量删除文件
func DeleteFiles(ids []string) error {
	_, err := gorm.G[native.FileORM](db).Where("id IN ?", ids).Delete(context.Background())
	return err

}

// GetFileList 获取文件列表
func GetFileList(parentID string) ([]*native.FileORM, int64, error) {
	var files []*native.FileORM
	var total int64
	var err error
	ctx := context.Background()
	gdb := gorm.G[*native.FileORM](db).Where("parent_id = ?", parentID)
	if total, err = gdb.Count(ctx, "*"); err != nil {
		return nil, 0, err
	}

	if files, err = gdb.Find(ctx); err != nil {
		return nil, 0, err
	}
	return files, total, nil
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
// func UpdateFile(file *native.FileORM) error {
// 	ctx := context.Background()
// 	_, err := gorm.G[*native.FileORM](db).Where("id = ?", *file.Id).Updates(ctx, file)

// 	return err

// }

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
