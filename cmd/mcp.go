package cmd

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/globals"
	"github.com/jenken827/pfm/internal/auth"
	"github.com/jenken827/pfm/internal/config"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/mcp"
	"github.com/jenken827/pfm/pkg/llog"
	"github.com/jenken827/pfm/utils"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
)

// mcpCmd MCP 服务管理（令牌签发 / 撤销 / 工具清单）
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "MCP 服务管理（令牌签发 / 撤销 / 工具清单）",
	Long: `MCP 服务管理。

MCP 默认关闭（config.yaml 的 mcp.enabled）。开启后 AI 客户端凭令牌访问 <path>：

  pfm mcp token create --user <用户名> --name <标签> [--read=false] [--write] [--root <文件夹ID>] [--days 90]
  pfm mcp token list [--user <用户名>]
  pfm mcp token revoke <id>
  pfm mcp tools

令牌明文只在 create 时打印一次，库里只存 sha256；写权限还要宿主配置
mcp.allow_write 也打开才生效（配置 + 令牌两个开关是"与"关系）。`,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

// mcpTokenCmd 令牌管理的父命令
var mcpTokenCmd = &cobra.Command{
	Use:   "token",
	Short: "管理 MCP 令牌（签发 / 列出 / 撤销）",
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

var (
	mcpTokenCreateCmd = &cobra.Command{
		Use:   "create",
		Short: "为用户签发一个 MCP 令牌（明文只输出一次）",
		RunE:  runMcpTokenCreate,
	}
	mcpTokenListCmd = &cobra.Command{
		Use:   "list",
		Short: "列出 MCP 令牌（不含明文）",
		RunE:  runMcpTokenList,
	}
	mcpTokenRevokeCmd = &cobra.Command{
		Use:   "revoke <id>",
		Short: "撤销令牌（立即失效）",
		Args:  cobra.ExactArgs(1),
		RunE:  runMcpTokenRevoke,
	}
	mcpToolsCmd = &cobra.Command{
		Use:   "tools",
		Short: "列出当前注册的 MCP 工具（排查用）",
		RunE:  runMcpTools,
	}
)

// create 子命令的参数
var (
	mcpTokenUser  string
	mcpTokenName  string
	mcpTokenRead  bool
	mcpTokenWrite bool
	mcpTokenRoot  string
	mcpTokenDays  int
)

func init() {
	rootCmd.AddCommand(mcpCmd)
	mcpCmd.AddCommand(mcpTokenCmd, mcpToolsCmd)

	mcpTokenCmd.AddCommand(mcpTokenCreateCmd, mcpTokenListCmd, mcpTokenRevokeCmd)

	mcpTokenCreateCmd.Flags().StringVar(&mcpTokenUser, "user", "", "归属用户名（必填）")
	mcpTokenCreateCmd.Flags().StringVar(&mcpTokenName, "name", "", "客户端标签（如 laptop / cursor）")
	mcpTokenCreateCmd.Flags().BoolVar(&mcpTokenRead, "read", true, "允许读工具（--read=false 关闭）")
	mcpTokenCreateCmd.Flags().BoolVar(&mcpTokenWrite, "write", false, "允许写工具（还需配置 mcp.allow_write）")
	mcpTokenCreateCmd.Flags().StringVar(&mcpTokenRoot, "root", "", "可见根目录 file_id（留空 = 用户根目录）")
	mcpTokenCreateCmd.Flags().IntVar(&mcpTokenDays, "days", 0, "有效天数（0 = 不过期）")
	_ = mcpTokenCreateCmd.MarkFlagRequired("user")

	mcpTokenListCmd.Flags().StringVar(&mcpTokenUser, "user", "", "只看某个用户的令牌")
}

// initMcpCmdEnv 命令的最小初始化（配置 + 日志 + 数据库）
func initMcpCmdEnv() error {
	if err := globals.ValidateConfig(); err != nil {
		return err
	}
	globals.NormalizePath()
	config.Init()
	llog.Init()
	db.Init()
	return nil
}

// runMcpTokenCreate 签发令牌
func runMcpTokenCreate(cmd *cobra.Command, args []string) error {
	if err := initMcpCmdEnv(); err != nil {
		return err
	}
	username := strings.TrimSpace(mcpTokenUser)
	if username == "" {
		return errors.New("--user 不能为空")
	}
	user, err := db.GetUserByUsername(username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("用户 %q 不存在（先用 pfm user add 创建）", username)
		}
		return fmt.Errorf("查询用户失败: %w", err)
	}
	ownerID := utils.DerefString(user.UserId)
	if ownerID == "" {
		return fmt.Errorf("用户 %q 没有 user_id，数据异常", username)
	}

	rootID := strings.TrimSpace(mcpTokenRoot)
	if rootID != "" {
		if err := checkMcpRootFolder(rootID, ownerID); err != nil {
			return err
		}
	}

	plain, hash, err := auth.GenerateMcpToken()
	if err != nil {
		return err
	}
	label := strings.TrimSpace(mcpTokenName)
	if label == "" {
		label = "default"
	}
	token := &entity.McpTokenORM{
		OwnerId:      proto.String(ownerID),
		Name:         proto.String(label),
		TokenHash:    proto.String(hash),
		AllowRead:    proto.Bool(mcpTokenRead),
		AllowWrite:   proto.Bool(mcpTokenWrite),
		RootFolderId: proto.String(rootID),
	}
	if mcpTokenDays > 0 {
		expireAt := time.Now().Add(time.Duration(mcpTokenDays) * 24 * time.Hour)
		token.ExpiresAt = &expireAt
	}
	if err := db.CreateMcpToken(token); err != nil {
		return fmt.Errorf("保存令牌失败: %w", err)
	}

	// stdout 只放令牌明文，方便 `pfm mcp token create ... > token.txt`；说明走 stderr
	fmt.Println(plain)
	fmt.Fprintf(os.Stderr, "已为 %s 签发令牌 #%d（%s）：读=%v 写=%v 根目录=%s 过期=%s\n",
		username, utils.DerefUint32(token.Id), label, mcpTokenRead, mcpTokenWrite,
		emptyAsRoot(rootID), describeExpire(token.ExpiresAt))
	fmt.Fprintln(os.Stderr, "令牌只显示这一次，请立刻保存；写权限还需 config.yaml 的 mcp.allow_write=true。")
	return nil
}

// checkMcpRootFolder 校验可见根目录存在、属于该用户、且是目录
func checkMcpRootFolder(rootID, ownerID string) error {
	folder, err := db.GetFileByID(rootID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("根目录 %s 不存在", rootID)
		}
		return fmt.Errorf("查询根目录失败: %w", err)
	}
	if utils.DerefString(folder.OwnerId) != ownerID {
		return fmt.Errorf("根目录 %s 不属于该用户", rootID)
	}
	if utils.DerefInt32(folder.Type) != int32(native.FileType_FOLDER) {
		return fmt.Errorf("%s 不是目录", rootID)
	}
	return nil
}

// runMcpTokenList 列出令牌
func runMcpTokenList(cmd *cobra.Command, args []string) error {
	if err := initMcpCmdEnv(); err != nil {
		return err
	}
	ownerID := ""
	if strings.TrimSpace(mcpTokenUser) != "" {
		user, err := db.GetUserByUsername(strings.TrimSpace(mcpTokenUser))
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("用户 %q 不存在", mcpTokenUser)
			}
			return fmt.Errorf("查询用户失败: %w", err)
		}
		ownerID = utils.DerefString(user.UserId)
	}
	tokens, err := db.ListMcpTokens(ownerID)
	if err != nil {
		return fmt.Errorf("查询令牌失败: %w", err)
	}
	if len(tokens) == 0 {
		fmt.Fprintln(os.Stderr, "没有令牌")
		return nil
	}
	fmt.Printf("%-6s %-10s %-10s %-6s %-6s %-38s %-22s %-22s\n",
		"ID", "用户", "标签", "读", "写", "可见根目录", "过期", "最后使用")
	for _, token := range tokens {
		fmt.Printf("%-6d %-10s %-10s %-6v %-6v %-38s %-22s %-22s\n",
			utils.DerefUint32(token.Id), shortUser(utils.DerefString(token.OwnerId)), utils.DerefString(token.Name),
			utils.DerefBool(token.AllowRead), utils.DerefBool(token.AllowWrite),
			emptyAsRoot(utils.DerefString(token.RootFolderId)),
			describeExpire(token.ExpiresAt),
			describeTime(token.LastUsedAt, "从未"),
		)
	}
	return nil
}

// runMcpTokenRevoke 撤销令牌
func runMcpTokenRevoke(cmd *cobra.Command, args []string) error {
	if err := initMcpCmdEnv(); err != nil {
		return err
	}
	id, err := strconv.ParseUint(strings.TrimSpace(args[0]), 10, 32)
	if err != nil {
		return fmt.Errorf("令牌 id 必须是数字: %q", args[0])
	}
	affected, err := db.DeleteMcpToken(uint32(id))
	if err != nil {
		return fmt.Errorf("撤销令牌失败: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("令牌 #%d 不存在", id)
	}
	fmt.Printf("已撤销令牌 #%d\n", id)
	return nil
}

// runMcpTools 打印当前注册的工具（不按权限过滤，排查"工具没出现"用）
func runMcpTools(cmd *cobra.Command, args []string) error {
	globals.NormalizePath()
	config.Init()
	server := mcp.NewServer(mcp.LoadSettings())
	tools := server.DescribeTools()
	fmt.Printf("%-20s %-6s %-6s %s\n", "NAME", "类型", "来源", "说明")
	for _, tool := range tools {
		kind := "write"
		if readOnly, _ := tool["readOnly"].(bool); readOnly {
			kind = "read"
		}
		source, _ := tool["source"].(string)
		description := truncateRunes(tool["description"], 60)
		name, _ := tool["name"].(string)
		fmt.Printf("%-20s %-6s %-6s %s\n", name, kind, source, description)
	}
	fmt.Fprintf(os.Stderr, "共 %d 个工具；路由 %s（enabled=%v，allow_write=%v）\n",
		len(tools), mcp.EndpointPath(), config.GetConfig().Mcp.Enabled, config.GetConfig().Mcp.AllowWrite)
	return nil
}

// truncateRunes 按字符（不是字节）截断，避免把中文切成乱码
func truncateRunes(value any, limit int) string {
	text, _ := value.(string)
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

// emptyAsRoot 空根目录显示成"用户根目录"
func emptyAsRoot(rootID string) string {
	if strings.TrimSpace(rootID) == "" {
		return "用户根目录"
	}
	return rootID
}

// shortUser 用户 UUID 太长，列表里只显示前 8 位
func shortUser(userID string) string {
	if len(userID) <= 8 {
		return userID
	}
	return userID[:8]
}

// describeExpire 过期时间的可读描述
func describeExpire(expireAt *time.Time) string {
	if expireAt == nil {
		return "不过期"
	}
	return expireAt.Format("2006-01-02 15:04")
}

// describeTime 通用时间描述
func describeTime(value *time.Time, fallback string) string {
	if value == nil {
		return fallback
	}
	return value.Format("2006-01-02 15:04")
}
