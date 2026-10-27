package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// stateFileName 插件安装状态（索引 seq + 每个插件的安装记录），放在插件目录根部
const stateFileName = ".state.json"

// archivedDirName 历史版本归档目录（rollback 用）
const archivedDirName = ".versions"

// stagingDirName 下载解包的临时目录（安装失败要清理）
const stagingDirName = ".staging"

// installedRecord 一个已安装插件的记录（用于完整性校验与回滚）
type installedRecord struct {
	// Kind 插件类型（driver / …），回滚与完整性校验都按它来
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	DriverID string `json:"driver_id"`
	// DownloaderID 下载器身份（kind=downloader 才有）
	DownloaderID string `json:"downloader_id,omitempty"`
	Version      string `json:"version"`
	ABIVersion   uint32 `json:"abi_version"`
	SHA256       string `json:"sha256"`
	// TreeSHA256 安装目录内容摘要（改过任何一个文件都能发现）
	TreeSHA256      string    `json:"tree_sha256"`
	InstalledAt     time.Time `json:"installed_at"`
	PreviousVersion string    `json:"previous_version,omitempty"`
}

// installState 插件目录的安装状态
type installState struct {
	// Seq 已接受的最新索引序号（防回滚：比它旧的一律拒绝）
	Seq       uint64                     `json:"seq"`
	Installed map[string]installedRecord `json:"installed"`
}

// loadState 读取安装状态（文件不存在 = 空状态）
func loadState(dir string) (*installState, error) {
	path := filepath.Join(dir, stateFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &installState{Installed: map[string]installedRecord{}}, nil
		}
		return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	state := &installState{}
	if err := unmarshalStrict(raw, state); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	if state.Installed == nil {
		state.Installed = map[string]installedRecord{}
	}
	return state, nil
}

// saveState 原子写入安装状态（先写临时文件再 rename）
func saveState(dir string, state *installState) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, stateFileName)
	temp := path + ".tmp"
	if err := os.WriteFile(temp, raw, 0o600); err != nil {
		return fmt.Errorf("写 %s 失败: %w", temp, err)
	}
	if err := os.Rename(temp, path); err != nil {
		return fmt.Errorf("更新 %s 失败: %w", path, err)
	}
	return nil
}

// unmarshalStrict 严格解析 JSON（未知字段报错，避免索引/状态格式漂移被静默忽略）
func unmarshalStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
