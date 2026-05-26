package db

import (
	"context"

	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"gorm.io/gorm"
)

// ==================== BackupTask ====================

// CreateBackupTask 创建备份任务
func CreateBackupTask(task *native.BackupTaskORM) error {
	ctx := context.Background()
	return gorm.G[native.BackupTaskORM](db).Create(ctx, task)
}

// UpdateBackupTask 更新备份任务
func UpdateBackupTask(task *native.BackupTaskORM) error {
	ctx := context.Background()
	id := *task.Id
	fields := []string{}

	if task.Name != nil {
		fields = append(fields, "name")
	}
	if task.SourcePath != nil {
		fields = append(fields, "source_path")
	}
	if task.TargetDirId != nil {
		fields = append(fields, "target_dir_id")
	}
	if task.Strategy != nil {
		fields = append(fields, "strategy")
	}
	if task.CronExpression != nil {
		fields = append(fields, "cron_expression")
	}
	if task.IgnoreRules != nil {
		fields = append(fields, "ignore_rules")
	}
	if task.Recursive != nil {
		fields = append(fields, "recursive")
	}
	if task.Enabled != nil {
		fields = append(fields, "enabled")
	}
	if task.LastRunAt != nil {
		fields = append(fields, "last_run_at")
	}
	if task.LastRunStatus != nil {
		fields = append(fields, "last_run_status")
	}
	if task.LastRunMessage != nil {
		fields = append(fields, "last_run_message")
	}
	if task.BackedUpCount != nil {
		fields = append(fields, "backed_up_count")
	}
	if task.BackedUpSize != nil {
		fields = append(fields, "backed_up_size")
	}

	if len(fields) == 0 {
		_, err := gorm.G[*native.BackupTaskORM](db).Where("id=?", id).Updates(ctx, task)
		return err
	}

	// 使用 map 进行更新，避免 GORM 的零值检查问题
	updateMap := make(map[string]interface{})
	for _, field := range fields {
		switch field {
		case "name":
			updateMap["name"] = *task.Name
		case "source_path":
			updateMap["source_path"] = *task.SourcePath
		case "target_dir_id":
			updateMap["target_dir_id"] = *task.TargetDirId
		case "strategy":
			updateMap["strategy"] = *task.Strategy
		case "cron_expression":
			updateMap["cron_expression"] = *task.CronExpression
		case "ignore_rules":
			updateMap["ignore_rules"] = *task.IgnoreRules
		case "recursive":
			updateMap["recursive"] = *task.Recursive
		case "enabled":
			updateMap["enabled"] = *task.Enabled
		case "last_run_at":
			updateMap["last_run_at"] = *task.LastRunAt
		case "last_run_status":
			updateMap["last_run_status"] = *task.LastRunStatus
		case "last_run_message":
			updateMap["last_run_message"] = *task.LastRunMessage
		case "backed_up_count":
			updateMap["backed_up_count"] = *task.BackedUpCount
		case "backed_up_size":
			updateMap["backed_up_size"] = *task.BackedUpSize
		}
	}

	result := db.WithContext(ctx).Model(&native.BackupTaskORM{}).Where("id = ?", id).Updates(updateMap)
	return result.Error
}

// DeleteBackupTask 删除备份任务
func DeleteBackupTask(ids []uint64) error {
	ctx := context.Background()
	return db.Transaction(func(tx *gorm.DB) error {
		// 先删除关联的 BackupFileRecord
		if _, err := gorm.G[native.BackupFileRecordORM](tx).Where("task_id IN ?", ids).Delete(ctx); err != nil {
			llog.L().Errorf("delete backup file records for tasks failed, ids: %v, err: %v", ids, err)
			return err
		}
		// 再删除任务
		if _, err := gorm.G[native.BackupTaskORM](tx).Where("id IN ?", ids).Delete(ctx); err != nil {
			llog.L().Errorf("delete backup tasks failed, ids: %v, err: %v", ids, err)
			return err
		}
		return nil
	})
}

// GetBackupTaskByID 根据ID获取备份任务
func GetBackupTaskByID(id uint64) (*native.BackupTaskORM, error) {
	ctx := context.Background()
	record, err := gorm.G[*native.BackupTaskORM](db).Where("id = ?", id).First(ctx)
	if err != nil {
		return nil, err
	}
	return record, nil
}

// GetEnabledBackupTasks 获取所有启用的备份任务
func GetEnabledBackupTasks() ([]*native.BackupTaskORM, error) {
	ctx := context.Background()
	return gorm.G[*native.BackupTaskORM](db).Where("enabled = ?", true).Find(ctx)
}

// GetBackupTaskBySourcePath 根据源路径获取备份任务
func GetBackupTaskBySourcePath(sourcePath string) (*native.BackupTaskORM, error) {
	ctx := context.Background()
	record, err := gorm.G[*native.BackupTaskORM](db).Where("source_path = ?", sourcePath).First(ctx)
	if err != nil {
		return nil, err
	}
	return record, nil
}

// GetBackupTaskBySourcePathExcludeID 根据源路径获取备份任务（排除指定ID，用于更新时去重）
func GetBackupTaskBySourcePathExcludeID(sourcePath string, excludeID uint64) (*native.BackupTaskORM, error) {
	ctx := context.Background()
	record, err := gorm.G[*native.BackupTaskORM](db).Where("source_path = ? AND id != ?", sourcePath, excludeID).First(ctx)
	if err != nil {
		return nil, err
	}
	return record, nil
}

// ListBackupTasks 获取备份任务列表（分页）
func ListBackupTasks(page, pageSize uint32, enabled *bool) ([]*native.BackupTaskORM, int64, error) {
	ctx := context.Background()

	if pageSize == 0 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	query := db.WithContext(ctx).Model(&native.BackupTaskORM{})
	if enabled != nil {
		query = query.Where("enabled = ?", *enabled)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var tasks []*native.BackupTaskORM
	if err := query.Offset(int(offset)).Limit(int(pageSize)).Order("id DESC").Find(&tasks).Error; err != nil {
		return nil, 0, err
	}

	return tasks, total, nil
}

// UpdateBackupTaskStatus 更新备份任务执行状态
func UpdateBackupTaskStatus(id uint64, status native.BackupRunStatus, message string) error {
	ctx := context.Background()
	values := map[string]interface{}{
		"last_run_status":  int32(status),
		"last_run_message": message,
	}
	return db.WithContext(ctx).Model(&native.BackupTaskORM{}).Where("id = ?", id).Updates(values).Error
}

// UpdateBackupTaskRunTime 更新备份任务执行时间
func UpdateBackupTaskRunTime(id uint64) error {
	ctx := context.Background()
	return db.WithContext(ctx).Model(&native.BackupTaskORM{}).
		Where("id = ?", id).
		Update("last_run_at", gorm.Expr("NOW()")).Error
}

// IncrementBackupStats 增加备份统计
func IncrementBackupStats(id uint64, count uint64, size uint64) error {
	ctx := context.Background()
	return db.WithContext(ctx).Model(&native.BackupTaskORM{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"backed_up_count": gorm.Expr("backed_up_count + ?", count),
			"backed_up_size":  gorm.Expr("backed_up_size + ?", size),
		}).Error
}

// ==================== BackupFileRecord ====================

// CreateBackupFileRecord 创建备份文件记录
func CreateBackupFileRecord(record *native.BackupFileRecordORM) error {
	ctx := context.Background()
	return gorm.G[native.BackupFileRecordORM](db).Create(ctx, record)
}

// UpdateBackupFileRecord 更新备份文件记录
func UpdateBackupFileRecord(record *native.BackupFileRecordORM) error {
	ctx := context.Background()
	id := *record.Id
	fields := []string{}

	if record.FileHash != nil {
		fields = append(fields, "file_hash")
	}
	if record.FileSize != nil {
		fields = append(fields, "file_size")
	}
	if record.ModifiedAt != nil {
		fields = append(fields, "modified_at")
	}
	if record.FileId != nil {
		fields = append(fields, "file_id")
	}
	if record.Status != nil {
		fields = append(fields, "status")
	}
	if record.Message != nil {
		fields = append(fields, "message")
	}

	if len(fields) == 0 {
		return nil
	}

	// 使用 map 进行更新，避免 GORM 的零值检查问题
	updateMap := make(map[string]interface{})
	for _, field := range fields {
		switch field {
		case "file_hash":
			updateMap["file_hash"] = *record.FileHash
		case "file_size":
			updateMap["file_size"] = *record.FileSize
		case "modified_at":
			updateMap["modified_at"] = *record.ModifiedAt
		case "file_id":
			updateMap["file_id"] = *record.FileId
		case "status":
			updateMap["status"] = *record.Status
		case "message":
			updateMap["message"] = *record.Message
		}
	}

	result := db.WithContext(ctx).Model(&native.BackupFileRecordORM{}).Where("id = ?", id).Updates(updateMap)
	return result.Error
}

// GetBackupFileRecord 获取备份文件记录
func GetBackupFileRecord(taskID uint64, relativePath string) (*native.BackupFileRecordORM, error) {
	ctx := context.Background()
	record, err := gorm.G[*native.BackupFileRecordORM](db).Where("task_id = ? AND relative_path = ?", taskID, relativePath).First(ctx)
	if err != nil {
		return nil, err
	}
	return record, nil
}

// ListBackupFileRecords 获取备份文件记录列表（分页）
func ListBackupFileRecords(taskID uint64, page, pageSize uint32, status *native.BackupFileStatus) ([]*native.BackupFileRecordORM, int64, error) {
	ctx := context.Background()

	if pageSize == 0 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	query := db.WithContext(ctx).Model(&native.BackupFileRecordORM{}).Where("task_id = ?", taskID)
	if status != nil {
		query = query.Where("status = ?", int32(*status))
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var records []*native.BackupFileRecordORM
	if err := query.Offset(int(offset)).Limit(int(pageSize)).Order("id DESC").Find(&records).Error; err != nil {
		return nil, 0, err
	}

	return records, total, nil
}

// GetPendingBackupFileRecords 获取待处理的备份文件记录（用于重启恢复）
func GetPendingBackupFileRecords(taskID uint64) ([]*native.BackupFileRecordORM, error) {
	ctx := context.Background()
	return gorm.G[*native.BackupFileRecordORM](db).
		Where("task_id = ? AND status IN ?", taskID,
			[]int32{
				int32(native.BackupFileStatus_BACKUP_FILE_STATUS_PENDING),
				int32(native.BackupFileStatus_BACKUP_FILE_STATUS_REGISTERING),
			}).
		Order("created_at ASC").
		Find(ctx)
}

// GetRecentBackupFileRecords 获取最近备份的文件记录
func GetRecentBackupFileRecords(taskID uint64, limit int) ([]*native.BackupFileRecordORM, error) {
	ctx := context.Background()
	if limit <= 0 {
		limit = 10
	}
	return gorm.G[*native.BackupFileRecordORM](db).
		Where("task_id = ?", taskID).
		Order("updated_at DESC").
		Limit(limit).
		Find(ctx)
}

// DeleteBackupFileRecordsByTaskID 删除指定任务的所有备份文件记录
func DeleteBackupFileRecordsByTaskID(taskID uint64) error {
	ctx := context.Background()
	_, err := gorm.G[native.BackupFileRecordORM](db).Where("task_id = ?", taskID).Delete(ctx)
	return err
}

// GetLocalProviderID 获取 LOCAL 类型的存储提供者ID
func GetLocalProviderID() (uint32, error) {
	ctx := context.Background()
	var provider native.StorageProviderORM
	err := db.WithContext(ctx).Where("provider_type = ? AND enabled = ?", int32(native.StorageProviderType_LOCAL), true).First(&provider).Error
	if err != nil {
		return 0, err
	}
	return *provider.Id, nil
}
