package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	abi "github.com/jenken827/pfm-plugin-sdk/proto/gen/abi"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	_ "github.com/jenken827/pfm/driver" // 注册存储驱动构造器
	"github.com/jenken827/pfm/globals"
	"github.com/jenken827/pfm/internal/config"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/service"
	"github.com/jenken827/pfm/internal/storage"
	"github.com/jenken827/pfm/pkg/llog"
	"github.com/spf13/cobra"
)

// cleanupStorageOrphansCmd 逆向孤儿：扫描存储提供者远端，找出 file_locations 已无记录的物理文件
//
// 与 cleanup-orphan-locations 互补：
//   - cleanup-orphan-locations：数据库有位置行、但 files/file_histories 无引用 → 删数据库行 + 物理副本
//   - 本命令：远端有文件、数据库连 file_locations 都没有 → 列清单/回收（逆向孤儿）
//
// 能力来自驱动的可选接口 types.RemoteLister（local / 百度 / 123 / 阿里 / 移动 / 天翼）；
// 只能按对象 ID 读取的提供者（没有目录枚举能力）不支持远端扫描，会被跳过并提示。
var cleanupStorageOrphansCmd = &cobra.Command{
	Use:   "cleanup-storage-orphans",
	Short: "扫描存储提供者远端、处理数据库无记录的孤儿文件（逆向孤儿，支持多提供者）",
	Long: `递归扫描存储提供者的远端目录，对每个远端文件查 file_locations：
有记录 → 跳过；无记录 → 判定为孤儿（远端有、数据库无）。

与 cleanup-orphan-locations 方向相反：那条命令清「数据库有、远端孤儿」，
本命令清「远端有、数据库无」。

支持的提供者（实现了 types.RemoteLister 的驱动）：
  local / 百度网盘 / 123 云盘 / 阿里云盘 / 移动云盘 / 天翼云盘
  只能按对象 ID 读取、没有目录树的提供者会被跳过。

安全默认：
  - 只扫描、默认只写清单文件，不删任何东西
  - --remove 才真正调驱动删除；删的是 file_locations 完全无记录的远端文件
  - 默认扫描根 = <provider 根>/<当前年份>，避免误扫到网盘里与 pfm 无关的目录；
    要扫全盘用 --root-path <provider 根>

示例:
  pfm -d ./data cleanup-storage-orphans --list-providers
      # 列出所有 provider 及是否支持远端枚举
  pfm -d ./data cleanup-storage-orphans --provider-name my-netdisk
      # 扫描该 provider 当前年份目录，孤儿路径写入 /tmp 清单
  pfm -d ./data cleanup-storage-orphans --all --root-path / --dry-run
      # 扫描所有支持的 provider 全盘，只打印统计不写文件
  pfm -d ./data cleanup-storage-orphans --provider-id 3 --remove
      # 确认清单后真正回收该 provider 当前年份目录下的孤儿
`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		providerName, _ := cmd.Flags().GetString("provider-name")
		providerID, _ := cmd.Flags().GetUint32("provider-id")
		all, _ := cmd.Flags().GetBool("all")
		rootOverride, _ := cmd.Flags().GetString("root-path")
		outputPath, _ := cmd.Flags().GetString("output")
		remove, _ := cmd.Flags().GetBool("remove")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		listOnly, _ := cmd.Flags().GetBool("list-providers")

		if err := globals.ValidateConfig(); err != nil {
			return err
		}
		globals.NormalizePath()
		config.Init()
		llog.Init()
		db.Init()

		if listOnly {
			return printProviderList()
		}
		if remove && dryRun {
			return fmt.Errorf("--remove 与 --dry-run 不能同时使用")
		}
		providers, err := selectOrphanProviders(providerName, providerID, all)
		if err != nil {
			return err
		}

		ctx := context.Background()
		var totalScan, totalOrphans, totalDeleted, totalFailed int
		for _, sp := range providers {
			scanned, orphans, deleted, failed, err := scanOneProvider(ctx, sp, rootOverride, outputPath, remove, dryRun)
			if err != nil {
				llog.L().Errorf("cleanup-storage-orphans: provider %s 处理失败: %v", derefStr(sp.Name), err)
				fmt.Printf("  ❌ 处理失败: %v\n", err)
				continue
			}
			totalScan += scanned
			totalOrphans += orphans
			totalDeleted += deleted
			totalFailed += failed
		}

		fmt.Println("\n===== 汇总 =====")
		fmt.Printf("provider 数: %d\n", len(providers))
		fmt.Printf("远端文件: %d，孤儿: %d\n", totalScan, totalOrphans)
		if remove {
			fmt.Printf("已删除: %d，失败: %d\n", totalDeleted, totalFailed)
		} else {
			fmt.Println("未删除任何文件（加 --remove 才真正回收）")
		}
		return nil
	},
}

// scanOneProvider 扫描并处理单个 provider，返回 扫描数/孤儿数/删除数/失败数
func scanOneProvider(ctx context.Context, sp *entity.StorageProviderORM, rootOverride, outputPath string,
	remove, dryRun bool) (scanned, orphans, deleted, failed int, err error) {
	providerID := *sp.Id
	providerName := derefStr(sp.Name)
	driverID := derefStr(sp.DriverId)
	fmt.Printf("\n===== provider #%d %s (%s) =====\n", providerID, providerName, driverID)
	if !remoteListSupported(driverID) {
		fmt.Println("  跳过：该提供者不支持远端目录枚举")
		return 0, 0, 0, 0, nil
	}

	drv, err := storage.EnsureProvider(providerID)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("初始化驱动失败: %w", err)
	}
	lister, ok := drv.(types.RemoteLister)
	if !ok {
		fmt.Println("  跳过：驱动未实现 RemoteLister")
		return 0, 0, 0, 0, nil
	}
	root, err := resolveOrphanScanRoot(lister, rootOverride)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	fmt.Printf("扫描根路径: %s\n", root)

	start := time.Now()
	scan, err := service.ScanRemoteOrphans(ctx, providerID, providerName, root, func(n int) {
		fmt.Printf("  ... 已扫描 %d 个远端文件\n", n)
	})
	if err != nil {
		return 0, 0, 0, 0, err
	}
	fmt.Printf("远端文件总数: %d（耗时 %s）\n", scan.Scanned, time.Since(start).Round(time.Second))
	fmt.Printf("数据库有记录: %d\n", scan.Recorded)
	fmt.Printf("远端孤儿（数据库无记录）: %d\n", len(scan.Orphans))
	printOrphanDistribution(scan)

	if remove && scan.Scanned > 0 && len(scan.Orphans) == scan.Scanned {
		// 全盘皆孤儿基本只可能是 provider 根配置 / 路径口径变了，不能照删
		return scan.Scanned, len(scan.Orphans), 0, 0,
			fmt.Errorf("全部 %d 个远端文件都被判定为孤儿，疑似 provider 根路径配置与数据库不一致，已拒绝删除；请先不带 --remove 出清单人工核对", scan.Scanned)
	}
	if remove {
		fmt.Printf("\n开始回收 %d 个孤儿文件...\n", len(scan.Orphans))
		deleted, failed, derr := service.RemoveRemoteOrphans(ctx, providerID, scan.Orphans)
		if derr != nil {
			return scan.Scanned, len(scan.Orphans), deleted, failed, derr
		}
		fmt.Printf("删除完成: 成功 %d，失败 %d\n", deleted, failed)
		verifyAfterRemove(ctx, providerID, providerName, root)
		return scan.Scanned, len(scan.Orphans), deleted, failed, nil
	}

	if dryRun || len(scan.Orphans) == 0 {
		if len(scan.Orphans) == 0 {
			fmt.Println("无孤儿")
		}
		fmt.Println("仅 dry-run，未写清单、未删除任何文件")
		return scan.Scanned, len(scan.Orphans), 0, 0, nil
	}
	outPath, werr := writeOrphanList(outputPath, providerName, scan.Orphans)
	if werr != nil {
		return scan.Scanned, len(scan.Orphans), 0, 0, werr
	}
	fmt.Printf("\n孤儿路径已写入: %s\n", outPath)
	fmt.Println("下一步：人工核对清单后，用 --remove 真正回收，或自行登录网盘删除。")
	return scan.Scanned, len(scan.Orphans), 0, 0, nil
}

// verifyAfterRemove 删除后复扫，确认没有残留孤儿
func verifyAfterRemove(ctx context.Context, providerID uint32, providerName, root string) {
	fmt.Println("\n复扫验证中...")
	rescan, err := service.ScanRemoteOrphans(ctx, providerID, providerName, root, nil)
	if err != nil {
		fmt.Printf("  ⚠️ 复扫失败: %v\n", err)
		return
	}
	fmt.Printf("复扫结果: 远端文件 %d，孤儿残留 %d\n", rescan.Scanned, len(rescan.Orphans))
	if len(rescan.Orphans) > 0 {
		fmt.Println("  ⚠️ 仍有孤儿残留（删除失败，详见日志）")
		return
	}
	fmt.Println("全部孤儿已回收")
}

// resolveOrphanScanRoot 计算扫描根：显式 --root-path 优先，
// 否则用 <provider 根>/<当前年份>（与 pfm 年/月归档结构一致，避免扫到无关目录）
func resolveOrphanScanRoot(lister types.RemoteLister, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	base := lister.RemoteRoot()
	if base == "" {
		return "", fmt.Errorf("provider 未配置根目录，请用 --root-path 指定扫描起点")
	}
	return path.Join(base, time.Now().Format("2006")), nil
}

// printOrphanDistribution 按父目录汇总孤儿分布（只列前 10）
func printOrphanDistribution(scan *service.RemoteOrphanScan) {
	if len(scan.Orphans) == 0 {
		return
	}
	byDir := make(map[string]int)
	for _, o := range scan.Orphans {
		byDir[path.Dir(o.Path)]++
	}
	type kv struct {
		k string
		v int
	}
	kvs := make([]kv, 0, len(byDir))
	for k, v := range byDir {
		kvs = append(kvs, kv{k, v})
	}
	sort.Slice(kvs, func(i, j int) bool {
		if kvs[i].v != kvs[j].v {
			return kvs[i].v > kvs[j].v
		}
		return kvs[i].k < kvs[j].k
	})
	limit := 10
	if len(kvs) < limit {
		limit = len(kvs)
	}
	fmt.Println("按目录分布 (Top 10):")
	for i := 0; i < limit; i++ {
		fmt.Printf("  %4d  %s\n", kvs[i].v, kvs[i].k)
	}
}

// writeOrphanList 把孤儿路径写成清单文件，返回实际路径
func writeOrphanList(outputPath, providerName string, orphans []service.RemoteOrphan) (string, error) {
	if outputPath == "" {
		outputPath = filepath.Join(os.TempDir(), fmt.Sprintf("pfm_orphans_%s_%s.txt",
			sanitizeFileName(providerName), time.Now().Format("20060102_150405")))
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return "", fmt.Errorf("创建输出目录失败: %w", err)
	}
	f, err := os.Create(outputPath)
	if err != nil {
		return "", fmt.Errorf("创建输出文件失败: %w", err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, o := range orphans {
		fmt.Fprintln(w, o.Path)
	}
	if err := w.Flush(); err != nil {
		return "", fmt.Errorf("写入清单失败: %w", err)
	}
	return outputPath, nil
}

// sanitizeFileName 把 provider 名变成安全的文件名片段
func sanitizeFileName(name string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", " ", "_", ":", "_")
	out := replacer.Replace(name)
	if out == "" {
		return "provider"
	}
	return out
}

// selectOrphanProviders 按 --provider-name / --provider-id / --all 选出目标 provider
func selectOrphanProviders(name string, id uint32, all bool) ([]*entity.StorageProviderORM, error) {
	if !all && name == "" && id == 0 {
		return nil, fmt.Errorf("必须指定 --provider-name、--provider-id 或 --all 之一（--list-providers 可先查看）")
	}
	providers, err := db.GetAllProviders()
	if err != nil {
		return nil, fmt.Errorf("查询 provider 失败: %w", err)
	}
	var picked []*entity.StorageProviderORM
	for _, sp := range providers {
		if sp.Id == nil || sp.Name == nil || sp.DriverId == nil {
			continue
		}
		switch {
		case id != 0:
			if *sp.Id != id {
				continue
			}
		case name != "":
			if *sp.Name != name {
				continue
			}
		default: // --all：只选启用且支持远端枚举的
			if sp.Enabled == nil || !*sp.Enabled || !remoteListSupported(*sp.DriverId) {
				continue
			}
		}
		if sp.Enabled == nil || !*sp.Enabled {
			return nil, fmt.Errorf("provider %q 未启用", *sp.Name)
		}
		picked = append(picked, sp)
	}
	if len(picked) == 0 {
		return nil, fmt.Errorf("没有匹配的 provider（--list-providers 可查看全部）")
	}
	return picked, nil
}

// printProviderList 列出所有 provider 及远端枚举支持情况（不初始化驱动、不连网）
func printProviderList() error {
	providers, err := db.GetAllProviders()
	if err != nil {
		return fmt.Errorf("查询 provider 失败: %w", err)
	}
	fmt.Printf("存储提供者列表 (%d 个):\n", len(providers))
	for _, sp := range providers {
		if sp.Id == nil || sp.Name == nil || sp.DriverId == nil {
			continue
		}
		driverID := *sp.DriverId
		enabled := sp.Enabled != nil && *sp.Enabled
		support := "不支持远端枚举"
		if remoteListSupported(driverID) {
			support = "支持远端枚举"
		}
		fmt.Printf("  id=%-3d %-24s driver=%-18s enabled=%-5v %s\n",
			*sp.Id, *sp.Name, driverID, enabled, support)
	}
	fmt.Println("\n说明：只按对象 ID 读取、没有目录树的提供者不做远端枚举；其余网盘/本地均支持。")
	return nil
}

// remoteListSupported 依据驱动声明的能力判断（驱动在 DriverInfo.Capabilities 里
// 声明 REMOTE_LIST，与其是否实现 types.RemoteLister 一致）
func remoteListSupported(driverID string) bool {
	return types.HasCapability(types.GetDriverMetadata(driverID),
		abi.DriverCapability_DRIVER_CAPABILITY_REMOTE_LIST)
}

func init() {
	rootCmd.AddCommand(cleanupStorageOrphansCmd)
	cleanupStorageOrphansCmd.Flags().String("provider-name", "", "目标存储提供者名称（与 --provider-id/--all 三选一）")
	cleanupStorageOrphansCmd.Flags().Uint32("provider-id", 0, "目标存储提供者 ID（与 --provider-name/--all 三选一）")
	cleanupStorageOrphansCmd.Flags().Bool("all", false, "处理所有启用且支持远端枚举的 provider")
	cleanupStorageOrphansCmd.Flags().String("root-path", "", "扫描根路径；默认 <provider 根>/<当前年份>，用 / 表示全盘")
	cleanupStorageOrphansCmd.Flags().String("output", "", "孤儿清单输出文件，默认 /tmp/pfm_orphans_<provider>_<时间>.txt")
	cleanupStorageOrphansCmd.Flags().Bool("remove", false, "真正回收：调驱动删除孤儿文件并复扫验证")
	cleanupStorageOrphansCmd.Flags().Bool("dry-run", false, "只打印统计，不写清单、不删除")
	cleanupStorageOrphansCmd.Flags().Bool("list-providers", false, "仅列出所有 provider 及远端枚举支持情况后退出")
}
