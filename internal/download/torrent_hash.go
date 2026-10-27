package download

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
)

// ============================================================
// .torrent → infohash
//
// 种子 URL 下载完成时拿到的只是 .torrent 文件本身，它派生出的真正 BT
// 任务可能已经找不到 followedBy（下载器重启后 gid 全变、关系丢失），
// 这时按种子文件 info 字典算 infohash 反查，是唯一稳定的关联方式。
// ============================================================

// torrentInfoHash 计算种子文件的 infohash（info 字典 bencode 原始字节的 sha1）
func torrentInfoHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	start, end, err := findDictValue(data, "info")
	if err != nil {
		return "", err
	}
	sum := sha1.Sum(data[start:end])
	return hex.EncodeToString(sum[:]), nil
}

// findDictValue 在顶层 bencode 字典里找 key，返回其 value 的字节区间 [start, end)
func findDictValue(data []byte, key string) (int, int, error) {
	if len(data) == 0 || data[0] != 'd' {
		return 0, 0, fmt.Errorf("不是 bencode 字典")
	}
	for i := 1; i < len(data); {
		if data[i] == 'e' {
			break
		}
		kStart, kEnd, next, err := readBencodeString(data, i)
		if err != nil {
			return 0, 0, err
		}
		vEnd, err := skipBencodeValue(data, next)
		if err != nil {
			return 0, 0, err
		}
		if string(data[kStart:kEnd]) == key {
			return next, vEnd, nil
		}
		i = vEnd
	}
	return 0, 0, fmt.Errorf("未找到 %q 字段", key)
}

// readBencodeString 解析 <len>:<bytes>，返回内容区间与下一个值的偏移
func readBencodeString(data []byte, i int) (start, end, next int, err error) {
	j := i
	for j < len(data) && data[j] >= '0' && data[j] <= '9' {
		j++
	}
	if j >= len(data) || data[j] != ':' {
		return 0, 0, 0, fmt.Errorf("非法 bencode 字符串，偏移 %d", i)
	}
	size := 0
	for k := i; k < j; k++ {
		size = size*10 + int(data[k]-'0')
	}
	start = j + 1
	end = start + size
	if end > len(data) {
		return 0, 0, 0, fmt.Errorf("bencode 字符串越界，偏移 %d", i)
	}
	return start, end, end, nil
}

// skipBencodeValue 跳过一个 bencode 值（整数 / 字符串 / 列表 / 字典），返回其结束偏移
func skipBencodeValue(data []byte, i int) (int, error) {
	if i >= len(data) {
		return 0, fmt.Errorf("bencode 值越界")
	}
	switch data[i] {
	case 'i':
		j := i + 1
		for j < len(data) && data[j] != 'e' {
			j++
		}
		if j >= len(data) {
			return 0, fmt.Errorf("整数缺少结尾 e")
		}
		return j + 1, nil
	case 'l', 'd':
		j := i + 1
		for j < len(data) && data[j] != 'e' {
			next, err := skipBencodeValue(data, j)
			if err != nil {
				return 0, err
			}
			j = next
		}
		if j >= len(data) {
			return 0, fmt.Errorf("容器缺少结尾 e")
		}
		return j + 1, nil
	default:
		_, _, next, err := readBencodeString(data, i)
		return next, err
	}
}
