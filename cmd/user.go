package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/auth"
	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gorm.io/gorm"
)

// userCmd 本地认证用户管理（auth.mode=local 时使用）
var userCmd = &cobra.Command{
	Use:   "user",
	Short: "本地认证用户管理（auth.mode=local 时使用）",
	Long: `本地认证用户管理（auth.mode=local 时使用）。

子命令:
  add <用户名>        创建用户（交互输入密码，可加 --admin 直接设为管理员）
  list                列出所有本地用户
  delete <用户名>     删除用户
  rename <旧用户名> <新用户名>  修改用户名（用户 ID 不变，关联不受影响）
  set-admin <用户名>  设为管理员（--remove 取消管理员）
  change-password <用户名>  修改密码
`,
	// 无子命令时显示帮助
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

func init() {
	rootCmd.AddCommand(userCmd)
	userCmd.AddCommand(
		userAddCmd, userListCmd, userDeleteCmd, userRenameCmd,
		userSetAdminCmd, userChangePasswordCmd,
	)
}

// initUserCmdEnv 用户管理命令的最小初始化（配置 + 日志 + 数据库）
func initUserCmdEnv() error {
	if err := globals.ValidateConfig(); err != nil {
		return err
	}
	globals.NormalizePath()
	config.Init()
	llog.Init()
	db.Init()
	return nil
}

// stdinReader 非终端（管道）输入时的共享读取器：
// 必须复用同一个 reader，否则 bufio 预读缓冲会吞掉后续行
var stdinReader *bufio.Reader

// promptPassword 交互式输入密码（终端不回显），两次输入一致才返回
func promptPassword(prompt string) (string, error) {
	fmt.Print(prompt)
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		// 非终端环境（管道输入）降级为普通行读取
		if stdinReader == nil {
			stdinReader = bufio.NewReader(os.Stdin)
		}
		line, rerr := stdinReader.ReadString('\n')
		if rerr != nil {
			return "", fmt.Errorf("读取密码失败: %w", rerr)
		}
		return strings.TrimSpace(line), nil
	}
	return string(pw), nil
}

// readPasswordTwice 两次输入密码并校验一致性与长度
func readPasswordTwice() (string, error) {
	pw1, err := promptPassword("密码: ")
	if err != nil {
		return "", err
	}
	if len(pw1) < 6 {
		return "", fmt.Errorf("密码长度至少 6 位")
	}
	pw2, err := promptPassword("再次输入密码: ")
	if err != nil {
		return "", err
	}
	if pw1 != pw2 {
		return "", fmt.Errorf("两次输入的密码不一致")
	}
	return pw1, nil
}

// derefStr2 安全解引用 string 指针
func derefStr2(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// validateUsername 校验用户名：非空且不含空白字符
func validateUsername(username string) error {
	if username == "" || strings.ContainsAny(username, " 	\r\n") {
		return fmt.Errorf("用户名不能为空且不能包含空格")
	}
	return nil
}

var userAddCmd = &cobra.Command{
	Use:   "add <用户名>",
	Short: "创建用户（交互输入密码）",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initUserCmdEnv(); err != nil {
			return err
		}
		username := strings.TrimSpace(args[0])
		if err := validateUsername(username); err != nil {
			return err
		}

		// 检查是否已存在
		_, err := db.GetUserByUsername(username)
		if err == nil {
			return fmt.Errorf("用户 %s 已存在", username)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("查询用户失败: %w", err)
		}

		password, err := readPasswordTwice()
		if err != nil {
			return err
		}
		hash, err := auth.HashPassword(password)
		if err != nil {
			return fmt.Errorf("密码哈希失败: %w", err)
		}

		userID := uuid.New().String()
		user := &native.UserORM{
			UserId:       &userID,
			Username:     &username,
			PasswordHash: &hash,
		}
		if err := db.CreateUser(user); err != nil {
			return fmt.Errorf("创建用户失败: %w", err)
		}

		isAdmin, _ := cmd.Flags().GetBool("admin")
		if isAdmin {
			if err := db.AddAdminUser(userID); err != nil {
				return fmt.Errorf("用户创建成功但设置管理员失败: %w", err)
			}
		}

		adminFlag := ""
		if isAdmin {
			adminFlag = "（已设为管理员）"
		}
		fmt.Printf("✅ 用户 %s 创建成功，user_id=%s%s\n", username, userID, adminFlag)
		fmt.Println("提示：将 config.yaml 的 auth.mode 设为 local 后，该用户即可通过用户名密码登录")
		return nil
	},
}

var userListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有本地用户",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initUserCmdEnv(); err != nil {
			return err
		}
		users, err := db.ListUsers()
		if err != nil {
			return fmt.Errorf("查询用户列表失败: %w", err)
		}
		if len(users) == 0 {
			fmt.Println("暂无本地用户")
			return nil
		}
		fmt.Printf("%-40s %-20s %-30s %s\n", "USER_ID", "USERNAME", "EMAIL", "ADMIN")
		for _, u := range users {
			admin := "否"
			if db.IsAdmin(derefStr2(u.UserId)) {
				admin = "是"
			}
			fmt.Printf("%-40s %-20s %-30s %s\n", derefStr2(u.UserId), derefStr2(u.Username), derefStr2(u.Email), admin)
		}
		return nil
	},
}

var userDeleteCmd = &cobra.Command{
	Use:   "delete <用户名>",
	Short: "删除用户",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initUserCmdEnv(); err != nil {
			return err
		}
		username := args[0]
		user, err := db.GetUserByUsername(username)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("用户 %s 不存在", username)
			}
			return fmt.Errorf("查询用户失败: %w", err)
		}
		userID := derefStr2(user.UserId)

		if err := db.DeleteUser(userID); err != nil {
			return fmt.Errorf("删除用户失败: %w", err)
		}
		// 清理管理员标记
		_ = db.RemoveAdminUser(userID)

		fmt.Printf("✅ 用户 %s 已删除（user_id=%s）\n", username, userID)
		fmt.Println("注意：该用户的 UserSpace（网盘根目录）与文件数据未删除，如需清理请联系管理员操作")
		return nil
	},
}

var userRenameCmd = &cobra.Command{
	Use:   "rename <旧用户名> <新用户名>",
	Short: "修改用户名（用户 ID 不变，关联不受影响）",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initUserCmdEnv(); err != nil {
			return err
		}
		oldUsername := strings.TrimSpace(args[0])
		newUsername := strings.TrimSpace(args[1])
		if err := validateUsername(newUsername); err != nil {
			return err
		}
		if oldUsername == newUsername {
			return fmt.Errorf("新旧用户名相同")
		}

		// 旧用户必须存在
		user, err := db.GetUserByUsername(oldUsername)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("用户 %s 不存在", oldUsername)
			}
			return fmt.Errorf("查询用户失败: %w", err)
		}
		userID := derefStr2(user.UserId)

		// 新用户名不能与现有用户冲突
		_, err = db.GetUserByUsername(newUsername)
		if err == nil {
			return fmt.Errorf("用户名 %s 已存在", newUsername)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("查询用户失败: %w", err)
		}

		if err := db.UpdateUserUsername(userID, newUsername); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") ||
				strings.Contains(err.Error(), "duplicate key") {
				return fmt.Errorf("用户名 %s 已存在", newUsername)
			}
			return fmt.Errorf("修改用户名失败: %w", err)
		}

		fmt.Printf("✅ 用户 %s 已改名为 %s（user_id=%s，管理员/空间关联不变）\n",
			oldUsername, newUsername, userID)
		return nil
	},
}

var userSetAdminCmd = &cobra.Command{
	Use:   "set-admin <用户名>",
	Short: "设为管理员（--remove 取消管理员）",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initUserCmdEnv(); err != nil {
			return err
		}
		username := args[0]
		user, err := db.GetUserByUsername(username)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("用户 %s 不存在", username)
			}
			return fmt.Errorf("查询用户失败: %w", err)
		}
		userID := derefStr2(user.UserId)

		remove, _ := cmd.Flags().GetBool("remove")
		if remove {
			if err := db.RemoveAdminUser(userID); err != nil {
				return fmt.Errorf("取消管理员失败: %w", err)
			}
			fmt.Printf("✅ 用户 %s 已取消管理员\n", username)
		} else {
			if err := db.AddAdminUser(userID); err != nil {
				return fmt.Errorf("设置管理员失败: %w", err)
			}
			fmt.Printf("✅ 用户 %s 已设为管理员\n", username)
		}
		return nil
	},
}

var userChangePasswordCmd = &cobra.Command{
	Use:   "change-password <用户名>",
	Short: "修改密码",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initUserCmdEnv(); err != nil {
			return err
		}
		username := args[0]
		user, err := db.GetUserByUsername(username)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("用户 %s 不存在", username)
			}
			return fmt.Errorf("查询用户失败: %w", err)
		}

		password, err := readPasswordTwice()
		if err != nil {
			return err
		}
		hash, err := auth.HashPassword(password)
		if err != nil {
			return fmt.Errorf("密码哈希失败: %w", err)
		}
		if err := db.UpdateUserPassword(derefStr2(user.UserId), hash); err != nil {
			return fmt.Errorf("更新密码失败: %w", err)
		}
		fmt.Printf("✅ 用户 %s 密码已更新\n", username)
		return nil
	},
}

func init() {
	userAddCmd.Flags().Bool("admin", false, "创建后直接设为管理员")
	userSetAdminCmd.Flags().Bool("remove", false, "取消管理员")
}
