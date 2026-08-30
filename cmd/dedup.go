package cmd

import (
	"fmt"

	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/service"
	"github.com/spf13/cobra"
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
