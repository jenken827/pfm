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

// GetFilesByIDs 批量获取文件
func GetFilesByIDs(ids []string) ([]*native.FileORM, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	files, err := gorm.G[*native.FileORM](db).Where("id IN ?", ids).Find(context.Background())
	if err != nil {
		return nil, err
	}
	return files, nil
}

// GetFilesByIDsUnscoped 批量获取文件（包含软删除的记录）
func GetFilesByIDsUnscoped(ids []string) ([]*native.FileORM, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ctx := context.Background()
	var files []*native.FileORM
	if err := db.WithContext(ctx).Unscoped().Where("id IN ?", ids).Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
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

// DeleteFiles 批量删除文件（软删除，移至回收站）
func DeleteFiles(ids []string) error {
	_, err := gorm.G[native.FileORM](db).Where("id IN ?", ids).Delete(context.Background())
	return err
}

// RestoreFromTrash 从回收站恢复文件
func RestoreFromTrash(ids []string) error {
	ctx := context.Background()
	return db.WithContext(ctx).Unscoped().Model(&native.FileORM{}).
		Where("id IN ?", ids).
		Update("deleted_at", nil).Error
}

// GetTrashList 获取回收站文件列表（分页）
// 只显示顶级被删除的文件：parent_id 指向未删除的文件夹
func GetTrashList(offset, pageSize uint32) ([]*native.FileORM, int64, error) {
	ctx := context.Background()

	// 默认每页500条
	if pageSize == 0 {
		pageSize = 500
	}

	// 获取总数
	var total int64
	if err := db.WithContext(ctx).Model(&native.FileORM{}).
		Unscoped(). // 查询包含软删除的记录
		Where("deleted_at IS NOT NULL").
		Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询 - 显示所有有软删除标记的文件
	// 因为删除文件夹时只会标记文件夹本身，所以有软删除标记的都是用户显式删除的
	var files []*native.FileORM
	if err := db.WithContext(ctx).
		Unscoped(). // 查询包含软删除的记录
		Where("deleted_at IS NOT NULL").
		Offset(int(offset)).
		Limit(int(pageSize)).
		Order("deleted_at DESC").
		Find(&files).Error; err != nil {
		return nil, 0, err
	}

	return files, total, nil
}

// DeletePermanently 彻底删除文件（从回收站永久删除，删除实际文件和数据库记录）
func DeletePermanently(ids []string) error {
	ctx := context.Background()
	return db.WithContext(ctx).
		Unscoped(). // 永久删除，绕过软删除
		Where("id IN ?", ids).
		Delete(&native.FileORM{}).Error
}

// GetAllChildrenIDs 递归获取文件夹下的所有子文件和子文件夹的ID（包括软删除的）
func GetAllChildrenIDs(parentIDs []string) ([]string, error) {
	if len(parentIDs) == 0 {
		return nil, nil
	}

	ctx := context.Background()
	allIDs := make([]string, 0)

	// 使用 BFS 遍历所有子节点
	queue := make([]string, len(parentIDs))
	copy(queue, parentIDs)

	for len(queue) > 0 {
		// 取出当前层的所有父ID
		currentParents := queue
		queue = make([]string, 0)

		// 查询这些父ID下的所有子文件和子文件夹（包括软删除的）
		var children []*native.FileORM
		if err := db.WithContext(ctx).Unscoped().
			Model(&native.FileORM{}).
			Where("parent_id IN ?", currentParents).
			Find(&children).Error; err != nil {
			return nil, err
		}

		// 收集子项ID，并添加到队列中继续处理
		for _, child := range children {
			if child.Id != nil {
				allIDs = append(allIDs, *child.Id)
				// 如果是文件夹，添加到队列继续处理
				if child.Type != nil && *child.Type == int32(native.FileType_FOLDER) {
					queue = append(queue, *child.Id)
				}
			}
		}
	}

	return allIDs, nil
}

// EmptyTrash 清空回收站
func EmptyTrash() error {
	ctx := context.Background()
	return db.WithContext(ctx).
		Unscoped(). // 永久删除，绕过软删除
		Where("deleted_at IS NOT NULL").
		Delete(&native.FileORM{}).Error
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

	if orm.BoundProviderIds != nil {
		fields = append(fields, "bound_provider_ids")
	}

	if len(fields) == 0 {
		_, err := gorm.G[*native.FileORM](db).Where("id = ?", *orm.Id).Updates(ctx, orm)
		return err
	}

	// 使用 map 进行更新，避免 GORM 的零值检查问题
	updateMap := make(map[string]interface{})
	for _, field := range fields {
		switch field {
		case "name":
			updateMap["name"] = *orm.Name
		case "enable_encryption":
			updateMap["enable_encryption"] = *orm.EnableEncryption
		case "bound_provider_ids":
			updateMap["bound_provider_ids"] = *orm.BoundProviderIds
		}
	}

	result := db.WithContext(ctx).Model(&native.FileORM{}).Where("id = ?", *orm.Id).Updates(updateMap)
	return result.Error
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

	// 使用 map 进行更新，避免 GORM 的零值检查问题
	updateMap := make(map[string]interface{})
	for _, field := range updateFields {
		switch field {
		case "name":
			updateMap["name"] = *file.Name
		case "hash":
			updateMap["hash"] = *file.Hash
		case "size":
			updateMap["size"] = *file.Size
		case "mime":
			updateMap["mime"] = *file.Mime
		case "type":
			updateMap["type"] = *file.Type
		case "parent_id":
			updateMap["parent_id"] = *file.ParentId
		case "is_ready":
			updateMap["is_ready"] = *file.IsReady
		case "enable_encryption":
			updateMap["enable_encryption"] = *file.EnableEncryption
		case "bound_provider_ids":
			updateMap["bound_provider_ids"] = *file.BoundProviderIds
		case "max_histories":
			updateMap["max_histories"] = *file.MaxHistories
		}
	}

	if err := GetDB().WithContext(ctx).Model(&native.FileORM{}).Where("id = ?", *file.Id).Updates(updateMap).Error; err != nil {
		return err
	}
	return nil
}

// UpdateFileWithHistory 更新文件记录并记录历史
func UpdateFileWithHistory(ctx context.Context, file *native.FileORM, oldFile *native.FileORM) error {

	err := db.Transaction(func(tx *gorm.DB) error {
		// 使用 map 进行更新，避免 GORM 的零值检查问题
		updateMap := map[string]interface{}{
			"hash": *file.Hash,
			"size": *file.Size,
		}
		if err := tx.WithContext(ctx).Model(&native.FileORM{}).Where("id = ?", *file.Id).Updates(updateMap).Error; err != nil {
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

// UpdateFileSimple 简单更新文件（不创建历史）
func UpdateFileSimple(file *native.FileORM) error {
	ctx := context.Background()
	// 使用 map 进行更新，避免 GORM 的零值检查问题
	updateMap := map[string]interface{}{
		"hash": *file.Hash,
		"size": *file.Size,
	}
	result := db.WithContext(ctx).Model(&native.FileORM{}).Where("id = ?", *file.Id).Updates(updateMap)
	return result.Error
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

// UpdateFileLocationStatus 批量更新文件位置记录状态
func UpdateFileLocationStatus(ids []uint64, status int32) error {
	if len(ids) == 0 {
		return nil
	}

	ctx := context.Background()
	_, err := gorm.G[*native.FileLocationORM](db).
		Where("id IN ?", ids).
		Update(ctx, "status", status)
	return err
}

// GetFileHistoryByFileID 根据文件 ID 获取历史记录
func GetFileHistoryByFileID(fileID string) ([]*native.FileHistoryORM, error) {
	ctx := context.Background()
	var histories []*native.FileHistoryORM
	err := db.WithContext(ctx).Where("file_id = ?", fileID).Find(&histories).Error
	if err != nil {
		return nil, err
	}
	return histories, nil
}

// DeleteFileHistoryByFileIDs 批量删除文件历史记录
func DeleteFileHistoryByFileIDs(fileIDs []string) error {
	if len(fileIDs) == 0 {
		return nil
	}

	return db.Where("file_id IN ?", fileIDs).Delete(&native.FileHistoryORM{}).Error
}

// GetFileHistoryByID 根据历史记录 ID 获取历史记录
func GetFileHistoryByID(id uint64) (*native.FileHistoryORM, error) {
	ctx := context.Background()
	var history native.FileHistoryORM
	err := db.WithContext(ctx).Where("id = ?", id).First(&history).Error
	if err != nil {
		return nil, err
	}
	return &history, nil
}

// DeleteFileHistoryByIDs 批量删除历史记录（根据 ID）
func DeleteFileHistoryByIDs(ids []uint64) error {
	if len(ids) == 0 {
		return nil
	}

	return db.Where("id IN ?", ids).Delete(&native.FileHistoryORM{}).Error
}

// DeleteFileHistory 删除单个历史记录
func DeleteFileHistory(id uint64) error {
	if id == 0 {
		return nil
	}

	return db.Where("id = ?", id).Delete(&native.FileHistoryORM{}).Error
}

// DeleteFileLocationsByBlake3Hash 根据 blake3_hash 删除文件位置记录
func DeleteFileLocationsByBlake3Hash(hash string) error {
	if hash == "" {
		return nil
	}

	return db.Where("blake3_hash = ?", hash).Delete(&native.FileLocationORM{}).Error
}

// CountActiveFilesByHashExcludingIDs 统计引用指定 hash 的未删除文件数量（排除指定 ID）
// 用于判断 file_location 记录是否可以安全删除
func CountActiveFilesByHashExcludingIDs(hash string, excludeIDs []string) (int64, error) {
	if hash == "" {
		return 0, nil
	}

	ctx := context.Background()
	var count int64
	query := db.WithContext(ctx).Model(&native.FileORM{}).
		Where("hash = ?", hash)
	if len(excludeIDs) > 0 {
		query = query.Where("id NOT IN ?", excludeIDs)
	}
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
