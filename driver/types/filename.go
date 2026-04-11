package types

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"time"

	"jenken827.com/jenken827/pfm/internal/llog"
)

// DefaultEncryptionSuffix 默认加密文件后缀
const DefaultEncryptionSuffix = ".bin"

// GenerateObfuscatedFilename 生成混淆文件名
// originalName: 原始文件名（仅用于日志记录）
// suffix: 加密后缀（可选，如 ".bin"，如果为空则使用默认值）
//
// 返回格式: 随机字符串_时间戳.后缀
// 示例: a1b2c3d4_20260404142345.bin
func GenerateObfuscatedFilename(originalName, suffix string) (string, error) {
	// 如果后缀为空，使用默认值
	if suffix == "" {
		suffix = DefaultEncryptionSuffix
	}
	// 确保后缀以点开头
	if len(suffix) > 0 && suffix[0] != '.' {
		suffix = "." + suffix
	}

	// 生成8位随机字符串
	randBytes := make([]byte, 4)
	if _, err := rand.Read(randBytes); err != nil {
		llog.L().Errorf("generate random bytes failed: %v", err)
		return "", fmt.Errorf("generate random bytes failed: %w", err)
	}
	randomStr := hex.EncodeToString(randBytes) // 8位十六进制字符串

	// 添加时间戳
	timestamp := time.Now().Format("20060102150405")

	// 最终文件名：随机字符串_时间戳.后缀
	obfuscatedName := fmt.Sprintf("%s_%s%s", randomStr, timestamp, suffix)
	llog.L().Infof("obfuscated filename: %s -> %s", originalName, obfuscatedName)

	return obfuscatedName, nil
}

// GetUniqueFileName 获取唯一文件名
func GetUniqueFileName(fileName string) string {
	timestamp := time.Now().Format("20060102150405")

	// 分离文件名和扩展名
	ext := filepath.Ext(fileName)
	name := fileName[:len(fileName)-len(ext)]

	// 在文件名和扩展名之间插入时间戳
	finalFileName := fmt.Sprintf("%s_%s%s", name, timestamp, ext)
	return finalFileName
}
