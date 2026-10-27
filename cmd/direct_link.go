package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	_ "jenken827.com/jenken827/pfm/driver" // 注册存储驱动构造器（取直链需要）
	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// 本文件是"直链下载"的**打表工具**：把某个文件在各存储提供者上的直链取出来，
// 可选实测 Range 支持、真实文件名与必需请求头。
//
// 它只调用驱动的 types.DirectLinker 能力，不经过 service/api —— 服务端直链接口
// 的资格判定（单分片 / 未加密 / provider 白名单）尚未实现，本工具的输出用于验证
// 各提供者的直链是否真的能被客户端使用（见 DESIGN.md §17）。
//
// ⚠ 输出里含临时下载凭证（URL 自带鉴权）：不要贴进 issue / 日志 / 提交。

// directLinkOptions direct-link 子命令参数
type directLinkOptions struct {
	fileID     string
	providerID uint32
	listLimit  uint32
	probe      bool
	noHeaders  bool
	jsonOut    bool
	timeout    time.Duration
}

// directLinkRow 单个提供者的取链结果（--json 直接序列化本结构）
type directLinkRow struct {
	ProviderID       uint32            `json:"provider_id"`
	ProviderName     string            `json:"provider_name"`
	DriverId         string            `json:"driver_id"`
	Chunks           int               `json:"chunks"`
	Encrypted        bool              `json:"encrypted"`
	Eligible         bool              `json:"eligible"`
	IneligibleReason string            `json:"ineligible_reason,omitempty"`
	URL              string            `json:"url,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"`
	ExpiresAt        string            `json:"expires_at,omitempty"`
	RangeHint        bool              `json:"range_hint"`
	Probes           []directLinkProbe `json:"probes,omitempty"`
	Error            string            `json:"error,omitempty"`
}

// directLinkProbe 一次 HTTP 实测的结果
type directLinkProbe struct {
	RangeHeader     string `json:"range_header"`
	Status          int    `json:"status"`
	ElapsedMS       int64  `json:"elapsed_ms"`
	SupportsRange   bool   `json:"supports_range"`
	TotalSize       int64  `json:"total_size,omitempty"`
	ContentLength   string `json:"content_length,omitempty"`
	ContentRange    string `json:"content_range,omitempty"`
	AcceptRanges    string `json:"accept_ranges,omitempty"`
	ContentType     string `json:"content_type,omitempty"`
	ContentDisp     string `json:"content_disposition,omitempty"`
	FinalURL        string `json:"final_url,omitempty"`
	DeclaredHeaders bool   `json:"declared_headers_sent"`
	Error           string `json:"error,omitempty"`
}

var directLinkCmd = &cobra.Command{
	Use:   "direct-link",
	Short: "取文件在存储提供者上的下载直链（打表/验证用，不经过服务端中转）",
	Long: `取指定文件在各存储提供者上的**直链**，用于验证「非加密、单分片文件绕过服务端中转」
是否可行（设计见 DESIGN.md §17）。

默认遍历该文件所有完整副本的提供者，逐行打印：直链、客户端必需请求头、驱动声明的有效期与
Range 支持、是否满足直链条件（单分片 + 未加密）。

加 --probe 会真的发 HTTP 请求实测（只取 Range 的少量字节，不会整文件下载）：
  - Range: bytes=0-0   → 206 说明支持 Range（可多线程/续传），200 说明服务端忽略 Range 全量返回
  - Range: bytes=1024-1025 → 复核中间偏移
并打印 Content-Disposition（提供者给出的文件名——直链无法由 pfm 指定文件名）、
Content-Range 总长度、Accept-Ranges 等响应头。

加 --no-headers 复现"浏览器直接打开"（不带任何自定义请求头），用于判断哪些提供者的直链
浏览器可用、哪些必须由能设头的客户端（App / aria2）使用。

⚠ 输出包含临时下载凭证，请勿粘贴到公开场合。
⚠ 本机测到的结果只代表**服务端出口 IP**；判断链接是否绑定请求 IP，需把 URL 拷到客户端网络再试。

示例:
  pfm -d ./data direct-link --list 20                     # 列最近 20 个文件，挑一个
  pfm -d ./data direct-link -f <file_id>                    # 列表 + 取链
  pfm -d ./data direct-link -f <file_id> --probe            # 追加 Range/文件名实测
  pfm -d ./data direct-link -f <file_id> --probe --no-headers   # 模拟浏览器
  pfm -d ./data direct-link -f <file_id> --provider 3 --json    # 单提供者、JSON 输出
`,
	Args: cobra.NoArgs,
	RunE: runDirectLink,
}

func runDirectLink(cmd *cobra.Command, _ []string) error {
	opts := directLinkOptions{}
	var err error
	opts.fileID, _ = cmd.Flags().GetString("file")
	pid, _ := cmd.Flags().GetUint32("provider")
	opts.providerID = pid
	opts.listLimit, _ = cmd.Flags().GetUint32("list")
	opts.probe, _ = cmd.Flags().GetBool("probe")
	opts.noHeaders, _ = cmd.Flags().GetBool("no-headers")
	opts.jsonOut, _ = cmd.Flags().GetBool("json")
	opts.timeout, _ = cmd.Flags().GetDuration("timeout")
	if opts.fileID == "" && opts.listLimit == 0 {
		return fmt.Errorf("必须用 -f/--file 指定文件 ID；不知道文件 ID 时先用 --list 20 挑一个")
	}

	if err := globals.ValidateConfig(); err != nil {
		return err
	}
	globals.NormalizePath()
	config.Init()
	llog.Init()
	db.Init()
	storage.InitProviders() // 只建驱动，不触发上传恢复等后台任务

	if opts.listLimit > 0 {
		return listDirectLinkCandidates(context.Background(), opts.listLimit, opts.jsonOut)
	}

	file, locations, err := loadDirectLinkTarget(opts.fileID)
	if err != nil {
		return err
	}

	providerIDs, err := resolveDirectLinkProviders(locations, opts.providerID)
	if err != nil {
		return err
	}
	if len(providerIDs) == 0 {
		return fmt.Errorf("文件 %s 没有任何完整副本的提供者", opts.fileID)
	}

	ctx := context.Background()
	rows := make([]directLinkRow, 0, len(providerIDs))
	for _, pid := range providerIDs {
		locs, err := storage.GetCompleteProviderLocations(locations, pid)
		if err != nil {
			rows = append(rows, directLinkRow{ProviderID: pid, Error: err.Error()})
			continue
		}
		rows = append(rows, buildDirectLinkRow(ctx, pid, locs, &opts))
	}

	if opts.jsonOut {
		out, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}
	printDirectLinkText(file, rows)
	return nil
}

// loadDirectLinkTarget 读文件元数据与全部位置行
func loadDirectLinkTarget(fileID string) (*native.FileORM, []*native.FileLocationORM, error) {
	file, err := db.GetFileByID(fileID)
	if err != nil {
		return nil, nil, fmt.Errorf("查询文件失败: %w", err)
	}
	if file.Hash == nil || *file.Hash == "" {
		return nil, nil, fmt.Errorf("文件 %s 的 hash 为空，无法定位物理副本", fileID)
	}
	locations, err := db.GetFileLocationsByBlake3Hash(*file.Hash)
	if err != nil {
		return nil, nil, fmt.Errorf("查询文件位置失败: %w", err)
	}
	if len(locations) == 0 {
		return nil, nil, fmt.Errorf("文件 %s 没有 file_locations 行", fileID)
	}
	return file, locations, nil
}

// resolveDirectLinkProviders 决定要对哪些提供者取链：显式指定则只取它，否则取全部完整副本
func resolveDirectLinkProviders(locations []*native.FileLocationORM, providerID uint32) ([]uint32, error) {
	if providerID != 0 {
		return []uint32{providerID}, nil
	}
	ids, err := storage.GetCompleteProviders(locations)
	if err != nil {
		return nil, err
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

// buildDirectLinkRow 取单个提供者的直链并（可选）实测
//
// 资格判定与服务端将采用的口径一致：单分片 + 未加密 + 驱动实现 DirectLinker。
// 不合格时**仍然取链**，只是标记出来——本工具是打表用的，需要看到真实行为。
func buildDirectLinkRow(ctx context.Context, providerID uint32,
	locs []*native.FileLocationORM, opts *directLinkOptions) directLinkRow {
	row := directLinkRow{ProviderID: providerID, Chunks: len(locs)}
	if sp, err := db.GetProviderByID(providerID); err == nil {
		if sp.Name != nil {
			row.ProviderName = *sp.Name
		}
		if sp.DriverId != nil {
			row.DriverId = *sp.DriverId
		}
	}
	for _, loc := range locs {
		if loc.Encrypted != nil && *loc.Encrypted {
			row.Encrypted = true
		}
	}

	drv := storage.GetStorage(providerID)
	if drv == nil {
		row.Error = "存储实例不存在（提供者未启用或初始化失败）"
		return row
	}
	linker, ok := drv.(types.DirectLinker)
	if !ok {
		row.IneligibleReason = fmt.Sprintf("%s 驱动未声明 DIRECT_LINK 能力（如 telegram/local）", row.DriverId)
		return row
	}

	link, err := linker.DirectLink(ctx, types.FileRefsFromLocations(locs)[0])
	if err != nil {
		row.Error = err.Error()
		return row
	}
	row.URL = link.URL
	row.Headers = link.Headers
	row.RangeHint = link.SupportsRange
	if !link.ExpiresAt.IsZero() {
		row.ExpiresAt = link.ExpiresAt.Format(time.RFC3339)
	}

	row.Eligible, row.IneligibleReason = directLinkEligibility(row.Chunks, row.Encrypted)
	if opts.probe && row.URL != "" {
		row.Probes = runDirectLinkProbes(ctx, link, opts)
	}
	return row
}

// directLinkEligibility 直链的硬性资格判定（与服务端口径一致）
func directLinkEligibility(chunks int, encrypted bool) (bool, string) {
	if chunks != 1 {
		return false, fmt.Sprintf("多分片（%d 个物理对象），一个 URL 无法表达拼接", chunks)
	}
	if encrypted {
		return false, "内容加密，必须由持密钥的服务端解密后再下发"
	}
	return true, ""
}

// runDirectLinkProbes 实测 Range 支持与响应头（只读少量字节）
func runDirectLinkProbes(ctx context.Context, link *types.DirectLink, opts *directLinkOptions) []directLinkProbe {
	headers := link.Headers
	if opts.noHeaders {
		headers = nil
	}

	first := probeDirectLinkURL(ctx, link.URL, headers, opts.timeout, "bytes=0-0")
	probes := []directLinkProbe{first}
	// 首个探测确认支持 Range 且对象够大时，再复核一个中间偏移
	if first.SupportsRange && first.TotalSize > 4096 {
		probes = append(probes, probeDirectLinkURL(ctx, link.URL, headers, opts.timeout, "bytes=1024-1025"))
	}
	return probes
}

// probeDirectLinkURL 对直链发一次带 Range 的 GET
//
// 只读极少量字节：服务端忽略 Range 返回 200 时立刻关闭 body，避免整文件下载。
func probeDirectLinkURL(ctx context.Context, url string, headers map[string]string,
	timeout time.Duration, rangeHeader string) directLinkProbe {
	probe := directLinkProbe{RangeHeader: rangeHeader, DeclaredHeaders: len(headers) > 0}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		probe.Error = err.Error()
		return probe
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Range", rangeHeader)

	client := &http.Client{}
	start := time.Now()
	resp, err := client.Do(req)
	probe.ElapsedMS = time.Since(start).Milliseconds()
	if err != nil {
		probe.Error = err.Error()
		return probe
	}
	defer resp.Body.Close()

	probe.Status = resp.StatusCode
	probe.ContentLength = resp.Header.Get("Content-Length")
	probe.ContentRange = resp.Header.Get("Content-Range")
	probe.AcceptRanges = resp.Header.Get("Accept-Ranges")
	probe.ContentType = resp.Header.Get("Content-Type")
	probe.ContentDisp = resp.Header.Get("Content-Disposition")
	if resp.Request != nil && resp.Request.URL != nil {
		probe.FinalURL = resp.Request.URL.String()
	}
	probe.SupportsRange = resp.StatusCode == http.StatusPartialContent
	probe.TotalSize = parseContentRangeTotal(probe.ContentRange)

	if probe.SupportsRange {
		// 206 时读掉这几字节，确认数据能真正下发（失败不影响结论）
		_, _ = io.CopyN(io.Discard, resp.Body, 2)
	}
	return probe
}

// parseContentRangeTotal 从 "bytes 0-0/12345" 解析总长度；解析不出返回 0
func parseContentRangeTotal(contentRange string) int64 {
	idx := strings.LastIndex(contentRange, "/")
	if idx < 0 || idx == len(contentRange)-1 {
		return 0
	}
	total, err := strconv.ParseInt(strings.TrimSpace(contentRange[idx+1:]), 10, 64)
	if err != nil {
		return 0
	}
	return total
}

// printDirectLinkText 打印人类可读的打表结果
func printDirectLinkText(file *native.FileORM, rows []directLinkRow) {
	fmt.Println("===== 文件 =====")
	name, hash, size := "<nil>", "<nil>", uint64(0)
	if file.Name != nil {
		name = *file.Name
	}
	if file.Hash != nil {
		hash = *file.Hash
	}
	if file.Size != nil {
		size = *file.Size
	}
	fmt.Printf("id=%s name=%s size=%s\n", strValue(file.Id), name, humanSize(int64(size)))
	fmt.Printf("hash=%s\n", shortHash(hash))

	for i := range rows {
		printDirectLinkRow(&rows[i])
	}

	fmt.Println("\n提示: 实测只代表服务端出口 IP；判断是否绑定请求 IP 需把 URL 拷到客户端网络再试。")
}

// printDirectLinkRow 打印单个提供者的结果
func printDirectLinkRow(row *directLinkRow) {
	title := row.DriverId
	if row.ProviderName != "" {
		title = fmt.Sprintf("%s(%s)", row.DriverId, row.ProviderName)
	}
	fmt.Printf("\n===== provider %d %s =====\n", row.ProviderID, title)
	fmt.Printf("分片=%d 加密=%v\n", row.Chunks, row.Encrypted)

	if row.Error != "" {
		fmt.Printf("取链失败: %s\n", row.Error)
		return
	}
	if row.IneligibleReason != "" {
		fmt.Printf("资格: ✗ %s\n", row.IneligibleReason)
	}
	if row.URL == "" {
		return
	}
	if row.Eligible {
		fmt.Println("资格: ✓ 可直链（单分片 + 未加密）")
	}
	fmt.Printf("直链: %s\n", row.URL)
	if len(row.Headers) == 0 {
		fmt.Println("请求头: 无（URL 自包含鉴权，浏览器也能直接打开）")
	} else {
		fmt.Println("请求头（客户端必须携带，浏览器无法设置）:")
		for k, v := range row.Headers {
			fmt.Printf("  %s: %s\n", k, v)
		}
	}
	if row.ExpiresAt == "" {
		fmt.Println("有效期: 未知（驱动未声明，按「尽快使用」处理）")
	} else {
		fmt.Printf("有效期至: %s\n", row.ExpiresAt)
	}
	fmt.Printf("驱动声明 Range 支持: %v\n", row.RangeHint)

	printDirectLinkProbes(row.Probes)
}

// printDirectLinkProbes 打印实测结果
func printDirectLinkProbes(probes []directLinkProbe) {
	for i := range probes {
		p := &probes[i]
		fmt.Printf("实测 Range:%s → ", p.RangeHeader)
		if p.Error != "" {
			fmt.Printf("失败: %s\n", p.Error)
			continue
		}
		fmt.Printf("HTTP %d 耗时 %dms Range支持=%v", p.Status, p.ElapsedMS, p.SupportsRange)
		if p.TotalSize > 0 {
			fmt.Printf(" 总长=%s", humanSize(p.TotalSize))
		} else if p.ContentLength != "" {
			fmt.Printf(" Content-Length=%s(服务端可能忽略了 Range)", p.ContentLength)
		}
		fmt.Println()
		if p.ContentDisp != "" {
			fmt.Printf("    文件名(提供者给出): %s\n", p.ContentDisp)
		}
		if p.AcceptRanges != "" {
			fmt.Printf("    Accept-Ranges=%s\n", p.AcceptRanges)
		}
		if p.ContentType != "" {
			fmt.Printf("    Content-Type=%s\n", p.ContentType)
		}
		fmt.Printf("    携带自定义请求头=%v\n", p.DeclaredHeaders)
	}
}

// strValue 取字符串指针值，nil 返回 "<nil>"
func strValue(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

// shortHash 只显示 hash 前 12 位（日志/终端里不铺满整行）
func shortHash(hash string) string {
	if len(hash) > 12 {
		return hash[:12] + "..."
	}
	return hash
}

// humanSize 人类可读的字节数
func humanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	value := float64(size)
	for _, u := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.2f %s", value, u)
		}
	}
	return fmt.Sprintf("%.2f EB", value/unit)
}

func init() {
	rootCmd.AddCommand(directLinkCmd)
	directLinkCmd.Flags().StringP("file", "f", "", "文件 ID（files.id）；不知道 ID 时先用 --list 挑")
	directLinkCmd.Flags().Uint32("list", 0, "列出最近 N 个文件及各自可直链情况（用它挑 -f 的目标）")
	directLinkCmd.Flags().Uint32("provider", 0, "只取指定存储提供者 ID 的直链；0=遍历所有完整副本")
	directLinkCmd.Flags().Bool("probe", false, "发 HTTP 请求实测 Range 支持与响应头（只读少量字节）")
	directLinkCmd.Flags().Bool("no-headers", false, "实测时不带驱动声明的请求头（模拟浏览器直接打开）")
	directLinkCmd.Flags().Bool("json", false, "以 JSON 输出（含直链凭证，勿公开）")
	directLinkCmd.Flags().Duration("timeout", 30*time.Second, "单次实测请求超时")
}
