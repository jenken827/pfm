package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	_ "jenken827.com/jenken827/pfm/driver"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// cleanupBaiduStorageOrphansCmd 扫描百度网盘上"实际存在但 file_locations 表里无对应记录"的文件
//
// 与 cleanup-baidu-orphans 的关系：
//   - cleanup-baidu-orphans: 数据库侧有 file_locations 行，但无任何引用且已失败 → 删数据库行
//   - 本命令: 百度侧有文件，数据库侧无 file_locations 行（逆向孤儿）→ 记录到临时文件供人工处理
//
// 流程：
//  1. 递归扫描百度网盘指定根路径下的所有文件（list 接口 Isdir=1 递归）
//  2. 对每个文件查 file_locations 表（provider_id 固定为 4 AND deleted_at IS NULL；
//     path 比对两侧都剥掉前导 "/" 以兼容 8-17 前后两种存储形态，
//     详见 filterExistingDBPaths）
//  3. 数据库存在 → 跳过；不存在 → 写入临时文件（一行一个 path）
//
// 安全说明：本命令只做"扫描+记录"，不调用百度 delete API。要清理临时文件中
// 记录的孤儿文件，建议人工审核后再人工登录网盘或单独写脚本删除。
//
// 多 provider：--provider-name 仅决定用哪个百度账户的 token 调 list API；
// 但 file_locations 比对固定用 provider_id=4（"百度网盘bdun"）。也就是说
// 选 bd_lp 只会扫它的网盘内容，却仍按主账户的库记录判定孤儿——对多账户
// 场景会误报，使用时须知晓此限制。
//
// 示例:
//
//	pfm -d ./data cleanup-baidu-storage-orphans --root-path /2026
//	    # 扫描 /2026 下所有文件，孤儿路径写入 /tmp/pfm_baidu_orphans_<ts>.txt
//	pfm -d ./data cleanup-baidu-storage-orphans --output /tmp/list.txt
//	    # 自定义输出文件
var cleanupBaiduStorageOrphansCmd = &cobra.Command{
	Use:   "cleanup-baidu-storage-orphans",
	Short: "扫描百度网盘侧实际存在但数据库无 file_locations 的文件（逆向孤儿）",
	Long: `递归扫描百度网盘指定根路径下的所有文件，对每个文件查询 file_locations 表：
存在-跳过；不存在-写入临时文件（一行一个 path），供人工核对后清理。


背景：cleanup-baidu-orphans 解决的是"数据库侧有记录但百度侧没有"的孤儿；
本命令解决相反方向的孤儿——百度网盘上实际还存在、但数据库没有任何记录引用的
文件。这些通常是 dedup-name 真实成功删除数据库行后百度侧残留、或服务异常时
上传到百度但 file_locations 创建失败留下的物理文件。

示例:
  pfm -d ./data cleanup-baidu-storage-orphans --root-path /2026
      # 默认根路径，扫描 /2026 下全部子目录
  pfm -d ./data cleanup-baidu-storage-orphans --root-path / --output /tmp/orphans.txt
      # 自定义输出文件路径
  pfm -d ./data cleanup-baidu-storage-orphans --dry-run
      # 仅打印统计信息，不写临时文件
`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		rootPath, _ := cmd.Flags().GetString("root-path")
		outputPath, _ := cmd.Flags().GetString("output")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		providerName, _ := cmd.Flags().GetString("provider-name")
		listOnly, _ := cmd.Flags().GetBool("list-providers")
		remove, _ := cmd.Flags().GetBool("remove")

		if err := globals.ValidateConfig(); err != nil {
			return err
		}
		globals.NormalizePath()
		config.Init()
		llog.Init()
		db.Init()

		// list-providers 模式：仅列出可用百度 provider 后退出
		if listOnly {
			type row struct {
				ID         uint32
				Name       string
				Enabled    bool
				RootPath   string
				ConfigSize int
			}
			var rows []row
			q := db.GetDB().Table("storage_providers").
				Select("id, name, enabled, config").
				Where("provider_type = ? AND deleted_at IS NULL", int32(native.StorageProviderType_BAIDU_NETDISK)).
				Order("id ASC")
			if err := q.Scan(&rows).Error; err != nil {
				return fmt.Errorf("查询 provider 列表失败: %w", err)
			}
			fmt.Printf("BAIDU_NETDISK 类型 provider 列表 (%d 个):\n", len(rows))
			for _, r := range rows {
				fmt.Printf("  id=%d name=%q enabled=%v config_size=%d\n",
					r.ID, r.Name, r.Enabled, r.ConfigSize)
			}
			fmt.Println("\n用法: --provider-name=name 选择其中一个")
			return nil
		}

		if !strings.HasPrefix(rootPath, "/") {
			rootPath = "/" + rootPath
		}
		rootPath = strings.TrimRight(rootPath, "/")
		if rootPath == "" {
			rootPath = "/"
		}

		// 拉取百度 provider + access_token
		tok, err := fetchBaiduAccessToken(providerName)
		if err != nil {
			return err
		}
		fmt.Printf("百度 access_token 长度: %d\n", len(tok))
		fmt.Printf("扫描 provider: %s\n", providerName)
		fmt.Printf("扫描根路径: %s\n", rootPath)

		start := time.Now()
		allFiles, err := listBaiduRecursive(providerName, rootPath)
		if err != nil {
			return fmt.Errorf("递归扫描百度网盘失败: %w", err)
		}
		scanDur := time.Since(start)
		fmt.Printf("百度侧文件总数: %d（递归耗时 %s）\n", len(allFiles), scanDur)

		// 批量查 file_locations（path IN 列表）
		existing, err := filterExistingDBPaths(allFiles)
		if err != nil {
			return err
		}
		orphans := make([]string, 0, len(allFiles)-len(existing))
		existingSet := existing // key 为去前导 "/" 的剥离形态
		for _, p := range allFiles {
			// 百度返回的 path 必带 "/" 前缀，与 DB 剥离形态比对时同样 strip
			if !existingSet[strings.TrimPrefix(p, "/")] {
				orphans = append(orphans, p)
			}
		}
		fmt.Printf("数据库侧有记录: %d\n", len(existing))
		fmt.Printf("百度孤儿（数据库无记录）: %d\n", len(orphans))

		if len(orphans) == 0 {
			fmt.Println("无孤儿，无需写入临时文件")
			return nil
		}

		// 汇总（按顶层目录分组）
		fmt.Println("\n按顶层目录分布 (Top 10):")
		byTop := make(map[string]int)
		for _, p := range orphans {
			segs := strings.SplitN(strings.TrimPrefix(p, "/"), "/", 2)
			top := "root"
			if len(segs) > 0 && segs[0] != "" {
				top = "/" + segs[0]
			}
			byTop[top]++
		}
		type kv struct {
			k string
			v int
		}
		var kvs []kv
		for k, v := range byTop {
			kvs = append(kvs, kv{k, v})
		}
		for i := 0; i < len(kvs); i++ {
			for j := i + 1; j < len(kvs); j++ {
				if kvs[j].v > kvs[i].v {
					kvs[i], kvs[j] = kvs[j], kvs[i]
				}
			}
		}
		limit := 10
		if len(kvs) < limit {
			limit = len(kvs)
		}
		for i := 0; i < limit; i++ {
			fmt.Printf("  %4d  %s\n", kvs[i].v, kvs[i].k)
		}

		if dryRun {
			fmt.Printf("\n仅 dry-run，未做任何修改。加 --remove 真正回收（调百度 API 删除孤儿文件）。\n")
			return nil
		}

		// --remove：调百度 API 真实删除孤儿文件
		if remove {
			if err := removeBaiduOrphans(providerName, tok, orphans); err != nil {
				return err
			}
			// 复扫验证
			fmt.Println("\n复扫验证中...")
			remaining, err := listBaiduRecursive(providerName, rootPath)
			if err != nil {
				return fmt.Errorf("复扫失败: %w", err)
			}
			remainSet := make(map[string]bool, len(remaining))
			for _, p := range remaining {
				remainSet[strings.TrimPrefix(p, "/")] = true
			}
			left := 0
			for _, p := range orphans {
				if remainSet[strings.TrimPrefix(p, "/")] {
					left++
				}
			}
			fmt.Printf("复扫结果: 百度侧文件总数 %d，孤儿残留 %d\n", len(remaining), left)
			if left > 0 {
				return fmt.Errorf("%d 个孤儿仍残留，请检查日志", left)
			}
			fmt.Println("全部孤儿已回收")
			return nil
		}

		// 无 --remove：仅记录到输出文件供人工审核
		if outputPath == "" {
			outputPath = filepath.Join(os.TempDir(),
				fmt.Sprintf("pfm_baidu_orphans_%s.txt", time.Now().Format("20060102_150405")))
		}
		if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			return fmt.Errorf("创建输出目录失败: %w", err)
		}
		f, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("创建输出文件失败: %w", err)
		}
		defer f.Close()
		w := bufio.NewWriter(f)
		for _, p := range orphans {
			fmt.Fprintln(w, p)
		}
		if err := w.Flush(); err != nil {
			return fmt.Errorf("写入失败: %w", err)
		}
		fmt.Printf("\n孤儿路径已写入: %s\n", outputPath)
		fmt.Println("下一步建议：人工审核该列表后，登录百度网盘客户端或通过 API 批量删除。")
		return nil
	},
}

// fetchBaiduAccessToken 拉取指定百度 provider 的 access_token
//
// 系统可能配置了多个百度账户（历史原因有 "百度网盘bdun" 和 "bd_lp" 等），
// 每个 provider 在百度服务端对应不同的账号（refresh_token 可能相同但
// access_token 独立）。通过 --provider-name 参数选择目标 provider。
//
// token 生命周期：百度 OAuth access_token 有效期约 2 小时。本命令只在
// 现有 token 不可用时才主动刷新（避免"短时间多次刷新"触发安全策略）。
// 正常情况下复用 DB 中现有的 token；如果复用后第一次 list 调用就拿到
// errno=-6/111，再在 listBaiduRecursive 重试路径里刷新一次。
func fetchBaiduAccessToken(providerName string) (string, error) {
	cfg, providerID, err := loadBaiduProviderCfg(providerName)
	if err != nil {
		return "", err
	}
	if cfg.AccessToken != nil && *cfg.AccessToken != "" {
		return *cfg.AccessToken, nil
	}
	// 现存 token 为空才主动刷新（兜底）
	return refreshAndPersistBaiduToken(cfg, providerID)
}

// refreshAndPersistBaiduToken 刷新百度 access_token 并写回 DB
func refreshAndPersistBaiduToken(cfg native.BaiduNetdiskDriverConfig, providerID uint32) (string, error) {
	tok, err := refreshBaiduToken(*cfg.RefreshToken, *cfg.ClientId, *cfg.ClientSecret)
	if err != nil {
		return "", err
	}
	cfg.AccessToken = &tok
	if newCfg, jerr := json.Marshal(cfg); jerr == nil {
		if uerr := db.GetDB().Table("storage_providers").
			Where("provider_type = ? AND deleted_at IS NULL AND enabled = true", int32(native.StorageProviderType_BAIDU_NETDISK)).
			Where("id = ?", providerID).
			Update("config", string(newCfg)).Error; uerr != nil {
			llog.L().Warnf("写回 access_token 失败: %v", uerr)
		}
	}
	return tok, nil
}

// refreshBaiduToken 主动刷新 access_token
func refreshBaiduToken(refresh, clientID, clientSecret string) (string, error) {
	v := url.Values{}
	v.Set("grant_type", "refresh_token")
	v.Set("refresh_token", refresh)
	v.Set("client_id", clientID)
	v.Set("client_secret", clientSecret)
	resp, err := http.Get("https://openapi.baidu.com/oauth/2.0/token?" + v.Encode())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var tr struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	_ = json.Unmarshal(body, &tr)
	if tr.AccessToken == "" {
		return "", fmt.Errorf("刷新 token 失败: %s body=%s", tr.Error, string(body))
	}
	return tr.AccessToken, nil
}

// listBaiduRecursive 递归扫描百度网盘指定路径下所有文件（返回 path 切片）
//
// token 过期自动刷新：第一次 list 收到 errno=-6/111 时，触发一次 refresh 后重试
// 整个扫描，避免 token 过期导致 0 文件结果。
func listBaiduRecursive(providerName, root string) ([]string, error) {
	tok, err := fetchBaiduAccessToken(providerName)
	if err != nil {
		return nil, err
	}
	var out []string
	visited := make(map[string]bool)
	var walk func(dir string) error
	walk = func(dir string) error {
		if visited[dir] {
			return nil
		}
		visited[dir] = true
		entries, err := listBaiduDir(tok, dir)
		if err != nil {
			return fmt.Errorf("list %s: %w", dir, err)
		}
		for _, e := range entries {
			if e.Isdir == 1 {
				childPath := e.Path
				if childPath == "" {
					childPath = strings.TrimRight(dir, "/") + "/" + e.ServerFilename
				}
				if err := walk(childPath); err != nil {
					return err
				}
			} else {
				p := e.Path
				if p == "" {
					p = strings.TrimRight(dir, "/") + "/" + e.ServerFilename
				}
				out = append(out, p)
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		if errors.Is(err, errAccessTokenExpired) {
			// token 过期：刷新一次重试整个扫描
			llog.L().Warn("baidu access_token expired, refreshing once and retrying")
			cfg, providerID, ferr := loadBaiduProviderCfg(providerName)
			if ferr != nil {
				return nil, fmt.Errorf("token 过期, 加载 provider 配置失败: %w", ferr)
			}
			newTok, ferr := refreshAndPersistBaiduToken(cfg, providerID)
			if ferr != nil {
				return nil, fmt.Errorf("token 过期, refresh 失败: %w", ferr)
			}
			tok = newTok
			visited = make(map[string]bool)
			out = nil
			if err := walk(root); err != nil {
				return nil, err
			}
			return out, nil
		}
		return nil, err
	}
	return out, nil
}

// loadBaiduProviderCfg 重新从 DB 加载指定 provider 的 cfg 与 ID（用于 token 过期后刷新）
func loadBaiduProviderCfg(providerName string) (native.BaiduNetdiskDriverConfig, uint32, error) {
	type providerRow struct {
		ID     uint32
		Config string
	}
	var row providerRow
	q := db.GetDB().Table("storage_providers").
		Select("id, config").
		Where("provider_type = ? AND deleted_at IS NULL AND enabled = true", int32(native.StorageProviderType_BAIDU_NETDISK)).
		Where("name = ?", providerName).
		Order("id ASC").
		Limit(1)
	if err := q.Scan(&row).Error; err != nil {
		return native.BaiduNetdiskDriverConfig{}, 0, err
	}
	if row.Config == "" {
		return native.BaiduNetdiskDriverConfig{}, 0, fmt.Errorf("provider %q 未找到", providerName)
	}
	var cfg native.BaiduNetdiskDriverConfig
	if err := json.Unmarshal([]byte(row.Config), &cfg); err != nil {
		return native.BaiduNetdiskDriverConfig{}, 0, err
	}
	return cfg, row.ID, nil
}

// baiduListEntry 百度 list 接口返回的简化条目
type baiduListEntry struct {
	Path           string `json:"path"`
	ServerFilename string `json:"server_filename"`
	Isdir          int    `json:"isdir"`
	FsId           int64  `json:"fs_id"`
	Size           int64  `json:"size"`
}

// listBaiduDir 调百度 list 接口拉取单层所有条目（含子目录）
//
// 对 errno=-6（access_token 已过期）特殊处理：返回 sentinel 错误让上层决定是否重试；
// 实际由 listBaiduRecursiveWithRefresh 包装层处理"刷新一次后重试"。
func listBaiduDir(token, dir string) ([]baiduListEntry, error) {
	var all []baiduListEntry
	start := 0
	limit := 1000
	for {
		params := url.Values{}
		params.Set("access_token", token)
		params.Set("method", "list")
		params.Set("dir", dir)
		params.Set("web", "1")
		params.Set("start", fmt.Sprintf("%d", start))
		params.Set("limit", fmt.Sprintf("%d", limit))
		resp, httpErr := http.Get("https://pan.baidu.com/rest/2.0/xpan/file?" + params.Encode())
		if httpErr != nil {
			return nil, httpErr
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var parsed struct {
			Errno int              `json:"errno"`
			List  []baiduListEntry `json:"list"`
		}
		if jerr := json.Unmarshal(body, &parsed); jerr != nil {
			return nil, fmt.Errorf("parse list resp: %w body=%s", jerr, string(body))
		}
		if parsed.Errno == -6 || parsed.Errno == 111 {
			// access_token 过期，让上层决定如何刷新
			return nil, errAccessTokenExpired
		}
		if parsed.Errno != 0 {
			return nil, fmt.Errorf("baidu list errno=%d dir=%s body=%s",
				parsed.Errno, dir, string(body))
		}
		all = append(all, parsed.List...)
		if len(parsed.List) < limit {
			break
		}
		start += limit
	}
	return all, nil
}

// errAccessTokenExpired 哨兵错误：表示百度服务端认为 access_token 已过期
var errAccessTokenExpired = fmt.Errorf("baidu access_token expired")

// removeBaiduOrphans 调百度 filemanager/delete API 分批删除孤儿文件。
// 这些孤儿在数据库中无 file_locations 行，删除后无需任何数据库操作。
// 逐批报告进度；返回失败条数（>0 时调用方决定是否报错）。
func removeBaiduOrphans(providerName, token string, orphans []string) error {
	const batchSize = 100
	var deleted, notFound, failed int
	start := time.Now()

	fmt.Printf("开始回收 %d 个百度孤儿文件（每批 %d 个）...\n", len(orphans), batchSize)
	for i := 0; i < len(orphans); i += batchSize {
		end := i + batchSize
		if end > len(orphans) {
			end = len(orphans)
		}
		batch := orphans[i:end]

		var d, nf, f int
		var lastErr error
		for attempt := 1; attempt <= 3; attempt++ { // 网络/限流重试
			d, nf, f, lastErr = baiduDeleteBatch(token, batch)
			if lastErr == nil {
				break
			}
			llog.L().Warnf("baidu orphan delete attempt %d/%d failed: %v", attempt, 3, lastErr)
			time.Sleep(2 * time.Second)
		}
		if lastErr != nil {
			return fmt.Errorf("批次 %d-%d 删除失败（已重试 3 次）: %w", i+1, end, lastErr)
		}
		deleted += d
		notFound += nf
		failed += f
		fmt.Printf("进度: %d/%d (deleted=%d not_found=%d failed=%d)\n", end, len(orphans), deleted, notFound, failed)
	}
	fmt.Printf("删除完成: deleted=%d not_found=%d failed=%d, 耗时 %s\n",
		deleted, notFound, failed, time.Since(start).Round(time.Second))
	return nil
}

// baiduDeleteBatch 调百度 filemanager delete 删除一批 path。
// errno 语义：0=成功；-7=路径无效（百度侧本就无此文件，等价已清理）。
func baiduDeleteBatch(token string, paths []string) (deleted, notFound, failed int, err error) {
	v := url.Values{}
	v.Set("async", "0")
	v.Set("ondup", "fail")
	fileList, _ := json.Marshal(paths)
	v.Set("filelist", string(fileList))
	reqURL := "https://pan.baidu.com/rest/2.0/xpan/file?access_token=" + token +
		"&method=filemanager&opera=delete"
	resp, herr := http.PostForm(reqURL, v)
	if herr != nil {
		return 0, 0, 0, herr
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Errno int                      `json:"errno"`
		Info  []map[string]interface{} `json:"info"`
	}
	if jerr := json.Unmarshal(body, &parsed); jerr != nil {
		return 0, 0, 0, fmt.Errorf("parse delete resp: %w body=%s", jerr, string(body))
	}
	if parsed.Errno == -6 || parsed.Errno == 111 {
		return 0, 0, 0, errAccessTokenExpired
	}
	if parsed.Errno != 0 {
		return 0, 0, 0, fmt.Errorf("top errno=%d body=%s", parsed.Errno, string(body))
	}
	for i, item := range parsed.Info {
		e := 0
		if ev, ok := item["errno"].(float64); ok {
			e = int(ev)
		}
		switch e {
		case 0:
			deleted++
		case -7:
			notFound++ // 路径无效 = 百度侧本就无此文件，视为已清理
		default:
			failed++
			if p, ok := item["path"].(string); ok {
				llog.L().Warnf("baidu orphan delete failed: path=%s errno=%d", p, e)
			} else {
				llog.L().Warnf("baidu orphan delete failed: index=%d errno=%d", i, e)
			}
		}
	}
	return deleted, notFound, failed, nil
}

// filterExistingDBPaths 批量查 file_locations，返回数据库中存在（provider=4 AND deleted_at IS NULL）的 path 集合
//
// 兼容：8-17 refactor 之前/之后的 file_locations.path 有无前导 "/" 不一致，
// 参见 driver/baidu_netdisk/driver.go rootPath()。这里对两侧都做 stripSlash 后比对，
// 后续所有路径归一化修复完成可去掉这层兼容逻辑。
func filterExistingDBPaths(allPaths []string) (map[string]bool, error) {
	out := make(map[string]bool)
	if len(allPaths) == 0 {
		return out, nil
	}
	// 客户端传进来的 path（百度服务端返回，必带 / 前缀），先归一化为"有 /"形态，
	// 数据库侧用 LTRIM 比较"去掉前导 /"匹配。这样新老数据都能命中。
	stripped := make([]string, len(allPaths))
	for i, p := range allPaths {
		stripped[i] = strings.TrimPrefix(p, "/")
	}
	const batch = 500
	for i := 0; i < len(stripped); i += batch {
		end := i + batch
		if end > len(stripped) {
			end = len(stripped)
		}
		batchPaths := stripped[i:end]
		var rows []struct {
			Path string
		}
		// 关键：DB 侧 LTRIM(path, '/') 兼容 with_slash/no_slash 两种存储形态
		if err := db.GetDB().
			Table("file_locations").
			Select("DISTINCT path").
			Where("provider_id = ?", 4).
			Where("deleted_at IS NULL").
			Where("LTRIM(path, '/') IN ?", batchPaths).
			Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("查 file_locations (batch %d-%d) 失败: %w", i, end, err)
		}
		// DB 里 LTRIM(path,'/') 即为去前缀后的"标准"形态——对每个查出的 path，
		// 反过来重建"带 /"形态 key 写入返回 map，调用方用原始百度 path 也能命中。
		for _, r := range rows {
			strippedKey := strings.TrimPrefix(r.Path, "/")
			out[strippedKey] = true
		}
	}
	return out, nil
}

func init() {
	rootCmd.AddCommand(cleanupBaiduStorageOrphansCmd)
	cleanupBaiduStorageOrphansCmd.Flags().String("root-path", "/2026",
		"扫描根路径，list 接口会递归扫描该路径下所有子目录")
	cleanupBaiduStorageOrphansCmd.Flags().String("output", "",
		"孤儿路径输出文件路径，默认 /tmp/pfm_baidu_orphans_<时间>.txt")
	cleanupBaiduStorageOrphansCmd.Flags().Bool("dry-run", false,
		"仅打印统计信息，不写入临时文件")
	cleanupBaiduStorageOrphansCmd.Flags().Bool("remove", false,
		"真正回收：调百度 API 删除孤儿文件并复扫验证（与 --output 互斥，--remove 优先）")
	cleanupBaiduStorageOrphansCmd.Flags().String("provider-name", "百度网盘bdun",
		"要扫描的百度 provider 名称（系统可能配置了多个百度账户，如 bd_lp；"+
			"不指定默认 '百度网盘bdun'，可用 'list' 查看所有可用 provider 后选择）")
	cleanupBaiduStorageOrphansCmd.Flags().Bool("list-providers", false,
		"仅打印所有 BAIDU_NETDISK 类型 provider 后退出（不连接百度 API、不扫描）")
}
