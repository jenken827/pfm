package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"jenken827.com/jenken827/pfm/driver/types"
)

// manifest 插件目录里的 plugin.json
//
// 一个插件是一个目录：plugin.json + 可执行文件（exec 相对该目录）。
// 只有 driver_id / exec / abi_version 是必填的，其余仅用于 pfm plugin list 展示。
type manifest struct {
	// DriverID 插件提供的驱动身份，必须与 Describe 返回的一致
	DriverID string `json:"driver_id"`
	// Exec 可执行文件（相对插件目录的路径，或绝对路径）
	Exec string `json:"exec"`
	// AbiVersion 插件编译时使用的协议版本
	AbiVersion uint32 `json:"abi_version"`
	// Name 插件包名（如 pfm-baidu），仅用于展示
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
	if err := types.ValidateDriverID(m.DriverID); err != nil {
		return err
	}
	if m.AbiVersion != 0 && m.AbiVersion != abiVersion {
		return fmt.Errorf("协议版本 %d 与宿主 %d 不兼容", m.AbiVersion, abiVersion)
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
