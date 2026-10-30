package plugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	abi "github.com/jenken827/pfm-plugin-sdk/proto/gen/abi"
	"github.com/jenken827/pfm-plugin-sdk/proto/gen/plugin"
	"github.com/jenken827/pfm/internal/downloader"
	"github.com/jenken827/pfm/pkg/llog"
)

// Options 插件管理器配置
type Options struct {
	// Dir 插件根目录，每个子目录是一个插件（plugin.json + 可执行文件）
	Dir string
	// HandshakeTimeout 拉起插件后的握手等待上限
	HandshakeTimeout time.Duration
	// ConfigureTimeout Configure 调用的等待上限（插件可能在这里做网络登录）
	ConfigureTimeout time.Duration
	// DownloaderID 只把这个 downloader_id 的 kind=downloader 插件登记成离线下载后端；
	// 为空表示不启用离线下载（插件仍会加载，但不接管任务）
	DownloaderID string
	// DownloaderOptions 下发给下载器后端的配置（来自宿主 config.yaml 的 downloader.options）
	DownloaderOptions map[string]string
}

// Manager 插件生命周期管理：扫描目录、拉起进程、注册驱动覆盖
type Manager struct {
	opts    Options
	host    *hostServer
	plugins []*loadedPlugin
	// backend 本管理器登记成离线下载后端的插件代理（没有则为 nil）
	backend downloader.Backend
}

// loadedPlugin 一个已就绪的插件
type loadedPlugin struct {
	driverID string
	manifest *manifest
	dir      string
	proc     *process
	metadata *abi.DriverInfo
	// tool kind=tool 插件的工具代理（其它类型为 nil）
	tool *ToolPlugin
}

// LoadedPlugin 对外暴露的插件概况（供 pfm plugin list 展示）
type LoadedPlugin struct {
	Kind         string
	ID           string
	DriverID     string
	Name         string
	Version      string
	AbiVersion   uint32
	Dir          string
	Capabilities []abi.DriverCapability
}

// NewManager 创建管理器（不启动任何东西）
func NewManager(opts Options) *Manager {
	if opts.HandshakeTimeout <= 0 {
		opts.HandshakeTimeout = 10 * time.Second
	}
	if opts.ConfigureTimeout <= 0 {
		opts.ConfigureTimeout = 30 * time.Second
	}
	return &Manager{opts: opts}
}

// Load 扫描插件目录并全部拉起。
//
// 单个插件失败不影响其它插件；返回的错误汇总用来提示用户，
// 全部成功时返回 nil。
func (m *Manager) Load(ctx context.Context) error {
	if strings.TrimSpace(m.opts.Dir) == "" {
		return fmt.Errorf("插件目录未配置")
	}
	entries, err := os.ReadDir(m.opts.Dir)
	if err != nil {
		return fmt.Errorf("读取插件目录 %s 失败: %w", m.opts.Dir, err)
	}
	host, err := startHostServer()
	if err != nil {
		return err
	}
	m.host = host

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		// 跳过 .staging / .versions / .state.json 这些管理用的隐藏项
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	var failures []string
	for _, name := range names {
		dir := filepath.Join(m.opts.Dir, name)
		if loadErr := m.loadOne(ctx, dir); loadErr != nil {
			llog.L().Warnf("加载插件 %s 失败: %v", name, loadErr)
			failures = append(failures, fmt.Sprintf("%s: %v", name, loadErr))
		}
	}
	m.publishTools()
	if len(failures) > 0 {
		return fmt.Errorf("部分插件加载失败: %s", strings.Join(failures, "; "))
	}
	return nil
}

// publishTools 把本次加载到的工具插件登记给 MCP 层（部分失败也登记成功的那些）
func (m *Manager) publishTools() {
	list := make([]*ToolPlugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		if p.tool != nil {
			list = append(list, p.tool)
		}
	}
	SetToolPlugins(m, list)
}

// loadOne 拉起单个插件，并按插件类型登记它的能力
func (m *Manager) loadOne(ctx context.Context, dir string) error {
	mf, err := loadManifest(dir)
	if err != nil {
		return err
	}
	expectABI, ok := ABI(mf.Kind)
	if !ok {
		return fmt.Errorf("未知插件类型 %q", mf.Kind)
	}
	switch mf.Kind {
	case KindDriver:
		return m.loadDriver(ctx, dir, mf, expectABI)
	case KindDownloader:
		return m.loadDownloader(ctx, dir, mf, expectABI)
	case KindTool:
		return m.loadTool(ctx, dir, mf, expectABI)
	default:
		// 类型已登记但宿主还没有对应的加载分支：装上也不该被当成驱动用
		return fmt.Errorf("插件类型 %q 尚未支持加载", mf.Kind)
	}
}

// loadDriver 拉起驱动插件并注册驱动覆盖
func (m *Manager) loadDriver(ctx context.Context, dir string, mf *manifest, expectABI uint32) error {
	proc, err := startProcess(mf.execPath(dir), mf.DriverID, expectABI, m.opts.HandshakeTimeout)
	if err != nil {
		return err
	}
	describeCtx, cancel := context.WithTimeout(ctx, m.opts.ConfigureTimeout)
	defer cancel()
	resp := &plugin.DescribeResp{}
	if err := proc.cli.call(describeCtx, "Describe", &plugin.DescribeReq{}, resp); err != nil {
		proc.Close()
		return fmt.Errorf("describe 失败: %w", err)
	}
	if resp.GetDriverId() != "" && resp.GetDriverId() != mf.DriverID {
		proc.Close()
		return fmt.Errorf("插件声明的 driver_id=%q 与 plugin.json 的 %q 不一致",
			resp.GetDriverId(), mf.DriverID)
	}
	metadata := resp.GetMetadata()
	if metadata == nil {
		metadata = &abi.DriverInfo{DriverId: proto.String(mf.DriverID)}
	}
	if metadata.GetDriverId() == "" {
		metadata.DriverId = proto.String(mf.DriverID)
	}
	loaded := &loadedPlugin{driverID: mf.DriverID, manifest: mf, dir: dir, proc: proc, metadata: metadata}
	m.plugins = append(m.plugins, loaded)
	types.SetDriverOverride(m.constructor(loaded), metadata)
	llog.L().Infof("插件已加载: driver=%s name=%s version=%s addr=%s pid=%d",
		mf.DriverID, mf.Name, mf.Version, proc.addr, proc.pid)
	return nil
}

// constructor 该插件的驱动构造器：宿主每次初始化 provider 都会调用
func (m *Manager) constructor(p *loadedPlugin) types.DriverConstructor {
	return func(provider *types.Provider) (types.StorageDriver, error) {
		return m.configure(p, provider)
	}
}

// configure 在插件侧创建 provider 实例，并按返回能力组装代理
func (m *Manager) configure(p *loadedPlugin, provider *types.Provider) (types.StorageDriver, error) {
	if provider == nil || provider.ID == 0 {
		return nil, fmt.Errorf("provider 缺少 id")
	}
	providerID := provider.ID
	ctx, cancel := context.WithTimeout(context.Background(), m.opts.ConfigureTimeout)
	defer cancel()

	resp := &plugin.ConfigureResp{}
	req := &plugin.ConfigureReq{
		ProviderId: proto.Uint32(providerID),
		Provider:   providerPB(provider),
		Host: &plugin.HostEndpoint{
			Addr:  proto.String(m.host.addr),
			Token: proto.String(m.host.token),
		},
	}
	if err := p.proc.cli.call(ctx, "Configure", req, resp); err != nil {
		return nil, fmt.Errorf("配置插件驱动 %s 失败: %w", p.driverID, err)
	}
	cfg := provider.Config
	core := &driverCore{
		cli:         p.proc.cli,
		driverID:    p.driverID,
		providerID:  providerID,
		providerCfg: &cfg,
	}
	return wrapCapabilities(core, resp.GetCapabilities()), nil
}

// providerPB 把驱动视角的 Provider 转成插件协议里的 provider 消息
//
// 只发送驱动需要/宿主已算好的字段：驱动私有配置原样透传，其余是提供者级的
// 通用配置视图（含 proxy——网络类驱动要按它设 transport）。数据库实体的时间戳、
// 容量、统计等与驱动无关，不发。
func providerPB(provider *types.Provider) *abi.Provider {
	return &abi.Provider{
		Id:                   proto.Uint32(provider.ID),
		DriverId:             proto.String(provider.DriverID),
		Name:                 proto.String(provider.Name),
		Config:               proto.String(provider.ConfigJSON),
		Enabled:              proto.Bool(provider.Config.Enabled),
		SortOrder:            proto.Uint32(provider.Config.SortOrder),
		FileSizeLimit:        proto.Uint64(provider.Config.FileSizeLimit),
		MaxConcurrentUploads: proto.Uint32(provider.Config.MaxConcurrentUploads),
		HashType:             provider.Config.HashType.Enum(),
		EncryptionKey:        proto.String(provider.Config.EncryptionKey),
		EncryptionSuffix:     proto.String(provider.Config.EncryptionSuffix),
		Proxy:                proto.String(provider.Proxy),
	}
}

// DriverIDs 返回本管理器当前加载的驱动身份（去重、按加载顺序）。
//
// 热加载时用前后两次的差集判断：哪些插件被删掉（要撤销驱动覆盖）、
// 哪些 provider 实例需要重建。
func (m *Manager) DriverIDs() []string {
	seen := make(map[string]struct{}, len(m.plugins))
	ids := make([]string, 0, len(m.plugins))
	for _, p := range m.plugins {
		if p.driverID == "" {
			continue
		}
		if _, ok := seen[p.driverID]; ok {
			continue
		}
		seen[p.driverID] = struct{}{}
		ids = append(ids, p.driverID)
	}
	return ids
}

// Plugins 返回已加载插件概况
func (m *Manager) Plugins() []LoadedPlugin {
	out := make([]LoadedPlugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		out = append(out, LoadedPlugin{
			Kind:         p.manifest.Kind,
			ID:           p.manifest.ID(),
			DriverID:     p.driverID,
			Name:         p.manifest.Name,
			Version:      p.manifest.Version,
			AbiVersion:   p.manifest.AbiVersion,
			Dir:          p.dir,
			Capabilities: p.metadata.GetCapabilities(),
		})
	}
	return out
}

// Backend 本管理器登记的离线下载后端（没有则为 nil）
func (m *Manager) Backend() downloader.Backend { return m.backend }

// Close 结束全部插件进程并关闭反向服务
func (m *Manager) Close() {
	if m.backend != nil {
		// 新管理器已经登记过新后端时这里是空操作（ClearBackend 只清自己）
		downloader.ClearBackend(m.backend)
		m.backend = nil
	}
	for _, p := range m.plugins {
		p.proc.Close()
	}
	m.plugins = nil
	ClearToolPlugins(m)
	if m.host != nil {
		m.host.Close()
		m.host = nil
	}
}
