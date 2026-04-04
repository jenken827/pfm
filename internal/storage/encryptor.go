package storage

import (
	"io"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/pkg/crypt"
)

// CryptEncryptor 基于crypt包的加密器实现
type CryptEncryptor struct {
	cipher *crypt.Cipher
}

// NewCryptEncryptor 创建加密器
func NewCryptEncryptor(password, salt string) (types.Encryptor, error) {
	cipher, err := crypt.NewCipher(password, salt)
	if err != nil {
		return nil, err
	}
	return &CryptEncryptor{cipher: cipher}, nil
}

// Encrypt 加密数据
func (e *CryptEncryptor) Encrypt(in io.Reader) (io.ReadCloser, error) {
	return e.cipher.EncryptData(in)
}

// Decrypt 解密数据
func (e *CryptEncryptor) Decrypt(in io.ReadCloser) (io.ReadCloser, error) {
	return e.cipher.DecryptData(in)
}

// IsEncrypted 是否加密
func (e *CryptEncryptor) IsEncrypted() bool {
	return true
}

// GetEncryptor 根据存储提供者和文件夹信息获取加密器
// encryptionKey: 存储提供者的加密密钥
// fileSalt: 文件夹的加密盐值
func GetEncryptor(encryptionKey string, fileSalt []byte) (types.Encryptor, error) {
	if encryptionKey == "" || len(fileSalt) == 0 {
		// 不加密
		return &types.NullEncryptor{}, nil
	}

	// 创建加密器
	return NewCryptEncryptor(encryptionKey, string(fileSalt))
}
