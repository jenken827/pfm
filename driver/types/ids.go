package types

import (
	"fmt"
	"regexp"
	"strings"

	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// 驱动身份（driver_id）。
//
// 身份是「这个 provider 由哪个驱动实现」的稳定字符串，落库在
// storage_providers.provider_type 里。发布后不可更改：改了库里的 provider
// 就找不到驱动。命名规则见 ValidateDriverID。
//
// 插件提供的驱动使用自己的身份（如 dropbox）；这里列出的只是内置驱动，
// 宿主逻辑不应再按这些常量做分支 —— 需要区分行为时看 DriverInfo.Capabilities。
const (
	DriverLocal           = "local"
	DriverTelegram        = "telegram"
	DriverDiscord         = "discord"
	DriverHuggingFace     = "huggingface"
	DriverPan123          = "pan123"
	DriverBaidu           = "baidu"
	DriverAliyundriveOpen = "aliyundrive_open"
	DriverYun139          = "yun139"
	DriverCloud189pc      = "cloud189pc"
)

// driverIDOrder 内置驱动的展示顺序（沿用原枚举声明顺序）。
//
// 类型列表直接喂给前端「新增存储源」下拉框，顺序乱跳会让用户每次看到的
// 第一项都不同；未列出的身份（插件新增驱动）排在最后、按字典序。
var driverIDOrder = []string{
	DriverLocal,
	DriverTelegram,
	DriverDiscord,
	DriverHuggingFace,
	DriverPan123,
	DriverBaidu,
	DriverAliyundriveOpen,
	DriverYun139,
	DriverCloud189pc,
}

// legacyDriverIDNames 旧枚举名 -> 驱动身份。
//
// 仅用于兼容存量配置（如 transfer.direct_link.providers 里写的是 BAIDU_NETDISK）。
var legacyDriverIDNames = map[string]string{
	"LOCAL":            DriverLocal,
	"TELEGRAM":         DriverTelegram,
	"DISCORD":          DriverDiscord,
	"HUGGINGFACE":      DriverHuggingFace,
	"PAN123":           DriverPan123,
	"BAIDU_NETDISK":    DriverBaidu,
	"ALIYUNDRIVE_OPEN": DriverAliyundriveOpen,
	"YUN139":           DriverYun139,
	"CLOUD189_PC":      DriverCloud189pc,
}

var driverIDPattern = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

// NormalizeDriverID 把配置/命令行里可能出现的旧枚举名归一成驱动身份。
// 未知名字原样返回（交给后续比较失败），不做模糊匹配。
func NormalizeDriverID(name string) string {
	if id, ok := legacyDriverIDNames[strings.ToUpper(strings.TrimSpace(name))]; ok {
		return id
	}
	return name
}

// ValidateDriverID 校验驱动身份：小写字母/数字/下划线，1~32 字符。
func ValidateDriverID(id string) error {
	if !driverIDPattern.MatchString(id) {
		return fmt.Errorf("非法驱动身份 %q：只允许小写字母、数字、下划线，长度 1~32", id)
	}
	return nil
}

// DriverRank 返回内置驱动身份在展示顺序里的位次，未知身份返回 len(driverIDOrder)。
func DriverRank(id string) int {
	for i, known := range driverIDOrder {
		if known == id {
			return i
		}
	}
	return len(driverIDOrder)
}

// HasCapability 判断驱动元数据是否声明了某项能力。
func HasCapability(md *native.DriverInfo, capability native.DriverCapability) bool {
	if md == nil {
		return false
	}
	for _, c := range md.GetCapabilities() {
		if c == capability {
			return true
		}
	}
	return false
}
