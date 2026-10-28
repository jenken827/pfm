package plugin

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// maxArchiveBytes 单个插件包的大小上限（也用于解包总量，防压缩炸弹）
const maxArchiveBytes = 512 << 20

// Installer 从签名索引安装 / 回滚插件
//
// 信任链：内嵌（或配置）的 ed25519 公钥 → 索引签名 → 索引里的 sha256 → 包内容。
// 这里不做"官方 = 安全"的假设：签名只证明包来自签名的发布流程。
type Installer struct {
	// Dir 插件根目录（每个插件一个子目录）
	Dir string
	// IndexURL 索引地址（index.json；签名在同 URL + ".sig"）
	IndexURL string
	// Keys 可信公钥
	Keys []ed25519.PublicKey
	// HTTP 下载客户端（nil 用默认）
	HTTP *http.Client
	// PfmVersion 当前 pfm 版本，用来校验 min_pfm_version
	PfmVersion string
}

// InstallResult 安装结果
type InstallResult struct {
	Kind            string
	Name            string
	DriverID        string
	DownloaderID    string
	Version         string
	PreviousVersion string
	Seq             uint64
	Dir             string
}

// Install 安装（或升级）指定插件
//
// version 为空时取索引里该名字的最高版本。
func (i *Installer) Install(ctx context.Context, name, version string) (*InstallResult, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("必须指定插件名（plugin.json 里的 name）")
	}
	client := i.HTTP
	if client == nil {
		client = defaultHTTPClient()
	}
	// 插件根目录 / 临时目录可能还不存在（首次安装）
	if err := os.MkdirAll(filepath.Join(i.Dir, stagingDirName), 0o755); err != nil {
		return nil, fmt.Errorf("创建插件目录失败: %w", err)
	}
	state, err := loadState(i.Dir)
	if err != nil {
		return nil, err
	}
	index, _, err := fetchIndex(ctx, client, i.IndexURL, indexVerifier{keys: i.Keys})
	if err != nil {
		return nil, err
	}
	// 防回滚：索引序号必须单调不减
	if index.Seq < state.Seq {
		return nil, fmt.Errorf("索引 seq=%d 比本地记录 %d 更旧，拒绝安装（防回滚）", index.Seq, state.Seq)
	}
	entry, err := index.Entry(name, version, currentPlatform())
	if err != nil {
		return nil, err
	}
	// 类型与协议版本在下载之前就判断：装完才发现宿主用不了，等于白下几十 MB
	abi, ok := ABI(entry.Kind)
	if !ok {
		return nil, fmt.Errorf("%s 是 %q 类型的插件，当前 pfm 不支持（支持的：%s），请升级 pfm",
			entry.Name, entry.Kind, strings.Join(Kinds(), " / "))
	}
	if entry.ABIVersion != abi {
		return nil, fmt.Errorf("%s@%s 的索引声明 abi_version=%d，%s 类型应为 %d",
			entry.Name, entry.Version, entry.ABIVersion, entry.Kind, abi)
	}
	if entry.ID() == "" {
		return nil, fmt.Errorf("%s@%s 的索引缺少 %s 类型的身份字段（driver_id / downloader_id）",
			entry.Name, entry.Version, entry.Kind)
	}
	if !versionAtLeast(i.PfmVersion, entry.MinPfmVersion) {
		return nil, fmt.Errorf("%s %s 需要 pfm >= %s（当前 %s）", entry.Name, entry.Version,
			entry.MinPfmVersion, i.PfmVersion)
	}
	archivePath, digest, err := i.download(ctx, client, entry)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(archivePath) }()
	if !strings.EqualFold(digest, entry.SHA256) {
		return nil, fmt.Errorf("包 sha256 不匹配（期望 %s，实际 %s）：下载可能被篡改", entry.SHA256, digest)
	}

	staging, err := os.MkdirTemp(filepath.Join(i.Dir, stagingDirName), entry.Name+"-")
	if err != nil {
		return nil, fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()
	if err := extractArchive(archivePath, staging); err != nil {
		return nil, err
	}
	mf, err := loadManifest(staging)
	if err != nil {
		return nil, fmt.Errorf("插件包内容不合法: %w", err)
	}
	if mf.Kind != entry.Kind {
		return nil, fmt.Errorf("包内 kind=%s 与索引声明的 %s 不一致", mf.Kind, entry.Kind)
	}
	if mf.DriverID != entry.DriverID || mf.DownloaderID != entry.DownloaderID {
		return nil, fmt.Errorf("包内身份（driver_id=%s downloader_id=%s）与索引声明的（%s / %s）不一致",
			mf.DriverID, mf.DownloaderID, entry.DriverID, entry.DownloaderID)
	}
	treeDigest, err := hashTree(staging)
	if err != nil {
		return nil, err
	}

	result := &InstallResult{
		Kind:         entry.Kind,
		Name:         entry.Name,
		DriverID:     entry.DriverID,
		DownloaderID: entry.DownloaderID,
		Version:      entry.Version,
		Seq:          index.Seq,
		Dir:          filepath.Join(i.Dir, entry.Name),
	}
	if previous, ok := state.Installed[entry.Name]; ok {
		result.PreviousVersion = previous.Version
	}
	if err := switchIn(staging, result.Dir, result.PreviousVersion); err != nil {
		return nil, err
	}
	state.Installed[entry.Name] = installedRecord{
		Kind:            entry.Kind,
		Name:            entry.Name,
		DriverID:        entry.DriverID,
		DownloaderID:    entry.DownloaderID,
		Version:         entry.Version,
		ABIVersion:      entry.ABIVersion,
		SHA256:          entry.SHA256,
		TreeSHA256:      treeDigest,
		InstalledAt:     time.Now(),
		PreviousVersion: result.PreviousVersion,
	}
	state.Seq = index.Seq
	if err := saveState(i.Dir, state); err != nil {
		return nil, err
	}
	return result, nil
}

// Rollback 回滚到上一个版本（再执行一次则退回更早版本）
func (i *Installer) Rollback(name string) (*InstallResult, error) {
	state, err := loadState(i.Dir)
	if err != nil {
		return nil, err
	}
	record, ok := state.Installed[name]
	if !ok {
		return nil, fmt.Errorf("插件 %s 没有安装记录", name)
	}
	if record.PreviousVersion == "" {
		return nil, fmt.Errorf("插件 %s 没有可回滚的历史版本", name)
	}
	archive := filepath.Join(i.Dir, archivedDirName, name, record.PreviousVersion)
	if _, statErr := os.Stat(archive); statErr != nil {
		return nil, fmt.Errorf("历史版本目录不可用: %w", statErr)
	}
	target := filepath.Join(i.Dir, name)
	treeDigest, err := hashTree(archive)
	if err != nil {
		return nil, err
	}
	if err := switchIn(archive, target, record.Version); err != nil {
		return nil, err
	}
	result := &InstallResult{
		Kind:            record.Kind,
		Name:            name,
		DriverID:        record.DriverID,
		Version:         record.PreviousVersion,
		PreviousVersion: record.Version,
		Seq:             state.Seq,
		Dir:             target,
	}
	record.Version = result.Version
	record.PreviousVersion = result.PreviousVersion
	record.TreeSHA256 = treeDigest
	record.InstalledAt = time.Now()
	state.Installed[name] = record
	if err := saveState(i.Dir, state); err != nil {
		return nil, err
	}
	return result, nil
}

// InstalledInfo 已安装插件的概况（CLI 展示用）
type InstalledInfo struct {
	Kind         string
	Name         string
	DriverID     string
	DownloaderID string
	Version      string
	Status       string
	Detail       string
}

// InstalledPlugins 汇总安装记录与当前内容校验结果
func InstalledPlugins(dir string) ([]InstalledInfo, error) {
	state, err := loadState(dir)
	if err != nil {
		return nil, err
	}
	integrity, err := CheckInstalled(dir)
	if err != nil {
		return nil, err
	}
	statusByDriver := make(map[string]Integrity, len(integrity))
	for _, item := range integrity {
		statusByDriver[item.Name] = item
	}
	infos := make([]InstalledInfo, 0, len(state.Installed))
	for name, record := range state.Installed {
		item := InstalledInfo{Kind: record.Kind, Name: name, DriverID: record.DriverID,
			DownloaderID: record.DownloaderID, Version: record.Version}
		if checked, ok := statusByDriver[name]; ok {
			item.Status = checked.Status
			item.Detail = checked.Detail
		}
		infos = append(infos, item)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}

// Integrity 一个插件的安装完整性
type Integrity struct {
	Kind         string
	Name         string
	Version      string
	DriverID     string
	DownloaderID string
	Status       string // ok / modified / missing / unmanaged
	Detail       string
}

// CheckInstalled 校验已安装插件的内容摘要（发现被改动 / 丢失）
func CheckInstalled(dir string) ([]Integrity, error) {
	state, err := loadState(dir)
	if err != nil {
		return nil, err
	}
	results := make([]Integrity, 0, len(state.Installed))
	names := make([]string, 0, len(state.Installed))
	for name := range state.Installed {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		record := state.Installed[name]
		item := Integrity{Kind: record.Kind, Name: name, Version: record.Version,
			DriverID: record.DriverID, DownloaderID: record.DownloaderID}
		target := filepath.Join(dir, name)
		if _, statErr := os.Stat(target); statErr != nil {
			item.Status = "missing"
			item.Detail = statErr.Error()
			results = append(results, item)
			continue
		}
		digest, hashErr := hashTree(target)
		if hashErr != nil {
			item.Status = "modified"
			item.Detail = hashErr.Error()
			results = append(results, item)
			continue
		}
		if !strings.EqualFold(digest, record.TreeSHA256) {
			item.Status = "modified"
			item.Detail = "内容与安装记录不符（可能被改动）"
			results = append(results, item)
			continue
		}
		item.Status = "ok"
		results = append(results, item)
	}
	return results, nil
}

// download 下载插件包到临时目录并返回 sha256
func (i *Installer) download(ctx context.Context, client *http.Client, entry *IndexEntry) (string, string, error) {
	if entry.URL == "" {
		return "", "", fmt.Errorf("索引条目缺少 url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, entry.URL, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("下载 %s 失败: %w", entry.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("下载 %s 返回 HTTP %d", entry.URL, resp.StatusCode)
	}
	temp, err := os.CreateTemp(filepath.Join(i.Dir, stagingDirName), "download-*.tar.gz")
	if err != nil {
		return "", "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temp, hasher), io.LimitReader(resp.Body, maxArchiveBytes))
	closeErr := temp.Close()
	if copyErr != nil {
		_ = os.Remove(temp.Name())
		return "", "", fmt.Errorf("写入插件包失败: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(temp.Name())
		return "", "", closeErr
	}
	if written == 0 {
		_ = os.Remove(temp.Name())
		return "", "", fmt.Errorf("插件包为空")
	}
	return temp.Name(), hex.EncodeToString(hasher.Sum(nil)), nil
}

// switchIn 把 staging 目录切换到插件目录，旧版本归档到 .versions/<name>/<version>
func switchIn(staging, target, previousVersion string) error {
	if _, err := os.Stat(target); err == nil {
		if previousVersion == "" {
			return fmt.Errorf("目标目录 %s 已存在但安装记录里没有版本号", target)
		}
		archive := filepath.Join(filepath.Dir(target), archivedDirName, filepath.Base(target), previousVersion)
		if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
			return fmt.Errorf("创建归档目录失败: %w", err)
		}
		_ = os.RemoveAll(archive)
		if err := moveDir(target, archive); err != nil {
			return fmt.Errorf("归档旧版本失败: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := moveDir(staging, target); err != nil {
		return fmt.Errorf("切换到新版本失败: %w", err)
	}
	return nil
}

// moveDir 移动目录：rename 优先，跨文件系统（EXDEV）时退化成拷贝 + 删除
//
// 插件目录可能被放在另一个挂载点上（数据盘 / 网络盘），rename 会失败；
// 这里兜住这种情况，代价只是多一次拷贝。
func moveDir(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyTree(src, dst); err != nil {
		_ = os.RemoveAll(dst)
		return err
	}
	return os.RemoveAll(src)
}

// copyTree 递归复制目录（只处理普通文件与目录，插件包不允许链接）
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, relative)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o755)
	})
}

// extractArchive 解包 tar.gz（拒绝绝对路径 / 跨目录 / 链接 / 设备文件）
func extractArchive(archivePath, targetDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("打开插件包失败: %w", err)
	}
	defer func() { _ = file.Close() }()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("插件包不是合法的 gzip: %w", err)
	}
	defer func() { _ = gzipReader.Close() }()

	reader := tar.NewReader(gzipReader)
	var total int64
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取插件包失败: %w", err)
		}
		name := filepath.Clean(header.Name)
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("插件包里有非法路径: %s", header.Name)
		}
		if header.Typeflag == tar.TypeSymlink || header.Typeflag == tar.TypeLink {
			return fmt.Errorf("插件包不允许包含链接: %s", header.Name)
		}
		destination := filepath.Join(targetDir, name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destination, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			total += header.Size
			if total > maxArchiveBytes {
				return fmt.Errorf("插件包解包后超过 %d 字节上限", maxArchiveBytes)
			}
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				return err
			}
			// 统一 0755：插件目录里可能有需要执行权限的二进制
			out, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, io.LimitReader(reader, header.Size)); err != nil {
				_ = out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("插件包里有不支持的文件类型: %s", header.Name)
		}
	}
	return nil
}

// hashTree 计算目录内容摘要（相对路径 + 内容哈希，排序后汇总）
func hashTree(dir string) (string, error) {
	paths := make([]string, 0, 8)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		paths = append(paths, relative)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("遍历 %s 失败: %w", dir, err)
	}
	sort.Strings(paths)

	hasher := sha256.New()
	for _, relative := range paths {
		fileHash, err := hashFile(filepath.Join(dir, relative))
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(hasher, "%s\x00%s\n", relative, fileHash)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// hashFile 单文件 sha256
func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
