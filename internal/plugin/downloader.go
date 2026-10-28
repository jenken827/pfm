package plugin

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	abi "github.com/jenken827/pfm-plugin-sdk/proto/gen/abi"
	"github.com/jenken827/pfm-plugin-sdk/proto/gen/plugin"
	"github.com/jenken827/pfm/internal/downloader"
	"github.com/jenken827/pfm/pkg/llog"
)

// downloaderCallTimeout 单次下载器 RPC 的等待上限（后端本身可能很慢，但别挂死宿主）
const downloaderCallTimeout = 15 * time.Second

// downloaderBackend kind=downloader 插件在宿主侧的代理（实现 downloader.Backend）
type downloaderBackend struct {
	cli  *client
	name string
}

var _ downloader.Backend = (*downloaderBackend)(nil)

func (b *downloaderBackend) Name() string { return b.name }

func (b *downloaderBackend) Version(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, downloaderCallTimeout)
	defer cancel()
	resp := &plugin.DownloaderVersionResp{}
	if err := b.cli.call(ctx, "Version", &plugin.DownloaderVersionReq{}, resp); err != nil {
		return "", err
	}
	return resp.GetVersion(), nil
}

func (b *downloaderBackend) AddURI(ctx context.Context, uris []string,
	opts downloader.Options) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, downloaderCallTimeout)
	defer cancel()
	req := &plugin.AddDownloadReq{Uris: uris}
	if values := optionMap(opts); len(values) > 0 {
		req.Options = values
	}
	resp := &plugin.AddDownloadResp{}
	if err := b.cli.call(ctx, "AddDownload", req, resp); err != nil {
		return "", err
	}
	if resp.GetGid() == "" {
		return "", fmt.Errorf("下载器没有返回任务 id")
	}
	return resp.GetGid(), nil
}

func (b *downloaderBackend) TellStatus(ctx context.Context, gid string) (downloader.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, downloaderCallTimeout)
	defer cancel()
	resp := &plugin.TellStatusResp{}
	if err := b.cli.call(ctx, "TellStatus", &plugin.TellStatusReq{Gid: proto.String(gid)}, resp); err != nil {
		return downloader.Task{}, err
	}
	return fromProtoTask(resp.GetTask()), nil
}

func (b *downloaderBackend) ListActive(ctx context.Context) ([]downloader.Task, error) {
	return b.list(ctx, []plugin.DownloadStatus{plugin.DownloadStatus_DOWNLOAD_STATUS_ACTIVE})
}

func (b *downloaderBackend) ListWaiting(ctx context.Context, offset, limit int) ([]downloader.Task, error) {
	return b.list(ctx, []plugin.DownloadStatus{plugin.DownloadStatus_DOWNLOAD_STATUS_WAITING})
}

func (b *downloaderBackend) ListStopped(ctx context.Context, offset, limit int) ([]downloader.Task, error) {
	return b.list(ctx, []plugin.DownloadStatus{
		plugin.DownloadStatus_DOWNLOAD_STATUS_COMPLETE,
		plugin.DownloadStatus_DOWNLOAD_STATUS_ERROR,
		plugin.DownloadStatus_DOWNLOAD_STATUS_REMOVED,
	})
}

// list 按状态拉任务。offset / limit 由后端忽略：一次取全量（对账只需要全集）。
func (b *downloaderBackend) list(ctx context.Context, states []plugin.DownloadStatus) ([]downloader.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, downloaderCallTimeout)
	defer cancel()
	resp := &plugin.ListTasksResp{}
	if err := b.cli.call(ctx, "ListTasks", &plugin.ListTasksReq{States: states}, resp); err != nil {
		return nil, err
	}
	tasks := make([]downloader.Task, 0, len(resp.GetTasks()))
	for _, item := range resp.GetTasks() {
		tasks = append(tasks, fromProtoTask(item))
	}
	return tasks, nil
}

func (b *downloaderBackend) Remove(ctx context.Context, gid string) error {
	ctx, cancel := context.WithTimeout(ctx, downloaderCallTimeout)
	defer cancel()
	return b.cli.call(ctx, "Remove", &plugin.RemoveTaskReq{Gid: proto.String(gid)}, &plugin.RemoveTaskResp{})
}

func (b *downloaderBackend) RemoveResult(ctx context.Context, gid string) error {
	ctx, cancel := context.WithTimeout(ctx, downloaderCallTimeout)
	defer cancel()
	return b.cli.call(ctx, "RemoveResult", &plugin.RemoveTaskReq{Gid: proto.String(gid)}, &plugin.RemoveTaskResp{})
}

func (b *downloaderBackend) Close() error { return nil }

// optionMap 把提交选项转成插件侧的键值对（未设置的不下发）
func optionMap(opts downloader.Options) map[string]string {
	values := make(map[string]string, 2)
	if opts.Dir != "" {
		values["dir"] = opts.Dir
	}
	if opts.Out != "" {
		values["out"] = opts.Out
	}
	return values
}

// fromProtoTask 把插件返回的任务转成宿主侧的中立结构
func fromProtoTask(task *plugin.DownloadTask) downloader.Task {
	if task == nil {
		return downloader.Task{}
	}
	out := downloader.Task{
		Gid:             task.GetGid(),
		Status:          statusFromProto(task.GetStatus()),
		Dir:             task.GetDir(),
		TotalLength:     task.GetTotalLength(),
		CompletedLength: task.GetCompletedLength(),
		DownloadSpeed:   task.GetDownloadSpeed(),
		InfoHash:        task.GetInfoHash(),
		ErrorCode:       task.GetErrorCode(),
		ErrorMessage:    task.GetErrorMessage(),
		FollowedBy:      task.GetFollowedBy(),
		TorrentName:     task.GetTorrentName(),
	}
	files := task.GetFiles()
	if len(files) > 0 {
		out.Files = make([]downloader.File, 0, len(files))
		for _, f := range files {
			uris := make([]downloader.URI, 0, len(f.GetUris()))
			for _, u := range f.GetUris() {
				uris = append(uris, downloader.URI{URI: u.GetUri(), Status: u.GetStatus()})
			}
			out.Files = append(out.Files, downloader.File{
				Path:            f.GetPath(),
				Length:          f.GetLength(),
				CompletedLength: f.GetCompletedLength(),
				Selected:        f.GetSelected(),
				URIs:            uris,
			})
		}
	}
	return out
}

// statusFromProto 枚举 → 中立字符串
func statusFromProto(s plugin.DownloadStatus) string {
	switch s {
	case plugin.DownloadStatus_DOWNLOAD_STATUS_WAITING:
		return downloader.StatusWaiting
	case plugin.DownloadStatus_DOWNLOAD_STATUS_ACTIVE:
		return downloader.StatusActive
	case plugin.DownloadStatus_DOWNLOAD_STATUS_PAUSED:
		return downloader.StatusPaused
	case plugin.DownloadStatus_DOWNLOAD_STATUS_COMPLETE:
		return downloader.StatusComplete
	case plugin.DownloadStatus_DOWNLOAD_STATUS_ERROR:
		return downloader.StatusError
	case plugin.DownloadStatus_DOWNLOAD_STATUS_REMOVED:
		return downloader.StatusRemoved
	default:
		return ""
	}
}

// loadDownloader 拉起下载器插件，配好后端连接，并（按配置）登记成宿主后端。
//
// 只有配置里 downloader.plugin 指定了它，才会接管离线下载任务；
// 其它 downloader 插件照样加载，但保持待命（多装一个不会互相抢）。
func (m *Manager) loadDownloader(ctx context.Context, dir string, mf *manifest, expectABI uint32) error {
	proc, err := startProcess(mf.execPath(dir), mf.DownloaderID, expectABI, m.opts.HandshakeTimeout)
	if err != nil {
		return err
	}
	describeCtx, cancel := context.WithTimeout(ctx, m.opts.ConfigureTimeout)
	defer cancel()
	resp := &plugin.DownloaderDescribeResp{}
	if err := proc.cli.call(describeCtx, "Describe", &plugin.DescribeReq{}, resp); err != nil {
		proc.Close()
		return fmt.Errorf("describe 失败: %w", err)
	}
	if id := resp.GetDownloaderId(); id != "" && id != mf.DownloaderID {
		proc.Close()
		return fmt.Errorf("插件声明的 downloader_id=%q 与 plugin.json 的 %q 不一致", id, mf.DownloaderID)
	}

	selected := m.opts.DownloaderID == mf.DownloaderID
	// 没被选用的后端不 Configure：Configure 的 options 是「给选中的那个后端」的参数，
	// 拿同一份参数去配别的下载器只会失败；这里只做握手 + Describe，让它出现在
	// list / verify 里，但不占后端（多个后端可以同时装着，由配置选一个）。
	var backendVersion string
	cfgResp := &plugin.DownloaderConfigureResp{}
	if selected {
		cfgReq := &plugin.DownloaderConfigureReq{
			Options: m.opts.DownloaderOptions,
			Host: &plugin.HostEndpoint{
				Addr:  proto.String(m.host.addr),
				Token: proto.String(m.host.token),
			},
		}
		if err := proc.cli.call(describeCtx, "Configure", cfgReq, cfgResp); err != nil {
			proc.Close()
			return fmt.Errorf("连接下载器后端失败: %w", err)
		}
		backendVersion = cfgResp.GetBackendVersion()
	}

	m.plugins = append(m.plugins, &loadedPlugin{
		driverID: mf.DownloaderID, manifest: mf, dir: dir, proc: proc, metadata: &abi.DriverInfo{},
	})
	llog.L().Infof("下载器插件已加载: downloader=%s name=%s version=%s backend=%s addr=%s pid=%d",
		mf.DownloaderID, mf.Name, mf.Version, backendVersion, proc.addr, proc.pid)

	if !selected {
		if m.opts.DownloaderID == "" {
			llog.L().Infof("未配置 downloader.plugin，离线下载保持关闭（插件 %s 待命）", mf.DownloaderID)
		} else {
			llog.L().Infof("未选用插件 %s（downloader.plugin=%s），只做握手不做配置",
				mf.DownloaderID, m.opts.DownloaderID)
		}
		return nil
	}
	backend := &downloaderBackend{cli: proc.cli, name: mf.DownloaderID}
	downloader.SetBackend(backend)
	m.backend = backend
	llog.L().Infof("离线下载后端已挂载: %s %s", mf.DownloaderID, backendVersion)
	return nil
}
