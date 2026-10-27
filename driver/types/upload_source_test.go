package types

import (
	"testing"

	"jenken827.com/jenken827/pfm/proto/gen/entity"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// TestFileRefFromLocation 验证 FileLocationORM -> FileRef 的字段映射完整性
func TestFileRefFromLocation(t *testing.T) {
	id := uint64(123)
	providerFileID := "provider-file-id"
	path := "/2026/08/file.txt"
	size := uint64(2048)
	chunkSize := uint64(1024)
	hash := "md5hash"
	chunkHash := "chunkhash"
	chunkNumber := uint32(0)
	channelID := "chat-1"
	messageID := int32(456)
	cacheStatus := int32(native.CacheStatus_CACHED)

	loc := &entity.FileLocationORM{
		Id:             &id,
		ProviderFileId: &providerFileID,
		Path:           &path,
		Size:           &size,
		ChunkSize:      &chunkSize,
		Hash:           &hash,
		ChunkHash:      &chunkHash,
		ChunkNumber:    &chunkNumber,
		ChannelId:      &channelID,
		MessageId:      &messageID,
		CacheStatus:    &cacheStatus,
	}

	ref := FileRefFromLocation(loc)

	if ref.LocationID != id {
		t.Errorf("LocationID: expected %d, got %d", id, ref.LocationID)
	}
	if ref.ProviderFileID != providerFileID {
		t.Errorf("ProviderFileID mismatch")
	}
	if ref.Path != path {
		t.Errorf("Path mismatch")
	}
	if ref.Size != size {
		t.Errorf("Size mismatch")
	}
	if ref.ChunkSize != chunkSize {
		t.Errorf("ChunkSize mismatch")
	}
	if ref.Hash != hash {
		t.Errorf("Hash mismatch")
	}
	if ref.ChunkHash != chunkHash {
		t.Errorf("ChunkHash mismatch")
	}
	if ref.ChunkNumber != chunkNumber {
		t.Errorf("ChunkNumber mismatch")
	}
	if ref.ChannelID != channelID {
		t.Errorf("ChannelID mismatch")
	}
	if ref.MessageID != int(messageID) {
		t.Errorf("MessageID mismatch")
	}
	if ref.CacheStatus != native.CacheStatus_CACHED {
		t.Errorf("CacheStatus mismatch")
	}
}

// TestFileRefFromLocation_Nil 验证 nil 安全
func TestFileRefFromLocation_Nil(t *testing.T) {
	if ref := FileRefFromLocation(nil); ref.LocationID != 0 || ref.ProviderFileID != "" {
		t.Errorf("nil location should produce zero FileRef")
	}
}

// TestProviderConfigFromORM 验证配置视图提取
func TestProviderConfigFromORM(t *testing.T) {
	key := "enc-key"
	suffix := ".enc"
	sortOrder := uint32(3)
	limit := uint64(100)
	concurrent := uint32(5)
	hashType := int32(native.HashType_MD5)
	enabled := true

	orm := &entity.StorageProviderORM{
		EncryptionKey:        &key,
		EncryptionSuffix:     &suffix,
		SortOrder:            &sortOrder,
		FileSizeLimit:        &limit,
		MaxConcurrentUploads: &concurrent,
		HashType:             &hashType,
		Enabled:              &enabled,
	}

	cfg := ProviderConfigFromORM(orm)

	if cfg.EncryptionKey != key || cfg.EncryptionSuffix != suffix {
		t.Errorf("encryption config mismatch")
	}
	if cfg.SortOrder != sortOrder || cfg.FileSizeLimit != limit {
		t.Errorf("limit/order mismatch")
	}
	if cfg.MaxConcurrentUploads != concurrent || cfg.HashType != native.HashType_MD5 || !cfg.Enabled {
		t.Errorf("concurrent/hash/enabled mismatch")
	}
}
