package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/jenken827/pfm-plugin-sdk/driver/cryptostream"
	"github.com/jenken827/pfm-plugin-sdk/driver/types"
)

// prepareEncryptor 生成加密器（如果需要加密），返回 (encryptor, salt, error)
// salt 非空表示启用了加密
func prepareEncryptor(fileEncrypt bool, pcfg *types.ProviderConfig) (types.Encryptor, string, error) {
	if fileEncrypt && pcfg.EncryptionKey != "" {
		// 生成随机salt（32字节）
		saltBytes := make([]byte, 32)
		if _, err := rand.Read(saltBytes); err != nil {
			return nil, "", fmt.Errorf("generate salt failed: %v", err)
		}
		saltStr := hex.EncodeToString(saltBytes)

		// 创建加密器
		enc, err := cryptostream.NewCryptEncryptor(pcfg.EncryptionKey, saltStr)
		if err != nil {
			return nil, "", fmt.Errorf("create encryptor failed: %v", err)
		}
		return enc, saltStr, nil
	}
	return &types.NullEncryptor{}, "", nil
}

// saveUploadChunks 处理上传结果，保存分片信息到数据库
