package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"google.golang.org/protobuf/types/known/timestamppb"
	ormconv "jenken827.com/jenken827/pfm/driver/orm"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/pkg/llog"
)

// 本文件实现"直链下载"的服务端判定与下发。
//
// 直链 = 存储提供者签发的、自带时效与鉴权的下载地址。满足全部硬性条件的文件，
// 客户端可以绕开服务端中转直接下载（服务端上行带宽是当前瓶颈）。
//
// 硬性条件（任一不满足即返回 DIRECT_LINK_UNAVAILABLE，客户端静默回退代理下载）：
//  1. 该提供者上恰好 1 条完整位置行（多分片是多个物理对象，一个 URL 表达不了拼接）
//  2. 位置行未加密（密文必须由持密钥的服务端解密，密钥不下发）
//  3. 驱动实现 types.DirectLinker（没实现 = 不支持直链，回落代理下载）
//  4. 提供者类型在配置白名单内（transfer.direct_link.providers，留空 = 不限制）
//
// ⚠ 直链是临时凭证：不要写日志、不要落库、不要进错误信息。

// GetFileDirectLink 取文件直链
func GetFileDirectLink(ctx context.Context, req *native.DirectLinkFileReq) (*native.DirectLinkFileRespData, *models.Err) {
	fileID := strValueOrEmpty(req.FileId)
	if fileID == "" {
		return nil, &models.Err{E: errors.New("file_id is required"), C: native.RespCode_PARAMS_ERROR}
	}
	if !directLinkEnabled() {
		return nil, directLinkUnavailable("直链下载已关闭（transfer.direct_link.enabled）")
	}
	if !CheckOwnership(ctx, fileID) {
		return nil, &models.Err{E: errors.New("permission denied: not owner of file"), C: native.RespCode_PERMISSION_DENIED}
	}

	file, locations, mErr := loadDirectLinkLocations(fileID)
	if mErr != nil {
		return nil, mErr
	}
	providerID, providerLocations, mErr := selectDirectLinkProvider(locations, req.ProviderId)
	if mErr != nil {
		return nil, mErr
	}
	if reason := directLinkIneligibleReason(providerLocations); reason != "" {
		return nil, directLinkUnavailable(reason)
	}
	if !directLinkProviderAllowed(providerID) {
		return nil, directLinkUnavailable(fmt.Sprintf("提供者 %d 不在直链白名单内", providerID))
	}

	linker, ok := storage.GetStorage(providerID).(types.DirectLinker)
	if !ok {
		return nil, directLinkUnavailable(fmt.Sprintf("提供者 %d 的驱动不支持直链", providerID))
	}
	link, err := linker.DirectLink(ctx, ormconv.FileRefsFromLocations(providerLocations)[0])
	if err != nil {
		// 底层错误可能带 URL 等凭证，只记服务端日志，不回给客户端
		llog.C(ctx).Errorf("get direct link failed, file_id: %s, provider: %d, err: %v", fileID, providerID, err)
		return nil, directLinkUnavailable("取直链失败，请改用代理下载")
	}

	// 发链之后服务端不再感知这次下载，访问统计只能在这里记
	UpdateFileAccess(fileID)
	return buildDirectLinkResp(file, providerID, link), nil
}

// loadDirectLinkLocations 读文件与全部位置行
func loadDirectLinkLocations(fileID string) (*entity.FileORM, []*entity.FileLocationORM, *models.Err) {
	file, err := db.GetFileByID(fileID)
	if err != nil {
		return nil, nil, &models.Err{E: fmt.Errorf("file not found: %w", err), C: native.RespCode_NOT_FOUND}
	}
	if file.Hash == nil || *file.Hash == "" {
		return nil, nil, directLinkUnavailable("文件 hash 为空，无法定位物理副本")
	}
	locations, err := db.GetFileLocationsByBlake3Hash(*file.Hash)
	if err != nil {
		return nil, nil, &models.Err{E: fmt.Errorf("file location not found: %w", err), C: native.RespCode_DB_ERROR}
	}
	if len(locations) == 0 {
		return nil, nil, directLinkUnavailable("文件没有任何物理副本")
	}
	return file, locations, nil
}

// selectDirectLinkProvider 选源：显式指定则只用它；否则按配置决定是否优先挑可直链的提供者
func selectDirectLinkProvider(locations []*entity.FileLocationORM,
	requested *uint32) (uint32, []*entity.FileLocationORM, *models.Err) {
	if requested != nil {
		locs, err := storage.GetCompleteProviderLocations(locations, *requested)
		if err != nil {
			return 0, nil, directLinkUnavailable(fmt.Sprintf("提供者 %d 没有完整副本", *requested))
		}
		return *requested, locs, nil
	}

	if directLinkPreferDirectable() {
		if pid, locs, ok := firstDirectableProvider(locations); ok {
			return pid, locs, nil
		}
	}

	providerID, locs, err := storage.SelectBestSourceProvider(locations)
	if err != nil {
		// 选不到可用提供者（副本不完整 / 提供者未启用）：对客户端而言同样是"直链不可用"，
		// 动作都是回退代理下载，所以不额外区分业务码
		llog.L().Debugf("direct link: no available provider, file locations: %d, err: %v", len(locations), err)
		return 0, nil, directLinkUnavailable("没有可用于直链的提供者，请改用代理下载")
	}
	return providerID, locs, nil
}

// firstDirectableProvider 按既有选源优先级找第一个"能直链"的提供者（prefer_directable_provider）
func firstDirectableProvider(locations []*entity.FileLocationORM) (uint32, []*entity.FileLocationORM, bool) {
	ordered, err := storage.OrderedCompleteProviders(locations)
	if err != nil {
		return 0, nil, false
	}
	for _, pid := range ordered {
		locs, err := storage.GetCompleteProviderLocations(locations, pid)
		if err != nil {
			continue
		}
		if directLinkIneligibleReason(locs) != "" || !directLinkProviderAllowed(pid) {
			continue
		}
		if _, ok := storage.GetStorage(pid).(types.DirectLinker); !ok {
			continue
		}
		return pid, locs, true
	}
	return 0, nil, false
}

// directLinkIneligibleReason 资格判定：返回空串表示可直链
func directLinkIneligibleReason(locations []*entity.FileLocationORM) string {
	if len(locations) != 1 {
		return fmt.Sprintf("多分片（%d 个物理对象），一个 URL 无法表达拼接", len(locations))
	}
	if locations[0].Encrypted != nil && *locations[0].Encrypted {
		return "内容已加密，必须由服务端解密后下发"
	}
	return ""
}

// buildDirectLinkResp 组装响应（fallback_url 保证任何客户端都有兜底）
func buildDirectLinkResp(file *entity.FileORM, providerID uint32,
	link *types.DirectLink) *native.DirectLinkFileRespData {
	resp := &native.DirectLinkFileRespData{
		Url:        &link.URL,
		Headers:    link.Headers,
		ProviderId: &providerID,
		FileName:   file.Name,
		FileSize:   file.Size,
		FallbackUrl: stringPtr(fmt.Sprintf("/api/file/stream/download?file_id=%s&provider_id=%d",
			strValueOrEmpty(file.Id), providerID)),
	}
	supportsRange := link.SupportsRange
	resp.SupportsRange = &supportsRange
	if !link.ExpiresAt.IsZero() {
		resp.ExpiresAt = timestamppb.New(link.ExpiresAt)
	}
	if sp, err := db.GetProviderByID(providerID); err == nil && sp.DriverId != nil {
		resp.DriverId = sp.DriverId
	}
	return resp
}

// directLinkEnabled 直链总开关（默认开）
func directLinkEnabled() bool {
	enabled := config.GetConfig().Transfer.DirectLink.Enabled
	return enabled == nil || *enabled
}

// directLinkPreferDirectable 是否优先挑可直链的提供者（默认关）
func directLinkPreferDirectable() bool {
	return config.GetConfig().Transfer.DirectLink.PreferDirectableProvider
}

// directLinkProviderAllowed 提供者类型是否在白名单内（白名单为空 = 不限制）
func directLinkProviderAllowed(providerID uint32) bool {
	whitelist := config.GetConfig().Transfer.DirectLink.Providers
	if len(whitelist) == 0 {
		return true
	}
	sp, err := db.GetProviderByID(providerID)
	if err != nil || sp.DriverId == nil {
		return false
	}
	driverID := *sp.DriverId
	for _, allowed := range whitelist {
		// 白名单里写驱动身份（storage_providers.provider_type 的值）；大小写与首尾
		// 空白容忍一下，别的一律不猜（不做"旧枚举名"之类的模糊匹配）。
		if strings.EqualFold(strings.TrimSpace(allowed), driverID) {
			return true
		}
	}
	return false
}

// directLinkUnavailable 构造"直链不可用"业务错误（客户端据此静默回退代理下载）
func directLinkUnavailable(message string) *models.Err {
	return &models.Err{E: errors.New(message), C: native.RespCode_DIRECT_LINK_UNAVAILABLE}
}

// strValueOrEmpty 取字符串指针值，nil 返回空串
func strValueOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// stringPtr 取地址（proto optional 字段需要指针）
func stringPtr(s string) *string {
	return &s
}
