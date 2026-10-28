package db

import (
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"gorm.io/gorm"
)

// CreateFileTags 创建文件标签
func CreateFileTags(fileID string, tags []string) error {
	if len(tags) == 0 {
		return nil
	}
	var tagRecords []entity.FileTagORM
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		tagRecords = append(tagRecords, entity.FileTagORM{
			FileId:  &fileID,
			TagName: &tag,
		})
	}
	if len(tagRecords) == 0 {
		return nil
	}
	return db.Create(&tagRecords).Error
}

// CreateFileTagsTx 在事务中创建文件标签
func CreateFileTagsTx(tx *gorm.DB, fileID string, tags []string) error {
	if len(tags) == 0 {
		return nil
	}
	var tagRecords []entity.FileTagORM
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		tagRecords = append(tagRecords, entity.FileTagORM{
			FileId:  &fileID,
			TagName: &tag,
		})
	}
	if len(tagRecords) == 0 {
		return nil
	}
	return tx.Create(&tagRecords).Error
}

// DeleteFileTagsByFileID 删除文件的所有标签
func DeleteFileTagsByFileID(fileID string) error {
	return db.Where("file_id = ?", fileID).Delete(&entity.FileTagORM{}).Error
}

// DeleteFileTagsByFileIDTx 在事务中删除文件的所有标签
func DeleteFileTagsByFileIDTx(tx *gorm.DB, fileID string) error {
	return tx.Where("file_id = ?", fileID).Delete(&entity.FileTagORM{}).Error
}

// GetFileTagsByFileID 获取文件的所有标签
func GetFileTagsByFileID(fileID string) ([]string, error) {
	var tags []entity.FileTagORM
	if err := db.Where("file_id = ?", fileID).Find(&tags).Error; err != nil {
		return nil, err
	}
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag.TagName != nil {
			result = append(result, *tag.TagName)
		}
	}
	return result, nil
}
