package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	_ "jenken827.com/jenken827/pfm/driver" // 注册存储驱动构造器（dedup-name 回收远程副本需要）
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/service"
)

// dedupCmd 去重：子树下同 hash 文件保留名称最短的
var dedupCmd = &cobra.Command{
	Use:   "dedup-files <父目录ID>",
	Short: "删除子树下的重复文件（同 hash 保留名称最短的）",
	Long: `扫描 <父目录ID> 子树下所有文件，按内容 hash 分组去重：
同一 hash 出现多份时，保留文件名最短的一个（同长度保留最早创建的），其余永久删除。

安全性说明：
  - 物理存储按 hash 共享（file_locations 按 blake3_hash 关联、缩略图按 hash 命名），
    删除冗余记录不影响幸存文件的存储位置、缩略图与提供者数据
  - 只删除冗余的 files 记录及其 file_histories 行
  - 建议先 --dry-run 预览确认

示例:
  pfm -d ./data dedup-files <folder_id>            # 执行去重
  pfm -d ./data dedup-files <folder_id> --dry-run  # 只预览不删除
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if err := globals.ValidateConfig(); err != nil {
			return err
		}
		globals.NormalizePath()
		config.Init()
		llog.Init()
		db.Init()

		result, err := service.DedupFilesByHash(args[0], dryRun)
		if err != nil {
			return err
		}

		verb := "去重"
		if dryRun {
			verb = "预览"
		}
		fmt.Println("===== " + verb + "结果 =====")
		fmt.Printf("扫描文件: %d\n", result.Scanned)
		fmt.Printf("重复 hash 组: %d\n", result.DuplicateGroups)
		fmt.Printf("保留: %d\n", result.Kept)
		if dryRun {
			fmt.Printf("待删除冗余文件: %d\n", result.ToDelete)
		} else {
			fmt.Printf("已删除冗余文件: %d\n", result.Deleted)
		}
		if len(result.Details) > 0 {
			fmt.Println("明细:")
			for _, d := range result.Details {
				fmt.Printf("  %s\n", d)
			}
		}
		if dryRun {
			fmt.Println("（dry-run 模式，未做任何修改）")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(dedupCmd)
	dedupCmd.Flags().Bool("dry-run", false, "只预览不删除")
}

// dedupNameCmd 去重同目录下的 "(N)" 后缀副本（"xxx (1).jpg" ≡ "xxx.jpg"）
var dedupNameCmd = &cobra.Command{
	Use:   "dedup-name <父目录ID>",
	Short: "删除子树下 同目录同名 的 \"(N)\" 后缀重复文件（内容核验后删除）",
	Long: `扫描 <父目录ID> 子树，把同目录下 "xxx (1).jpg" 类副本与其原名文件配对处理：
只处理尺寸相同的配对，并做内容核验（JPEG 比较 SOS 之后的像素数据，无需解码；
其他格式整文件字节比较），核验为实质重复才删除。

背景：浏览器/IM 下载同名文件会自动追加 " (1)"，两次迁移/上传的同一张照片
往往像素一致但 EXIF 元数据不同（blake3 不同），按 hash 去重永远无法命中。

保留策略（JPEG 对）：
  - 一方含 EXIF、另一方不含 → 保留含 EXIF 的（拍摄时间等元数据更有价值）
  - 其余 → 保留无后缀的原名文件
  - 若保留的是带后缀副本，会改回原名，删除原文件记录

回收策略：被删记录 hash 无任何引用（含回收站软删、历史版本）时，回收其在所有
存储提供者上的物理副本（本地明文文件 + Telegram/百度 远程副本）与缩略图、扣减
各提供者容量；个别副本删除失败（网络/驱动不可用）标记 DELETED_FAILED 保留待重试。

注意：同名但尺寸不同/内容确实不同的文件不会删除，会列出供人工确认。

示例:
  pfm -d ./data dedup-name <folder_id>            # 执行去重
  pfm -d ./data dedup-name <folder_id> --dry-run  # 只预览不删除
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if err := globals.ValidateConfig(); err != nil {
			return err
		}
		globals.NormalizePath()
		config.Init()
		llog.Init()
		db.Init()

		result, err := service.DedupNameSuffixDuplicates(args[0], dryRun)
		if err != nil {
			return err
		}

		verb := "去重"
		if dryRun {
			verb = "预览"
		}
		fmt.Println("===== " + verb + "结果 =====")
		fmt.Printf("扫描文件: %d\n", result.Scanned)
		fmt.Printf("同名配对组: %d\n", result.Groups)
		fmt.Printf("核验为重复的副本: %d\n", result.Duplicates)
		fmt.Printf("压缩衍生副本(删小保大): %d\n", result.CompressedDup)
		fmt.Printf("保留后缀副本(删除原文件): %d\n", result.KeepSuffix)
		fmt.Printf("同名不同尺寸(需人工): %d\n", result.DiffSize)
		fmt.Printf("同尺寸内容不同(需人工): %d\n", result.DiffContent)
		fmt.Printf("无法核验(跳过): %d\n", result.Skipped)
		fmt.Printf("无同名原文件的 (N) 文件: %d\n", result.Unpaired)
		if dryRun {
			fmt.Printf("待删除冗余文件: %d\n", result.ToDelete)
			fmt.Printf("待改名: %d\n", result.Renamed)
		} else {
			fmt.Printf("已删除冗余文件: %d\n", result.Deleted)
			fmt.Printf("已改名: %d\n", result.Renamed)
			if result.Reclaimed > 0 {
				fmt.Printf("已回收存储(本地+远程合计): %.1f MB\n", float64(result.Reclaimed)/1024/1024)
			}
		}
		if len(result.Details) > 0 {
			fmt.Println("明细:")
			for _, d := range result.Details {
				fmt.Printf("  %s\n", d)
			}
		}
		if dryRun {
			fmt.Println("（dry-run 模式，未做任何修改）")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(dedupNameCmd)
	dedupNameCmd.Flags().Bool("dry-run", false, "只预览不删除")
}
