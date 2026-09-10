package service

import (
	"context"
	"errors"
	"os"

	"jenken827.com/jenken827/pfm/internal/backup"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
	"github.com/robfig/cron/v3"
)

// CreateBackupTask 创建备份任务
func CreateBackupTask(ctx context.Context, req *native.CreateBackupTaskReq) *models.Err {
	// 管理员专属：源路径为服务端本地文件系统路径，普通用户不得访问
	if mErr := RequireAdmin(ctx); mErr != nil {
		return mErr
	}
	// 获取当前用户作为 owner
	ownerID := getCurrentUserIDFromCtx(ctx)
	if ownerID == "" {
		return &models.Err{E: errors.New("unauthorized"), C: native.RespCode_TOKEN_INVALID}
	}

	// 1. 参数基础校验
	if req.Name == nil || *req.Name == "" {
		return &models.Err{E: errors.New("task name is required"), C: native.RespCode_PARAMS_ERROR}
	}
	if req.SourcePath == nil || *req.SourcePath == "" {
		return &models.Err{E: errors.New("source path is required"), C: native.RespCode_PARAMS_ERROR}
	}
	if req.TargetDirId == nil || *req.TargetDirId == "" {
		return &models.Err{E: errors.New("target dir id is required"), C: native.RespCode_PARAMS_ERROR}
	}

	// 2. 检查源路径是否存在且可访问
	info, err := os.Stat(*req.SourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &models.Err{E: errors.New("source path does not exist"), C: native.RespCode_PARAMS_ERROR}
		}
		return &models.Err{E: errors.New("source path is not accessible: " + err.Error()), C: native.RespCode_PARAMS_ERROR}
	}
	if !info.IsDir() {
		return &models.Err{E: errors.New("source path is not a directory"), C: native.RespCode_PARAMS_ERROR}
	}

	// 3. 确定策略和 Cron 表达式
	strategy := native.BackupStrategy_BACKUP_STRATEGY_SCHEDULED
	if req.Strategy != nil {
		strategy = *req.Strategy
	}

	cronExpr := "0 0 * * *"
	if req.CronExpression != nil {
		cronExpr = *req.CronExpression
	}

	// 4. 如果是定时策略，校验 Cron 表达式
	if strategy == native.BackupStrategy_BACKUP_STRATEGY_SCHEDULED {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, err := parser.Parse(cronExpr); err != nil {
			return &models.Err{E: errors.New("invalid cron expression: " + err.Error()), C: native.RespCode_PARAMS_ERROR}
		}
	}

	// 5. 检查相同源路径是否已有备份任务
	_, err = db.GetBackupTaskBySourcePath(*req.SourcePath)
	if err == nil {
		return &models.Err{E: errors.New("a backup task with the same source path already exists"), C: native.RespCode_PARAMS_ERROR}
	}

	// 6. 检查目标目录是否存在
	targetDir, err := db.GetFileByID(*req.TargetDirId)
	if err != nil || targetDir == nil {
		return &models.Err{E: errors.New("target directory not found"), C: native.RespCode_PARAMS_ERROR}
	}

	orm := &native.BackupTaskORM{
		Name:        req.Name,
		SourcePath:  req.SourcePath,
		TargetDirId: req.TargetDirId,
		IgnoreRules: req.IgnoreRules,
		OwnerId:     &ownerID,
	}

	orm.Strategy = utils.Int32Ptr(int32(strategy))
	orm.CronExpression = &cronExpr

	if req.Recursive != nil {
		orm.Recursive = req.Recursive
	} else {
		orm.Recursive = utils.BoolPtr(true)
	}

	if req.Enabled != nil {
		orm.Enabled = req.Enabled
	} else {
		orm.Enabled = utils.BoolPtr(true)
	}

	idleStatus := int32(native.BackupRunStatus_BACKUP_RUN_STATUS_IDLE)
	orm.LastRunStatus = &idleStatus

	if err := db.CreateBackupTask(orm); err != nil {
		llog.C(ctx).Errorf("create backup task failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 添加到调度器
	if *orm.Enabled {
		backup.GetScheduler().AddTask(orm)
	}

	return nil
}

// UpdateBackupTask 更新备份任务
func UpdateBackupTask(ctx context.Context, req *native.UpdateBackupTaskReq) *models.Err {
	// 管理员专属：可修改服务端源路径
	if mErr := RequireAdmin(ctx); mErr != nil {
		return mErr
	}
	task, err := db.GetBackupTaskByID(*req.Id)
	if err != nil {
		return &models.Err{E: errors.New("backup task not found"), C: native.RespCode_NOT_FOUND}
	}

	// 所有权检查
	if !checkBackupTaskOwnership(ctx, task) {
		return &models.Err{E: errors.New("permission denied"), C: native.RespCode_PERMISSION_DENIED}
	}

	// 先从调度器移除
	backup.GetScheduler().RemoveTask(*task.Id)

	// 更新字段
	if req.Name != nil {
		task.Name = req.Name
	}
	if req.SourcePath != nil {
		// 检查源路径是否存在且可访问
		info, err := os.Stat(*req.SourcePath)
		if err != nil {
			if os.IsNotExist(err) {
				return &models.Err{E: errors.New("source path does not exist"), C: native.RespCode_PARAMS_ERROR}
			}
			return &models.Err{E: errors.New("source path is not accessible: " + err.Error()), C: native.RespCode_PARAMS_ERROR}
		}
		if !info.IsDir() {
			return &models.Err{E: errors.New("source path is not a directory"), C: native.RespCode_PARAMS_ERROR}
		}
		// 检查相同源路径是否已有其他备份任务
		_, err = db.GetBackupTaskBySourcePathExcludeID(*req.SourcePath, *task.Id)
		if err == nil {
			return &models.Err{E: errors.New("a backup task with the same source path already exists"), C: native.RespCode_PARAMS_ERROR}
		}
		task.SourcePath = req.SourcePath
	}
	if req.TargetDirId != nil {
		// 检查目标目录是否存在
		targetDir, err := db.GetFileByID(*req.TargetDirId)
		if err != nil || targetDir == nil {
			return &models.Err{E: errors.New("target directory not found"), C: native.RespCode_PARAMS_ERROR}
		}
		task.TargetDirId = req.TargetDirId
	}
	if req.Strategy != nil {
		task.Strategy = utils.Int32Ptr(int32(*req.Strategy))
	}
	if req.CronExpression != nil {
		task.CronExpression = req.CronExpression
	}
	if req.IgnoreRules != nil {
		task.IgnoreRules = req.IgnoreRules
	}
	if req.Recursive != nil {
		task.Recursive = req.Recursive
	}
	if req.Enabled != nil {
		task.Enabled = req.Enabled
	}

	// 校验：如果是定时策略，Cron 表达式必须合法
	effectiveStrategy := native.BackupStrategy_BACKUP_STRATEGY_SCHEDULED
	if task.Strategy != nil {
		effectiveStrategy = native.BackupStrategy(*task.Strategy)
	}
	if effectiveStrategy == native.BackupStrategy_BACKUP_STRATEGY_SCHEDULED {
		cronExpr := ""
		if task.CronExpression != nil {
			cronExpr = *task.CronExpression
		}
		if cronExpr == "" {
			return &models.Err{E: errors.New("cron expression is required for scheduled strategy"), C: native.RespCode_PARAMS_ERROR}
		}
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, err := parser.Parse(cronExpr); err != nil {
			return &models.Err{E: errors.New("invalid cron expression: " + err.Error()), C: native.RespCode_PARAMS_ERROR}
		}
	}

	if err := db.UpdateBackupTask(task); err != nil {
		llog.C(ctx).Errorf("update backup task failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 重新添加到调度器
	if *task.Enabled {
		backup.GetScheduler().AddTask(task)
	}

	return nil
}

// DeleteBackupTask 删除备份任务
func DeleteBackupTask(ctx context.Context, req *native.DeleteBackupTaskReq) *models.Err {
	// 管理员专属
	if mErr := RequireAdmin(ctx); mErr != nil {
		return mErr
	}
	// 所有权检查：验证每个任务都属于当前用户
	for _, id := range req.Ids {
		task, err := db.GetBackupTaskByID(id)
		if err != nil {
			return &models.Err{E: errors.New("backup task not found"), C: native.RespCode_NOT_FOUND}
		}
		if !checkBackupTaskOwnership(ctx, task) {
			return &models.Err{E: errors.New("permission denied"), C: native.RespCode_PERMISSION_DENIED}
		}
	}

	// 先从调度器移除
	for _, id := range req.Ids {
		backup.GetScheduler().RemoveTask(id)
	}

	if err := db.DeleteBackupTask(req.Ids); err != nil {
		llog.C(ctx).Errorf("delete backup task failed: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	return nil
}

// ListBackupTasks 获取备份任务列表
func ListBackupTasks(ctx context.Context, req *native.ListBackupTasksReq) (*native.ListBackupTasksRespData, *models.Err) {
	// 管理员专属：任务列表暴露服务端源路径
	if mErr := RequireAdmin(ctx); mErr != nil {
		return nil, mErr
	}
	ownerID := getCurrentUserIDFromCtx(ctx)
	if ownerID == "" {
		return nil, &models.Err{E: errors.New("unauthorized"), C: native.RespCode_TOKEN_INVALID}
	}

	page := req.GetPage()
	pageSize := req.GetPageSize()
	if page == 0 {
		page = 1
	}

	// 管理员可查看所有任务，普通用户只能查看自己的
	filterOwnerID := ownerID
	if db.IsAdmin(ownerID) {
		filterOwnerID = "" // 空字符串表示不过滤
	}

	tasks, total, err := db.ListBackupTasks(page, pageSize, req.Enabled, filterOwnerID)
	if err != nil {
		llog.C(ctx).Errorf("list backup tasks failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	items := make([]*native.BackupTask, 0, len(tasks))
	for _, t := range tasks {
		pb, pbErr := t.ToPB(ctx)
		if pbErr != nil {
			llog.C(ctx).Errorf("convert backup task to PB failed: %v", pbErr)
			continue
		}
		items = append(items, &pb)
	}

	totalUint32 := uint32(total)
	return &native.ListBackupTasksRespData{
		Tasks: items,
		Total: &totalUint32,
	}, nil
}

// GetBackupTask 获取备份任务详情
func GetBackupTask(ctx context.Context, req *native.GetBackupTaskReq) (*native.GetBackupTaskRespData, *models.Err) {
	// 管理员专属：任务详情暴露服务端源路径
	if mErr := RequireAdmin(ctx); mErr != nil {
		return nil, mErr
	}
	task, err := db.GetBackupTaskByID(*req.Id)
	if err != nil {
		return nil, &models.Err{E: errors.New("backup task not found"), C: native.RespCode_NOT_FOUND}
	}

	// 所有权检查
	if !checkBackupTaskOwnership(ctx, task) {
		return nil, &models.Err{E: errors.New("permission denied"), C: native.RespCode_PERMISSION_DENIED}
	}

	pb, pbErr := task.ToPB(ctx)
	if pbErr != nil {
		return nil, &models.Err{E: pbErr, C: native.RespCode_INTERNAL_ERROR}
	}

	// 获取最近备份的文件记录
	recentRecords, _ := db.GetRecentBackupFileRecords(*task.Id, 10)
	recentFiles := make([]*native.BackupFileRecord, 0, len(recentRecords))
	for _, r := range recentRecords {
		rpb, rErr := r.ToPB(ctx)
		if rErr != nil {
			continue
		}
		recentFiles = append(recentFiles, &rpb)
	}

	return &native.GetBackupTaskRespData{
		Task:        &pb,
		RecentFiles: recentFiles,
	}, nil
}

// TriggerBackup 手动触发备份
func TriggerBackup(ctx context.Context, req *native.TriggerBackupReq) *models.Err {
	// 管理员专属
	if mErr := RequireAdmin(ctx); mErr != nil {
		return mErr
	}
	llog.C(ctx).Infof("TriggerBackup called, req.Id: %v", req.Id)
	task, err := db.GetBackupTaskByID(*req.Id)
	if err != nil {
		llog.C(ctx).Errorf("TriggerBackup: task not found, id: %v, err: %v", req.Id, err)
		return &models.Err{E: errors.New("backup task not found"), C: native.RespCode_NOT_FOUND}
	}

	// 所有权检查
	if !checkBackupTaskOwnership(ctx, task) {
		return &models.Err{E: errors.New("permission denied"), C: native.RespCode_PERMISSION_DENIED}
	}

	// 防重复触发：任务正在运行中
	if task.LastRunStatus != nil && *task.LastRunStatus == int32(native.BackupRunStatus_BACKUP_RUN_STATUS_RUNNING) {
		return &models.Err{E: errors.New("backup task is already running"), C: native.RespCode_PARAMS_ERROR}
	}

	// 手动模式不需要检查 enabled
	isManual := task.Strategy != nil && *task.Strategy == int32(native.BackupStrategy_BACKUP_STRATEGY_MANUAL)
	if !isManual && !*task.Enabled {
		return &models.Err{E: errors.New("backup task is disabled"), C: native.RespCode_PARAMS_ERROR}
	}

	llog.C(ctx).Infof("TriggerBackup: calling scheduler.TriggerBackup, task_id: %d", *task.Id)
	backup.GetScheduler().TriggerBackup(*task.Id)
	return nil
}

// checkBackupTaskOwnership 检查当前用户是否为备份任务的所有者（或管理员）
func checkBackupTaskOwnership(ctx context.Context, task *native.BackupTaskORM) bool {
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return false
	}
	if db.IsAdmin(userID) {
		return true
	}
	if task.OwnerId != nil && *task.OwnerId == userID {
		return true
	}
	return false
}

// ListBackupFiles 获取备份文件记录
func ListBackupFiles(ctx context.Context, req *native.ListBackupFilesReq) (*native.ListBackupFilesRespData, *models.Err) {
	// 管理员专属
	if mErr := RequireAdmin(ctx); mErr != nil {
		return nil, mErr
	}
	// 归属校验：任务必须属于当前用户（或管理员），防止越权查看他人备份文件
	task, err := db.GetBackupTaskByID(*req.TaskId)
	if err != nil || task == nil {
		return nil, &models.Err{E: errors.New("backup task not found"), C: native.RespCode_NOT_FOUND}
	}
	if !checkBackupTaskOwnership(ctx, task) {
		return nil, &models.Err{E: errors.New("permission denied"), C: native.RespCode_PERMISSION_DENIED}
	}

	page := req.GetPage()
	pageSize := req.GetPageSize()
	if page == 0 {
		page = 1
	}

	records, total, err := db.ListBackupFileRecords(*req.TaskId, page, pageSize, req.Status)
	if err != nil {
		llog.C(ctx).Errorf("list backup files failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	items := make([]*native.BackupFileRecord, 0, len(records))
	for _, r := range records {
		pb, pbErr := r.ToPB(ctx)
		if pbErr != nil {
			continue
		}
		items = append(items, &pb)
	}

	totalUint32 := uint32(total)
	return &native.ListBackupFilesRespData{
		Files: items,
		Total: &totalUint32,
	}, nil
}
