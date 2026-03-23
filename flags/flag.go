package flags

import (
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
)

var (
	// DataDir 数据目录
	DataDir string
	// TempDir 临时目录
	TempDir string
	// ConfigPath 配置文件路径
	ConfigPath string
	// Version 版本号
	Version string
	// ShowVersion 显示版本
	ShowVersion bool
)

// Parse 解析命令行参数
func Parse() {
	flag.StringVar(&DataDir, "dir", "", "Data directory")
	flag.StringVar(&DataDir, "d", "", "Data directory (shorthand)")
	flag.StringVar(&ConfigPath, "config", "", "Config file path")
	flag.StringVar(&ConfigPath, "c", "", "Config file path (shorthand)")
	flag.BoolVar(&ShowVersion, "version", false, "Show version")
	flag.BoolVar(&ShowVersion, "v", false, "Show version (shorthand)")

	// Custom usage message
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		fmt.Fprintf(os.Stderr, "  -dir, -d string\tData directory\n")
		fmt.Fprintf(os.Stderr, "  -config, -c string\tConfig file path\n")
		fmt.Fprintf(os.Stderr, "  -version, -v\t\tShow version\n")
		fmt.Fprintf(os.Stderr, "  -h, --help\t\tShow this help message\n")
		fmt.Fprintf(os.Stderr, "\nNote: At least one of -dir or -config must be specified.\n")
	}

	flag.Parse()

	// Check version flag first (highest priority)
	if ShowVersion {
		fmt.Printf("version: %s\n", Version)
		os.Exit(0)
	}

	if err := validateConfig(); err != nil {
		flag.Usage()
		fmt.Fprintf(os.Stderr, "\n%v\n", err)
		os.Exit(1)
	}
	normalizePath()
}

func validateConfig() error {
	if DataDir == "" && ConfigPath == "" {
		return fmt.Errorf("error: at least one of -dir or -config must be specified")
	}
	return nil
}

// normalizePath 规范化路径
func normalizePath() {
	if !filepath.IsAbs(DataDir) {
		wd, err := os.Getwd()
		if err != nil {
			panic(err)
		}
		DataDir = path.Join(wd, DataDir)
		if _, err := os.Stat(DataDir); os.IsNotExist(err) {
			er := os.MkdirAll(DataDir, 0755)
			if er != nil {
				panic(er)
			}
		}

	}
	if !filepath.IsAbs(ConfigPath) {
		ConfigPath = path.Join(DataDir, "config.yaml")
	}
	//创建临时文件夹
	tempDir := filepath.Join(DataDir, ".temp")
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		os.MkdirAll(tempDir, 0755)
	}
	TempDir = tempDir
}
