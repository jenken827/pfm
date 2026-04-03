package db

import (
	"context"
	"fmt"
	"strings"

	"jenken827.com/jenken827/pfm/proto/gen/native"
	"gorm.io/gorm"
)

// GetFiles 获取文件
func GetFiles(expression string, args ...any) ([]*native.FileORM, error) {
	return gorm.G[*native.FileORM](db).Where(expression, args...).Find(context.Background())
}

// CreateDir 创建文件夹
func CreateDir(file *native.FileORM) error {
	return db.Where(map[string]any{"parent_id": *file.ParentId, "name": *file.Name}).Attrs(file).FirstOrCreate(file).Error
}

// DeleteFiles 批量删除文件（软删除）
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
func UpdateDir(orm native.FileORM) error {
	ctx := context.Background()
	_, err := gorm.G[native.FileORM](db).Where("id = ?", *orm.Id).Updates(ctx, orm)
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

// CreateFile 创建文件记录
func CreateFile(file *native.FileORM) error {
	ctx := context.Background()
	return gorm.G[native.FileORM](db).Create(ctx, file)
}

// UpdateFile 更新文件记录
func UpdateFile(file *native.FileORM) error {
	ctx := context.Background()
	_, err := gorm.G[*native.FileORM](db).Where("id = ?", *file.Id).Updates(ctx, file)

	return err

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
