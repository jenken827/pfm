package service

import (
	"fmt"
	"sync"
	"time"
)

// uploadDoneCache 缓存已完成上传的 uploadID，防止异步处理窗口期内前端重试导致报错
// 等效于 Redis SETNX upload:done:{id} = 1 EX 300
type uploadDoneCache struct {
	mu    sync.RWMutex
	items map[string]time.Time
}

var uploadDone = &uploadDoneCache{
	items: make(map[string]time.Time),
}

// MarkDone 标记上传已完成（SETNX + EX 300s）
func (c *uploadDoneCache) MarkDone(uploadID uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := fmt.Sprintf("%d", uploadID)
	if _, exists := c.items[key]; !exists {
		c.items[key] = time.Now().Add(5 * time.Minute)
	}
}

// IsDone 检查上传是否已完成
func (c *uploadDoneCache) IsDone(uploadID uint32) bool {
	c.mu.RLock()
	key := fmt.Sprintf("%d", uploadID)
	expiry, exists := c.items[key]
	c.mu.RUnlock()

	if !exists {
		return false
	}
	if time.Now().After(expiry) {
		// 已过期，清理
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		return false
	}
	return true
}

func init() {
	// 定期清理过期项
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			uploadDone.mu.Lock()
			now := time.Now()
			for k, v := range uploadDone.items {
				if now.After(v) {
					delete(uploadDone.items, k)
				}
			}
			uploadDone.mu.Unlock()
		}
	}()
}
