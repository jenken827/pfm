package storage

import (
	"fmt"
	"strings"

	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	abi "github.com/jenken827/pfm-plugin-sdk/proto/gen/abi"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"golang.org/x/sync/syncmap"
	ormconv "jenken827.com/jenken827/pfm/driver/orm"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/pkg/llog"
)

// storages 存储实例映射
var storages syncmap.Map

// Init 初始化存储
func Init() {
	InitProviders()

	// 启动时恢复未完成的上传任务
	go resumeDispatch()

	// 启动 temp 文件定期清理
	StartTempCleanup()
}

// InitProviders 只按数据库中启用的 provider 创建驱动实例，**不启动**任何后台任务
// （不恢复未完成上传、不清理临时文件）。
//
// 供只读型 CLI 使用（如 direct-link 直链打表）：短生命周期进程不应该触发上传恢复
// 这类会改状态的后台流程。服务端仍走 Init()。
func InitProviders() {
	sps, err := db.GetEnabledProviders()
	if err != nil {
		llog.L().Errorf("get enabled storage provider failed, err: %v", err)
		panic(err)
	}
	for _, sp := range sps {
		if err := createAndStoreDriver(sp); err != nil {
			llog.L().Errorf("create storage driver failed, id: %d, name: %s, err: %v",
				*sp.Id, *sp.Name, err)
			// 继续初始化其他存储驱动，不中断启动
			continue
		}
		llog.L().Infof("storage provider initialized, id: %d, name: %s", *sp.Id, *sp.Name)
	}
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
func createAndStoreDriver(sp *entity.StorageProviderORM) error {
	driverID := ""
	if sp.DriverId != nil {
		driverID = *sp.DriverId
	}
	dc := types.GetDriverConstructor(driverID)
	if dc == nil {
		// 驱动未安装（插件被卸载或身份未知）：provider 数据保留，只报告不可用。
		llog.L().Errorf("storage provider driver constructor is nil, driver_id=%s", driverID)
		return fmt.Errorf("storage provider driver not installed: driver_id=%s", driverID)
	}

	ins, err := dc(ormconv.ProviderFromORM(sp))
	if err != nil {
		llog.L().Errorf("create storage provider failed, err: %v", err)
		return err
	}

	// 注入缓存状态更新器（驱动需要，避免驱动直接依赖 db 层）
	if setter, ok := ins.(types.CacheUpdaterSetter); ok {
		setter.SetCacheUpdater(dbCacheUpdater{})
	}
	// 注入配置持久化器（驱动刷新凭据时需要）
	if setter, ok := ins.(types.ConfigPersisterSetter); ok {
		setter.SetConfigPersister(dbConfigPersister{})
	}

	storages.Store(*sp.Id, ins)
	return nil
}

// ReloadDrivers 重建指定驱动身份下的 provider 实例（插件热加载后调用）。
//
// 热加载会换掉插件进程：旧的驱动实例还握着已经断开的 loopback 连接，必须按驱动
// 身份重建一遍，否则对应 provider 会一直被算作「可用」但每次调用都失败。
//
// 返回重建成功的实例数；单个 provider 重建失败只记日志（例如插件配置过期），
// 失败的实例已从缓存摘掉，等下一次 EnsureProvider / 热加载再试。
func ReloadDrivers(driverIDs map[string]struct{}) (int, error) {
	if len(driverIDs) == 0 {
		return 0, nil
	}
	sps, err := db.GetEnabledProviders()
	if err != nil {
		return 0, fmt.Errorf("读取存储提供者失败: %w", err)
	}
	rebuilt := 0
	var failures []string
	for _, sp := range sps {
		if sp.Id == nil || sp.DriverId == nil {
			continue
		}
		if _, ok := driverIDs[*sp.DriverId]; !ok {
			continue
		}
		id := *sp.Id
		// 先摘掉旧实例：它绑着已经关闭（或即将关闭）的旧插件进程
		storages.Delete(id)
		if err := createAndStoreDriver(sp); err != nil {
			failures = append(failures, fmt.Sprintf("%d(%s): %v", id, *sp.DriverId, err))
			continue
		}
		rebuilt++
		llog.L().Infof("storage provider reloaded, id: %d, driver: %s", id, *sp.DriverId)
	}
	if len(failures) > 0 {
		return rebuilt, fmt.Errorf("重建 %d 个 provider 实例失败: %s", len(failures), strings.Join(failures, "; "))
	}
	return rebuilt, nil
}

// EnsureProvider 确保指定 provider 的驱动已初始化（懒加载、幂等）。
// 供 CLI 等未整体 Init() 的入口按需使用（如去重命令回收远程物理副本）。
func EnsureProvider(id uint32) (types.StorageDriver, error) {
	if st := GetStorage(id); st != nil {
		return st, nil
	}
	sps, err := db.GetEnabledProviders()
	if err != nil {
		return nil, err
	}
	for _, sp := range sps {
		if sp.Id == nil || *sp.Id != id {
			continue
		}
		if err := createAndStoreDriver(sp); err != nil {
			return nil, err
		}
		st := GetStorage(id)
		if st == nil {
			return nil, fmt.Errorf("driver not stored after create, id: %d", id)
		}
		return st, nil
	}
	return nil, fmt.Errorf("no enabled storage provider with id: %d", id)
}

// dbCacheUpdater 基于 db 层的缓存状态更新器
type dbCacheUpdater struct{}

func (dbCacheUpdater) UpdateCacheStatus(locationID uint64, status abi.CacheStatus) error {
	return db.UpdateFileLocationCacheStatus(locationID, native.CacheStatus(status))
}

// dbConfigPersister 基于 db 层的配置持久化器
type dbConfigPersister struct{}

func (dbConfigPersister) UpdateConfig(providerID uint32, configJSON string) error {
	return db.UpdateStorageProviderConfig(providerID, configJSON)
}

// closeAndDeleteStorage 关闭并删除存储实例
func closeAndDeleteStorage(id uint32) {
	if v, ok := storages.Load(id); ok {
		driver := v.(types.StorageDriver)
		// 检查是否实现了 Closer 接口
		if closer, ok := driver.(types.Closer); ok {
			closer.Close()
		}
		storages.Delete(id)
	}
}

// AddStorage 添加存储实例（异步执行，不阻塞请求）
func AddStorage(sp *entity.StorageProviderORM) {
	if !*sp.Enabled {
		return
	}

	// 异步创建驱动实例
	go func() {
		defer func() {
			if r := recover(); r != nil {
				llog.L().Errorf("AddStorage panicked, id: %d, recover: %v", *sp.Id, r)
			}
		}()
		if err := createAndStoreDriver(sp); err != nil {
			llog.L().Errorf("create storage driver failed, id: %d, err: %v", *sp.Id, err)
			return
		}
		llog.L().Infof("storage provider added, id: %d", *sp.Id)
	}()
}

// UpdateStorage 更新存储实例（异步执行，不阻塞请求）
func UpdateStorage(sp *entity.StorageProviderORM) {
	// 先关闭并删除旧实例（这个操作很快，可以同步执行）
	closeAndDeleteStorage(*sp.Id)

	// 如果启用则异步重新创建
	if *sp.Enabled {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					llog.L().Errorf("UpdateStorage panicked, id: %d, recover: %v", *sp.Id, r)
				}
			}()
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
			// 检查是否实现了 Closer 接口
			if closer, ok := driver.(types.Closer); ok {
				closer.Close()
				llog.L().Infof("storage provider closed, id: %v", key)
			}
		}
		return true
	})
	storages = syncmap.Map{}
}
