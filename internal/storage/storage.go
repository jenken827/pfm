package storage

import (
	"context"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"golang.org/x/sync/syncmap"
)

// storages 存储实例映射
var storages syncmap.Map

// Init 初始化存储
func Init() {
	sps, _, err := db.Query[native.StorageProviderORM](context.Background(), &db.DBQuery{Where: "enabled = ?", Args: []any{true}}, false)
	if err != nil {
		llog.L().Errorf("get enabled storage provider failed, err: %v", err)
		panic(err)
	}
	for _, sp := range sps {
		createAndStoreDriver(sp)
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

// createAndStoreDriver 创建并存储驱动实例
func createAndStoreDriver(sp *native.StorageProviderORM) error {
	dc := types.GetDriverConstructor(native.StorageProviderType(*sp.ProviderType))
	if dc == nil {
		llog.L().Error("storage provider driver constructor is nil", *sp.ProviderType)
		return nil
	}

	ins, err := dc(sp)
	if err != nil {
		llog.L().Errorf("create storage provider failed, err: %v", err)
		return err
	}

	storages.Store(*sp.Id, ins)
	return nil
}

// AddStorage 添加存储实例
func AddStorage(sp *native.StorageProviderORM) error {
	if !*sp.Enabled {
		return nil
	}

	if err := createAndStoreDriver(sp); err != nil {
		return err
	}

	llog.L().Infof("storage provider added, id: %d", *sp.Id)
	return nil
}

// UpdateStorage 更新存储实例
func UpdateStorage(sp *native.StorageProviderORM) error {
	// 先删除旧实例
	storages.Delete(*sp.Id)

	// 如果启用则重新创建
	if *sp.Enabled {
		if err := createAndStoreDriver(sp); err != nil {
			return err
		}
		llog.L().Infof("storage provider updated, id: %d", *sp.Id)
	} else {
		llog.L().Infof("storage provider disabled, id: %d", *sp.Id)
	}

	return nil
}

// DeleteStorage 删除存储实例
func DeleteStorage(ids []uint32) {
	for _, id := range ids {
		storages.Delete(id)
		llog.L().Infof("storage provider deleted, id: %d", id)
	}
}
