package types

import (
	"fmt"
	"io"
	"os"
)

// PackToTempFile 若需要加密，加密到临时文件；返回 (最终路径, 混淆后文件名, 最终大小)。
// 不加密时返回原路径/原名/原大小。
// 加密产生的临时文件由调用方负责删除（os.Remove）。
func PackToTempFile(rawPath, fileName string, opt UploadOptions) (path, name string, size uint64, err error) {
	uploadName := fileName
	if opt.ObfuscateName {
		uploadName, err = GenerateObfuscatedFilename(fileName, opt.EncryptionSuffix)
		if err != nil {
			return "", "", 0, err
		}
	}

	if opt.Encryptor == nil || !opt.Encryptor.IsEncrypted() {
		stat, statErr := os.Stat(rawPath)
		if statErr != nil {
			return "", "", 0, fmt.Errorf("stat source file failed: %w", statErr)
		}
		return rawPath, uploadName, uint64(stat.Size()), nil
	}

	encryptedPath, encSize, err := encryptToTempFile(rawPath, opt.Encryptor)
	if err != nil {
		return "", "", 0, err
	}
	return encryptedPath, uploadName, encSize, nil
}

// PackStream 流式包装（telegram 用），返回加密 reader + 混淆名。
// 不加密时返回包装原始 reader 的 NopCloser。
// 返回的 ReadCloser 由调用方负责 Close。
func PackStream(raw io.Reader, fileName string, opt UploadOptions) (io.ReadCloser, string, error) {
	uploadName := fileName
	if opt.ObfuscateName {
		name, err := GenerateObfuscatedFilename(fileName, opt.EncryptionSuffix)
		if err != nil {
			return nil, "", err
		}
		uploadName = name
	}
	if opt.Encryptor == nil || !opt.Encryptor.IsEncrypted() {
		return io.NopCloser(raw), uploadName, nil
	}
	encryptedReader, err := opt.Encryptor.Encrypt(raw)
	if err != nil {
		return nil, "", err
	}
	return encryptedReader, uploadName, nil
}

// ComputeEncryptedChunkSize 计算加密后的分片大小（预留加密开销）。
// fileSizeLimit: provider 的单文件大小限制（字节）；encrypted: 是否加密。
// 返回应使用的分片大小。
func ComputeEncryptedChunkSize(fileSizeLimit int64, encrypted bool) int64 {
	if !encrypted {
		return fileSizeLimit
	}
	// NaCl secretbox 加密开销：每 64KB 块增加约 40 字节
	// 预留值 = fileSizeLimit * (40 / 65536) * 1.05（向上取整并加 5% 安全余量）
	encryptionOverhead := float64(fileSizeLimit) * (40.0 / 65536.0) * 1.05
	return fileSizeLimit - int64(encryptionOverhead)
}

// encryptToTempFile 加密到临时文件，返回 (路径, 加密后大小)。
// 临时文件由调用方负责删除。
func encryptToTempFile(rawPath string, enc Encryptor) (string, uint64, error) {
	srcFile, err := os.Open(rawPath)
	if err != nil {
		return "", 0, fmt.Errorf("open source file failed: %w", err)
	}
	defer srcFile.Close()

	encryptedReader, err := enc.Encrypt(srcFile)
	if err != nil {
		return "", 0, fmt.Errorf("create encrypted reader failed: %w", err)
	}
	defer encryptedReader.Close()

	tempFile, err := os.CreateTemp("", "encrypted-upload-*")
	if err != nil {
		return "", 0, fmt.Errorf("create temp file failed: %w", err)
	}
	tempName := tempFile.Name()

	written, err := io.Copy(tempFile, encryptedReader)
	closeErr := tempFile.Close()
	if err != nil {
		os.Remove(tempName)
		return "", 0, fmt.Errorf("write encrypted data failed: %w", err)
	}
	if closeErr != nil {
		os.Remove(tempName)
		return "", 0, fmt.Errorf("close temp file failed: %w", closeErr)
	}
	return tempName, uint64(written), nil
}
