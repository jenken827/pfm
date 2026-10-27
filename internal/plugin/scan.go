package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"jenken827.com/jenken827/pfm/globals"
)

// ManifestInfo 插件目录的静态概况（不拉起进程）
type ManifestInfo struct {
	DriverID   string `json:"driver_id"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	AbiVersion uint32 `json:"abi_version"`
	Dir        string `json:"dir"`
	// Error 该插件目录的问题（清单缺失/非法/可执行文件不可用）
	Error string `json:"error,omitempty"`
}

// ResolveDir 把配置里的插件目录解析成绝对路径
//
// 相对路径按配置文件所在目录（即数据目录）解析，这样 pfm 在任何工作目录下
// 启动都指向同一份插件。
func ResolveDir(dir string) string {
	if dir == "" || filepath.IsAbs(dir) {
		return dir
	}
	if globals.ConfigPath != "" {
		return filepath.Join(filepath.Dir(globals.ConfigPath), dir)
	}
	return dir
}

// Scan 列出目录下的全部插件清单（不启动进程，供 pfm plugin list 使用）
func Scan(dir string) ([]ManifestInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// 目录还不存在 = 一个插件都没装，属于正常状态
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取插件目录 %s 失败: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		// 跳过 .staging / .versions / .state.json 这些管理用的隐藏项
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	infos := make([]ManifestInfo, 0, len(names))
	for _, name := range names {
		pluginDir := filepath.Join(dir, name)
		info := ManifestInfo{Dir: pluginDir}
		mf, loadErr := loadManifest(pluginDir)
		if loadErr != nil {
			info.Error = loadErr.Error()
			infos = append(infos, info)
			continue
		}
		info.DriverID = mf.DriverID
		info.Name = mf.Name
		info.Version = mf.Version
		info.AbiVersion = mf.AbiVersion
		infos = append(infos, info)
	}
	return infos, nil
}
