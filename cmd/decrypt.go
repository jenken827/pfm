package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/storage"
)

// decryptCmd 按 file_locations id 解密本地加密文件
// 用于排查加密文件问题：手动下载密文后，用 DB 里的盐 + 提供者密钥解密验证内容
var decryptCmd = &cobra.Command{
	Use:   "decrypt <密文文件路径>",
	Short: "按 file_locations id 解密本地加密文件",
	Long: `按 file_locations id 查询加密盐与存储提供者密钥，解密已手动下载到本地的加密文件。

示例:
  pfm -d ./data decrypt -l 32942 ./file_57.bin              # 输出到 file_57.bin.dec
  pfm -d ./data decrypt -l 32942 ./file_57.bin -o out.jpg   # 指定输出文件
`,
	Args: cobra.ExactArgs(1),
	// 错误时只输出错误信息，不重复打印 usage
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		locID, _ := cmd.Flags().GetUint64("location")
		if locID == 0 {
			return fmt.Errorf("必须指定 --location <file_locations id>")
		}
		cipherPath := args[0]

		// 最小初始化：配置 + 日志 + 数据库（不启动完整服务）
		if err := globals.ValidateConfig(); err != nil {
			return err
		}
		globals.NormalizePath()
		config.Init()
		llog.Init()
		db.Init()

		// 1. 查询 file_locations
		loc, err := db.GetFileLocationByID(locID)
		if err != nil {
			return fmt.Errorf("查询 file_locations id=%d 失败: %w", locID, err)
		}
		fmt.Printf("location: id=%d provider_id=%d path=%q\n",
			locID, derefUint32(loc.ProviderId), derefStr(loc.Path))

		// 2. 校验加密状态与盐
		if loc.Encrypted == nil || !*loc.Encrypted {
			return fmt.Errorf("该 location 未标记加密（encrypted=false），无需解密")
		}
		salt := derefStr(loc.EncryptionSalt)
		if salt == "" {
			return fmt.Errorf("该 location 没有 encryption_salt，无法解密")
		}

		// 3. 查询存储提供者密钥
		providerID := derefUint32(loc.ProviderId)
		provider, err := db.GetProviderByID(providerID)
		if err != nil {
			return fmt.Errorf("查询存储提供者 id=%d 失败: %w", providerID, err)
		}
		key := derefStr(provider.EncryptionKey)
		if key == "" {
			return fmt.Errorf("存储提供者 id=%d 未配置 encryption_key", providerID)
		}
		fmt.Printf("provider: id=%d name=%q encryption_key_len=%d\n",
			providerID, derefStr(provider.Name), len(key))

		// 4. 校验密文文件大小（与 location.size 对比，提示文件是否完整）
		cipherSize, err := fileSize(cipherPath)
		if err != nil {
			return err
		}
		if loc.Size != nil && *loc.Size > 0 && cipherSize != int64(*loc.Size) {
			fmt.Printf("⚠️  密文文件大小 %d 与 location.size=%d 不一致（文件可能不完整/占位）\n",
				cipherSize, *loc.Size)
		}

		// 5. 解密
		enc, err := storage.NewCryptEncryptor(key, salt)
		if err != nil {
			return fmt.Errorf("创建加密器失败: %w", err)
		}

		f, err := os.Open(cipherPath)
		if err != nil {
			return fmt.Errorf("打开密文文件失败: %w", err)
		}
		defer f.Close()

		rc, err := enc.Decrypt(f)
		if err != nil {
			return fmt.Errorf("解密失败: %w", err)
		}
		plain, err := io.ReadAll(rc)
		if err != nil {
			return fmt.Errorf("读取明文失败: %w", err)
		}

		// 6. 输出
		output, _ := cmd.Flags().GetString("output")
		if output == "" {
			output = cipherPath + ".dec"
		}
		if err := os.WriteFile(output, plain, 0644); err != nil {
			return fmt.Errorf("写入输出文件失败: %w", err)
		}

		// 7. 摘要
		head := ""
		if len(plain) >= 4 {
			head = fmt.Sprintf("%02x %02x %02x %02x", plain[0], plain[1], plain[2], plain[3])
		}
		jpegHint := ""
		if len(plain) >= 2 && plain[0] == 0xFF && plain[1] == 0xD8 {
			jpegHint = "（JPEG 图片 ✓）"
		}
		fmt.Printf("解密完成: 密文 %d 字节 → 明文 %d 字节\n", cipherSize, len(plain))
		fmt.Printf("输出文件: %s\n", output)
		fmt.Printf("明文头部: %s%s\n", head, jpegHint)
		return nil
	},
}

func init() {
	decryptCmd.Flags().Uint64P("location", "l", 0, "file_locations id（必填）")
	decryptCmd.Flags().StringP("output", "o", "", "输出文件路径（默认 <密文路径>.dec）")
	rootCmd.AddCommand(decryptCmd)
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefUint32(v *uint32) uint32 {
	if v == nil {
		return 0
	}
	return *v
}

func fileSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("stat 密文文件失败: %w", err)
	}
	return fi.Size(), nil
}
