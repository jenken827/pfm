package utils

import (
	"encoding/json"
	"errors"

	"jenken827.com/jenken827/pfm/internal/llog"
)

// Slice转换Map时的错误定义
var (
	ErrEmptySlice   = errors.New("slice is empty")
	ErrInvalidKey   = errors.New("key is invalid (empty or zero value)")
	ErrDuplicateKey = errors.New("duplicate key found in slice")
)

// JsonMarshalString 将任意类型转换为json字符串
func JsonMarshalString(v any) (string, error) {
	jv, err := json.Marshal(v)
	if err != nil {
		llog.L().Errorf("Failed to marshal JSON,value: %v, err: %v", v, err)
		return "", err
	}
	return string(jv), nil
}

// SliceToMap slice结构体转换为map
// K: 键类型 (必须是可比较的, 支持 string、uint32 等)
// V: 值类型 (结构体类型)
// keyFunc: 从结构体中提取键的函数
//
// 返回值:
// - map[K]V: 转换后的map
// - error: 错误信息 (空切片或键为空时返回错误)
func SliceToMap[K comparable, V any](slice []V, keyFunc func(V) K) (map[K]V, error) {
	if len(slice) == 0 {
		return nil, ErrEmptySlice
	}

	result := make(map[K]V, len(slice))
	for _, v := range slice {
		key := keyFunc(v)
		// 检查键是否为空
		var zero K
		if key == zero {
			return nil, ErrInvalidKey
		}
		// 检查键是否重复
		if _, exists := result[key]; exists {
			return nil, ErrDuplicateKey
		}
		result[key] = v
	}
	return result, nil
}

// SliceToMapWithOverride slice转换为map, 键冲突时覆盖
func SliceToMapWithOverride[K comparable, V any](slice []V, keyFunc func(V) K) map[K]V {
	if len(slice) == 0 {
		return nil
	}

	result := make(map[K]V, len(slice))
	for _, v := range slice {
		key := keyFunc(v)
		result[key] = v
	}
	return result
}

// ==================== 指针辅助函数 ====================

// StringPtr 辅助函数：字符串指针
func StringPtr(s string) *string {
	return &s
}

// Int32Ptr 辅助函数：int32指针
func Int32Ptr(i int32) *int32 {
	return &i
}

// Int64Ptr 辅助函数：int64指针
func Int64Ptr(i int64) *int64 {
	return &i
}

// Float64Ptr 辅助函数：float64指针
func Float64Ptr(f float64) *float64 {
	return &f
}

// BoolPtr 辅助函数：bool指针
func BoolPtr(b bool) *bool {
	return &b
}

// EnumPtr 泛型辅助函数：枚举类型转int32指针
// 支持所有枚举类型，返回*int32以便与protobuf的optional int32字段兼容
func EnumPtr[T ~int32](v T) *int32 {
	p := int32(v)
	return &p
}
