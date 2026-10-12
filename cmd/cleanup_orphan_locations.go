package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	_ "jenken827.com/jenken827/pfm/driver" // 注册存储驱动构造器（回收物理副本需要）
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/service"
)

// cleanupOrphanLocationsCmd 清理孤儿 file_locations 行（跨所有提供者）
//
// 定义"孤儿"：file_locations 中存在（deleted_at IS NULL），但按 blake3_hash
// 关联时 files 和 file_histories 两张表都没有任何记录引用。
// 这类行的物理副本可能是成功上传的残留（files 记录被删但位置行没删）、
// 或删除流程中途失败的遗留（DELETED_FAILED）等。
//
// 处理方式：按 hash 调用 service.ReclaimOrphanHashStorage——真实调用各提供者
// 驱动（local / telegram / baidu…）删除物理副本，成功后删 file_locations 行
// 并扣减容量；删除失败的行标记 DELETED_FAILED 保留待重试。
//
// 安全性：只处理 files + file_histories 都无引用的 hash，活跃文件绝不受影响；
// 默认 dry-run，--remove 才真正执行。
var cleanupOrphanLocationsCmd = &cobra.Command{
	Use:   "cleanup-orphan-locations",
	Short: "清理无引用的孤儿 file_locations（调提供者删除物理副本 + 删库行）",
	Long: `扫描 file_locations 中按 blake3_hash 关联后、files 与 file_histories
都没有任何记录引用的孤儿行（不限提供者、不限状态），逐 hash 回收：

  1. 调用各提供者驱动（local/telegram/baidu…）批量删除物理副本
     - 删除失败的行为标记 DELETED_FAILED 保留待重试
  2. 删除已成功的 file_locations 行
  3. 扣减各提供者的已用容量统计
  4. 删除按 hash 命名的缩略图

默认 dry-run 只预览；加 --remove 真正执行。
--older-than 阈值（默认 24h）跳过新产生的行，避免干扰正在进行的删除重试。

示例:
  pfm -d ./data cleanup-orphan-locations                    # dry-run 预览
  pfm -d ./data cleanup-orphan-locations --remove           # 真正回收
  pfm -d ./data cleanup-orphan-locations --older-than=72h   # 自定义阈值
`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		remove, _ := cmd.Flags().GetBool("remove")
		olderThan, _ := cmd.Flags().GetDuration("older-than")

		if err := globals.ValidateConfig(); err != nil {
			return err
		}
		globals.NormalizePath()
		config.Init()
		llog.Init()
		db.Init()

		cutoff := time.Now().Add(-olderThan)
		orphans, err := findOrphanLocationHashes(cutoff)
		if err != nil {
			return err
		}

		fmt.Println("===== 孤儿 file_locations 回收 =====")
		fmt.Printf("条件: files/file_histories 均无引用 AND created_at < %s\n",
			cutoff.Format("2006-01-02 15:04:05"))
		fmt.Printf("孤儿 hash 数: %d\n", len(orphans))
		if len(orphans) == 0 {
			fmt.Println("无需处理")
			return nil
		}

		// 预览每个 hash 的位置行分布
		var totalLocs int
		byProvider := make(map[uint32]int)
		for _, o := range orphans {
			locs, err := db.GetFileLocationsByBlake3Hash(o)
			if err != nil {
				return fmt.Errorf("查询 hash %s 的位置行失败: %w", o, err)
			}
			totalLocs += len(locs)
			for _, loc := range locs {
				if loc.ProviderId != nil {
					byProvider[*loc.ProviderId]++
				}
			}
			if len(orphans) <= 20 { // 小批量时直接列出
				fmt.Printf("  hash=%s locations=%d\n", o[:12]+"...", len(locs))
			}
		}
		fmt.Printf("涉及位置行: %d\n", totalLocs)
		fmt.Print("按提供者分布:")
		for pid, n := range byProvider {
			fmt.Printf("  provider %d: %d 行", pid, n)
		}
		fmt.Println()

		if !remove {
			fmt.Println("\n仅 dry-run，未做任何修改。加 --remove 真正执行。")
			return nil
		}

		var (
			okCount, failCount int
			totalReclaimed     int64
		)
		start := time.Now()
		for i, hash := range orphans {
			reclaimed, err := service.ReclaimOrphanHashStorage(hash)
			if err != nil {
				failCount++
				llog.L().Warnf("cleanup-orphan-locations: hash 回收失败: %s, err: %v", hash, err)
				continue
			}
			okCount++
			totalReclaimed += reclaimed
			if (i+1)%50 == 0 || i == len(orphans)-1 {
				fmt.Printf("进度: %d/%d (失败 %d, 已回收 %.1f MB)\n",
					i+1, len(orphans), failCount, float64(totalReclaimed)/1024/1024)
			}
		}
		fmt.Printf("\n===== 完成 =====\n")
		fmt.Printf("成功回收 hash: %d, 失败: %d, 耗时: %s\n",
			okCount, failCount, time.Since(start).Round(time.Second))
		fmt.Printf("回收存储(本地+远程合计): %.1f MB\n", float64(totalReclaimed)/1024/1024)
		return nil
	},
}

// findOrphanLocationHashes 查找 files / file_histories 均无引用的位置行 hash 集合
// （去重后返回；upload_records 不参与判定——失败上传任务的残留也应回收）
func findOrphanLocationHashes(cutoff time.Time) ([]string, error) {
	var hashes []string
	q := `SELECT DISTINCT fl.blake3_hash
FROM file_locations fl
WHERE fl.deleted_at IS NULL
  AND fl.blake3_hash != ''
  AND fl.created_at < ?
  AND NOT EXISTS (SELECT 1 FROM files          f  WHERE f.hash  = fl.blake3_hash)
  AND NOT EXISTS (SELECT 1 FROM file_histories fh WHERE fh.hash = fl.blake3_hash)
ORDER BY fl.blake3_hash`
	if err := db.GetDB().Raw(q, cutoff).Scan(&hashes).Error; err != nil {
		return nil, fmt.Errorf("查询孤儿 hash 失败: %w", err)
	}
	return hashes, nil
}

func init() {
	rootCmd.AddCommand(cleanupOrphanLocationsCmd)
	cleanupOrphanLocationsCmd.Flags().Bool("remove", false, "真正执行回收（调提供者删除+删库行，不可恢复）；不指定则仅 dry-run")
	cleanupOrphanLocationsCmd.Flags().Duration("older-than", 24*time.Hour,
		"只回收创建时间早于 now-older-than 的孤儿行（避免干扰正在进行的删除重试）")
}
