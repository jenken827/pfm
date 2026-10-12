// Package cmd 定义 pfm 命令行入口
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"jenken827.com/jenken827/pfm/globals"
)

// rootCmd 根命令
var rootCmd = &cobra.Command{
	Use:   "pfm",
	Short: "Personal File Manager (FS-Server)",
	Long: `Personal File Manager 服务端。

子命令:
  server            启动服务器
  user              本地认证用户管理（auth.mode=local 时使用）
  migrate-photos    修复旧数据：按文件名日期把图片/视频移到 年/月 目录
  dedup-files       删除子树下的重复文件（同 hash 保留名称最短的）
  dedup-name        删除子树下同名 "(N)" 后缀重复文件（内容核验，EXIF 差异也能识别）
  cleanup-orphan-locations 回收无引用的孤儿 file_locations（调提供者删物理副本+删库行）
  cleanup-baidu-storage-orphans 扫描并回收百度网盘上无数据库记录的文件（逆向孤儿）
  direct-link       取文件在存储提供者上的下载直链（打表/验证用，不经过服务端中转）
  decrypt           按 file_locations id 解密本地加密文件
  version           显示版本信息`,
	// 无子命令时显示帮助
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

// Execute 执行根命令
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// 持久化全局参数（所有子命令可用）
	rootCmd.PersistentFlags().StringVarP(&globals.DataDir, "dir", "d", "", "Data directory")
	rootCmd.PersistentFlags().StringVarP(&globals.ConfigPath, "config", "c", "", "Config file path")
	rootCmd.PersistentFlags().BoolVar(&globals.Debug, "debug", false, "Debug mode (bypass auth, use temp port)")
	rootCmd.PersistentFlags().IntVar(&globals.DebugPort, "debug-port", 0, "Debug mode port (default: random available port)")
}
