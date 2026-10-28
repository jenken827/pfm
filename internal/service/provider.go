package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	abi "github.com/jenken827/pfm-plugin-sdk/proto/gen/abi"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/driver/local"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/models"
	"github.com/jenken827/pfm/internal/storage"
	"github.com/jenken827/pfm/pkg/llog"
	"github.com/jenken827/pfm/utils"
)

// builtinDriverOrder 宿主自带驱动的展示顺序（新增内置驱动时在这里加一行）。
//
// 只列**随宿主发布**的驱动：插件驱动不在这里，它们由插件自己注册，排在后面按
// driver_id 字典序——宿主代码不该认识任何一个插件驱动的名字。
var builtinDriverOrder = []string{local.DriverID}

// driverOrderRank 内置驱动在展示顺序里的位次，非内置返回 len(builtinDriverOrder)
func driverOrderRank(driverID string) int {
	for i, id := range builtinDriverOrder {
		if id == driverID {
			return i
		}
	}
	return len(builtinDriverOrder)
}

// GetStorageProviderTypes 获取支持的存储提供者类型列表
func GetStorageProviderTypes(ctx context.Context, req *native.GetStorageProviderTypesReq) (*native.GetStorageProviderTypesRespData, *models.Err) {
	metadatas := types.GetAllDriverMetadata()
	// 注册表只保证确定性（字典序）；"内置驱动排前面"是宿主的展示策略，在这里做。
	sort.SliceStable(metadatas, func(i, j int) bool {
		ri, rj := driverOrderRank(metadatas[i].GetDriverId()), driverOrderRank(metadatas[j].GetDriverId())
		if ri != rj {
			return ri < rj
		}
		return metadatas[i].GetDriverId() < metadatas[j].GetDriverId()
	})
	typesOut := make([]*native.DriverInfo, 0, len(metadatas))
	for _, info := range metadatas {
		typesOut = append(typesOut, nativeDriverInfo(info))
	}

	return &native.GetStorageProviderTypesRespData{
		Types: typesOut,
	}, nil
}

// nativeDriverInfo 把插件 ABI 的驱动元数据转成应用侧 proto
//
// 两套消息刻意分开：abi.DriverInfo 是**插件契约**（只随 ABI 演进，插件与宿主
// 共享），native.DriverInfo 是**应用 API**（前端表单按它渲染）。两边字段一一对应，
// 改动时要同时改；一致性由 driver_info_compat_test.go 盯着。
func nativeDriverInfo(info *abi.DriverInfo) *native.DriverInfo {
	if info == nil {
		return nil
	}
	out := &native.DriverInfo{
		DriverId:    info.DriverId,
		Name:        info.Name,
		DisplayName: info.DisplayName,
		Description: info.Description,
		IconName:    info.IconName,
	}
	for _, capability := range info.GetCapabilities() {
		out.Capabilities = append(out.Capabilities, native.DriverCapability(capability))
	}
	for _, field := range info.GetConfigFields() {
		if field == nil {
			continue
		}
		converted := &native.ConfigField{
			Name:              field.Name,
			FieldType:         native.ConfigFieldType(field.GetFieldType()).Enum(),
			Label:             field.Label,
			Placeholder:       field.Placeholder,
			HelperText:        field.HelperText,
			Required:          field.Required,
			DefaultValue:      field.DefaultValue,
			ValidationPattern: field.ValidationPattern,
			ValidationMessage: field.ValidationMessage,
			IconName:          field.IconName,
			InputType:         field.InputType,
			MaxLength:         field.MaxLength,
			MinValue:          field.MinValue,
			MaxValue:          field.MaxValue,
			MultilineRows:     field.MultilineRows,
		}
		for _, option := range field.GetSelectOptions() {
			converted.SelectOptions = append(converted.SelectOptions, &native.ConfigSelectOption{
				Value: option.GetValue(),
				Label: option.GetLabel(),
			})
		}
		out.ConfigFields = append(out.ConfigFields, converted)
	}
	return out
}

// AddStorageProvider 添加存储提供者
func AddStorageProvider(ctx context.Context, req *native.AddStorageProviderReq) *models.Err {
	driverID := req.GetDriverId()
	if err := types.ValidateDriverID(driverID); err != nil {
		return &models.Err{E: err, C: native.RespCode_PARAMS_ERROR}
	}
	if types.GetDriverConstructor(driverID) == nil {
		// 驱动未安装：先装插件再建 provider，否则落库后也没有实例可用。
		return &models.Err{
			E: fmt.Errorf("驱动 %q 未安装", driverID),
			C: native.RespCode_PARAMS_ERROR,
		}
	}
	orm := &entity.StorageProviderORM{
		DriverId:      utils.StringPtr(driverID),
		Name:          req.Name,
		Config:        req.Config,
		TotalCapacity: req.TotalCapacity,
		UsedCapacity:  new(uint64),
		Enabled:       req.Enabled,
		SortOrder:     req.SortOrder,
		FileSizeLimit: req.FileSizeLimit,
		DefaultBound:  req.DefaultBound,
	}

	if req.Description != nil {
		orm.Description = req.Description
	}
	if req.EncryptionKey != nil {
		orm.EncryptionKey = req.EncryptionKey
	}
	if req.EncryptionSuffix != nil {
		orm.EncryptionSuffix = req.EncryptionSuffix
	}
	if req.Proxy != nil {
		orm.Proxy = req.Proxy
	}
	if req.MaxConcurrentUploads != nil {
		orm.MaxConcurrentUploads = req.MaxConcurrentUploads
	}

	err := db.CreateStorageProvider(orm)
	if err != nil {
		llog.C(ctx).Errorf("CreateStorageProvider err: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 异步添加存储实例（不阻塞请求）
	storage.AddStorage(orm)

	return nil
}

// GetStorageProvider 获取存储提供者详情
func GetStorageProvider(ctx context.Context, req *native.StorageProviderGetReq) (*entity.StorageProviderORM, *models.Err) {
	provider, err := db.GetProviderByID(req.GetId())
	if err != nil {
		llog.C(ctx).Errorf("GetStorageProviders, id: %d, err: %v", req.GetId(), err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	return provider, nil
}

// ListStorageProviders 获取存储提供者列表
func ListStorageProviders(ctx context.Context) ([]*entity.StorageProviderORM, *models.Err) {
	providers, err := db.GetAllProviders()
	if err != nil {
		llog.C(ctx).Errorf("GetStorageProviders, err: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	return providers, nil
}

// UpdateStorageProvider update
func UpdateStorageProvider(ctx context.Context, req *native.UpdateStorageProviderReq) *models.Err {
	orm := &entity.StorageProviderORM{
		Id: req.Id,
	}

	if req.Name != nil {
		orm.Name = req.Name
	}
	if req.Description != nil {
		orm.Description = req.Description
	}
	if req.Config != nil {
		orm.Config = req.Config
	}
	if req.Enabled != nil {
		orm.Enabled = req.Enabled
	}
	if req.SortOrder != nil {
		orm.SortOrder = req.SortOrder
	}
	if req.TotalCapacity != nil {
		orm.TotalCapacity = req.TotalCapacity
	}
	if req.FileSizeLimit != nil {
		orm.FileSizeLimit = req.FileSizeLimit
	}
	if req.DefaultBound != nil {
		orm.DefaultBound = req.DefaultBound
	}
	if req.Proxy != nil {
		orm.Proxy = req.Proxy
	}
	if req.MaxConcurrentUploads != nil {
		orm.MaxConcurrentUploads = req.MaxConcurrentUploads
	}

	err := db.UpdateStorageProvider(orm)
	if err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 查询完整数据后异步更新存储实例（不阻塞请求）
	fullOrm, err := db.GetProviderByID(req.GetId())
	if err != nil {
		llog.C(ctx).Errorf("GetProviderByID err: %v", err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	storage.UpdateStorage(fullOrm)

	// 刷新并发信号量缓存（max_concurrent_uploads 变更后需要重建信号量）
	storage.RefreshProviderSemaphore(req.GetId())

	return nil
}

// DeleteStorageProvider 删除存储提供者
func DeleteStorageProvider(ctx context.Context, req *native.StorageProviderDeleteReq) *models.Err {
	err := db.DeleteStorageProvider(req.GetIds())
	if err != nil {
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	// 同步删除存储实例
	storage.DeleteStorage(req.GetIds())

	return nil
}
