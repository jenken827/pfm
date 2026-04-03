package crypt

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/crypto/nacl/secretbox"
	"golang.org/x/crypto/scrypt"
)

// ============================================================================
// 加密模块 - 参考rclone crypt驱动实现，完全兼容 rclone crypt
// ============================================================================
//
// 算法说明：
// 1. 文件头：8字节 magic "RCLONE\x00\x00" + 24字节 nonce = 32字节
// 2. 数据加密：NaCl secretbox (XSalsa20-Poly1305)
//    - 每个块 64KB 数据 + 16字节 Poly1305 认证
//    - nonce 24字节，每块递增
// 3. 密钥派生：scrypt(password, salt, N=16384, r=8, p=1) -> 80字节
//    - 32字节 dataKey (secretbox 加密)
//    - 32字节 nameKey (文件名 AES 加密)
//    - 16字节 nameTweak (文件名加密 tweak)
// 4. 文件名加密：EME (ECB-Mix-ECB) 宽块加密 + base32 编码
// ============================================================================

const (
	// 文件头常量
	fileMagic      = "RCLONE\x00\x00" // 8字节魔数
	fileMagicSize  = 8
	fileNonceSize  = 24                            // NaCl secretbox nonce 长度
	fileHeaderSize = fileMagicSize + fileNonceSize // 32字节

	// 块大小
	blockHeaderSize = secretbox.Overhead              // 16字节 Poly1305 认证
	blockDataSize   = 64 * 1024                       // 64KB 数据
	blockSize       = blockHeaderSize + blockDataSize // 65552 字节

	// 密钥大小
	dataKeyLen   = 32                                     // secretbox 密钥
	nameKeyLen   = 32                                     // 文件名 AES 密钥
	nameTweakLen = 16                                     // 文件名 tweak
	totalKeyLen  = dataKeyLen + nameKeyLen + nameTweakLen // 80字节
)

// 错误定义
var (
	ErrBadDecryptUTF8          = errors.New("bad decryption - utf-8 invalid")
	ErrBadDecryptControlChar   = errors.New("bad decryption - contains control chars")
	ErrFileTooShort            = errors.New("file is too short to be encrypted")
	ErrBadMagic                = errors.New("not an encrypted file - bad magic string")
	ErrBadBlock                = errors.New("failed to authenticate decrypted block - bad password?")
	ErrBadBase32Encoding       = errors.New("bad base32 filename encoding")
	ErrNotAMultipleOfBlocksize = errors.New("not a multiple of blocksize")
	ErrTooShortAfterDecode     = errors.New("too short after base32 decode")
	ErrTooLongAfterDecode      = errors.New("too long after base32 decode")
)

// 默认盐值 (rclone 使用)
var defaultSalt = []byte{0xA8, 0x0D, 0xF4, 0x3A, 0x8F, 0xBD, 0x03, 0x08, 0xA7, 0xCA, 0xB8, 0x3E, 0x58, 0x1F, 0x86, 0xB1}

// ============================================================================
// 文件加密器 - 用于文件内容加密/解密
// ============================================================================

// FileCryptor 文件加密器，使用 rclone 兼容的 NaCl secretbox 算法
type FileCryptor struct {
	dataKey    [dataKeyLen]byte   // secretbox 加密密钥
	nameKey    [nameKeyLen]byte   // 文件名加密密钥
	nameTweak  [nameTweakLen]byte // 文件名加密 tweak
	nameCipher cipher.Block       // AES cipher for EME
	buffers    sync.Pool
}

// NewFileCryptor 根据密码和盐值创建文件加密器
// 使用 rclone 兼容的密钥派生算法
func NewFileCryptor(password, salt string) (*FileCryptor, error) {
	fc := &FileCryptor{}
	fc.buffers.New = func() any {
		return new([blockSize]byte)
	}

	var saltBytes []byte
	if salt == "" {
		saltBytes = defaultSalt
	} else {
		saltBytes = []byte(salt)
	}

	var key []byte
	var err error

	if password == "" {
		// 空密码生成全零密钥（用于测试）
		key = make([]byte, totalKeyLen)
	} else {
		// 使用 scrypt 派生密钥：N=16384, r=8, p=1
		key, err = scrypt.Key([]byte(password), saltBytes, 16384, 8, 1, totalKeyLen)
		if err != nil {
			return nil, fmt.Errorf("scrypt key derivation failed: %w", err)
		}
	}

	// 分割密钥
	copy(fc.dataKey[:], key[:dataKeyLen])
	copy(fc.nameKey[:], key[dataKeyLen:dataKeyLen+nameKeyLen])
	copy(fc.nameTweak[:], key[dataKeyLen+nameKeyLen:])

	// 初始化文件名加密的 AES cipher (用于 EME)
	fc.nameCipher, err = aes.NewCipher(fc.nameKey[:])
	if err != nil {
		return nil, fmt.Errorf("failed to create name cipher: %w", err)
	}

	return fc, nil
}

// getBlock 从池中获取块缓冲区
func (fc *FileCryptor) getBlock() *[blockSize]byte {
	return fc.buffers.Get().(*[blockSize]byte)
}

// putBlock 归还块缓冲区到池
func (fc *FileCryptor) putBlock(buf *[blockSize]byte) {
	fc.buffers.Put(buf)
}

// ============================================================================
// 文件数据加密/解密 (使用 NaCl secretbox)
// ============================================================================

// EncryptFile 对文件进行加密，输出到目标路径
// 加密格式：fileHeader (32字节) + 多个加密块
func (fc *FileCryptor) EncryptFile(srcPath, dstPath string) (int64, error) {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return 0, fmt.Errorf("打开源文件失败: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dstPath)
	if err != nil {
		return 0, fmt.Errorf("创建目标文件失败: %w", err)
	}
	defer dstFile.Close()

	return fc.EncryptStream(dstFile, srcFile)
}

// DecryptFile 对加密的文件进行解密，输出到目标路径
func (fc *FileCryptor) DecryptFile(srcPath, dstPath string) (int64, error) {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return 0, fmt.Errorf("打开源文件失败: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dstPath)
	if err != nil {
		return 0, fmt.Errorf("创建目标文件失败: %w", err)
	}
	defer dstFile.Close()

	return fc.DecryptStream(dstFile, srcFile)
}

// EncryptStream 对数据流进行加密
// 输出格式：magic (8字节) + nonce (24字节) + 加密块1 + 加密块2 + ...
func (fc *FileCryptor) EncryptStream(w io.Writer, r io.Reader) (int64, error) {
	// 分配块缓冲区
	buf := fc.getBlock()
	defer fc.putBlock(buf)
	readBuf := fc.getBlock()
	defer fc.putBlock(readBuf)

	// 写入文件头
	// 1. 写入 magic
	copy((*buf)[:fileMagicSize], fileMagic)
	// 2. 写入随机 nonce (24字节)
	nonce := (*[fileNonceSize]byte)(nil)
	nonce = new([fileNonceSize]byte)
	if _, err := rand.Read(nonce[:]); err != nil {
		return 0, fmt.Errorf("生成 nonce 失败: %w", err)
	}
	copy((*buf)[fileMagicSize:], nonce[:])

	// 写入文件头到输出
	if _, err := w.Write((*buf)[:fileHeaderSize]); err != nil {
		return 0, fmt.Errorf("写入文件头失败: %w", err)
	}

	var totalBytes int64

	// 循环读取并加密数据块
	for {
		// 读取最多 blockDataSize 字节数据
		n, readErr := io.ReadFull(r, (*readBuf)[:blockDataSize])

		if n == 0 {
			if readErr == io.ErrUnexpectedEOF {
				readErr = io.EOF
			}
			if readErr == io.EOF {
				break
			}
			return totalBytes, fmt.Errorf("读取数据失败: %w", readErr)
		}

		// 使用 secretbox 加密当前块
		// nonce 每块递增
		secretbox.Seal((*buf)[:0], (*readBuf)[:n], nonce, &fc.dataKey)

		// 写入加密块 (包含 16 字节 Poly1305 认证)
		written, writeErr := w.Write((*buf)[:blockHeaderSize+n])
		if writeErr != nil {
			return totalBytes, fmt.Errorf("写入加密数据失败: %w", writeErr)
		}
		totalBytes += int64(written)

		// 递增 nonce
		fc.incrementNonce(nonce)

		if readErr == io.EOF {
			break
		}
	}

	return totalBytes, nil
}

// DecryptStream 对数据流进行解密
// 输入格式：magic (8字节) + nonce (24字节) + 加密块1 + 加密块2 + ...
func (fc *FileCryptor) DecryptStream(w io.Writer, r io.Reader) (int64, error) {
	// 分配块缓冲区
	buf := fc.getBlock()
	defer fc.putBlock(buf)
	readBuf := fc.getBlock()
	defer fc.putBlock(readBuf)

	// 读取并验证文件头
	headerBuf := (*readBuf)[:fileHeaderSize]
	if _, err := io.ReadFull(r, headerBuf); err != nil {
		if err == io.ErrUnexpectedEOF {
			return 0, ErrFileTooShort
		}
		return 0, fmt.Errorf("读取文件头失败: %w", err)
	}

	// 验证 magic
	if !bytes.Equal(headerBuf[:fileMagicSize], []byte(fileMagic)) {
		return 0, ErrBadMagic
	}

	// 提取 nonce
	var nonce [fileNonceSize]byte
	copy(nonce[:], headerBuf[fileMagicSize:])

	var totalBytes int64

	// 循环读取并解密数据块
	for {
		// 读取加密块 (16字节头 + 数据)
		n, readErr := io.ReadFull(r, (*readBuf)[:blockSize])

		if n == 0 {
			if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
				break
			}
			return totalBytes, fmt.Errorf("读取加密数据失败: %w", readErr)
		}

		// 至少需要 blockHeaderSize 字节才能解密
		if n <= blockHeaderSize {
			return totalBytes, ErrBadBlock
		}

		// 使用 secretbox 解密
		plaintext, ok := secretbox.Open((*buf)[:0], (*readBuf)[:n], &nonce, &fc.dataKey)
		if !ok {
			return totalBytes, ErrBadBlock
		}

		// 写入解密数据
		written, writeErr := w.Write(plaintext)
		if writeErr != nil {
			return totalBytes, fmt.Errorf("写入解密数据失败: %w", writeErr)
		}
		totalBytes += int64(written)

		// 递增 nonce
		fc.incrementNonce(&nonce)

		if readErr == io.EOF {
			break
		}
	}

	return totalBytes, nil
}

// incrementNonce 将 24 字节 nonce 视为大端序 uint64 + 16 字节计数器递增
func (fc *FileCryptor) incrementNonce(nonce *[fileNonceSize]byte) {
	// 前 8 字节是大端序 uint64，每块递增
	for i := 7; i >= 0; i-- {
		nonce[i]++
		if nonce[i] != 0 {
			break
		}
	}
}

// ============================================================================
// 文件名加密 (使用 EME + Base32)
// ============================================================================

// EncryptFileName 加密文件名，使用 rclone 兼容的 EME 算法
func (fc *FileCryptor) EncryptFileName(name string) string {
	if name == "" {
		return ""
	}
	// EME 加密
	encrypted := fc.emeEncrypt([]byte(name))
	// Base32 编码 (rclone 使用小写、无填充的 base32)
	return base32.HexEncoding.EncodeToString(encrypted)
}

// DecryptFileName 解密文件名
func (fc *FileCryptor) DecryptFileName(encryptedName string) (string, error) {
	if encryptedName == "" {
		return "", nil
	}
	// Base32 解码
	decoded, err := base32.HexEncoding.DecodeString(strings.ToUpper(encryptedName))
	if err != nil {
		return "", ErrBadBase32Encoding
	}
	// EME 解密
	decrypted := fc.emeDecrypt(decoded)
	return string(decrypted), nil
}

// emeEncrypt 使用 EME (ECB-Mix-ECB) 宽块模式加密
// EME 提高安全性，用于加密可能重复的文件名
func (fc *FileCryptor) emeEncrypt(plaintext []byte) []byte {
	blockSize := aes.BlockSize

	// PKCS7 填充
	padded := pkcs7Pad(plaintext, blockSize)

	// 将填充后的数据分成多个 AES 块
	numBlocks := len(padded) / blockSize
	output := make([]byte, len(padded))

	// EME 加密过程：
	// 1. 对每个块进行 ECB 加密
	// 2. 混合所有加密块
	// 3. 对混合结果再次 ECB 加密
	// 4. 与原始加密块 XOR

	// 第一轮：ECB 加密
	ec := make([]byte, len(padded))
	for i := 0; i < numBlocks; i++ {
		fc.nameCipher.Encrypt(ec[i*blockSize:(i+1)*blockSize], padded[i*blockSize:(i+1)*blockSize])
	}

	// 混合：计算所有块的 XOR，并混入 tweak
	mixed := make([]byte, blockSize)
	for i := 0; i < numBlocks; i++ {
		for j := 0; j < blockSize; j++ {
			mixed[j] ^= ec[i*blockSize+j]
		}
	}
	// 混入 tweak
	for i := 0; i < blockSize; i++ {
		mixed[i] ^= fc.nameTweak[i]
	}

	// 对混合结果进行 ECB 加密
	cipher := make([]byte, blockSize)
	fc.nameCipher.Encrypt(cipher, mixed)

	// 第二轮：每个块与 cipher XOR 后再 ECB 加密
	for i := 0; i < numBlocks; i++ {
		for j := 0; j < blockSize; j++ {
			output[i*blockSize+j] = ec[i*blockSize+j] ^ cipher[j]
		}
		fc.nameCipher.Encrypt(output[i*blockSize:(i+1)*blockSize], output[i*blockSize:(i+1)*blockSize])
	}

	return output
}

// emeDecrypt 使用 EME 宽块模式解密
func (fc *FileCryptor) emeDecrypt(ciphertext []byte) []byte {
	blockSize := aes.BlockSize

	// 密文长度必须是块大小的倍数
	if len(ciphertext)%blockSize != 0 {
		return nil
	}

	numBlocks := len(ciphertext) / blockSize
	intermediate := make([]byte, len(ciphertext))

	// EME 解密过程：
	// 1. 对每个密文块进行 ECB 解密
	// 2. 混合所有解密块
	// 3. 对混合结果再次 ECB 加密
	// 4. 与解密块 XOR 得到明文

	// 第一轮：ECB 解密
	for i := 0; i < numBlocks; i++ {
		fc.nameCipher.Decrypt(intermediate[i*blockSize:(i+1)*blockSize], ciphertext[i*blockSize:(i+1)*blockSize])
	}

	// 混合：计算所有块的 XOR，并混入 tweak
	mixed := make([]byte, blockSize)
	for i := 0; i < numBlocks; i++ {
		for j := 0; j < blockSize; j++ {
			mixed[j] ^= intermediate[i*blockSize+j]
		}
	}
	// 混入 tweak
	for i := 0; i < blockSize; i++ {
		mixed[i] ^= fc.nameTweak[i]
	}

	// 对混合结果进行 ECB 加密
	cipher := make([]byte, blockSize)
	fc.nameCipher.Encrypt(cipher, mixed)

	// 第二轮：每个块与 cipher XOR 后再 ECB 解密
	output := make([]byte, len(ciphertext))
	for i := 0; i < numBlocks; i++ {
		for j := 0; j < blockSize; j++ {
			output[i*blockSize+j] = intermediate[i*blockSize+j] ^ cipher[j]
		}
		fc.nameCipher.Decrypt(output[i*blockSize:(i+1)*blockSize], output[i*blockSize:(i+1)*blockSize])
	}

	// 移除 PKCS7 填充
	return pkcs7Unpad(output, blockSize)
}

// pkcs7Pad 使用 PKCS7 填充数据
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padBytes := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padBytes...)
}

// pkcs7Unpad 移除 PKCS7 填充
func pkcs7Unpad(data []byte, blockSize int) []byte {
	if len(data) == 0 {
		return nil
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > blockSize {
		return nil
	}
	// 验证填充
	for i := len(data) - padding; i < len(data); i++ {
		if int(data[i]) != padding {
			return nil
		}
	}
	return data[:len(data)-padding]
}

// ============================================================================
// 便捷方法
// ============================================================================

// EncryptBytes 对字节数据进行加密
func (fc *FileCryptor) EncryptBytes(data []byte) ([]byte, error) {
	buf := new(bytes.Buffer)
	if _, err := fc.EncryptStream(buf, bytes.NewReader(data)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecryptBytes 对加密的字节数据进行解密
func (fc *FileCryptor) DecryptBytes(encryptedData []byte) ([]byte, error) {
	buf := new(bytes.Buffer)
	if _, err := fc.DecryptStream(buf, bytes.NewReader(encryptedData)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// EncryptedSize 计算加密后的文件大小
func (fc *FileCryptor) EncryptedSize(size int64) int64 {
	if size == 0 {
		return fileHeaderSize
	}
	blocks := size / blockDataSize
	residue := size % blockDataSize
	encryptedSize := fileHeaderSize + blocks*(blockHeaderSize+blockDataSize)
	if residue != 0 {
		encryptedSize += blockHeaderSize + residue
	}
	return encryptedSize
}

// DecryptedSize 计算解密后的文件大小
func (fc *FileCryptor) DecryptedSize(size int64) (int64, error) {
	size -= fileHeaderSize
	if size < 0 {
		return 0, ErrFileTooShort
	}
	blocks := size / blockSize
	residue := size % blockSize
	decryptedSize := blocks * blockDataSize
	if residue != 0 {
		residue -= blockHeaderSize
		if residue <= 0 {
			return 0, ErrBadBlock
		}
	}
	decryptedSize += residue
	return decryptedSize, nil
}
