package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

// GetFileByID 获取文件
func GetFileByID(id string) (*native.FileORM, error) {
	file, err := gorm.G[*native.FileORM](db).Where("id=?", id).First(context.Background())
	if err != nil {
		return nil, err
	}
	return file, nil
}

// maxAncestorDepth 祖先链回溯上限，防止脏数据造成的父子环导致死循环
const maxAncestorDepth = 256

// GetNearestBoundProviderIds 沿 parent_id 祖先链向上查找最近一个「显式绑定」了
// 存储提供者的文件夹，返回其 bound_provider_ids（JSON 数组）。
//
// 语义（见 DESIGN.md「存储提供者绑定策略」）：
//   - 从 fileID 自身开始向上回溯；fileID 可以是文件也可以是文件夹
//     （文件自身永远为空值，等价于从父目录开始）；
//   - 链上全部为空（'[]'、空串、NULL）时返回 (nil, nil)，由调用方回退到
//     用户分配集 UserSpace.bound_provider_ids；
//   - JSON 解析失败属脏数据：打告警日志并按「未绑定」继续向上，不中断业务。
func GetNearestBoundProviderIds(fileID string) ([]uint32, error) {
	if fileID == "" || fileID == "0" {
		return nil, nil
	}

	current := fileID
	for depth := 0; depth < maxAncestorDepth; depth++ {
		if current == "" || current == "0" {
			break
		}
		file, err := GetFileByID(current)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// 祖先链断在软删/不存在的节点上，视作未绑定
				return nil, nil
			}
			return nil, err
		}
		if file.BoundProviderIds != nil && *file.BoundProviderIds != "" && *file.BoundProviderIds != "[]" {
			var ids []uint32
			if err := json.Unmarshal([]byte(*file.BoundProviderIds), &ids); err != nil {
				llog.L().Warnf("invalid bound_provider_ids on file %s: %v", current, err)
			} else if len(ids) > 0 {
				return ids, nil
			}
		}
		if file.ParentId == nil {
			break
		}
		current = *file.ParentId
	}

	return nil, nil
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

// GetFileByHash 获取文件（全局，任意 owner）
func GetFileByHash(hash string) (*native.FileORM, error) {
	file, err := gorm.G[*native.FileORM](db).Where("hash=?", hash).First(context.Background())
	if err != nil {
		return nil, err
	}
	return file, nil
}

// GetFileByHashAndOwner 获取文件（限定 owner，用于复用判断）
// 只查自己名下的同 hash 记录，避免全局 First 命中其他用户的记录
func GetFileByHashAndOwner(hash, ownerID string) (*native.FileORM, error) {
	file, err := gorm.G[*native.FileORM](db).
		Where("hash=? AND owner_id=?", hash, ownerID).
		First(context.Background())
	if err != nil {
		return nil, err
	}
	return file, nil
}

// GetFileByParentAndName 获取文件
func GetFileByParentAndName(parentID, name, ownerID string) (*native.FileORM, error) {
	file, err := gorm.G[*native.FileORM](db).Where("parent_id=? AND name=? AND owner_id=?", parentID, name, ownerID).First(context.Background())
	if err != nil {
		return nil, err
	}
	return file, nil
}

// FindFileByParentAndName 按父目录+名称查询文件（"是否存在"类判断用）
// 与 GetFileByParentAndName 的区别：不存在时返回 (nil, nil) 而不是
// gorm.ErrRecordNotFound，避免 GORM 默认 logger 打印大量 record not found 噪音
// （高频存在性检查场景：目录逐级查找、同名冲突检测等）
func FindFileByParentAndName(parentID, name, ownerID string) (*native.FileORM, error) {
	var files []*native.FileORM
	if err := db.Where("parent_id=? AND name=? AND owner_id=?", parentID, name, ownerID).
		Find(&files).Error; err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}
	return files[0], nil
}

// ResolveNameConflict 目标目录里已有同名文件时给出不冲突的名字，返回空串表示原名可用。
//
// 判定依据是 (owner_id, name, parent_id) 上的**部分**唯一索引（WHERE deleted_at IS NULL，
// 见 db.Init），所以回收站里的同名文件不算冲突，这里也走默认作用域查询。
// 命名规则与客户端一致：`原名 (1).扩展名`、`原名 (2).扩展名`…；999 个都冲突时用随机后缀兜底。
func ResolveNameConflict(parentID, name, ownerID string) string {
	existing, err := FindFileByParentAndName(parentID, name, ownerID)
	if err != nil || existing == nil {
		return "" // 无冲突（查询失败也放行，交给唯一索引兜底并报错）
	}

	ext := filepath.Ext(name)
	baseName := strings.TrimSuffix(name, ext)
	for i := 1; i <= 999; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", baseName, i, ext)
		conflict, qErr := FindFileByParentAndName(parentID, candidate, ownerID)
		if qErr != nil || conflict == nil {
			return candidate
		}
	}
	return fmt.Sprintf("%s_%s%s", baseName, uuid.New().String()[:8], ext)
}

// GetFileNamesInParent 获取指定父目录下当前用户的「文件名 -> file」映射（单条 SQL）
// 用于批量同名冲突检测，避免逐文件查询的 N+1。key 为文件名。
func GetFileNamesInParent(parentID, ownerID string) (map[string]*native.FileORM, error) {
	var files []*native.FileORM
	if err := db.Where("parent_id=? AND owner_id=?", parentID, ownerID).
		Find(&files).Error; err != nil {
		return nil, err
	}
	result := make(map[string]*native.FileORM, len(files))
	for _, f := range files {
		if f.Name != nil {
			result[*f.Name] = f
		}
	}
	return result, nil
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
func GetTrashList(offset, pageSize uint32, ownerID string) ([]*native.FileORM, int64, error) {
	ctx := context.Background()

	// 默认每页500条
	if pageSize == 0 {
		pageSize = 500
	}

	// 获取总数
	var total int64
	if err := db.WithContext(ctx).Model(&native.FileORM{}).
		Unscoped(). // 查询包含软删除的记录
		Where("deleted_at IS NOT NULL AND owner_id = ?", ownerID).
		Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询 - 显示所有有软删除标记的文件
	// 因为删除文件夹时只会标记文件夹本身，所以有软删除标记的都是用户显式删除的
	var files []*native.FileORM
	if err := db.WithContext(ctx).
		Unscoped(). // 查询包含软删除的记录
		Where("deleted_at IS NOT NULL AND owner_id = ?", ownerID).
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
func EmptyTrash(ownerID string) error {
	ctx := context.Background()
	return db.WithContext(ctx).
		Unscoped(). // 永久删除，绕过软删除
		Where("deleted_at IS NOT NULL AND owner_id = ?", ownerID).
		Delete(&native.FileORM{}).Error
}

// GetFileList 获取文件列表
// GetFileList 获取文件列表（支持分页）
// parentID: 父目录ID
// offset: 偏移量
// pageSize: 每页数量，如果为0则使用默认值500
func GetFileList(parentID string, offset, pageSize uint32, ownerID string) ([]*native.FileORM, int64, bool, error) {
	var files []*native.FileORM
	var total int64
	var err error
	ctx := context.Background()

	// 默认每页500条
	if pageSize == 0 {
		pageSize = 500
	}

	// 获取总数
	gdb := gorm.G[*native.FileORM](db).Where("parent_id = ?", parentID).Where("owner_id = ?", ownerID)
	if total, err = gdb.Count(ctx, "*"); err != nil {
		return nil, 0, false, err
	}

	// 分页查询
	if files, err = gorm.G[*native.FileORM](db).
		Where("parent_id = ?", parentID).Where("owner_id = ?", ownerID).
		Offset(int(offset)).
		Limit(int(pageSize)).
		Find(ctx); err != nil {
		return nil, 0, false, err
	}

	// 判断是否有更多数据
	hasMore := int64(offset)+int64(len(files)) < total

	return files, total, hasMore, nil
}

// SearchFiles 按文件名搜索文件（排除已删除和文件夹）
func SearchFiles(keyword string, offset, pageSize uint32, ownerID string) ([]*native.FileORM, int64, bool, error) {
	var files []*native.FileORM
	var total int64
	ctx := context.Background()

	if pageSize == 0 {
		pageSize = 50
	}

	folderType := int32(native.FileType_FOLDER)
	query := db.WithContext(ctx).
		Where("name LIKE ? AND type != ? AND owner_id = ?", "%"+keyword+"%", folderType, ownerID)

	// 获取总数
	if err := query.Model(&native.FileORM{}).Count(&total).Error; err != nil {
		return nil, 0, false, err
	}

	// 分页查询
	if err := query.
		Offset(int(offset)).
		Limit(int(pageSize)).
		Order("name ASC").
		Find(&files).Error; err != nil {
		return nil, 0, false, err
	}

	hasMore := int64(offset)+int64(len(files)) < total
	return files, total, hasMore, nil
}

// SearchFilesByTag 按标签搜索文件（排除已删除和文件夹）
func SearchFilesByTag(keyword string, offset, pageSize uint32) ([]*native.FileORM, int64, bool, error) {
	var files []*native.FileORM
	var total int64
	ctx := context.Background()

	if pageSize == 0 {
		pageSize = 50
	}

	folderType := int32(native.FileType_FOLDER)

	// 子查询：匹配标签的文件ID
	matchingFileIDs := db.WithContext(ctx).
		Model(&native.FileTagORM{}).
		Select("file_id").
		Where("tag_name LIKE ?", "%"+keyword+"%")

	// 查询匹配的文件（排除文件夹和已删除）
	query := db.WithContext(ctx).
		Where("id IN (?) AND type != ?", matchingFileIDs, folderType)

	if err := query.Model(&native.FileORM{}).Count(&total).Error; err != nil {
		return nil, 0, false, err
	}

	if err := query.
		Offset(int(offset)).
		Limit(int(pageSize)).
		Order("name ASC").
		Find(&files).Error; err != nil {
		return nil, 0, false, err
	}

	hasMore := int64(offset)+int64(len(files)) < total
	return files, total, hasMore, nil
}

// GetFileChildren 获取指定目录下的所有子文件（不含软删除）
func GetFileChildren(parentID string, ownerID string) ([]*native.FileORM, error) {
	var children []*native.FileORM
	ctx := context.Background()
	if err := db.WithContext(ctx).
		Where("parent_id = ? AND owner_id = ?", parentID, ownerID).
		Find(&children).Error; err != nil {
		return nil, err
	}
	return children, nil
}

// GetFolderList 获取指定目录下的所有文件夹
func GetFolderList(parentID string, ownerID string) ([]*native.FileORM, error) {
	var folders []*native.FileORM
	var err error
	ctx := context.Background()

	// 查询文件夹类型（FileType_FOLDER = 2）
	folderType := int32(native.FileType_FOLDER)
	if folders, err = gorm.G[*native.FileORM](db).
		Where("parent_id = ?", parentID).Where("owner_id = ?", ownerID).
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
	if orm.MaxHistories != nil {
		fields = append(fields, "max_histories")
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
		case "max_histories":
			// 0 是合法值（不保留历史），必须用 map 写入绕开 GORM 零值忽略
			updateMap["max_histories"] = *orm.MaxHistories
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
// version > 0 时仅当文件当前 version 等于该值才更新（CAS，防止旧分发把
// 已被新 PUT 接管的新 hash 错误标记为就绪）
//
// 返回受影响行数：0 行表示 CAS 没匹配上（版本已变 / 文件已被移动改名），
// 调用方必须据此告警——这个更新失败是静默的，漏掉就会留下"位置行都完成了
// 但 is_ready 还是 false"的脏数据。
func UpdateFileIsReady(parentID, fileName, ownerID string, version uint64) (int64, error) {
	ctx := context.Background()
	isReady := true
	q := gorm.G[*native.FileORM](db).
		Where("parent_id = ? AND name = ?", parentID, fileName)
	if ownerID != "" {
		q = q.Where("owner_id = ?", ownerID)
	}
	if version > 0 {
		q = q.Where("version = ?", version)
	}
	matched, err := q.Update(ctx, "is_ready", isReady)
	return int64(matched), err
}

// TakeoverFileVersion CAS 抢占文件版本号并更新内容字段（hash/size）。
// 仅当文件当前 version < myVersion 时更新成功（后到达的上传持有更大版本号，
// 版本落后的请求无法覆盖新内容）。返回是否抢占成功。
// myVersion == 0（存量任务/无版本上下文）时退化为无条件更新，兼容老数据。
// updates 额外字段（如 is_ready）通过传入的 file 记录应用。
func TakeoverFileVersion(fileID string, myVersion uint64, newHash string, newSize uint64, extra map[string]interface{}) (bool, error) {
	ctx := context.Background()
	updateMap := map[string]interface{}{
		"hash":    newHash,
		"size":    newSize,
		"version": myVersion,
	}
	for k, v := range extra {
		updateMap[k] = v
	}
	q := db.WithContext(ctx).Model(&native.FileORM{})
	if myVersion > 0 {
		q = q.Where("id = ? AND version < ?", fileID, myVersion)
	} else {
		q = q.Where("id = ?", fileID)
	}
	result := q.Updates(updateMap)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// UpdateFileWithHistoryCAS 更新文件记录（CAS 抢占版本）并记录历史。
// myVersion 必须大于文件当前 version 才生效，返回是否成功。
// myVersion == 0（存量任务/无版本上下文）时退化为无条件更新，兼容老数据。
// 旧内容始终写入 file_histories，max_histories 由调用方异步裁剪。
func UpdateFileWithHistoryCAS(ctx context.Context, file *native.FileORM, oldFile *native.FileORM, myVersion uint64) (bool, error) {
	taken := false
	err := db.Transaction(func(tx *gorm.DB) error {
		updateMap := map[string]interface{}{
			"hash":    *file.Hash,
			"size":    *file.Size,
			"version": myVersion,
		}
		q := tx.WithContext(ctx).Model(&native.FileORM{})
		if myVersion > 0 {
			q = q.Where("id = ? AND version < ?", *file.Id, myVersion)
		} else {
			q = q.Where("id = ?", *file.Id)
		}
		result := q.Updates(updateMap)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			// 版本落后：被更新的上传接管，不写历史
			return nil
		}
		taken = true

		history := &native.FileHistoryORM{
			FileId:        file.Id,
			Hash:          oldFile.Hash,
			Size:          oldFile.Size,
			VersionNumber: oldFile.UpdatedAt,
		}
		return gorm.G[native.FileHistoryORM](tx).Create(ctx, history)
	})
	return taken, err
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

// GetFileHistoriesByIDs 批量获取文件历史记录（用于避免逐条查询的 N+1）
func GetFileHistoriesByIDs(ids []uint64) ([]*native.FileHistoryORM, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ctx := context.Background()
	var histories []*native.FileHistoryORM
	if err := db.WithContext(ctx).Where("id IN ?", ids).Find(&histories).Error; err != nil {
		return nil, err
	}
	return histories, nil
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

// CountFileHistoriesByHash 统计指定 hash 在文件历史表中的引用数
// 用于判断该 hash 的 file_location 记录是否仍被历史版本引用
func CountFileHistoriesByHash(hash string) (int64, error) {
	if hash == "" {
		return 0, nil
	}

	ctx := context.Background()
	gdb := gorm.G[*native.FileHistoryORM](db).Where("hash = ?", hash)
	count, err := gdb.Count(ctx, "*")
	if err != nil {
		return 0, err
	}
	return count, nil
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

// CountFilesByHashIncludingDeleted 统计引用指定 hash 的文件记录数（Unscoped，含回收站软删记录）。
// 用于去重清理物理数据前的安全校验：只要有任何记录（含可恢复的软删记录）引用，就不允许清理。
func CountFilesByHashIncludingDeleted(hash string) (int64, error) {
	if hash == "" {
		return 0, nil
	}

	ctx := context.Background()
	var count int64
	err := db.WithContext(ctx).Unscoped().Model(&native.FileORM{}).
		Where("hash = ?", hash).Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

// CountActiveUploadRecordsByHash 统计指定 hash 的活跃上传记录数（PENDING/UPLOADING）。
// 用于判断该 hash 的 file_location 是否仍在上传窗口期——分发未完成的 hash
// 严禁被"覆盖清理"逻辑删除（否则会导致 upload_record 卡死，见并发覆盖场景）。
func CountActiveUploadRecordsByHash(hash string) (int64, error) {
	if hash == "" {
		return 0, nil
	}

	ctx := context.Background()
	var count int64
	err := db.WithContext(ctx).Model(&native.UploadRecordORM{}).
		Where("file_hash = ? AND task_status IN ?", hash,
			[]int32{int32(native.UploadStatus_PENDING), int32(native.UploadStatus_UPLOADING)}).
		Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

// ArchiveHashAsHistoryIfAbsent 将 hash 幂等归档为该文件的历史记录（防孤儿）。
// 场景：并发覆盖中版本落后的上传，其内容可能已分发到存储提供者，但 file
// 记录已被更新的内容接管。若该 hash 既不在 file 当前 hash 也不在历史中，
// 其 file_location 将成为无引用的孤儿——这里补一条历史引用使其可被追溯。
// 幂等：同 (file_id, hash) 已存在则跳过；允许重复历史（不占物理空间）。
func ArchiveHashAsHistoryIfAbsent(fileID, hash string, size uint64) error {
	if fileID == "" || hash == "" {
		return nil
	}

	ctx := context.Background()
	var count int64
	if err := db.WithContext(ctx).Model(&native.FileHistoryORM{}).
		Where("file_id = ? AND hash = ?", fileID, hash).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil // 已归档过，幂等跳过
	}

	now := time.Now()
	history := &native.FileHistoryORM{
		FileId:        &fileID,
		Hash:          &hash,
		Size:          &size,
		VersionNumber: &now,
	}
	return db.WithContext(ctx).Create(history).Error
}

// GetFileHistoryByFileIDAndHash 按文件ID+hash查询历史记录（幂等归档时用）
func GetFileHistoryByFileIDAndHash(fileID, hash string) (*native.FileHistoryORM, error) {
	ctx := context.Background()
	var history native.FileHistoryORM
	err := db.WithContext(ctx).
		Where("file_id = ? AND hash = ?", fileID, hash).
		First(&history).Error
	if err != nil {
		return nil, err
	}
	return &history, nil
}

// GetRecentFiles 获取最近使用的文件（排除已删除和文件夹）
func GetRecentFiles(offset, pageSize uint32, ownerID string) ([]*native.FileORM, int64, bool, error) {
	var files []*native.FileORM
	var total int64
	ctx := context.Background()

	if pageSize == 0 {
		pageSize = 20
	}

	folderType := int32(native.FileType_FOLDER)
	query := db.WithContext(ctx).
		Where("type != ? AND last_accessed_at IS NOT NULL AND owner_id = ?", folderType, ownerID)

	// 获取总数
	if err := query.Model(&native.FileORM{}).Count(&total).Error; err != nil {
		return nil, 0, false, err
	}

	// 分页查询，按 last_accessed_at 降序
	if err := query.
		Order("last_accessed_at DESC").
		Offset(int(offset)).
		Limit(int(pageSize)).
		Find(&files).Error; err != nil {
		return nil, 0, false, err
	}

	hasMore := int64(offset)+int64(len(files)) < total
	return files, total, hasMore, nil
}

// GetFilesByType 按文件类型获取文件列表（排除已删除）
func GetFilesByType(fileType native.FileType, offset, pageSize uint32, ownerID string) ([]*native.FileORM, int64, bool, error) {
	var files []*native.FileORM
	var total int64
	ctx := context.Background()

	if pageSize == 0 {
		pageSize = 50
	}

	typeVal := int32(fileType)
	query := db.WithContext(ctx).
		Where("type = ? AND owner_id = ?", typeVal, ownerID)

	// 获取总数
	if err := query.Model(&native.FileORM{}).Count(&total).Error; err != nil {
		return nil, 0, false, err
	}

	// 分页查询，按 created_at 降序
	if err := query.
		Order("created_at DESC").
		Offset(int(offset)).
		Limit(int(pageSize)).
		Find(&files).Error; err != nil {
		return nil, 0, false, err
	}

	hasMore := int64(offset)+int64(len(files)) < total
	return files, total, hasMore, nil
}

// UpdateLastAccessedAt 更新文件的最后访问时间
func UpdateLastAccessedAt(fileID string) error {
	ctx := context.Background()
	return db.WithContext(ctx).
		Model(&native.FileORM{}).
		Where("id = ?", fileID).
		Update("last_accessed_at", time.Now()).Error
}

// CountChildrenByParentID 统计父目录下的直接子文件数（不含软删除）
func CountChildrenByParentID(parentID string, ownerID string) (int64, error) {
	ctx := context.Background()
	var count int64
	err := db.WithContext(ctx).
		Model(&native.FileORM{}).
		Where("parent_id = ? AND type != ? AND owner_id = ?", parentID, native.FileType_FOLDER, ownerID).
		Count(&count).Error
	return count, err
}

// GetSubfoldersByParentID 获取父目录下的所有子文件夹（不含软删除，按名称排序）
func GetSubfoldersByParentID(parentID string, ownerID string) ([]*native.FileORM, error) {
	ctx := context.Background()
	var folders []*native.FileORM
	err := db.WithContext(ctx).
		Where("parent_id = ? AND type = ? AND owner_id = ?", parentID, native.FileType_FOLDER, ownerID).
		Order("name ASC").
		Find(&folders).Error
	return folders, err
}

// ==================== 容量/大小统计 ====================

// SumAllFileSizesByOwner 统计用户所有文件（含回收站软删，物理仍占用存储）的 size 总和。
// 口径：不含文件夹（type != FOLDER）；软删行仍计入（用户空间“已用”应含回收站）；
// 永久删除/清空回收站后行消失，总量自然回落。供容量异步更新器写回 used_bytes。
func SumAllFileSizesByOwner(ownerID string) (uint64, error) {
	ctx := context.Background()
	var sum int64
	err := db.WithContext(ctx).Unscoped().
		Model(&native.FileORM{}).
		Where("owner_id = ? AND type != ?", ownerID, native.FileType_FOLDER).
		Select("COALESCE(SUM(size), 0)").
		Scan(&sum).Error
	if err != nil {
		return 0, err
	}
	return uint64(sum), nil
}
