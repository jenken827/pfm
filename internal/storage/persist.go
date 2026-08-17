package storage

import (
	"errors"
	"fmt"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

// failFileLocation 标记文件位置为失败状态
func failFileLocation(fl *native.FileLocationORM, msg string) error {
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
func saveUploadChunks(fl *native.FileLocationORM, refs []types.FileRef, providerID uint32) uint64 {
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
func updateFirstChunkLocation(fl *native.FileLocationORM, ref types.FileRef, chunkTotal, chunkNumber uint32) {
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
func createChunkLocation(fl *native.FileLocationORM, ref types.FileRef, chunkTotal, chunkNumber uint32) error {
	newFl := &native.FileLocationORM{
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
func getOrCreateFileLocation(providerID uint32, blake3Hash string) (*native.FileLocationORM, error) {
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
	fl := &native.FileLocationORM{
		ProviderId: &providerID,
		Blake3Hash: &blake3Hash,
		Status:     (*int32)(native.LocationStatus_PPENDING.Enum()),
	}
	if err := db.CreateFileLocation(fl); err != nil {
		return nil, fmt.Errorf("create file location failed: %v", err)
	}

	return fl, nil
}

// completeUpload 完成上传，清理临时文件
