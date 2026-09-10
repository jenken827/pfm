package service

import (
	"sync"
	"time"

	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
)

// ============================================================
// 用户占用容量异步更新器（user_spaces.used_bytes）
//
// 背景：used_bytes 是聚合值（用户所有文件 size 总和），若每次读请求实时
// 聚合会随数据规模线性放大。方案：写时标记 + 防抖 + 异步批量重算写回。
//   1. 文件变动（上传/覆盖/删除/清空回收站/分享保存等）后调用
//      MarkFileCapacityChanged(ownerID)，把受影响的用户记入"脏集合"；
//   2. 800ms 防抖合并短时间内的多次变动；
//   3. 定时器触发 flush：对脏用户重算 used_bytes 并写回
//      user_spaces.used_bytes。读路径（GetUserSpace/ListUserSpaces）
//      直接查表，零聚合。
//
// 口径：used_bytes = 用户所有文件（含回收站软删，物理仍占用存储）size 总和，
// 不含文件夹。永久删除/清空回收站后行消失，总量自然回落。
// 注：文件夹不展示大小（子树遍历统计不划算，与主流网盘一致），
// 故不再维护 folder 行的 size 统计。
// ============================================================

const (
	// capacityFlushDelay 防抖窗口：变动后等待该时长无新变动才 flush
	capacityFlushDelay = 800 * time.Millisecond
)

// dirtyUsers 待重算 used_bytes 的用户
var capacityDirtyUsers = struct {
	sync.Mutex
	users map[string]bool
}{users: make(map[string]bool)}

var (
	capacityTimer    *time.Timer
	capacityTimerMu  sync.Mutex
	capacityFlushing sync.Mutex // 防并发 flush
)

// MarkCapacityChanged 记录一次用户容量变动（内部自动启动防抖 flush，可安全高频调用）
func MarkCapacityChanged(ownerID string) {
	if ownerID == "" {
		return
	}
	capacityDirtyUsers.Lock()
	capacityDirtyUsers.users[ownerID] = true
	capacityDirtyUsers.Unlock()

	capacityTimerMu.Lock()
	if capacityTimer == nil {
		capacityTimer = time.AfterFunc(capacityFlushDelay, scheduleFlush)
	} else {
		capacityTimer.Reset(capacityFlushDelay)
	}
	capacityTimerMu.Unlock()
}

// MarkFileCapacityChanged 便捷入口：给定文件所属用户，标记其占用容量需要重算
func MarkFileCapacityChanged(ownerID string) {
	MarkCapacityChanged(ownerID)
}

// scheduleFlush 定时器回调：确保串行执行，并在执行期间又有新变动时再次调度
func scheduleFlush() {
	capacityFlushing.Lock()
	defer capacityFlushing.Unlock()

	flushCapacityStats()

	// flush 期间可能有新标记进来：若仍有脏数据则再次调度
	capacityDirtyUsers.Lock()
	remaining := len(capacityDirtyUsers.users)
	capacityDirtyUsers.Unlock()
	if remaining > 0 {
		capacityTimerMu.Lock()
		if capacityTimer != nil {
			capacityTimer.Reset(capacityFlushDelay)
		}
		capacityTimerMu.Unlock()
	}
}

// flushCapacityStats 执行一轮容量重算：清空脏集合 → 批量重算 → 写回
func flushCapacityStats() {
	// 1. 取出并清空脏集合
	capacityDirtyUsers.Lock()
	users := make([]string, 0, len(capacityDirtyUsers.users))
	for u := range capacityDirtyUsers.users {
		users = append(users, u)
	}
	capacityDirtyUsers.users = make(map[string]bool)
	capacityDirtyUsers.Unlock()

	if len(users) == 0 {
		return
	}

	llog.L().Debugf("capacity flush: users=%d", len(users))

	// 2. 用户用量写回 user_spaces.used_bytes（含回收站口径）
	for _, uid := range users {
		used, err := db.SumAllFileSizesByOwner(uid)
		if err != nil {
			llog.L().Errorf("capacity flush: sum sizes failed, user: %s, err: %v", uid, err)
			continue
		}
		if err := db.UpdateUserSpaceUsedBytes(uid, used); err != nil {
			llog.L().Errorf("capacity flush: update used_bytes failed, user: %s, err: %v", uid, err)
		}
	}
}

// RefreshAllCapacityStats 全量刷新所有用户占用（服务启动时调用一次，
// 补齐历史存量 used_bytes。启动异步执行不阻塞）
func RefreshAllCapacityStats() {
	go func() {
		userIDs, err := db.GetAllUserIDs()
		if err != nil {
			llog.L().Errorf("refresh all capacity: get user ids failed: %v", err)
			return
		}
		for _, uid := range userIDs {
			used, err := db.SumAllFileSizesByOwner(uid)
			if err != nil {
				llog.L().Errorf("refresh all capacity: sum user sizes failed, user: %s, err: %v", uid, err)
				continue
			}
			if err := db.UpdateUserSpaceUsedBytes(uid, used); err != nil {
				llog.L().Errorf("refresh all capacity: update user used_bytes failed, user: %s, err: %v", uid, err)
			}
		}
		llog.L().Infof("refresh all capacity: completed, users=%d", len(userIDs))
	}()
}

// StartCapacityStatsUpdater 启动容量更新器（幂等；纯后台协程，无请求上下文依赖）
var capacityStatsOnce sync.Once

func StartCapacityStatsUpdater() {
	capacityStatsOnce.Do(func() {
		// 定时器按需由 MarkCapacityChanged 创建，此处仅记录启动日志
		llog.L().Infof("capacity stats updater started (debounce %v)", capacityFlushDelay)
	})
}
