package plugin

import (
	"fmt"
	"sort"
	"strings"

	abi "github.com/jenken827/pfm-plugin-sdk/proto/gen/abi"
)

// 插件类型（kind）。
//
// 进程模型（握手 + loopback RPC + 安装 / 验签 / 热加载）与具体类型无关，
// 但每种类型有自己的线上方法与协议版本：`driver` 插件注册到驱动注册表，
// `downloader` 插件注册成离线下载后端。再加新类型时在这里登记 kind + ABI，
// 再由 Manager 按 kind 分派即可，不需要动安装链。
const (
	// KindDriver 存储驱动插件：实现 DriverPlugin 服务，按 driver_id 注册驱动覆盖
	KindDriver = "driver"
	// KindDownloader 离线下载后端插件：实现 DownloaderPlugin 服务，按 downloader_id
	// 提供「提交任务 / 查进度 / 取消」，产物入库仍在宿主
	KindDownloader = "downloader"
)

// kindABI 每种插件类型的线上协议版本（握手行里的版本号，也是包内 plugin.json 的 abi_version）
// 版本号直接取自 SDK 的 ABI 枚举，避免这里写死 1 而和契约漂移。
var kindABI = map[string]uint32{
	KindDriver:     uint32(abi.AbiVersion_ABI_VERSION_1),
	KindDownloader: uint32(abi.AbiVersion_ABI_VERSION_1),
}

// ABI 返回该插件类型的协议版本；未知类型返回 false
func ABI(kind string) (uint32, bool) {
	version, ok := kindABI[kind]
	return version, ok
}

// Kinds 返回当前支持的插件类型（排序，供报错与文档列举）
func Kinds() []string {
	kinds := make([]string, 0, len(kindABI))
	for kind := range kindABI {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

// validateKind 校验插件类型并返回它的协议版本
func validateKind(kind string) (uint32, error) {
	trimmed := strings.TrimSpace(kind)
	if trimmed == "" {
		return 0, fmt.Errorf("plugin.json 缺少 kind（当前支持：%s）", strings.Join(Kinds(), ", "))
	}
	version, ok := ABI(trimmed)
	if !ok {
		return 0, fmt.Errorf("未知插件类型 %q（当前支持：%s）", trimmed, strings.Join(Kinds(), ", "))
	}
	return version, nil
}
