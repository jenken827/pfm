package sharelink

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// GenerateSign 生成分享链接签名
// shareCode: 分享码
// fileID: 文件ID
// signToken: 签名密钥
// expireAt: 过期时间（Unix秒）
func GenerateSign(shareCode, fileID, signToken string, expireAt int64) string {
	message := fmt.Sprintf("%s:%s:%d", shareCode, fileID, expireAt)
	mac := hmac.New(sha256.New, []byte(signToken))
	mac.Write([]byte(message))
	sign := mac.Sum(nil)
	return hex.EncodeToString(sign)
}

// VerifySign 验证分享链接签名
func VerifySign(shareCode, fileID, signToken, sign string, expireAt int64) bool {
	expectedSign := GenerateSign(shareCode, fileID, signToken, expireAt)
	return hmac.Equal([]byte(expectedSign), []byte(sign))
}

// ParseSignURL 从URL查询参数解析签名信息
// sign格式: expireAt:signature
func ParseSignURL(sign string) (int64, string, error) {
	parts := strings.SplitN(sign, ":", 2)
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("invalid sign format")
	}
	expireAt, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid expire time: %w", err)
	}
	return expireAt, parts[1], nil
}

// BuildSignParam 构建签名参数
// 格式: expireAt:signature
func BuildSignParam(shareCode, fileID, signToken string, expireAt int64) string {
	sign := GenerateSign(shareCode, fileID, signToken, expireAt)
	return fmt.Sprintf("%d:%s", expireAt, sign)
}

// GenerateShareCode 生成分享码（8位随机字符串）
func GenerateShareCode() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const length = 8
	result := make([]byte, length)
	max := big.NewInt(int64(len(charset)))
	for i := range length {
		n, _ := rand.Int(rand.Reader, max)
		result[i] = charset[n.Int64()]
	}
	return string(result)
}
