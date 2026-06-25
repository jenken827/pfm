package service

import (
	"context"
	"fmt"
	"sync"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
	"github.com/google/uuid"
)

// === 异步任务管理 ===

type AsyncTaskStatus string

const (
	TaskPending  AsyncTaskStatus = "PENDING"
	TaskRunning  AsyncTaskStatus = "RUNNING"
	TaskSuccess  AsyncTaskStatus = "SUCCESS"
	TaskFailed   AsyncTaskStatus = "FAILED"
)

type AsyncTask struct {
	TaskID    string
	Status    AsyncTaskStatus
	Total     int32
	Completed int32
	Error     string
}

var (
	asyncTasks   = make(map[string]*AsyncTask)
	asyncTasksMu sync.RWMutex
)

func getAsyncTask(taskID string) *AsyncTask {
	asyncTasksMu.RLock()
	defer asyncTasksMu.RUnlock()
	return asyncTasks[taskID]
}

func updateAsyncTask(taskID string, fn func(t *AsyncTask)) {
	asyncTasksMu.Lock()
	defer asyncTasksMu.Unlock()
	if t, ok := asyncTasks[taskID]; ok {
		fn(t)
	}
}

// GetAsyncTaskProgress 查询异步任务进度
func GetAsyncTaskProgress(taskID string) (*AsyncTask, error) {
	t := getAsyncTask(taskID)
	if t == nil {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}
	return t, nil
}

// === SaveShareToMySpace ===

// SaveShareToMySpace 保存分享到我的网盘（异步）
// 创建后台任务，递归复制 file 记录（hash 引用同一内容，不额外占存储）
func SaveShareToMySpace(ctx context.Context, shareCode, targetParentID, displayName string) (string, error) {
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return "", fmt.Errorf("unauthorized")
	}

	// 1. 验证分享有效性
	share, err := db.GetFileShareByCode(shareCode)
	if err != nil {
		return "", fmt.Errorf("share not found: %w", err)
	}

	// 2. 验证目标目录属于当前用户
	targetDir, err := db.GetFileByID(targetParentID)
	if err != nil || targetDir == nil {
		return "", fmt.Errorf("target directory not found")
	}
	if targetDir.OwnerId == nil || *targetDir.OwnerId != userID {
		return "", fmt.Errorf("target directory not owned by you")
	}

	// 3. 获取源文件
	sourceFile, err := db.GetFileByID(*share.FileId)
	if err != nil || sourceFile == nil {
		return "", fmt.Errorf("source file not found")
	}

	// 4. 确定显示名称
	if displayName == "" {
		if sourceFile.Name != nil {
			displayName = *sourceFile.Name
		} else {
			displayName = "shared_file"
		}
	}

	// 5. 创建异步任务
	taskID := uuid.New().String()
	asyncTasksMu.Lock()
	asyncTasks[taskID] = &AsyncTask{
		TaskID: taskID,
		Status: TaskPending,
	}
	asyncTasksMu.Unlock()

	// 6. 启动后台任务
	utils.Go(func() {
		saveShareTask(taskID, sourceFile, targetParentID, displayName, userID)
	})

	return taskID, nil
}

// saveShareTask 后台执行保存逻辑
func saveShareTask(taskID string, source *native.FileORM, targetParentID, displayName, ownerID string) {
	updateAsyncTask(taskID, func(t *AsyncTask) {
		t.Status = TaskRunning
	})

	// 统计总数
	total := countFilesRecursive(*source.Id)
	updateAsyncTask(taskID, func(t *AsyncTask) {
		t.Total = total
	})

	var completed int32
	var saveErr error

	if source.Type != nil && *source.Type == int32(native.FileType_FOLDER) {
		saveErr = saveFolderRecursive(taskID, source, targetParentID, displayName, ownerID, &completed)
	} else {
		saveErr = saveSingleFile(source, targetParentID, displayName, ownerID)
		completed = 1
		updateAsyncTask(taskID, func(t *AsyncTask) {
			t.Completed = 1
		})
	}

	updateAsyncTask(taskID, func(t *AsyncTask) {
		if saveErr != nil {
			t.Status = TaskFailed
			t.Error = saveErr.Error()
		} else {
			t.Status = TaskSuccess
			t.Completed = completed
		}
	})

	if saveErr != nil {
		llog.L().Errorf("SaveShareToMySpace failed: taskID=%s, err=%v", taskID, saveErr)
	} else {
		llog.L().Infof("SaveShareToMySpace completed: taskID=%s, files=%d", taskID, completed)
	}
}

// countFilesRecursive 递归统计文件数
func countFilesRecursive(fileID string) int32 {
	children, err := db.GetFileChildren(fileID, "")
	if err != nil {
		return 1
	}
	count := int32(1)
	for _, child := range children {
		if child.Type != nil && *child.Type == int32(native.FileType_FOLDER) {
			count += countFilesRecursive(*child.Id)
		} else {
			count++
		}
	}
	return count
}

// saveSingleFile 保存单个文件（创建新记录，hash 引用同一内容）
func saveSingleFile(source *native.FileORM, parentID, name, ownerID string) error {
	newFile := &native.FileORM{
		Name:        &name,
		ParentId:    &parentID,
		OwnerId:     &ownerID,
		Hash:        source.Hash,
		Size:        source.Size,
		Mime:        source.Mime,
		Type:        source.Type,
		IsReady:     boolPtr(true),
	}
	return db.CreateFile(newFile)
}

// saveFolderRecursive 递归保存目录
func saveFolderRecursive(taskID string, source *native.FileORM, parentID, name, ownerID string, completed *int32) error {
	// 创建当前目录
	newDir := &native.FileORM{
		Name:     &name,
		ParentId: &parentID,
		OwnerId:  &ownerID,
		Type:     source.Type,
		IsReady:  boolPtr(true),
	}
	if err := db.CreateFile(newDir); err != nil {
		return fmt.Errorf("create folder '%s': %w", name, err)
	}

	*completed++
	updateAsyncTask(taskID, func(t *AsyncTask) {
		t.Completed = *completed
	})

	// 获取子文件（用空 ownerID 因为源文件属于分享者）
	children, err := db.GetFileChildren(*source.Id, "")
	if err != nil {
		return fmt.Errorf("list children of '%s': %w", name, err)
	}

	for _, child := range children {
		childName := ""
		if child.Name != nil {
			childName = *child.Name
		}

		if child.Type != nil && *child.Type == int32(native.FileType_FOLDER) {
			if err := saveFolderRecursive(taskID, child, *newDir.Id, childName, ownerID, completed); err != nil {
				llog.L().Warnf("save subfolder failed: %s/%s: %v", name, childName, err)
				continue
			}
		} else {
			if err := saveSingleFile(child, *newDir.Id, childName, ownerID); err != nil {
				llog.L().Warnf("save file failed: %s/%s: %v", name, childName, err)
				continue
			}
			*completed++
			updateAsyncTask(taskID, func(t *AsyncTask) {
				t.Completed = *completed
			})
		}
	}

	return nil
}

func boolPtr(b bool) *bool { return &b }

// === UserSpace 管理 ===

// GetUserSpace 获取当前用户的空间信息
func GetUserSpace(ctx context.Context) (*native.UserSpace, error) {
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return nil, fmt.Errorf("unauthorized")
	}

	orm, err := db.GetUserSpaceByUserID(userID)
	if err != nil {
		return nil, fmt.Errorf("user space not found, please login again to auto-create")
	}

	pb, err := orm.ToPB(ctx)
	if err != nil {
		return nil, err
	}
	return &pb, nil
}

// ListUserSpaces 管理员：列出所有用户空间
func ListUserSpaces(ctx context.Context, req *native.ListUserSpacesReq) (*native.ListUserSpacesResp, *models.Err) {
	if !isAdminUser(ctx) {
		return nil, &models.Err{E: fmt.Errorf("admin only"), C: native.RespCode_PERMISSION_DENIED}
	}

	page := req.GetPage()
	pageSize := req.GetPageSize()
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	spaces, total, err := db.ListUserSpaces(offset, pageSize)
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	var pbSpaces []*native.UserSpace
	for _, s := range spaces {
		pb, err := s.ToPB(ctx)
		if err != nil {
			continue
		}
		pbSpaces = append(pbSpaces, &pb)
	}

	return &native.ListUserSpacesResp{
		Spaces: pbSpaces,
		Total:  uint32Ptr(uint32(total)),
	}, nil
}

// UpdateUserSpace 管理员：更新用户空间配额
func UpdateUserSpace(ctx context.Context, req *native.UpdateUserSpaceReq) *models.Err {
	if !isAdminUser(ctx) {
		return &models.Err{E: fmt.Errorf("admin only"), C: native.RespCode_PERMISSION_DENIED}
	}

	userID := req.GetUserId()
	space, err := db.GetUserSpaceByUserID(userID)
	if err != nil {
		return &models.Err{E: fmt.Errorf("user space not found"), C: native.RespCode_NOT_FOUND}
	}

	if req.QuotaBytes != nil {
		space.QuotaBytes = req.QuotaBytes
	}
	if err := db.UpdateUserSpace(space); err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	return nil
}

func uint32Ptr(v uint32) *uint32 { return &v }
