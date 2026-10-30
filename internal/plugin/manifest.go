package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jenken827/pfm-plugin-sdk/driver/types"
)

// manifest 插件目录里的 plugin.json
//
// 一个插件是一个目录：plugin.json + 可执行文件（exec 相对该目录）。
// kind / exec / abi_version 是必填的；driver 类型还要求 driver_id，
// downloader 类型还要求 downloader_id，tool 类型还要求 tool_id，其余字段仅用于展示。
type manifest struct {
	// Kind 插件类型（见 abi.go 的 Kind* 常量），决定宿主怎么用这个插件
	Kind string `json:"kind"`
	// DriverID 插件提供的驱动身份（kind=driver 必填），必须与 Describe 返回的一致
	DriverID string `json:"driver_id"`
	// DownloaderID 插件提供的下载器身份（kind=downloader 必填），必须与 Describe 返回的一致
	DownloaderID string `json:"downloader_id"`
	// ToolID 插件提供的工具身份（kind=tool 必填），工具在 MCP 里暴露为 <tool_id>.<name>
	ToolID string `json:"tool_id"`
	// Exec 可执行文件（相对插件目录的路径，或绝对路径）
	Exec string `json:"exec"`
	// AbiVersion 插件编译时使用的协议版本
	AbiVersion uint32 `json:"abi_version"`
	// Name 插件包名（如 pfm-plugin-driver-xxx），仅用于展示
	Name string `json:"name"`
	// Version 插件版本，仅用于展示
	Version string `json:"version"`
}

// loadManifest 读取并校验 plugin.json
func loadManifest(dir string) (*manifest, error) {
	path := filepath.Join(dir, "plugin.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	m := &manifest{}
	if err := json.Unmarshal(raw, m); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	if err := m.validate(dir); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

func (m *manifest) validate(dir string) error {
	expectABI, err := validateKind(m.Kind)
	if err != nil {
		return err
	}
	switch m.Kind {
	case KindDriver:
		if err := types.ValidateDriverID(m.DriverID); err != nil {
			return err
		}
	case KindDownloader:
		if err := validateKindID("downloader_id", m.DownloaderID); err != nil {
			return err
		}
	case KindTool:
		if err := validateKindID("tool_id", m.ToolID); err != nil {
			return err
		}
	}
	if m.AbiVersion != expectABI {
		return fmt.Errorf("协议版本 %d 与宿主 %d 不兼容", m.AbiVersion, expectABI)
	}
	if strings.TrimSpace(m.Exec) == "" {
		return fmt.Errorf("缺少 exec")
	}
	if _, err := os.Stat(m.execPath(dir)); err != nil {
		return fmt.Errorf("可执行文件不可用: %w", err)
	}
	return nil
}

// execPath 返回可执行文件的绝对路径
func (m *manifest) execPath(dir string) string {
	if filepath.IsAbs(m.Exec) {
		return m.Exec
	}
	return filepath.Join(dir, m.Exec)
}

// validateKindID 插件身份（下载器 / 工具）：小写字母 / 数字 / 下划线，与驱动身份同一套规则
func validateKindID(field, id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return fmt.Errorf("缺少 %s", field)
	}
	if trimmed != id {
		return fmt.Errorf("%s %q 前后不能有空白", field, id)
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
		default:
			return fmt.Errorf("%s %q 只能包含小写字母、数字和下划线", field, id)
		}
	}
	return nil
}

// ID 插件的身份标识（driver_id 或 downloader_id），按 kind 取
func (m *manifest) ID() string {
	switch m.Kind {
	case KindDownloader:
		return m.DownloaderID
	case KindTool:
		return m.ToolID
	default:
		return m.DriverID
	}
}
