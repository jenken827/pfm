package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"jenken827.com/jenken827/pfm/globals"
)

// versionCmd 显示版本信息
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示版本信息",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("pfm version: %s\n", globals.Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
