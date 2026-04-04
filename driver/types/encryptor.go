package types

import "io"

// Encryptor 加密器接口
type Encryptor interface {
	// Encrypt 加密数据流
	Encrypt(in io.Reader) (io.ReadCloser, error)
	// Decrypt 解密数据流
	Decrypt(in io.ReadCloser) (io.ReadCloser, error)
	// IsEncrypted 是否加密
	IsEncrypted() bool
}

// NullEncryptor 空加密器（不加密）
type NullEncryptor struct{}

func (n *NullEncryptor) Encrypt(in io.Reader) (io.ReadCloser, error) {
	return io.NopCloser(in), nil
}

func (n *NullEncryptor) Decrypt(in io.ReadCloser) (io.ReadCloser, error) {
	return in, nil
}

func (n *NullEncryptor) IsEncrypted() bool {
	return false
}
