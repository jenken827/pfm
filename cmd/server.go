package cmd

import (
	"jenken827.com/jenken827/pfm/bootstrap"
	_ "jenken827.com/jenken827/pfm/driver" // 注册存储驱动
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/server"
	"github.com/spf13/cobra"
)

// serverCmd 启动服务器
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "启动服务器",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := globals.ValidateConfig(); err != nil {
			return err
		}
		globals.NormalizePath()

		bootstrap.Init()
		server.Run()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(serverCmd)
}
