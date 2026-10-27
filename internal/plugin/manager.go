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

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/proto/gen/entity"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/proto/gen/plugin"
)

// Options 插件管理器配置
type Options struct {
	// Dir 插件根目录，每个子目录是一个插件（plugin.json + 可执行文件）
	Dir string
	// HandshakeTimeout 拉起插件后的握手等待上限
	HandshakeTimeout time.Duration
	// ConfigureTimeout Configure 调用的等待上限（插件可能在这里做网络登录）
	ConfigureTimeout time.Duration
}

// Manager 插件生命周期管理：扫描目录、拉起进程、注册驱动覆盖
type Manager struct {
	opts    Options
	host    *hostServer
	plugins []*loadedPlugin
}

// loadedPlugin 一个已就绪的插件
type loadedPlugin struct {
	driverID string
	manifest *manifest
	dir      string
	proc     *process
	metadata *native.DriverInfo
}

// LoadedPlugin 对外暴露的插件概况（供 pfm plugin list 展示）
type LoadedPlugin struct {
	DriverID     string
	Name         string
	Version      string
	AbiVersion   uint32
	Dir          string
	Capabilities []native.DriverCapability
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
	if len(failures) > 0 {
		return fmt.Errorf("部分插件加载失败: %s", strings.Join(failures, "; "))
	}
	return nil
}

// loadOne 拉起单个插件并注册驱动覆盖
func (m *Manager) loadOne(ctx context.Context, dir string) error {
	mf, err := loadManifest(dir)
	if err != nil {
		return err
	}
	proc, err := startProcess(mf.execPath(dir), mf.DriverID, m.opts.HandshakeTimeout)
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
		metadata = &native.DriverInfo{DriverId: proto.String(mf.DriverID)}
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
	return func(orm *entity.StorageProviderORM) (types.StorageDriver, error) {
		return m.configure(p, orm)
	}
}

// configure 在插件侧创建 provider 实例，并按返回能力组装代理
func (m *Manager) configure(p *loadedPlugin, orm *entity.StorageProviderORM) (types.StorageDriver, error) {
	if orm.Id == nil || *orm.Id == 0 {
		return nil, fmt.Errorf("provider 缺少 id")
	}
	providerID := *orm.Id
	ctx, cancel := context.WithTimeout(context.Background(), m.opts.ConfigureTimeout)
	defer cancel()
	pb, err := orm.ToPB(ctx)
	if err != nil {
		return nil, fmt.Errorf("转换 provider 失败: %w", err)
	}
	resp := &plugin.ConfigureResp{}
	req := &plugin.ConfigureReq{
		ProviderId: proto.Uint32(providerID),
		Provider:   pb,
		Host: &plugin.HostEndpoint{
			Addr:  proto.String(m.host.addr),
			Token: proto.String(m.host.token),
		},
	}
	if err := p.proc.cli.call(ctx, "Configure", req, resp); err != nil {
		return nil, fmt.Errorf("配置插件驱动 %s 失败: %w", p.driverID, err)
	}
	cfg := types.ProviderConfigFromORM(orm)
	core := &driverCore{
		cli:         p.proc.cli,
		driverID:    p.driverID,
		providerID:  providerID,
		providerCfg: &cfg,
	}
	return wrapCapabilities(core, resp.GetCapabilities()), nil
}

// Plugins 返回已加载插件概况
func (m *Manager) Plugins() []LoadedPlugin {
	out := make([]LoadedPlugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		out = append(out, LoadedPlugin{
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

// Close 结束全部插件进程并关闭反向服务
func (m *Manager) Close() {
	for _, p := range m.plugins {
		p.proc.Close()
	}
	m.plugins = nil
	if m.host != nil {
		m.host.Close()
		m.host = nil
	}
}
