package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	abi "github.com/jenken827/pfm-plugin-sdk/proto/gen/abi"
	"github.com/jenken827/pfm-plugin-sdk/proto/gen/plugin"
)

// closeTimeout 通知插件释放实例的等待上限
const closeTimeout = 5 * time.Second

// streamWindow 单条 Open 流的有效窗口（超过就按新 offset 重开）
const streamWindow = 4 << 20

// driverCore 插件驱动在宿主侧的代理（只实现 StorageDriver + Closer）
type driverCore struct {
	cli         *client
	driverID    string
	providerID  uint32
	providerCfg *types.ProviderConfig
}

func (d *driverCore) GetType() string                          { return d.driverID }
func (d *driverCore) GetName() string                          { return d.driverID }
func (d *driverCore) GetConfig() any                           { return nil }
func (d *driverCore) GetProviderConfig() *types.ProviderConfig { return d.providerCfg }

// Close 通知插件释放该 provider 的实例（进程由管理器统一回收）
func (d *driverCore) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	if err := d.cli.call(ctx, "Close", &plugin.CloseReq{ProviderId: proto.Uint32(d.providerID)}, nil); err != nil {
		// 进程已退出是正常情况，不打扰上层
		if !errors.Is(err, io.EOF) {
			return
		}
	}
}

// Upload 上传：源文件走宿主临时目录（同机文件，不经过 RPC）
func (d *driverCore) Upload(ctx context.Context, src types.UploadSource,
	opt types.UploadOptions) ([]types.FileRef, error) {
	req := &plugin.UploadReq{
		ProviderId:       proto.Uint32(d.providerID),
		TempPath:         proto.String(src.TempPath),
		FileName:         proto.String(src.FileName),
		FileSize:         proto.Uint64(src.FileSize),
		FileHash:         proto.String(src.FileHash),
		Obfuscate:        proto.Bool(opt.ObfuscateName),
		EncryptionSuffix: proto.String(opt.EncryptionSuffix),
		Encryption:       encryptionOf(opt.Encryptor),
	}
	resp := &plugin.UploadResp{}
	if err := d.cli.call(ctx, "Upload", req, resp); err != nil {
		return nil, err
	}
	return fromProtoRefs(resp.GetRefs()), nil
}

// Open 打开文件：返回一条可按 offset 重开的流
func (d *driverCore) Open(ctx context.Context, refs []types.FileRef,
	opt types.OpenOptions) (types.ReadSeekCloser, error) {
	total := int64(0)
	for _, ref := range refs {
		total += int64(ref.Size)
	}
	reader := &streamReader{
		cli:        d.cli,
		providerID: d.providerID,
		refs:       toProtoRefs(refs),
		encryption: encryptionOf(opt.Encryptor),
		total:      total,
	}
	// 先建立连接，让"文件不存在/鉴权失效"在 Open 阶段就暴露给调用方
	if err := reader.reopen(ctx, 0); err != nil {
		return nil, err
	}
	return reader, nil
}

// streamReader 把插件的 Open 字节流转成 ReadSeekCloser。
//
// Seek 语义：目标落在当前流已读范围内就直接跳过；往回或跳远则按新 offset
// 重开一条流（插件侧会重新 Seek 到该位置，代价是一次 HTTP 建连）。
type streamReader struct {
	cli        *client
	providerID uint32
	refs       []*plugin.FileRef
	encryption *plugin.Encryption
	total      int64

	pos      int64
	cur      io.ReadCloser
	streamAt int64 // 当前流起点
}

func (r *streamReader) Read(p []byte) (int, error) {
	if r.cur == nil {
		if err := r.reopen(context.Background(), r.pos); err != nil {
			return 0, err
		}
	}
	n, err := r.cur.Read(p)
	r.pos += int64(n)
	if err != nil {
		_ = r.cur.Close()
		r.cur = nil
	}
	return n, err
}

func (r *streamReader) Seek(offset int64, whence int) (int64, error) {
	var target int64
	switch whence {
	case io.SeekStart:
		target = offset
	case io.SeekCurrent:
		target = r.pos + offset
	case io.SeekEnd:
		// 总长来自 refs 的 size 之和（加密后长度，与驱动读取口径一致）
		target = r.total + offset
	default:
		return 0, fmt.Errorf("不支持的 whence: %d", whence)
	}
	if target < 0 {
		return 0, fmt.Errorf("非法的负偏移 %d", target)
	}
	if target == r.pos {
		return target, nil
	}
	// 当前流只能向前跳，且跳得太远不如重开
	if r.cur != nil && target > r.pos && target-r.streamAt <= streamWindow {
		if _, err := io.CopyN(io.Discard, r.cur, target-r.pos); err != nil {
			return 0, err
		}
		r.pos = target
		return target, nil
	}
	r.closeStream()
	r.pos = target
	return target, nil
}

func (r *streamReader) Close() error {
	r.closeStream()
	return nil
}

func (r *streamReader) closeStream() {
	if r.cur != nil {
		_ = r.cur.Close()
		r.cur = nil
	}
}

// reopen 按指定 offset 建一条新流
func (r *streamReader) reopen(ctx context.Context, offset int64) error {
	r.closeStream()
	req := newOpenReq(r.providerID, r.refs, offset, 0, r.encryption)
	stream, err := r.cli.open(ctx, req)
	if err != nil {
		return err
	}
	r.cur = stream
	r.streamAt = offset
	r.pos = offset
	return nil
}

// —— 可选能力：mixin + 定长组合 ——
//
// 宿主侧靠接口断言判断能力（types.BatchDeleter 等），所以代理必须是"恰好实现
// 已声明能力"的类型。做法：每个能力一个只加一个方法的 mixin，再用 16 个
// 具名组合类型把 mixin 摆上去（mixin 不嵌入 StorageDriver，避免方法歧义）。

// batchMixin 批量删除
type batchMixin struct{ core *driverCore }

func (m *batchMixin) BatchDelete(ctx context.Context, refs []types.FileRef) ([]types.DeleteResult, error) {
	return m.core.batchDelete(ctx, refs)
}

// directLinkMixin 直链
type directLinkMixin struct{ core *driverCore }

func (m *directLinkMixin) DirectLink(ctx context.Context, ref types.FileRef) (*types.DirectLink, error) {
	return m.core.directLink(ctx, ref)
}

// remoteListMixin 远端枚举
type remoteListMixin struct{ core *driverCore }

func (m *remoteListMixin) RemoteRoot() string { return m.core.remoteRoot() }

func (m *remoteListMixin) ListRemote(ctx context.Context, dir string) ([]types.RemoteEntry, error) {
	return m.core.listRemote(ctx, dir)
}

// preDownloadMixin 预下载
type preDownloadMixin struct{ core *driverCore }

func (m *preDownloadMixin) PreDownload(ctx context.Context, refs []types.FileRef) error {
	return m.core.preDownload(ctx, refs)
}

// 能力位（与组合类型一一对应）
const (
	capBitBatch = 1 << iota
	capBitDirect
	capBitRemote
	capBitPreDownload
)

type driverCaps0 struct{ *driverCore }
type driverCaps1 struct {
	*driverCore
	batchMixin
}
type driverCaps2 struct {
	*driverCore
	directLinkMixin
}
type driverCaps3 struct {
	*driverCore
	batchMixin
	directLinkMixin
}
type driverCaps4 struct {
	*driverCore
	remoteListMixin
}
type driverCaps5 struct {
	*driverCore
	batchMixin
	remoteListMixin
}
type driverCaps6 struct {
	*driverCore
	directLinkMixin
	remoteListMixin
}
type driverCaps7 struct {
	*driverCore
	batchMixin
	directLinkMixin
	remoteListMixin
}
type driverCaps8 struct {
	*driverCore
	preDownloadMixin
}
type driverCaps9 struct {
	*driverCore
	batchMixin
	preDownloadMixin
}
type driverCaps10 struct {
	*driverCore
	directLinkMixin
	preDownloadMixin
}
type driverCaps11 struct {
	*driverCore
	batchMixin
	directLinkMixin
	preDownloadMixin
}
type driverCaps12 struct {
	*driverCore
	remoteListMixin
	preDownloadMixin
}
type driverCaps13 struct {
	*driverCore
	batchMixin
	remoteListMixin
	preDownloadMixin
}
type driverCaps14 struct {
	*driverCore
	directLinkMixin
	remoteListMixin
	preDownloadMixin
}
type driverCaps15 struct {
	*driverCore
	batchMixin
	directLinkMixin
	remoteListMixin
	preDownloadMixin
}

// wrapCapabilities 按能力组合出代理对象
func wrapCapabilities(core *driverCore, caps []abi.DriverCapability) types.StorageDriver {
	mask := 0
	for _, capability := range caps {
		switch capability {
		case abi.DriverCapability_DRIVER_CAPABILITY_BATCH_DELETE:
			mask |= capBitBatch
		case abi.DriverCapability_DRIVER_CAPABILITY_DIRECT_LINK:
			mask |= capBitDirect
		case abi.DriverCapability_DRIVER_CAPABILITY_REMOTE_LIST:
			mask |= capBitRemote
		case abi.DriverCapability_DRIVER_CAPABILITY_PREDOWNLOAD:
			mask |= capBitPreDownload
		}
	}
	batch := batchMixin{core: core}
	direct := directLinkMixin{core: core}
	remote := remoteListMixin{core: core}
	pre := preDownloadMixin{core: core}
	switch mask {
	case 0:
		return &driverCaps0{core}
	case 1:
		return &driverCaps1{core, batch}
	case 2:
		return &driverCaps2{core, direct}
	case 3:
		return &driverCaps3{core, batch, direct}
	case 4:
		return &driverCaps4{core, remote}
	case 5:
		return &driverCaps5{core, batch, remote}
	case 6:
		return &driverCaps6{core, direct, remote}
	case 7:
		return &driverCaps7{core, batch, direct, remote}
	case 8:
		return &driverCaps8{core, pre}
	case 9:
		return &driverCaps9{core, batch, pre}
	case 10:
		return &driverCaps10{core, direct, pre}
	case 11:
		return &driverCaps11{core, batch, direct, pre}
	case 12:
		return &driverCaps12{core, remote, pre}
	case 13:
		return &driverCaps13{core, batch, remote, pre}
	case 14:
		return &driverCaps14{core, direct, remote, pre}
	default:
		return &driverCaps15{core, batch, direct, remote, pre}
	}
}

// —— 可选能力的具体实现（由 mixin 转发到这里）——

func (d *driverCore) batchDelete(ctx context.Context, refs []types.FileRef) ([]types.DeleteResult, error) {
	resp := &plugin.BatchDeleteResp{}
	req := &plugin.BatchDeleteReq{ProviderId: proto.Uint32(d.providerID), Refs: toProtoRefs(refs)}
	if err := d.cli.call(ctx, "BatchDelete", req, resp); err != nil {
		return nil, err
	}
	results := make([]types.DeleteResult, 0, len(resp.GetResults()))
	for _, item := range resp.GetResults() {
		result := types.DeleteResult{
			ID:        item.GetLocationId(),
			Success:   item.GetSuccess(),
			Permanent: item.GetPermanent(),
		}
		if item.GetError() != "" {
			result.Error = errors.New(item.GetError())
			if result.Permanent {
				// 统一成哨兵错误，宿主一律用 types.IsPermanent 判断
				result.Error = fmt.Errorf("%w: %v", types.ErrPermanent, result.Error)
			}
		}
		results = append(results, result)
	}
	return results, nil
}

func (d *driverCore) directLink(ctx context.Context, ref types.FileRef) (*types.DirectLink, error) {
	resp := &plugin.DirectLinkResp{}
	req := &plugin.DirectLinkReq{ProviderId: proto.Uint32(d.providerID), Ref: toProtoRef(ref)}
	if err := d.cli.call(ctx, "DirectLink", req, resp); err != nil {
		return nil, err
	}
	link := &types.DirectLink{URL: resp.GetUrl(), Headers: resp.GetHeaders(), SupportsRange: resp.GetSupportsRange()}
	if resp.GetExpiresAt() != nil {
		link.ExpiresAt = resp.GetExpiresAt().AsTime()
	}
	return link, nil
}

func (d *driverCore) remoteRoot() string {
	resp := &plugin.RemoteRootResp{}
	req := &plugin.RemoteRootReq{ProviderId: proto.Uint32(d.providerID)}
	if err := d.cli.call(context.Background(), "RemoteRoot", req, resp); err != nil {
		return ""
	}
	return resp.GetPath()
}

func (d *driverCore) listRemote(ctx context.Context, dir string) ([]types.RemoteEntry, error) {
	resp := &plugin.ListRemoteResp{}
	req := &plugin.ListRemoteReq{ProviderId: proto.Uint32(d.providerID), Dir: proto.String(dir)}
	if err := d.cli.call(ctx, "ListRemote", req, resp); err != nil {
		return nil, err
	}
	entries := make([]types.RemoteEntry, 0, len(resp.GetEntries()))
	for _, entry := range resp.GetEntries() {
		entries = append(entries, types.RemoteEntry{
			Path:  entry.GetPath(),
			Name:  entry.GetName(),
			ID:    entry.GetId(),
			IsDir: entry.GetIsDir(),
			Size:  entry.GetSize(),
		})
	}
	return entries, nil
}

func (d *driverCore) preDownload(ctx context.Context, refs []types.FileRef) error {
	req := &plugin.PreDownloadReq{ProviderId: proto.Uint32(d.providerID), Refs: toProtoRefs(refs)}
	return d.cli.call(ctx, "PreDownload", req, nil)
}

// encryptionOf 把宿主加密器转成下发参数（只需要 key+salt，插件侧可重建同一个加密器）
func encryptionOf(enc types.Encryptor) *plugin.Encryption {
	if enc == nil {
		return nil
	}
	key, salt, ok := types.EncryptorParams(enc)
	if !ok {
		return nil
	}
	return &plugin.Encryption{Key: proto.String(key), Salt: proto.String(salt)}
}
