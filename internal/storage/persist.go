package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/utils"
)

// failFileLocation 标记文件位置为失败状态
func failFileLocation(fl *entity.FileLocationORM, msg string) error {
	fl.Message = &msg
	fl.Status = (*int32)(native.LocationStatus_PFAILED.Enum())
	// 乐观锁：仅当非 PCOMPLETED 时才标记失败，防止并发覆盖已完成分片
	if fl.Id != nil {
		_ = db.TransitionFileLocationStatus(*fl.Id,
			[]native.LocationStatus{native.LocationStatus_PPENDING, native.LocationStatus_PUPLOADING, native.LocationStatus_PFAILED},
			native.LocationStatus_PFAILED, &msg)
	}
	return errors.New(msg)
}

// prepareEncryptor 生成加密器（如果需要加密），返回 (encryptor, salt, error)
// salt 非空表示启用了加密

// saveUploadChunks 处理上传结果，保存分片信息到数据库
func saveUploadChunks(fl *entity.FileLocationORM, refs []types.FileRef, providerID uint32) uint64 {
	chunkTotal := uint32(len(refs))
	var totalUploadedSize uint64

	// 安全获取 hash
	hash := ""
	if fl.Blake3Hash != nil {
		hash = *fl.Blake3Hash
	}

	for i, ref := range refs {
		chunkNumber := uint32(i)

		if i == 0 {
			// 第一个分片：更新已有的 FileLocation 记录
			updateFirstChunkLocation(fl, ref, chunkTotal, chunkNumber)
			totalUploadedSize += ref.ChunkSize
		} else {
			// 后续分片：创建新的 FileLocation 记录
			if err := createChunkLocation(fl, ref, chunkTotal, chunkNumber); err != nil {
				llog.L().Errorf("create file location for chunk %d failed: %v", chunkNumber, err)
				continue
			}
			totalUploadedSize += ref.ChunkSize
		}

		llog.L().Infof("chunk %d/%d uploaded, hash: %s, provider: %d, file_id: %s",
			chunkNumber+1, chunkTotal, hash, providerID, ref.ProviderFileID)
	}

	return totalUploadedSize
}

// updateFirstChunkLocation 更新第一个分片的 FileLocation 记录

// updateFirstChunkLocation 更新第一个分片的 FileLocation 记录
func updateFirstChunkLocation(fl *entity.FileLocationORM, ref types.FileRef, chunkTotal, chunkNumber uint32) {
	updateFields := []string{"provider_file_id", "status", "chunk_number", "chunk_total", "message"}
	fl.ProviderFileId = &ref.ProviderFileID
	fl.Status = (*int32)(native.LocationStatus_PCOMPLETED.Enum())
	fl.ChunkTotal = &chunkTotal
	fl.ChunkNumber = &chunkNumber
	// 清除之前的错误信息
	emptyMsg := ""
	fl.Message = &emptyMsg

	if ref.Path != "" {
		fl.Path = &ref.Path
		updateFields = append(updateFields, "path")
	}
	if ref.Hash != "" {
		fl.Hash = &ref.Hash
		updateFields = append(updateFields, "hash")
	}
	if ref.Size > 0 {
		fl.Size = &ref.Size
		updateFields = append(updateFields, "size")
	}
	if ref.ChunkSize > 0 {
		fl.ChunkSize = &ref.ChunkSize
		updateFields = append(updateFields, "chunk_size")
	}
	if ref.ChunkHash != "" {
		fl.ChunkHash = &ref.ChunkHash
		updateFields = append(updateFields, "chunk_hash")
	}
	if ref.ChannelID != "" {
		fl.ChannelId = &ref.ChannelID
		updateFields = append(updateFields, "channel_id")
	}
	if ref.MessageID != 0 {
		fl.MessageId = utils.Int32Ptr(int32(ref.MessageID))
		updateFields = append(updateFields, "message_id")
	}
	if fl.Encrypted != nil {
		updateFields = append(updateFields, "encrypted", "encryption_salt")
	}

	db.UpdateFileLocation(fl, updateFields)
}

// createChunkLocation 创建后续分片的 FileLocation 记录

// createChunkLocation 创建后续分片的 FileLocation 记录
func createChunkLocation(fl *entity.FileLocationORM, ref types.FileRef, chunkTotal, chunkNumber uint32) error {
	newFl := &entity.FileLocationORM{
		Blake3Hash:     fl.Blake3Hash,
		ProviderId:     fl.ProviderId,
		ProviderFileId: &ref.ProviderFileID,
		Status:         (*int32)(native.LocationStatus_PCOMPLETED.Enum()),
		ChunkNumber:    &chunkNumber,
		ChunkTotal:     &chunkTotal,
		// 复制加密字段，确保分片的加密状态一致
		Encrypted:      fl.Encrypted,
		EncryptionSalt: fl.EncryptionSalt,
	}

	if ref.Path != "" {
		newFl.Path = &ref.Path
	}
	if ref.Hash != "" {
		newFl.Hash = &ref.Hash
	}
	if ref.Size > 0 {
		newFl.Size = &ref.Size
	}
	if ref.ChunkSize > 0 {
		newFl.ChunkSize = &ref.ChunkSize
	}
	if ref.ChunkHash != "" {
		newFl.ChunkHash = &ref.ChunkHash
	}
	if ref.ChannelID != "" {
		newFl.ChannelId = &ref.ChannelID
	}

	if ref.MessageID != 0 {
		newFl.MessageId = utils.Int32Ptr(int32(ref.MessageID))
	}

	return db.CreateFileLocation(newFl)
}

// getOrCreateFileLocation 获取或创建文件位置记录

// getOrCreateFileLocation 获取或创建文件位置记录
func getOrCreateFileLocation(providerID uint32, blake3Hash string) (*entity.FileLocationORM, error) {
	// 先查询是否已存在
	location, err := db.GetFileLocationByProviderAndHash(providerID, blake3Hash)
	if err == nil {
		return location, nil
	}

	// 不存在则创建，但先检查存储提供者是否可用
	st := GetStorage(providerID)
	if st == nil {
		return nil, fmt.Errorf("storage provider not found: %d", providerID)
	}

	pcfg := st.GetProviderConfig()
	if pcfg == nil || !pcfg.Enabled {
		return nil, fmt.Errorf("storage provider not enabled: %d", providerID)
	}

	// 创建文件位置记录
	fl := &entity.FileLocationORM{
		ProviderId: &providerID,
		Blake3Hash: &blake3Hash,
		Status:     (*int32)(native.LocationStatus_PPENDING.Enum()),
	}
	if err := db.CreateFileLocation(fl); err != nil {
		return nil, fmt.Errorf("create file location failed: %v", err)
	}

	return fl, nil
}

// PreallocateFileLocations 为即将分发的新内容预建未完成的 file_location 行
//
// 目的：维持「文件记录指向的 hash 必有 file_location 行」这一不变式。
//
// 为什么需要：文件记录切换 hash 是同步立即生效的，而 file_location 行要到异步
// 分发真正上传成功才落库——中间隔着全局/提供者信号量排队与 provider 上传
// （实测可达数秒）。该窗口内 GetFileLocationsByBlake3Hash 返回空，读取路径会落在
// service.getFileStream 的 len(locations)==0 分支上直接返回
// "no file location found" 硬 500；而「无可用 provider」降级（读本地 staging
// 明文 / 短轮询等待 provider 完成）的判断在该分支之后，救不回来。表现即
// WebDAV PUT（覆盖/新建）后立即 GET 的同步客户端稳定拿到 500。
//
// 预建行状态为 PPENDING：读取端因此进入既有的降级路径（同一上传的明文此刻
// 仍在 TempDir/<hash>/ 下，staging 直读可直接命中）；分发完成后
// updateFirstChunkLocation 就地把同一行更新为 PCOMPLETED，不会多出行。
//
// 与异步分发共用 (hash, provider) 互斥锁：file_locations 没有
// (provider_id, blake3_hash) 唯一索引，check-then-insert 竞态会插出两行，而
// 分片完整性按「实际行数 == chunk_total」判定——多出的那行会让该 provider
// 永久被视为不完整（读取端选不到源）。抢不到锁说明已有 worker 在处理该
// (hash, provider)，行由它创建，跳过即可。
//
// 失败不阻断上传：单个 provider 预建失败仅记警告，分发侧
// uploadToProviderAsync 的 getOrCreateFileLocation 会兜底创建（退化为修复前行为）。
// 并发输家（CAS 落后被接管的请求）也会留下未被分发的预建行——无害：该 hash
// 没有任何文件记录引用它，后续同内容上传会复用同一行。
func PreallocateFileLocations(ctx context.Context, blake3Hash string, providerIDs []uint32) {
	if blake3Hash == "" || len(providerIDs) == 0 {
		return
	}

	ready := make([]uint32, 0, len(providerIDs))
	for _, providerID := range providerIDs {
		key := uploadKey{blake3Hash: blake3Hash, providerID: providerID}
		if !acquireLock(key) {
			llog.C(ctx).Debugf("preallocate file location skipped, upload in progress: hash=%s, provider=%d", blake3Hash, providerID)
			continue
		}
		_, err := getOrCreateFileLocation(providerID, blake3Hash)
		releaseLock(key)
		if err != nil {
			llog.C(ctx).Warnf("preallocate file location failed: hash=%s, provider=%d, err=%v", blake3Hash, providerID, err)
			continue
		}
		ready = append(ready, providerID)
	}

	llog.C(ctx).Infof("preallocated file locations: hash=%s, providers=%v", blake3Hash, ready)
}

// completeUpload 完成上传，清理临时文件
