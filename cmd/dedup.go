package cmd

import (
	"fmt"

	_ "github.com/jenken827/pfm/driver" // 注册存储驱动构造器（阶段 2 回收远程副本需要）
	"github.com/jenken827/pfm/globals"
	"github.com/jenken827/pfm/internal/config"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/service"
	"github.com/jenken827/pfm/pkg/llog"
	"github.com/spf13/cobra"
)

// dedupCmd 子树去重：阶段 1 按 hash 合并，阶段 2 按「同名 + (N) 后缀」配对核验（保大）
var dedupCmd = &cobra.Command{
	Use:   "dedup-files <父目录ID>",
	Short: "子树去重：同 hash 合并 + 同名 (N) 后缀副本核验（始终保留更大的一方）",
	Long: `扫描 <父目录ID> 子树，依次执行两个阶段：

阶段 1｜内容 hash
  同一 hash 出现多份时只保留一条记录，其余删除；保留名称最短的（同长度保留最早创建）。
  物理存储按 hash 共享，删记录不影响幸存文件的存储位置、缩略图与提供者数据。

阶段 2｜同名 (N) 后缀
  同目录下 "xxx (1).jpg" 与 "xxx.jpg" 配对处理：
  - 带 (N) 后缀的副本不会因为名字被直接删掉：先比文件大小，大的一方优先保留
  - 尺寸相同 → 内容核验（JPEG 比较像素数据，其他格式比较字节），一致才删
  - 尺寸不同 → 图片做压缩衍生核验（画面一致），确认衍生后删小保大；否则保留供人工确认
  - 保留的若带 (N) 后缀，会改回原名，再删除原文件记录
  - 被删记录的 hash 无任何引用（含回收站软删、历史版本）时，回收其在所有存储
    提供者上的物理副本与缩略图、扣减容量；删除失败的副本标记 DELETED_FAILED 待重试

安全性：
  - 默认 dry-run（只预览），不会删除任何文件或记录
  - --execute 才真正执行删除与物理副本回收
  - 同名但内容确实不同的文件一律不删，只在明细里列出

示例:
  pfm -d ./data dedup-files <folder_id>             # 预览（默认，不删除）
  pfm -d ./data dedup-files <folder_id> --execute   # 确认后真正执行
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRunFlag, _ := cmd.Flags().GetBool("dry-run")
		execute, _ := cmd.Flags().GetBool("execute")
		dryRun := resolveDedupDryRun(dryRunFlag, execute)

		if err := globals.ValidateConfig(); err != nil {
			return err
		}
		globals.NormalizePath()
		config.Init()
		llog.Init()
		db.Init()

		hashResult, err := service.DedupFilesByHash(args[0], dryRun)
		if err != nil {
			return err
		}
		nameResult, err := service.DedupNameSuffixDuplicates(args[0], dryRun)
		if err != nil {
			return err
		}

		printDedupResult(hashResult, nameResult, dryRun)
		return nil
	},
}

// resolveDedupDryRun 计算本次是否只预览：默认预览，--execute 才真正执行。
// 传 --dry-run=false 也可执行（保留该写法给旧脚本用）。
func resolveDedupDryRun(dryRun, execute bool) bool {
	return dryRun && !execute
}

// printDedupResult 输出 内容 hash + 同名 (N) 后缀 两个阶段的结果与明细
func printDedupResult(h *service.DedupResult, n *service.NameSuffixDedupResult, dryRun bool) {
	verb := "去重"
	if dryRun {
		verb = "预览"
	}
	fmt.Println("===== " + verb + "结果 =====")

	fmt.Println("[阶段 1] 内容 hash")
	fmt.Printf("  扫描文件: %d\n", h.Scanned)
	fmt.Printf("  重复 hash 组: %d\n", h.DuplicateGroups)
	fmt.Printf("  保留: %d\n", h.Kept)
	if dryRun {
		fmt.Printf("  待删除冗余文件: %d\n", h.ToDelete)
	} else {
		fmt.Printf("  已删除冗余文件: %d\n", h.Deleted)
	}

	fmt.Println("[阶段 2] 同名 (N) 后缀")
	fmt.Printf("  扫描文件: %d\n", n.Scanned)
	fmt.Printf("  同名配对组: %d\n", n.Groups)
	fmt.Printf("  核验为重复的副本: %d\n", n.Duplicates)
	fmt.Printf("  压缩衍生副本(删小保大): %d\n", n.CompressedDup)
	fmt.Printf("  保留后缀副本(删除原文件): %d\n", n.KeepSuffix)
	fmt.Printf("  其中因文件更大保留: %d\n", n.KeptLarger)
	fmt.Printf("  同名不同尺寸(需人工): %d\n", n.DiffSize)
	fmt.Printf("  同尺寸内容不同(需人工): %d\n", n.DiffContent)
	fmt.Printf("  无法核验(跳过): %d\n", n.Skipped)
	fmt.Printf("  无同名原文件的 (N) 文件: %d\n", n.Unpaired)
	if dryRun {
		fmt.Printf("  待删除冗余文件: %d\n", n.ToDelete)
		fmt.Printf("  待改名: %d\n", n.Renamed)
	} else {
		fmt.Printf("  已删除冗余文件: %d\n", n.Deleted)
		fmt.Printf("  已改名: %d\n", n.Renamed)
		if n.Reclaimed > 0 {
			fmt.Printf("  已回收存储(本地+远程合计): %.1f MB\n", float64(n.Reclaimed)/1024/1024)
		}
	}

	details := make([]string, 0, len(h.Details)+len(n.Details))
	details = append(details, h.Details...)
	details = append(details, n.Details...)
	if len(details) > 0 {
		fmt.Println("明细:")
		for _, d := range details {
			fmt.Printf("  %s\n", d)
		}
	}
	if dryRun {
		fmt.Println()
		fmt.Println("⚠️  以上为 dry-run 预览：未删除任何文件、未改动数据库、未回收任何存储。")
		fmt.Println("    确认无误后加 --execute 真正执行。")
	}
}

func init() {
	rootCmd.AddCommand(dedupCmd)
	dedupCmd.Flags().Bool("dry-run", true, "只预览不删除（默认开启）")
	dedupCmd.Flags().Bool("execute", false, "真正执行删除与物理副本回收（覆盖 --dry-run）")
}
