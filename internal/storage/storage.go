package storage

import (
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
	sps, err := db.GetEnabledProviders()
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

// closeAndDeleteStorage 关闭并删除存储实例
func closeAndDeleteStorage(id uint32) {
	if v, ok := storages.Load(id); ok {
		driver := v.(types.StorageDriver)
		driver.Close()
		storages.Delete(id)
	}
}

// AddStorage 添加存储实例（异步执行，不阻塞请求）
func AddStorage(sp *native.StorageProviderORM) {
	if !*sp.Enabled {
		return
	}

	// 异步创建驱动实例
	go func() {
		if err := createAndStoreDriver(sp); err != nil {
			llog.L().Errorf("create storage driver failed, id: %d, err: %v", *sp.Id, err)
			return
		}
		llog.L().Infof("storage provider added, id: %d", *sp.Id)
	}()
}

// UpdateStorage 更新存储实例（异步执行，不阻塞请求）
func UpdateStorage(sp *native.StorageProviderORM) {
	// 先关闭并删除旧实例（这个操作很快，可以同步执行）
	closeAndDeleteStorage(*sp.Id)

	// 如果启用则异步重新创建
	if *sp.Enabled {
		go func() {
			if err := createAndStoreDriver(sp); err != nil {
				llog.L().Errorf("create storage driver failed, id: %d, err: %v", *sp.Id, err)
				return
			}
			llog.L().Infof("storage provider updated, id: %d", *sp.Id)
		}()
	} else {
		llog.L().Infof("storage provider disabled, id: %d", *sp.Id)
	}
}

// DeleteStorage 删除存储实例
func DeleteStorage(ids []uint32) {
	for _, id := range ids {
		closeAndDeleteStorage(id)
		llog.L().Infof("storage provider deleted, id: %d", id)
	}
}

// CloseAll 关闭所有存储实例
func CloseAll() {
	storages.Range(func(key, value any) bool {
		if driver, ok := value.(types.StorageDriver); ok {
			driver.Close()
			llog.L().Infof("storage provider closed, id: %v", key)
		}
		return true
	})
	storages = syncmap.Map{}
}
