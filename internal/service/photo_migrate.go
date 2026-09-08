package service

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// mediaExts 参与年/月归档迁移的媒体文件扩展名（图片 + 视频）
var mediaExts = map[string]bool{
	// 图片
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
	".webp": true, ".heic": true, ".heif": true, ".bmp": true,
	// 视频
	".mp4": true, ".mov": true, ".avi": true, ".mkv": true,
	".3gp": true, ".m4v": true, ".webm": true, ".wmv": true,
	".flv": true, ".ts": true, ".mts": true, ".m2ts": true,
}

// MigratePhotoResult 图片年/月归档迁移结果统计
type MigratePhotoResult struct {
	Scanned        int      // 扫描到的图片总数
	Moved          int      // 可迁移（dry-run 为预计迁移数）
	AlreadyInPlace int      // 已处于正确年/月目录（跳过）
	NoDate         int      // 无法解析日期（保持原位置）
	Failed         int      // 失败
	Failures       []string // 失败明细（含文件 id/原因）
	CreatedDirs    []string // 新创建的年/月目录（dry-run 为预计创建）
}

// MigratePhotosToYearMonth 修复旧数据：把 baseParentID 子树下的所有媒体文件
// （图片 + 视频）按文件名解析拍摄日期，移动到 "父目录/年/月" 目录（不存在则自动创建）。
//
// 与相册备份的年/月归档规则一致（解析规则见 ParsePhotoDateFromFilename）：
//   - 已处于正确年/月目录的文件跳过
//   - 无法解析日期的文件保持原位置不动
//   - 目标目录同名冲突自动追加后缀（与移动/恢复行为一致）
//   - 移动仅更新 files.parent_id（逻辑目录），物理存储按 hash 寻址不受影响
//
// dryRun 为 true 时只统计与预览，不创建目录、不移动文件。
func MigratePhotosToYearMonth(baseParentID string, dryRun bool) (*MigratePhotoResult, error) {
	base, err := db.GetFileByID(baseParentID)
	if err != nil {
		return nil, fmt.Errorf("查询基础目录失败: %w", err)
	}
	if base.Type == nil || *base.Type != int32(native.FileType_FOLDER) {
		return nil, fmt.Errorf("parent_id %s 不是文件夹", baseParentID)
	}
	ownerID := strPtrVal(base.OwnerId)
	if ownerID == "" {
		return nil, fmt.Errorf("基础目录缺少 owner_id，无法迁移")
	}

	result := &MigratePhotoResult{}

	// 已统计的目录集合：目录列表去重，且每个月份目录的存在性只检查一次
	// （dry-run 不创建目录，若不缓存会导致同一月份重复输出）
	listedDirs := map[string]bool{}

	// 递归收集子树下所有媒体文件（图片 + 视频，不含软删除）
	var photos []*native.FileORM
	var walk func(parentID string) error
	walk = func(parentID string) error {
		children, err := db.GetFileChildren(parentID, ownerID)
		if err != nil {
			return err
		}
		for _, c := range children {
			if c.Id == nil || c.Name == nil || c.ParentId == nil {
				continue
			}
			if c.Type != nil && *c.Type == int32(native.FileType_FOLDER) {
				if err := walk(*c.Id); err != nil {
					return err
				}
				continue
			}
			if mediaExts[strings.ToLower(filepath.Ext(*c.Name))] {
				photos = append(photos, c)
			}
		}
		return nil
	}
	if err := walk(baseParentID); err != nil {
		return nil, fmt.Errorf("扫描目录失败: %w", err)
	}
	result.Scanned = len(photos)

	for _, f := range photos {
		name := *f.Name
		taken := ParsePhotoDateFromFilenameWithConfig(name, config.GetConfig().PhotoDate)
		if taken == nil {
			result.NoDate++
			continue
		}
		ym := fmt.Sprintf("%04d/%02d", taken.Year(), taken.Month())

		// 已处于正确年/月目录 → 跳过
		if isInYearMonthDir(f, baseParentID, *taken) {
			result.AlreadyInPlace++
			continue
		}

		// 目标目录不存在则创建（记录到报告，去重 + 存在性只查一次）
		if !listedDirs[ym] {
			listedDirs[ym] = true
			if !dirPathExists(ownerID, baseParentID, ym) {
				result.CreatedDirs = append(result.CreatedDirs, ym)
			}
		}

		if dryRun {
			result.Moved++
			llog.L().Infof("migrate photo (dry-run): %s -> %s", name, ym)
			continue
		}

		leafID, err := ensureDirPathForOwner(ownerID, baseParentID, ym)
		if err != nil {
			result.Failed++
			result.Failures = append(result.Failures,
				fmt.Sprintf("%s (id=%s): 创建目录 %s 失败: %v", name, *f.Id, ym, err))
			continue
		}

		// 同名冲突自动加后缀，然后移动（仅更新 parent_id / name）
		newName := resolveNameConflict(leafID, name, ownerID)
		updates := map[string]interface{}{"parent_id": leafID}
		if newName != "" {
			updates["name"] = newName
		}
		if err := db.GetDB().Model(&native.FileORM{}).
			Where("id = ?", *f.Id).
			Updates(updates).Error; err != nil {
			result.Failed++
			result.Failures = append(result.Failures,
				fmt.Sprintf("%s (id=%s): 移动到 %s 失败: %v", name, *f.Id, ym, err))
			continue
		}
		result.Moved++
		if newName != "" {
			llog.L().Infof("migrate photo: %s -> %s/%s (重命名为 %s)", name, ym, newName, newName)
		} else {
			llog.L().Infof("migrate photo: %s -> %s/%s", name, ym, name)
		}
	}

	return result, nil
}

// dirPathExists 判断 ownerID 下 parentID/relPath 的叶子目录是否已存在
func dirPathExists(ownerID, parentID, relPath string) bool {
	currentID := parentID
	for _, seg := range strings.Split(relPath, "/") {
		dir, err := db.FindFileByParentAndName(currentID, seg, ownerID)
		if err != nil || dir == nil || dir.Id == nil {
			return false
		}
		currentID = *dir.Id
	}
	return true
}

// isInYearMonthDir 判断文件是否已位于 基础目录/YYYY/MM 的正确年/月目录中
// 父链（自底向上）需为 [MM, YYYY] 且再上一层为 baseParentID
func isInYearMonthDir(f *native.FileORM, baseParentID string, taken time.Time) bool {
	parentID := strPtrVal(f.ParentId)
	if parentID == "" {
		return false
	}
	mmDir, err := db.GetFileByID(parentID)
	if err != nil || mmDir.Name == nil || strPtrVal(mmDir.ParentId) == "" {
		return false
	}
	if *mmDir.Name != fmt.Sprintf("%02d", taken.Month()) {
		return false
	}
	yyyyDir, err := db.GetFileByID(*mmDir.ParentId)
	if err != nil || yyyyDir.Name == nil {
		return false
	}
	if *yyyyDir.Name != fmt.Sprintf("%04d", taken.Year()) {
		return false
	}
	return strPtrVal(yyyyDir.ParentId) == baseParentID
}

// strPtrVal 安全解引用 string 指针
func strPtrVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
