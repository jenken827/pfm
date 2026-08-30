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

// migratePhotosCmd 修复旧数据：把子树下媒体文件（图片+视频）按文件名日期移到 年/月 目录
var migratePhotosCmd = &cobra.Command{
	Use:   "migrate-photos <父目录ID>",
	Short: "把子树下图片/视频按文件名日期移到 年/月 目录（修复旧备份数据）",
	Long: `把 <父目录ID> 子树下的所有媒体文件（图片 + 视频）按文件名解析拍摄日期，
移动到 父目录/年/月 目录（目录不存在自动创建），与相册备份的年/月归档规则一致。

解析规则（与前端一致）：
  - IMG_20260811_063006.jpg / PANO_20170121_xxxx.jpg / VID_20150218_xxx.jpg
    / Screenshot_20240109_... : "_" 分隔第二段为日期（YYYYMMDD 或 YYYY-MM-DD）
  - IMG20150908xxx.jpg : "IMG" 前缀直接跟 8 位 YYYYMMDD
  - mmexport1713701399029.jpg / wx_camera_1725958479520.jpg / microMsg.1713701399029.jpg
    : 前缀后前 10 位时间戳
  - 1784212534296.png : 开头连续数字前 10 位时间戳

行为：
  - 已处于正确年/月目录的图片跳过
  - 无法解析日期的图片保持原位置不动
  - 目标目录同名冲突自动追加后缀（与移动/恢复行为一致）
  - 只更新逻辑目录（files.parent_id），物理存储按 hash 寻址不受影响

示例:
  pfm -d ./data migrate-photos <folder_id>              # 执行迁移
  pfm -d ./data migrate-photos <folder_id> --dry-run    # 只预览不执行
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

		result, err := service.MigratePhotosToYearMonth(args[0], dryRun)
		if err != nil {
			return err
		}

		verb := "迁移"
		if dryRun {
			verb = "预览"
		}
		fmt.Println("===== " + verb + "结果 =====")
		fmt.Printf("扫描图片: %d\n", result.Scanned)
		fmt.Printf("可迁移到年/月目录: %d\n", result.Moved)
		fmt.Printf("已在年/月目录（跳过）: %d\n", result.AlreadyInPlace)
		fmt.Printf("无法解析日期（保持原位置）: %d\n", result.NoDate)
		fmt.Printf("失败: %d\n", result.Failed)
		if len(result.CreatedDirs) > 0 {
			if dryRun {
				fmt.Println("预计新创建目录:")
			} else {
				fmt.Println("新创建目录:")
			}
			for _, d := range result.CreatedDirs {
				fmt.Printf("  %s\n", d)
			}
		}
		for _, f := range result.Failures {
			fmt.Printf("  ❌ %s\n", f)
		}
		if dryRun {
			fmt.Println("（dry-run 模式，未做任何修改）")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(migratePhotosCmd)
	migratePhotosCmd.Flags().Bool("dry-run", false, "只预览不执行")
}
