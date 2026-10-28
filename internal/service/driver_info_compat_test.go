package service

import (
	"testing"

	abi "github.com/jenken827/pfm-plugin-sdk/proto/gen/abi"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestDriverInfoAbiMatchesAppProto 锁住 ABI 元数据与应用侧 proto 的字段一致性
//
// abi.DriverInfo / abi.ConfigField 是**插件契约**（在 SDK 仓库里维护），
// native.DriverInfo / native.ConfigField 是**应用 API**（前端按它渲染表单）。
// 两者由 nativeDriverInfo 一一映射，任何一侧加字段而另一侧没跟上，都会让
// 「插件声明了配置项、前端却渲染不出来」这种问题悄悄发生。
//
// 这个测试不要求两边永远相同（应用侧可以有应用侧的字段），只要求：
//   - ABI 的每个字段在应用侧都存在，且 protobuf 编号与类型相同；
//   - 应用侧多出来的字段必须显式列在下面的白名单里（逼人想清楚要不要展示）。
func TestDriverInfoAbiMatchesAppProto(t *testing.T) {
	cases := []struct {
		name     string
		abiMsg   protoreflect.MessageDescriptor
		appMsg   protoreflect.MessageDescriptor
		appExtra []string
	}{
		{
			name:   "DriverInfo",
			abiMsg: (&abi.DriverInfo{}).ProtoReflect().Descriptor(),
			appMsg: (&native.DriverInfo{}).ProtoReflect().Descriptor(),
		},
		{
			name:   "ConfigField",
			abiMsg: (&abi.ConfigField{}).ProtoReflect().Descriptor(),
			appMsg: (&native.ConfigField{}).ProtoReflect().Descriptor(),
		},
		{
			name:   "ConfigSelectOption",
			abiMsg: (&abi.ConfigSelectOption{}).ProtoReflect().Descriptor(),
			appMsg: (&native.ConfigSelectOption{}).ProtoReflect().Descriptor(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			abiFields := tc.abiMsg.Fields()
			appFields := tc.appMsg.Fields()

			for i := 0; i < abiFields.Len(); i++ {
				field := abiFields.Get(i)
				appField := appFields.ByNumber(field.Number())
				if appField == nil {
					t.Fatalf("应用侧 %s 缺少 ABI 字段 %s（编号 %d）：请同步 proto 与 nativeDriverInfo",
						tc.name, field.Name(), field.Number())
				}
				if appField.Name() != field.Name() {
					t.Errorf("字段编号 %d 名字不一致: abi=%s app=%s", field.Number(), field.Name(), appField.Name())
				}
				if appField.Kind() != field.Kind() {
					t.Errorf("字段 %s 类型不一致: abi=%v app=%v", field.Name(), field.Kind(), appField.Kind())
				}
			}

			for i := 0; i < appFields.Len(); i++ {
				appField := appFields.Get(i)
				if abiFields.ByNumber(appField.Number()) != nil {
					continue
				}
				allowed := false
				for _, name := range tc.appExtra {
					if string(appField.Name()) == name {
						allowed = true
						break
					}
				}
				if !allowed {
					t.Errorf("应用侧 %s 多出字段 %s（编号 %d）：要么加进 ABI，要么登记到白名单",
						tc.name, appField.Name(), appField.Number())
				}
			}
		})
	}
}

// TestDriverCapabilityAbiMatchesAppProto ABI 与应用的驱动能力枚举必须同值
//
// 能力是宿主按位/按值比较的开关，两套枚举一旦错位，插件声明「支持批量删除」
// 会被宿主当成别的能力。
func TestDriverCapabilityAbiMatchesAppProto(t *testing.T) {
	abiEnum := abi.DriverCapability(0).Descriptor().Values()
	appEnum := native.DriverCapability(0).Descriptor().Values()
	if abiEnum.Len() != appEnum.Len() {
		t.Fatalf("能力枚举数量不一致: abi=%d app=%d", abiEnum.Len(), appEnum.Len())
	}
	for i := 0; i < abiEnum.Len(); i++ {
		av, nv := abiEnum.Get(i), appEnum.Get(i)
		if av.Number() != nv.Number() || av.Name() != nv.Name() {
			t.Errorf("能力枚举不一致: abi=%s(%d) app=%s(%d)", av.Name(), av.Number(), nv.Name(), nv.Number())
		}
	}
}
