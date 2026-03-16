package db

import (
	"context"

	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"gorm.io/gorm"
)

// GetFileByID 根据id获取文件信息
func GetFileByID(id string) (*native.FileORM, error) {
	var f native.FileORM
	var e error
	if f, e = gorm.G[native.FileORM](db).Where("id = ?", id).First(context.Background()); e != nil {
		llog.GetLogger().Errorf("get file by id failed,id: %s,err: %v", id, e)
		return nil, e
	}
	return &f, e
}

// CreateDir 创建文件夹
func CreateDir(file *native.FileORM, dirConfig *native.DirConfigORM) error {
	ctx := context.Background()

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := gorm.G[native.FileORM](tx).Create(ctx, file); err != nil {
			llog.GetLogger().Errorf("create file failed, err: %v", err)
			return err
		}

		if err := gorm.G[native.DirConfigORM](tx).Create(ctx, dirConfig); err != nil {
			llog.GetLogger().Errorf("create dir config failed, err: %v", err)
			return err
		}
		return nil
	})
	return err
}

// DeleteFile 删除文件（软删除）
func DeleteFile(id string) error {
	ctx := context.Background()
	_, err := gorm.G[native.FileORM](db).Where("id = ?", id).Delete(ctx)
	return err
}

// DeleteFiles 批量删除文件（软删除）
func DeleteFiles(ids []string) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		if result := tx.Where("id IN ?", ids).Delete(&native.FileORM{}); result.Error != nil {
			llog.GetLogger().Errorf("delete files failed, err: %v", result.Error)
			return result.Error
		}
		return nil
	})
	return err
}

// GetFileList 获取文件列表
func GetFileList(parentID string) ([]native.FileORM, int64, error) {
	var files []native.FileORM
	var total int64

	if err := db.Model(&native.FileORM{}).Where("parent_id IS NULL OR parent_id = ?", parentID).Count(&total).Error; err != nil {
		llog.GetLogger().Errorf("count files failed, err: %v", err)
		return nil, 0, err
	}

	if err := db.Where("parent_id IS NULL OR parent_id = ?", parentID).Find(&files).Error; err != nil {
		llog.GetLogger().Errorf("get file list failed, err: %v", err)
		return nil, 0, err
	}

	return files, total, nil
}

// UpdateDir 更新文件夹配置
func UpdateDir(id string, req *native.UpdateDirReq) error {
	// 更新文件表中的名称
	updates := make(map[string]interface{})
	if req.Name != nil {
		updates["name"] = *req.Name
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		// 更新文件
		if len(updates) > 0 {
			if result := tx.Model(&native.FileORM{}).Where("id = ?", id).Updates(updates); result.Error != nil {
				llog.GetLogger().Errorf("update file failed, err: %v", result.Error)
				return result.Error
			}
		}

		// 更新文件夹配置
		dirUpdates := make(map[string]interface{})
		if req.EncryptName != nil {
			dirUpdates["encrypt_name"] = *req.EncryptName
		}
		if req.EncryptionSuffix != nil {
			dirUpdates["encryption_suffix"] = *req.EncryptionSuffix
		}
		if req.EncryptionKey != nil {
			dirUpdates["encryption_key"] = *req.EncryptionKey
		}
		if req.EncryptionSalt != nil {
			dirUpdates["encryption_salt"] = *req.EncryptionSalt
		}

		if len(dirUpdates) > 0 {
			if result := tx.Model(&native.DirConfigORM{}).Where("file_id = ?", id).Updates(dirUpdates); result.Error != nil {
				llog.GetLogger().Errorf("update dir config failed, err: %v", result.Error)
				return result.Error
			}
		}

		return nil
	})

	return err
}

// RenameFile 重命名文件
func RenameFile(id string, newName string) error {
	if result := db.Model(&native.FileORM{}).Where("id = ?", id).Update("name", newName); result.Error != nil {
		llog.GetLogger().Errorf("rename file failed, id: %s, err: %v", id, result.Error)
		return result.Error
	}
	return nil
}

// RenameFiles 批量重命名文件
func RenameFiles(renames []map[string]string) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		for _, r := range renames {
			id, ok := r["id"]
			newName, ok2 := r["new_name"]
			if !ok || !ok2 {
				continue
			}
			if result := tx.Model(&native.FileORM{}).Where("id = ?", id).Update("name", newName); result.Error != nil {
				llog.GetLogger().Errorf("rename file failed, id: %s, err: %v", id, result.Error)
				return result.Error
			}
		}
		return nil
	})

	return err
}

// CreateFile 创建文件记录
func CreateFile(file *native.FileORM) error {
	if result := db.Create(file); result.Error != nil {
		llog.GetLogger().Errorf("create file failed, err: %v", result.Error)
		return result.Error
	}
	return nil
}

// CheckFileExistsByHash 通过哈希检查文件是否存在
func CheckFileExistsByHash(hash string) (bool, string, error) {
	var f native.FileORM
	if err := db.Where("hash = ?", hash).First(&f).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return false, "", nil
		}
		return false, "", err
	}
	return true, *f.Id, nil
}
