package db

import (
	"context"

	"gorm.io/gorm"
)

// DBQuery 数据库查询
type DBQuery struct {
	Where  string
	Args   []any
	Order  string
	Limit  int
	Offset int
}

// type DBUpdate struct {
// 	Where  string
// 	Args   []any
// 	Select []string
// }

// Query 数据库查询
func Query[T any](ctx context.Context, query *DBQuery, withCount bool) ([]*T, int64, error) {
	count := int64(0)
	gdb := gorm.G[*T](db)
	if query == nil {
		items, err := gdb.Find(ctx)
		return items, count, err
	}
	var chain gorm.ChainInterface[*T]
	if query.Where != "" {
		chain = gdb.Where(query.Where, query.Args...)
	}
	if withCount {
		c, err := chain.Count(ctx, "*")
		if err != nil {
			return nil, 0, err
		}
		count = c
	}
	if query.Order != "" {
		chain = chain.Order(query.Order)
	}
	if query.Limit > 0 {
		chain = chain.Limit(query.Limit)
	}
	if query.Offset > 0 {
		chain = chain.Offset(query.Offset)
	}
	items, err := chain.Find(ctx)
	return items, count, err
}

// Create 创建数据
func Create[T any](ctx context.Context, data *T, filter func(*T)) error {
	if filter != nil {
		filter(data)
	}

	return gorm.G[T](db).Create(ctx, data)
}

// Update 更新数据
func Update[T any](ctx context.Context, data *T, where string, args ...any) error {
	_, err := gorm.G[*T](db).Where(where, args...).Select("*").Updates(ctx, data)
	return err
}

// Delete 删除数据
func Delete[T any](ctx context.Context, ids []any) error {
	_, err := gorm.G[T](db).Where("id in ?", ids).Delete(ctx)
	return err
}
