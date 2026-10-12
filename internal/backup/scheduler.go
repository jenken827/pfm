package backup

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/robfig/cron/v3"
	ignore "github.com/sabhiram/go-gitignore"
	"gorm.io/gorm"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/fileutil"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
	"lukechampine.com/blake3"
)

// BackupScheduler 备份调度器
type BackupScheduler struct {
	cron       *cron.Cron
	mu         sync.RWMutex
	taskMap    map[uint64]cron.EntryID // taskID -> cronEntryID
	watcher    *fsnotify.Watcher
	watchMu    sync.Mutex
	watchMap   map[uint64][]string // taskID -> watched paths
	registerCh chan *BackupRegisterItem

	// Watch 模式防抖：taskID -> 定时器
	debounceMu    sync.Mutex
	debounceTimer map[uint64]*time.Timer
}

// BackupRegisterItem 注册队列项
type BackupRegisterItem struct {
	Task      *native.BackupTaskORM
	Candidate *BackupCandidate
}

// BackupCandidate 备份候选文件
type BackupCandidate struct {
	RelativePath string
	AbsPath      string
	FileSize     uint64
	ModifiedAt   int64
}

var scheduler *BackupScheduler

// Init 初始化备份调度器
func Init() {
	scheduler = &BackupScheduler{
		cron:          cron.New(cron.WithSeconds()),
		registerCh:    make(chan *BackupRegisterItem, 100),
		taskMap:       make(map[uint64]cron.EntryID),
		watchMap:      make(map[uint64][]string),
		debounceTimer: make(map[uint64]*time.Timer),
	}

	// 从数据库加载所有启用的备份任务
	tasks, err := db.GetEnabledBackupTasks()
	if err != nil {
		llog.L().Errorf("load enabled backup tasks failed: %v", err)
	} else {
		for _, task := range tasks {
			scheduler.addTask(task)
		}
	}

	// 启动注册消费者
	go scheduler.consumeRegisters()

	scheduler.cron.Start()

	llog.L().Infof("backup scheduler initialized, %d tasks loaded", len(tasks))
}

// GetScheduler 获取调度器实例
func GetScheduler() *BackupScheduler {
	return scheduler
}

// AddTask 添加备份任务到调度器
func (s *BackupScheduler) AddTask(task *native.BackupTaskORM) {
	s.addTask(task)
}

// RemoveTask 从调度器移除备份任务
func (s *BackupScheduler) RemoveTask(taskID uint64) {
	s.removeTask(taskID)
}

// addTask 添加备份任务（内部）
func (s *BackupScheduler) addTask(task *native.BackupTaskORM) {
	if task.Id == nil || task.Strategy == nil || task.SourcePath == nil {
		return
	}

	strategy := native.BackupStrategy(*task.Strategy)
	switch strategy {
	case native.BackupStrategy_BACKUP_STRATEGY_SCHEDULED:
		if task.CronExpression != nil && *task.CronExpression != "" {
			// 适配秒级 cron：如果表达式是 5 字段（标准格式），自动补 "0 " 前缀（秒字段）
			cronExpr := adaptCronExpr(*task.CronExpression)
			entryID, err := s.cron.AddFunc(cronExpr, func() {
				s.safeExecuteBackup(*task.Id)
			})
			if err != nil {
				llog.L().Errorf("add cron task failed, task_id: %d, cron: %s, err: %v", *task.Id, *task.CronExpression, err)
				return
			}
			s.mu.Lock()
			s.taskMap[*task.Id] = entryID
			s.mu.Unlock()
			llog.L().Infof("added scheduled backup task, id: %d, cron: %s", *task.Id, *task.CronExpression)
		}

	case native.BackupStrategy_BACKUP_STRATEGY_WATCH:
		s.addWatch(task)
	}
}

// removeTask 移除备份任务
func (s *BackupScheduler) removeTask(taskID uint64) {
	s.mu.Lock()
	if entryID, ok := s.taskMap[taskID]; ok {
		s.cron.Remove(entryID)
		delete(s.taskMap, taskID)
	}
	s.mu.Unlock()

	// 清理防抖定时器
	s.debounceMu.Lock()
	if timer, ok := s.debounceTimer[taskID]; ok {
		timer.Stop()
		delete(s.debounceTimer, taskID)
	}
	s.debounceMu.Unlock()

	s.removeWatch(taskID)
}

// TriggerBackup 手动触发备份
func (s *BackupScheduler) TriggerBackup(taskID uint64) {
	llog.L().Infof("triggering backup manually, task_id: %d", taskID)
	go s.safeExecuteBackup(taskID)
}

// safeExecuteBackup 带panic recover的备份执行
func (s *BackupScheduler) safeExecuteBackup(taskID uint64) {
	defer func() {
		if r := recover(); r != nil {
			llog.L().Errorf("backup task panicked, task_id: %d, recover: %v", taskID, r)
			db.UpdateBackupTaskStatus(taskID, native.BackupRunStatus_BACKUP_RUN_STATUS_FAILED, "internal error: panic recovered")
		}
	}()
	s.executeBackup(taskID)
}

// executeBackup 执行备份
func (s *BackupScheduler) executeBackup(taskID uint64) {
	// 1. 获取任务
	task, err := db.GetBackupTaskByID(taskID)
	if err != nil {
		llog.L().Errorf("get backup task failed, id: %d, err: %v", taskID, err)
		return
	}

	if task.SourcePath == nil || task.TargetDirId == nil {
		db.UpdateBackupTaskStatus(taskID, native.BackupRunStatus_BACKUP_RUN_STATUS_FAILED, "task config incomplete")
		return
	}

	// 2. 标记任务为运行中
	db.UpdateBackupTaskStatus(taskID, native.BackupRunStatus_BACKUP_RUN_STATUS_RUNNING, "")
	db.UpdateBackupTaskRunTime(taskID)

	// 3. 扫描源目录
	recursive := task.Recursive != nil && *task.Recursive
	ignoreRules := ""
	if task.IgnoreRules != nil {
		ignoreRules = *task.IgnoreRules
	}

	files, err := scanDirectory(*task.SourcePath, recursive, ignoreRules)
	if err != nil {
		db.UpdateBackupTaskStatus(taskID, native.BackupRunStatus_BACKUP_RUN_STATUS_FAILED, "scan directory failed: "+err.Error())
		return
	}

	// 将源路径的基目录名作为相对路径前缀，确保备份时创建顶层目录
	// 例如：源路径 /a/b/c，文件 e -> c/e；文件 sub/f -> c/sub/f
	sourceDirName := filepath.Base(*task.SourcePath)
	for _, f := range files {
		originalPath := f.RelativePath
		f.RelativePath = filepath.Join(sourceDirName, f.RelativePath)
		llog.L().Debugf("backup file: original=%s, fullRelativePath=%s", originalPath, f.RelativePath)
	}

	// 4. 逐文件判断是否需要备份
	var toBackup []*BackupCandidate
	for _, file := range files {
		record, err := db.GetBackupFileRecord(taskID, file.RelativePath)
		if err == gorm.ErrRecordNotFound {
			// 无历史记录，需要备份
			toBackup = append(toBackup, file)
		} else if err != nil {
			llog.L().Errorf("get backup file record failed: %s, err: %v", file.RelativePath, err)
			continue
		} else if record.Status != nil && *record.Status == int32(native.BackupFileStatus_BACKUP_FILE_STATUS_STALE) {
			// 上次注册后文件又变了，需要重新备份
			toBackup = append(toBackup, file)
		} else if record.ModifiedAt != nil && file.ModifiedAt > *record.ModifiedAt {
			// 修改时间变化，需要备份
			toBackup = append(toBackup, file)
		} else if record.FileSize != nil && file.FileSize != *record.FileSize {
			// 文件大小变化，需要备份
			toBackup = append(toBackup, file)
		}
	}

	llog.L().Infof("backup scan result, task_id: %d, total: %d, to_backup: %d", taskID, len(files), len(toBackup))

	// 5. 将需要备份的文件推入注册队列
	for _, candidate := range toBackup {
		s.registerCh <- &BackupRegisterItem{
			Task:      task,
			Candidate: candidate,
		}
	}

	// 6. 更新任务状态
	db.UpdateBackupTaskStatus(taskID, native.BackupRunStatus_BACKUP_RUN_STATUS_SUCCESS, "")
}

// scanDirectory 扫描目录
func scanDirectory(rootPath string, recursive bool, ignoreRules string) ([]*BackupCandidate, error) {
	var ign *ignore.GitIgnore
	if ignoreRules != "" {
		ign = ignore.CompileIgnoreLines(strings.Split(ignoreRules, "\n")...)
	}

	var results []*BackupCandidate

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsPermission(err) && info != nil && info.IsDir() {
				llog.L().Warnf("scanDirectory: permission denied, skipping directory: %s", path)
				return filepath.SkipDir
			}
			return err
		}
		if info.IsDir() {
			if !recursive && path != rootPath {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, _ := filepath.Rel(rootPath, path)

		if ign != nil && ign.MatchesPath(relPath) {
			llog.L().Infof("scanDirectory: ignoring file: %s", relPath)
			return nil
		}

		results = append(results, &BackupCandidate{
			RelativePath: relPath,
			AbsPath:      path,
			FileSize:     uint64(info.Size()),
			ModifiedAt:   info.ModTime().Unix(),
		})
		return nil
	})

	return results, err
}

// consumeRegisters 消费注册队列
func (s *BackupScheduler) consumeRegisters() {
	sem := make(chan struct{}, 5)

	for item := range s.registerCh {
		sem <- struct{}{}
		go func(item *BackupRegisterItem) {
			defer func() {
				<-sem
				if r := recover(); r != nil {
					llog.L().Errorf("register file panicked, file: %s, recover: %v", item.Candidate.RelativePath, r)
				}
			}()
			s.registerFile(item)
		}(item)
	}
}

// registerFile 注册文件（核心逻辑）
// 备份文件统一通过 storage.Dispatch 上传到目标目录绑定的所有存储提供者
// 无论是 local 还是 telegram，都通过各自驱动的 Upload 方法物理写入文件
func (s *BackupScheduler) registerFile(item *BackupRegisterItem) {
	ctx := context.Background()
	task := item.Task
	candidate := item.Candidate

	// === 注册前二次校验 ===
	info, err := os.Stat(candidate.AbsPath)
	if err != nil {
		llog.L().Debugf("file deleted before register, skip: %s", candidate.RelativePath)
		return
	}
	if info.ModTime().Unix() != candidate.ModifiedAt || uint64(info.Size()) != candidate.FileSize {
		llog.L().Infof("file changed before register, skip: %s", candidate.RelativePath)
		return
	}

	// 1. 计算文件 hash
	hash, err := computeFileHash(candidate.AbsPath)
	if err != nil {
		llog.L().Errorf("compute hash failed: %s, err: %v", candidate.RelativePath, err)
		return
	}

	// 2. 确保目标目录结构存在
	targetParentID, err := ensureDirStructure(ctx, *task.TargetDirId, candidate.RelativePath, derefStr(task.OwnerId))
	if err != nil {
		llog.L().Errorf("ensure dir structure failed: %s, err: %v", candidate.RelativePath, err)
		return
	}

	// 3. 检查是否已存在同名文件
	existingFile, _ := db.GetFileByParentAndName(targetParentID, filepath.Base(candidate.RelativePath), derefStr(task.OwnerId))

	var fileID string
	if existingFile != nil && existingFile.Hash != nil && *existingFile.Hash == hash {
		llog.L().Debugf("file already exists with same hash: %s", candidate.RelativePath)
		s.updateBackupFileRecord(task, candidate, hash, existingFile.Id, native.BackupFileStatus_BACKUP_FILE_STATUS_SKIPPED)
		return
	}

	// 4. 创建或更新 File 记录
	if existingFile != nil {
		oldFile := *existingFile
		existingFile.Hash = &hash
		existingFile.Size = &candidate.FileSize
		existingFile.IsReady = utils.BoolPtr(false) // 上传完成前标记为未就绪
		db.UpdateFileWithHistory(ctx, existingFile, &oldFile)
		fileID = *existingFile.Id
	} else {
		fileType, mime := fileutil.GetFileType(candidate.AbsPath, filepath.Base(candidate.RelativePath))
		newFile := &native.FileORM{
			Name:     utils.StringPtr(filepath.Base(candidate.RelativePath)),
			ParentId: &targetParentID,
			Size:     &candidate.FileSize,
			Hash:     &hash,
			Type:     utils.Int32Ptr(fileType),
			Mime:     &mime,
			IsReady:  utils.BoolPtr(false), // 上传完成前标记为未就绪
			OwnerId:  utils.StringPtr(derefStr(task.OwnerId)),
		}
		if err := db.CreateFile(newFile); err != nil {
			llog.L().Errorf("create file failed: %s, err: %v", candidate.RelativePath, err)
			return
		}
		fileID = *newFile.Id
	}

	// 5. 目标目录绑定决定落盘 provider（目录绑定 → 祖先继承 → 用户分配集）
	targetDir, err := db.GetFileByID(*task.TargetDirId)
	if err != nil || targetDir == nil || targetDir.OwnerId == nil || *targetDir.OwnerId == "" {
		llog.L().Errorf("get target dir or owner failed, target_dir_id: %s, err: %v", *task.TargetDirId, err)
		return
	}

	providerIds, err := storage.ResolveProviderIds(*targetDir.OwnerId, *task.TargetDirId)
	if err != nil {
		llog.L().Errorf("resolve providers for owner %s, dir %s failed: %v", *targetDir.OwnerId, *task.TargetDirId, err)
		return
	}

	providerIdsJSON, _ := json.Marshal(providerIds)
	targetProviderIds := string(providerIdsJSON)

	// 6. 创建 UploadRecord 并通过 storage.Dispatch 上传到所有目标存储提供者
	// 备份源文件在本地，将其路径作为 TempPath，走普通上传流程
	// 每个 provider（包括 local）都会通过自己的 Upload 方法物理写入文件
	fileName := filepath.Base(candidate.RelativePath)
	uploadRecord := &native.UploadRecordORM{
		FileHash:          &hash,
		FileName:          &fileName,
		FileSize:          &candidate.FileSize,
		ParentId:          &targetParentID,
		TempPath:          &candidate.AbsPath,  // 源文件路径作为临时文件路径
		OverwriteId:       utils.StringPtr(""), // 空字符串表示不覆盖
		Status:            utils.EnumPtr(native.LocationStatus_PPENDING),
		TaskStatus:        utils.EnumPtr(native.UploadStatus_PENDING),
		TargetProviderIds: &targetProviderIds,                          // 目标目录绑定解析出的 provider
		TaskType:          utils.EnumPtr(native.UploadTaskType_BACKUP), // 自动备份任务
	}

	if err := db.GetDB().Create(uploadRecord).Error; err != nil {
		llog.L().Errorf("create upload record for backup failed: %s, err: %v", candidate.RelativePath, err)
		return
	}

	llog.L().Infof("backup file dispatching, file: %s, hash: %s, target_dir: %s, providers: %s",
		candidate.RelativePath, hash, *task.TargetDirId, targetProviderIds)
	go storage.Dispatch(context.Background(), uploadRecord)

	// 7. 更新 BackupFileRecord
	s.updateBackupFileRecord(task, candidate, hash, &fileID, native.BackupFileStatus_BACKUP_FILE_STATUS_COMPLETED)

	// 8. 更新统计
	db.IncrementBackupStats(*task.Id, 1, candidate.FileSize)
}

// updateBackupFileRecord 更新或创建备份文件记录
func (s *BackupScheduler) updateBackupFileRecord(task *native.BackupTaskORM, candidate *BackupCandidate, hash string, fileID *string, status native.BackupFileStatus) {
	record, err := db.GetBackupFileRecord(*task.Id, candidate.RelativePath)
	if err == nil {
		// 记录存在，更新
		record.FileHash = &hash
		record.FileSize = &candidate.FileSize
		record.ModifiedAt = &candidate.ModifiedAt
		record.FileId = fileID
		record.Status = utils.EnumPtr(status)
		db.UpdateBackupFileRecord(record)
	} else {
		// 记录不存在，创建
		newRecord := &native.BackupFileRecordORM{
			TaskId:       task.Id,
			RelativePath: &candidate.RelativePath,
			FileHash:     &hash,
			FileSize:     &candidate.FileSize,
			ModifiedAt:   &candidate.ModifiedAt,
			FileId:       fileID,
			Status:       utils.EnumPtr(status),
		}
		db.CreateBackupFileRecord(newRecord)
	}
}

// ensureDirStructure 确保目标目录结构存在
// 创建子目录时逐层继承父目录的加密属性
func ensureDirStructure(ctx context.Context, targetParentID, relativePath, ownerID string) (string, error) {
	dir := filepath.Dir(relativePath)
	llog.C(ctx).Debugf("ensureDirStructure: relativePath=%s, dir=%s, targetParentID=%s", relativePath, dir, targetParentID)
	if dir == "." || dir == "" {
		return targetParentID, nil
	}

	// 检查目标父目录的加密设置
	targetDir, err := db.GetFileByID(targetParentID)
	if err == nil && targetDir != nil {
		llog.C(ctx).Infof("ensureDirStructure: target folder encryption=%v, targetParentID=%s",
			targetDir.EnableEncryption != nil && *targetDir.EnableEncryption, targetParentID)
	}

	parts := strings.Split(dir, string(filepath.Separator))
	currentParentID := targetParentID

	for _, part := range parts {
		existing, err := db.GetFileByParentAndName(currentParentID, part, ownerID)
		if err == nil && existing != nil {
			// 检查已存在目录的属性是否需要从父目录继承更新
			parentDir, err := db.GetFileByID(currentParentID)
			if err == nil && parentDir != nil {
				var fieldsToUpdate []string

				// 检查加密属性
				if parentDir.EnableEncryption != nil &&
					(existing.EnableEncryption == nil || *existing.EnableEncryption != *parentDir.EnableEncryption) {
					llog.C(ctx).Infof("ensureDirStructure: updating folder '%s' encryption from %v to %v",
						part, existing.EnableEncryption, parentDir.EnableEncryption)
					existing.EnableEncryption = parentDir.EnableEncryption
					fieldsToUpdate = append(fieldsToUpdate, "enable_encryption")
				}

				if len(fieldsToUpdate) > 0 {
					db.UpdateFile(existing, fieldsToUpdate)
				}
			}
			currentParentID = *existing.Id
			continue
		}

		// 从当前父目录获取加密设置（逐层继承）
		var parentEncrypt *bool
		parentDir, err := db.GetFileByID(currentParentID)
		if err == nil && parentDir != nil {
			if parentDir.EnableEncryption != nil {
				parentEncrypt = parentDir.EnableEncryption
				llog.C(ctx).Infof("ensureDirStructure: creating folder '%s', parent encryption=%v, parentID=%s",
					part, *parentEncrypt, currentParentID)
			} else {
				llog.C(ctx).Infof("ensureDirStructure: creating folder '%s', parent encryption=nil/false, parentID=%s",
					part, currentParentID)
			}
		}

		newDir := &native.FileORM{
			Name:             utils.StringPtr(part),
			ParentId:         &currentParentID,
			Type:             utils.EnumPtr(native.FileType_FOLDER),
			IsReady:          utils.BoolPtr(true),
			EnableEncryption: parentEncrypt, // 继承父目录的加密属性
			OwnerId:          &ownerID,
		}
		// 继承父目录的存储提供者绑定（父目录未显式绑定时回退用户分配集）
		if ids, rErr := storage.ResolveProviderIds(ownerID, currentParentID); rErr == nil && len(ids) > 0 {
			encoded := storage.EncodeProviderIds(ids)
			newDir.BoundProviderIds = &encoded
		} else if rErr != nil {
			llog.C(ctx).Warnf("ensureDirStructure: resolve providers failed (owner=%s, dir=%s): %v", ownerID, currentParentID, rErr)
		}
		if err := db.CreateFile(newDir); err != nil {
			// 并发竞态：其他 goroutine 可能刚创建了同一目录，重试查找
			existing, retryErr := db.GetFileByParentAndName(currentParentID, part, ownerID)
			if retryErr == nil && existing != nil {
				llog.C(ctx).Debugf("ensureDirStructure: directory '%s' created by concurrent goroutine, using existing", part)
				currentParentID = *existing.Id
				continue
			}
			return "", err
		}
		currentParentID = *newDir.Id
	}

	return currentParentID, nil
}

// derefStr 解引用 *string，nil 时返回空字符串
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// computeFileHash 计算文件 blake3 hash
func computeFileHash(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	h := blake3.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}

// adaptCronExpr 将标准 5 字段 cron 表达式适配为 6 字段（秒级）格式
// robfig/cron 使用 WithSeconds 时要求 6 字段：秒 分 时 日 月 周
// 用户输入的是标准 5 字段：分 时 日 月 周
func adaptCronExpr(expr string) string {
	fields := strings.Fields(expr)
	if len(fields) == 5 {
		return "0 " + expr
	}
	return expr
}

// ==================== WATCH 策略 ====================

// addWatch 添加文件监听
func (s *BackupScheduler) addWatch(task *native.BackupTaskORM) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()

	if s.watcher == nil {
		var err error
		s.watcher, err = fsnotify.NewWatcher()
		if err != nil {
			llog.L().Errorf("create fsnotify watcher failed: %v", err)
			return
		}
		go s.watchEvents()
	}

	if task.SourcePath == nil {
		return
	}

	s.watcher.Add(*task.SourcePath)
	s.watchMap[*task.Id] = []string{*task.SourcePath}

	if task.Recursive != nil && *task.Recursive {
		filepath.WalkDir(*task.SourcePath, func(path string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				s.watcher.Add(path)
				s.watchMap[*task.Id] = append(s.watchMap[*task.Id], path)
			}
			return nil
		})
	}

	llog.L().Infof("added watch for backup task, id: %d, path: %s", *task.Id, *task.SourcePath)
}

// removeWatch 移除文件监听
func (s *BackupScheduler) removeWatch(taskID uint64) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()

	if s.watcher == nil {
		return
	}

	if paths, ok := s.watchMap[taskID]; ok {
		for _, p := range paths {
			s.watcher.Remove(p)
		}
		delete(s.watchMap, taskID)
	}
}

// watchDebounceInterval Watch 模式防抖间隔：文件变动后等待该时间不再变动才触发备份
const watchDebounceInterval = 60 * time.Second

// watchEvents 处理文件变化事件
func (s *BackupScheduler) watchEvents() {
	for {
		select {
		case event, ok := <-s.watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Create == 0 && event.Op&fsnotify.Write == 0 {
				continue
			}
			s.handleFileChange(event.Name)

		case _, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

// handleFileChange 处理文件变化（带防抖）
// 文件频繁变动时（如编辑器不断保存），不会立即触发备份，
// 而是等待 60s 内不再有变动后才触发
func (s *BackupScheduler) handleFileChange(filePath string) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()

	for taskID, paths := range s.watchMap {
		for _, p := range paths {
			if strings.HasPrefix(filePath, p) {
				s.debounceBackup(taskID)
				return
			}
		}
	}
}

// debounceBackup 防抖触发备份
// 如果该任务已有待执行的定时器，则重置（顺延）；否则创建新定时器
func (s *BackupScheduler) debounceBackup(taskID uint64) {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()

	if timer, exists := s.debounceTimer[taskID]; exists {
		// 已有待执行的定时器，重置（Stop 返回 false 表示已触发或已停止，无需处理）
		timer.Stop()
	}

	s.debounceTimer[taskID] = time.AfterFunc(watchDebounceInterval, func() {
		s.debounceMu.Lock()
		delete(s.debounceTimer, taskID)
		s.debounceMu.Unlock()

		llog.L().Infof("watch debounce triggered, executing backup for task_id: %d", taskID)
		s.safeExecuteBackup(taskID)
	})
}
