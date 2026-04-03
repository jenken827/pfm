package db

import (
	"context"
	"fmt"
	"strings"

	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// SortGroup 批量更新存储组排序
func SortGroup(ctx context.Context, sorts []*native.StorageGroupSortItem) error {
	var caseWhenParts []string
	var args []interface{}

	for _, r := range sorts {
		caseWhenParts = append(caseWhenParts, "WHEN ? THEN ?")
		args = append(args, r.GetId(), r.GetOrder())
	}

	// 构建 WHERE IN 部分的占位符
	var wherePlaceholders []string
	for _, r := range sorts {
		wherePlaceholders = append(wherePlaceholders, "?")
		args = append(args, r.GetId())
	}

	query := fmt.Sprintf(
		"UPDATE storage_groups SET order = CASE id %s END WHERE id IN (%s)",
		strings.Join(caseWhenParts, " "),
		strings.Join(wherePlaceholders, ","),
	)

	if err := db.Exec(query, args...).Error; err != nil {
		return err
	}
	return nil
}
