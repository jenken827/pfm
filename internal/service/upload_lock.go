package service

import "sync"

// uploadTargetKey 同一「目标文件」的串行化键：owner + 父目录 + 文件名。
//
// 为什么按这个键：files 上的唯一索引 idx_f_npi 就是 (owner_id, parent_id, name)。
// 同一目标文件被并发上传时，两个请求会各自"查重 → 建记录 → 写 temp → 分发"，
// 结果是 A 插入成功、B 撞唯一约束；而 B 的错误清理又会删掉 A/B 共享的按 hash
// 内容寻址的临时文件（TempDir/<hash>/<name>），让 A 的分发在
// "temp file not found" 上整单失败。串行化后后到者能看到前者刚建的记录，
// 走 "同名同 hash → 秒传" 分支直接返回成功，不再重复上传。
type uploadTargetKey struct {
	ownerID  string
	parentID string
	fileName string
}

// uploadTargetLock 每个键一个互斥锁 + 引用计数（引用归零回收，避免 map 无限增长）
type uploadTargetLock struct {
	mu   sync.Mutex
	refs int
}

var uploadTargetLocks = struct {
	sync.Mutex
	m map[uploadTargetKey]*uploadTargetLock
}{m: make(map[uploadTargetKey]*uploadTargetLock)}

// lockUploadTarget 获取「同一目标文件」的上传锁，返回解锁函数（调用方 defer）。
//
// 注意加锁顺序：先持全局表锁取/建锁并 refs++，再等在各自的互斥锁上；解锁时的
// 回收也在全局表锁内完成，保证「refs 归零即无人持有/等待」。
func lockUploadTarget(ownerID, parentID, fileName string) func() {
	key := uploadTargetKey{ownerID: ownerID, parentID: parentID, fileName: fileName}

	uploadTargetLocks.Lock()
	l, ok := uploadTargetLocks.m[key]
	if !ok {
		l = &uploadTargetLock{}
		uploadTargetLocks.m[key] = l
	}
	l.refs++
	uploadTargetLocks.Unlock()

	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		uploadTargetLocks.Lock()
		l.refs--
		if l.refs == 0 {
			delete(uploadTargetLocks.m, key)
		}
		uploadTargetLocks.Unlock()
	}
}
