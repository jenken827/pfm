package plugin

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/jenken827/pfm/pkg/llog"
)

// handshakePrefix 插件启动后打印的握手前缀
const handshakePrefix = "PFM-PLUGIN|"

// process 一个已拉起并完成握手的插件进程
type process struct {
	cmd  *exec.Cmd
	cli  *client
	addr string
	pid  int
}

// startProcess 拉起插件、等待握手、建立 RPC 客户端。
// expectABI 是该插件类型（kind）对应的协议版本，握手行里的版本必须与它一致。
func startProcess(execPath, driverID string, expectABI uint32, timeout time.Duration) (*process, error) {
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(execPath)
	cmd.Env = append(os.Environ(), tokenEnv+"="+token)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("接管插件 stdout 失败: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("接管插件 stderr 失败: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动插件 %s 失败: %w", execPath, err)
	}

	reader := bufio.NewReader(stdout)
	type handshakeResult struct {
		addr string
		err  error
	}
	done := make(chan handshakeResult, 1)
	go func() {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			done <- handshakeResult{err: readErr}
			return
		}
		addr, parseErr := parseHandshake(strings.TrimSpace(line), expectABI)
		done <- handshakeResult{addr: addr, err: parseErr}
	}()

	select {
	case result := <-done:
		if result.err != nil {
			killProcess(cmd)
			return nil, fmt.Errorf("插件 %s 握手失败: %w", driverID, result.err)
		}
		go drainLogs(reader, driverID, "stdout")
		go drainLogs(bufio.NewReader(stderr), driverID, "stderr")
		return &process{cmd: cmd, cli: newClient(result.addr, token), addr: result.addr, pid: cmd.Process.Pid}, nil
	case <-time.After(timeout):
		killProcess(cmd)
		return nil, fmt.Errorf("插件 %s 在 %s 内未完成握手", driverID, timeout)
	}
}

// parseHandshake 解析 PFM-PLUGIN|<abi>|127.0.0.1:port|pid
func parseHandshake(line string, expectABI uint32) (string, error) {
	if !strings.HasPrefix(line, handshakePrefix) {
		return "", fmt.Errorf("非法的握手行 %q", line)
	}
	parts := strings.Split(strings.TrimPrefix(line, handshakePrefix), "|")
	if len(parts) < 3 {
		return "", fmt.Errorf("握手行字段不足: %q", line)
	}
	version, err := strconv.Atoi(parts[0])
	if err != nil {
		return "", fmt.Errorf("握手中的协议版本非法: %q", parts[0])
	}
	if uint32(version) != expectABI {
		return "", fmt.Errorf("插件协议版本 %d 与宿主 %d 不兼容", version, expectABI)
	}
	if parts[1] == "" {
		return "", fmt.Errorf("握手行缺少监听地址")
	}
	return parts[1], nil
}

// drainLogs 把插件输出转发到宿主日志（每条一行，带驱动身份）
func drainLogs(reader *bufio.Reader, driverID, stream string) {
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			llog.L().Infof("[plugin %s %s] %s", driverID, stream, strings.TrimRight(line, "\n"))
		}
		if err != nil {
			return
		}
	}
}

// Close 关闭插件：先尝试 Close RPC，再结束进程（不阻塞过久）
func (p *process) Close() {
	killProcess(p.cmd)
}

func killProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()
}

// newToken 生成一次性握手 token
func newToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成插件 token 失败: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// tokenEnv 宿主下发 token 的环境变量名（与 plugin/sdk.TokenEnv 一致）
const tokenEnv = "PFM_PLUGIN_TOKEN"
