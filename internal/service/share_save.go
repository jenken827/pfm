package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/models"
	"github.com/jenken827/pfm/internal/storage"
	"github.com/jenken827/pfm/pkg/llog"
	"github.com/jenken827/pfm/utils"
)

// === 异步任务管理 ===

type AsyncTaskStatus string

const (
	TaskPending AsyncTaskStatus = "PENDING"
	TaskRunning AsyncTaskStatus = "RUNNING"
	TaskSuccess AsyncTaskStatus = "SUCCESS"
	TaskFailed  AsyncTaskStatus = "FAILED"
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
func saveShareTask(taskID string, source *entity.FileORM, targetParentID, displayName, ownerID string) {
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

	// 保存到目标目录的文件/目录，其存储提供者绑定继承目标目录
	// （解析失败不阻断保存：留空 = 未显式绑定，落库后由解析规则回退到分配集）
	binding := ""
	if ids, err := storage.ResolveProviderIds(ownerID, targetParentID); err != nil {
		llog.L().Warnf("resolve provider ids for share save failed (owner=%s, dir=%s): %v", ownerID, targetParentID, err)
	} else {
		binding = storage.EncodeProviderIds(ids)
	}

	if source.Type != nil && *source.Type == int32(native.FileType_FOLDER) {
		saveErr = saveFolderRecursive(taskID, source, targetParentID, displayName, ownerID, binding, &completed)
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
func saveSingleFile(source *entity.FileORM, parentID, name, ownerID string) error {
	newFile := &entity.FileORM{
		Name:     &name,
		ParentId: &parentID,
		OwnerId:  &ownerID,
		Hash:     source.Hash,
		Size:     source.Size,
		Mime:     source.Mime,
		Type:     source.Type,
		IsReady:  boolPtr(true),
	}
	// 容量统计：分享保存新增文件影响用户占用
	if err := db.CreateFile(newFile); err != nil {
		return err
	}
	MarkFileCapacityChanged(ownerID)
	return nil
}

// saveFolderRecursive 递归保存目录
// binding：本次保存创建的目录统一继承目标目录解析出的绑定（JSON 数组，空串 = 未显式绑定）
func saveFolderRecursive(taskID string, source *entity.FileORM, parentID, name, ownerID, binding string, completed *int32) error {
	// 创建当前目录
	newDir := &entity.FileORM{
		Name:     &name,
		ParentId: &parentID,
		OwnerId:  &ownerID,
		Type:     source.Type,
		IsReady:  boolPtr(true),
	}
	if binding != "" {
		newDir.BoundProviderIds = utils.StringPtr(binding)
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
			if err := saveFolderRecursive(taskID, child, *newDir.Id, childName, ownerID, binding, completed); err != nil {
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

// GetUserSpace 获取当前用户的空间信息（用量由异步容量更新器维护，纯读表零聚合）
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
	return pb, nil
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
		pbSpaces = append(pbSpaces, pb)
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

// AssignRootDir 管理员：为用户分配根目录（首次创建或重新绑定存储提供者）
func AssignRootDir(ctx context.Context, req *native.AssignRootDirReq) (*native.AssignRootDirResp, *models.Err) {
	if mErr := RequireAdmin(ctx); mErr != nil {
		return nil, mErr
	}

	targetUserID := req.GetUserId()
	providerIds := req.GetProviderIds()

	if len(providerIds) == 0 {
		return nil, &models.Err{
			E: fmt.Errorf("至少需要指定一个存储提供者"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 验证提供者是否存在且启用
	providers, err := db.GetEnabledProvidersByIds(providerIds)
	if err != nil {
		llog.C(ctx).Errorf("query providers failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	if len(providers) != len(providerIds) {
		return nil, &models.Err{
			E: fmt.Errorf("部分存储提供者无效或已禁用"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	// 将 provider IDs 序列化为 JSON
	providerIdsJSON, err := json.Marshal(providerIds)
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	boundProviderIds := string(providerIdsJSON)

	// 查询该用户是否已有 UserSpace
	existingSpace, err := db.GetUserSpaceByUserID(targetUserID)
	if err == nil && existingSpace != nil {
		// 已有 UserSpace，更新 bound_provider_ids
		existingSpace.BoundProviderIds = &boundProviderIds
		if err := db.UpdateUserSpace(existingSpace); err != nil {
			return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
		}
		// 根目录行同步该绑定：根目录是「无显式绑定的子孙目录」的兜底来源，
		// 保持与 UserSpace 一致可让解析规则与展示都直观
		if mErr := updateRootDirBinding(existingSpace, boundProviderIds); mErr != nil {
			return nil, mErr
		}
		llog.C(ctx).Infof("updated bound_provider_ids for user %s: %s", targetUserID, boundProviderIds)
		return &native.AssignRootDirResp{RootFileId: existingSpace.RootFileId}, nil
	}

	// 没有 UserSpace，创建根目录 File + UserSpace 记录
	rootFileID := uuid.New().String()
	rootFile := &entity.FileORM{
		Id:               &rootFileID,
		Name:             &rootFileID, // name = UUID
		ParentId:         utils.StringPtr(""),
		Type:             utils.EnumPtr(native.FileType_FOLDER),
		IsReady:          utils.BoolPtr(true),
		EnableEncryption: utils.BoolPtr(false),
		BoundProviderIds: &boundProviderIds,
		OwnerId:          &targetUserID,
	}
	if err := db.CreateFile(rootFile); err != nil {
		llog.C(ctx).Errorf("create root dir failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	newSpace := &entity.UserSpaceORM{
		UserId:           &targetUserID,
		RootFileId:       &rootFileID,
		BoundProviderIds: &boundProviderIds,
		QuotaBytes:       utils.Uint64Ptr(0),
		UsedBytes:        utils.Uint64Ptr(0),
	}
	if err := db.CreateUserSpace(newSpace); err != nil {
		llog.C(ctx).Errorf("create user space failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	llog.C(ctx).Infof("created root dir %s for user %s with providers: %s", rootFileID, targetUserID, boundProviderIds)
	return &native.AssignRootDirResp{RootFileId: &rootFileID}, nil
}

// updateRootDirBinding 把用户根目录行的 bound_provider_ids 同步为管理员最新分配。
// 根目录是所有「未显式绑定目录」的解析兜底点，与 UserSpace 保持一致。
func updateRootDirBinding(space *entity.UserSpaceORM, boundProviderIds string) *models.Err {
	if space == nil || space.RootFileId == nil || *space.RootFileId == "" {
		return nil
	}
	orm := entity.FileORM{
		Id:               space.RootFileId,
		BoundProviderIds: &boundProviderIds,
	}
	if err := db.UpdateDir(&orm); err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	return nil
}

func uint32Ptr(v uint32) *uint32 { return &v }
