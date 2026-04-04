package local

import (
	"encoding/json"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

func init() {
	// 定义本地存储驱动的元数据
	metadata := &native.StorageProviderTypeInfo{
		Type:        native.StorageProviderType_LOCAL.Enum(),
		Name:        utils.StringPtr("LOCAL"),
		DisplayName: utils.StringPtr("本地存储"),
		Description: utils.StringPtr("使用本地文件系统存储文件"),
		IconName:    utils.StringPtr("storage"),
		ConfigFields: []*native.ConfigField{
			{
				Name:        utils.StringPtr("root_path"),
				FieldType:   native.ConfigFieldType_STRING.Enum(),
				Label:       utils.StringPtr("根路径"),
				Placeholder: utils.StringPtr("例如：/data/storage"),
				HelperText:  utils.StringPtr("存储文件的根目录路径"),
				Required:    utils.BoolPtr(true),
				IconName:    utils.StringPtr("folder_open"),
				InputType:   utils.StringPtr("text"),
			},
			{
				Name:         utils.StringPtr("mkdir_permission"),
				FieldType:    native.ConfigFieldType_STRING.Enum(),
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

	types.RegisterStorageDriverWithMetadata(func(provider *native.StorageProviderORM) (types.StorageDriver, error) {
		c := native.LocalDriverConfig{}
		err := json.Unmarshal([]byte(*provider.Config), &c)
		if err != nil {
			llog.L().Errorf("unmarshal config failed, config: %s, err: %v", *provider.Config, err)
			return nil, err
		}

		return &LocalDriver{
			Config: &c,
			Orm:    provider,
		}, nil
	}, metadata)
}
