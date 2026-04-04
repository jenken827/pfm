package storage

import (
	"context"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"golang.org/x/sync/syncmap"
)

var storages syncmap.Map

// Init 初始化存储
func Init() {
	sps, _, err := db.Query[native.StorageProviderORM](context.Background(), &db.DBQuery{Where: "enabled = ?", Args: []any{true}}, false)
	if err != nil {
		llog.L().Errorf("get enabled storage provider failed, err: %v", err)
		panic(err)
	}
	for _, sp := range sps {
		dc := types.GetDriverConstructor(native.StorageProviderType(*sp.ProviderType))
		if dc == nil {
			llog.L().Error("storage provider driver constructor is nil", *sp.ProviderType)
			continue
		}
		ins, err := dc(sp)
		if err != nil {
			llog.L().Errorf("create storage provider failed, err: %v", err)
			continue
		}
		storages.Store(*sp.Id, ins)
	}

	// 启动时恢复未完成的上传任务
	go resumeDispatch()
}

// GetStorages 获取存储实例
func GetStorages() *syncmap.Map {
	return &storages
}

// GetStorage 获取指定存储实例
func GetStorage(id uint32) types.StorageDriver {
	if v, ok := storages.Load(id); ok {
		return v.(types.StorageDriver)
	}
	return nil
}
