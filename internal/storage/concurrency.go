package storage

import (
	"sync"
	"time"

	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
)

const (
	// DefaultMaxConcurrentUploads 默认全局最大并发上传数
	DefaultMaxConcurrentUploads = 10
	// DefaultProviderConcurrentUploads 默认单个存储提供者最大并发上传数
	DefaultProviderConcurrentUploads = 3
)

// uploadKey 并发控制键

// uploadKey 并发控制键
type uploadKey struct {
	blake3Hash string
	providerID uint32
}

// providerSemaphore 存储提供者信号量

// providerSemaphore 存储提供者信号量
type providerSemaphore struct {
	semaphore chan struct{}
}

// dispatcher 分发器

// dispatcher 分发器
type dispatcher struct {
	processing          sync.Map // map[uploadKey]chan struct{}
	lockTime            sync.Map // map[uploadKey]time.Time — 锁创建时间，用于超时清理
	globalSemaphore     chan struct{}
	providerSemaphores  sync.Map // map[uint32]*providerSemaphore
	providerConcurrence sync.Map // map[uint32]int 存储提供者配置的并发数
}

var globalDispatcher = &dispatcher{}

func init() {
	// 初始化全局信号量，后续会被 config 覆盖
	maxConcurrent := DefaultMaxConcurrentUploads
	if cfg := config.GetConfig(); cfg != nil && cfg.Upload.MaxConcurrentUploads > 0 {
		maxConcurrent = cfg.Upload.MaxConcurrentUploads
	}
	globalDispatcher.globalSemaphore = make(chan struct{}, maxConcurrent)
}

// getProviderSemaphore 获取存储提供者的信号量

// getProviderSemaphore 获取存储提供者的信号量
func getProviderSemaphore(providerID uint32) chan struct{} {
	if v, ok := globalDispatcher.providerSemaphores.Load(providerID); ok {
		return v.(*providerSemaphore).semaphore
	}

	// 从数据库读取配置
	concurrent := DefaultProviderConcurrentUploads
	if provider, err := db.GetProviderByID(providerID); err == nil && provider != nil {
		if provider.MaxConcurrentUploads != nil && *provider.MaxConcurrentUploads > 0 {
			concurrent = int(*provider.MaxConcurrentUploads)
		}
	}

	sem := make(chan struct{}, concurrent)
	if actual, loaded := globalDispatcher.providerSemaphores.LoadOrStore(providerID, &providerSemaphore{semaphore: sem}); loaded {
		return actual.(*providerSemaphore).semaphore
	}
	globalDispatcher.providerConcurrence.Store(providerID, concurrent)
	llog.L().Infof("Created semaphore for provider %d with concurrency %d", providerID, concurrent)
	return sem
}

// refreshProviderSemaphore 刷新存储提供者的信号量（配置变更时调用）

// refreshProviderSemaphore 刷新存储提供者的信号量（配置变更时调用）
func RefreshProviderSemaphore(providerID uint32) {
	globalDispatcher.providerSemaphores.Delete(providerID)
	globalDispatcher.providerConcurrence.Delete(providerID)
}

// Dispatch 分发处理（文件上传完成后调用）
// 支持两种场景：
// 1. 普通上传：有临时文件，从临时文件上传
// 2. 同步任务：无临时文件，从现有提供者同步

// acquireLock 获取并发控制锁（非阻塞），带过期锁自动清理
func acquireLock(key uploadKey) bool {
	cleanupExpiredLocks()

	ch := make(chan struct{})
	_, loaded := globalDispatcher.processing.LoadOrStore(key, ch)
	if loaded {
		// 已经有任务在处理，直接返回false，不等待
		return false
	}
	// 记录锁的创建时间
	globalDispatcher.lockTime.Store(key, time.Now())
	return true
}

// releaseLock 释放并发控制锁

// releaseLock 释放并发控制锁
func releaseLock(key uploadKey) {
	if ch, ok := globalDispatcher.processing.Load(key); ok {
		close(ch.(chan struct{}))
		globalDispatcher.processing.Delete(key)
		globalDispatcher.lockTime.Delete(key)
	}
}

// cleanupExpiredLocks 清理超时未释放的锁（防止崩溃后锁永久占用）

// cleanupExpiredLocks 清理超时未释放的锁（防止崩溃后锁永久占用）
func cleanupExpiredLocks() {
	const lockTimeout = 30 * time.Minute
	globalDispatcher.lockTime.Range(func(key, value interface{}) bool {
		if t, ok := value.(time.Time); ok && time.Since(t) > lockTimeout {
			llog.L().Warnf("cleaning expired upload lock: hash=%s provider=%v",
				key.(uploadKey).blake3Hash[:8], key.(uploadKey).providerID)
			releaseLock(key.(uploadKey))
		}
		return true
	})
}

// resumeDispatch 恢复分发（系统启动时调用）
