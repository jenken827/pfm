package local

import (
	"encoding/json"

	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	abi "github.com/jenken827/pfm-plugin-sdk/proto/gen/abi"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/utils"
)

// DriverID 是本地存储驱动的身份（落库在 storage_providers.provider_type）。
//
// 内置驱动的身份由驱动自己声明；宿主代码不维护"有哪些驱动"的清单——插件在
// init() 里注册自己的身份，宿主只按注册表与能力位工作。
const DriverID = "local"

func init() {
	// 定义本地存储驱动的元数据
	metadata := &abi.DriverInfo{
		DriverId: utils.StringPtr(DriverID),
		Capabilities: []abi.DriverCapability{
			abi.DriverCapability_DRIVER_CAPABILITY_BATCH_DELETE,
			abi.DriverCapability_DRIVER_CAPABILITY_REMOTE_LIST,
		},
		Name:        utils.StringPtr("LOCAL"),
		DisplayName: utils.StringPtr("本地存储"),
		Description: utils.StringPtr("使用本地文件系统存储文件"),
		IconName:    utils.StringPtr("storage"),
		ConfigFields: []*abi.ConfigField{
			{
				Name:        utils.StringPtr("root_path"),
				FieldType:   abi.ConfigFieldType_STRING.Enum(),
				Label:       utils.StringPtr("根路径"),
				Placeholder: utils.StringPtr("例如：/data/storage"),
				HelperText:  utils.StringPtr("存储文件的根目录路径"),
				Required:    utils.BoolPtr(true),
				IconName:    utils.StringPtr("folder_open"),
				InputType:   utils.StringPtr("text"),
			},
			{
				Name:         utils.StringPtr("mkdir_permission"),
				FieldType:    abi.ConfigFieldType_STRING.Enum(),
				Label:        utils.StringPtr("文件夹权限"),
				Placeholder:  utils.StringPtr("0755"),
				HelperText:   utils.StringPtr("新建文件夹的权限（八进制）"),
				Required:     utils.BoolPtr(false),
				DefaultValue: utils.StringPtr("0755"),
				IconName:     utils.StringPtr("security"),
				InputType:    utils.StringPtr("text"),
			},
		},
	}

	types.RegisterStorageDriverWithMetadata(func(provider *types.Provider) (types.StorageDriver, error) {
		c := native.LocalDriverConfig{}
		if err := json.Unmarshal([]byte(provider.ConfigJSON), &c); err != nil {
			llog.L().Errorf("unmarshal config failed, provider: %d, err: %v", provider.ID, err)
			return nil, err
		}

		return &LocalDriver{
			Config:         &c,
			ProviderConfig: provider.Config,
		}, nil
	}, metadata)
}
