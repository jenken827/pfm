package transcode

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/utils"
)

// Config 转码配置（已归一化）
type Config struct {
	FfmpegPath  string
	FfprobePath string
	MaxHeight   int
	VideoKbps   int
	AudioKbps   int
	Preset      string
	MaxJobs     int
}

// 默认值（与 models.TranscodeConfig 的默认一致）
const (
	DefaultMaxHeight = 720
	DefaultVideoKbps = 1500
	DefaultAudioKbps = 96
	DefaultPreset    = "veryfast"
	DefaultMaxJobs   = 1
)

// ConfigFromModel 把配置文件的 transcode 段转成内部 Config
func ConfigFromModel(cfg models.TranscodeConfig) Config {
	return Config{
		FfmpegPath:  cfg.FfmpegPath,
		FfprobePath: cfg.FfprobePath,
		MaxHeight:   cfg.MaxHeight,
		VideoKbps:   cfg.VideoKbps,
		AudioKbps:   cfg.AudioKbps,
		Preset:      cfg.Preset,
		MaxJobs:     cfg.MaxJobs,
	}
}

// 缓存文件放在数据目录下的哪个子目录
const cacheSubdir = "transcodes"

// job 一次转码任务
type job struct {
	key    string
	source string

	mu       sync.Mutex
	state    string
	progress int
	err      string
}

func (j *job) status() Status {
	j.mu.Lock()
	defer j.mu.Unlock()
	return Status{State: j.state, Progress: j.progress, Error: j.err}
}

// Manager 管理转码任务与磁盘缓存
type Manager struct {
	cfg      Config
	cacheDir string

	mu      sync.Mutex
	jobs    map[string]*job
	queue   []*job
	running int
}

var (
	defaultMu      sync.RWMutex
	defaultManager *Manager
)

// Init 按配置初始化全局 Manager（ffmpeg 不可用时禁用并记日志）
func Init(cfg Config, cacheDir string) {
	m := NewManager(cfg, cacheDir)
	defaultMu.Lock()
	defaultManager = m
	defaultMu.Unlock()

	// 上次进程被杀时可能留下半成品，启动时清掉
	if m != nil {
		m.cleanupPartials()
		llog.L().Infof("transcode enabled: %dp / %dkbps / preset=%s / jobs=%d / cache=%s",
			m.cfg.MaxHeight, m.cfg.VideoKbps, m.cfg.Preset, m.cfg.MaxJobs, m.cacheDir)
	}
}

// Default 返回全局 Manager，未启用时为 nil
func Default() *Manager {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultManager
}

// NewManager 创建 Manager；ffmpeg 不存在时返回 nil
func NewManager(cfg Config, cacheDir string) *Manager {
	cfg = normalizeConfig(cfg)
	path, err := exec.LookPath(cfg.FfmpegPath)
	if err != nil {
		llog.L().Warnf("transcode disabled: ffmpeg not found: %v", err)
		return nil
	}
	cfg.FfmpegPath = path
	if probe, err := exec.LookPath(cfg.FfprobePath); err == nil {
		cfg.FfprobePath = probe
	} else {
		// 没 ffprobe 也能转，只是进度条不准
		llog.L().Warnf("transcode: ffprobe not found (%v), progress will be coarse", err)
		cfg.FfprobePath = ""
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		llog.L().Warnf("transcode disabled: create cache dir %s: %v", cacheDir, err)
		return nil
	}
	return &Manager{cfg: cfg, cacheDir: cacheDir, jobs: make(map[string]*job)}
}

// normalizeConfig 收敛到安全范围，避免配置写错把机器打挂
func normalizeConfig(cfg Config) Config {
	if cfg.MaxHeight < 240 {
		cfg.MaxHeight = DefaultMaxHeight
	}
	if cfg.MaxHeight > 2160 {
		cfg.MaxHeight = 2160
	}
	if cfg.VideoKbps < 200 {
		cfg.VideoKbps = DefaultVideoKbps
	}
	if cfg.VideoKbps > 20000 {
		cfg.VideoKbps = 20000
	}
	if cfg.AudioKbps < 32 {
		cfg.AudioKbps = DefaultAudioKbps
	}
	if cfg.AudioKbps > 320 {
		cfg.AudioKbps = 320
	}
	switch cfg.Preset {
	case "ultrafast", "superfast", "veryfast", "faster", "fast", "medium", "slow":
	default:
		cfg.Preset = DefaultPreset
	}
	if cfg.MaxJobs < 1 {
		cfg.MaxJobs = DefaultMaxJobs
	}
	if cfg.MaxJobs > 4 {
		cfg.MaxJobs = 4
	}
	if cfg.FfmpegPath == "" {
		cfg.FfmpegPath = "ffmpeg"
	}
	if cfg.FfprobePath == "" {
		cfg.FfprobePath = "ffprobe"
	}
	return cfg
}

// Config 返回归一化后的配置
func (m *Manager) Config() Config { return m.cfg }

// Key 缓存键：同一份内容 + 同一套参数共享一份转码结果
func (m *Manager) Key(hash string) string {
	return fmt.Sprintf("%s_%dp_%dk_%s", hash, m.cfg.MaxHeight, m.cfg.VideoKbps, m.cfg.Preset)
}

// CachePath 缓存文件路径
func (m *Manager) CachePath(hash string) string {
	return filepath.Join(m.cacheDir, m.Key(hash)+".mp4")
}

// Lookup 已就绪的缓存文件
func (m *Manager) Lookup(hash string) (string, bool) {
	path := m.CachePath(hash)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return "", false
	}
	return path, true
}

// Status 当前任务状态（没有任务时返回空状态）
func (m *Manager) Status(hash string) Status {
	m.mu.Lock()
	j, ok := m.jobs[m.Key(hash)]
	m.mu.Unlock()
	if !ok {
		return Status{State: ""}
	}
	return j.status()
}

// Request 请求转码。已就绪返回 done；在跑返回 running；排队返回 queued。
func (m *Manager) Request(hash, source string) Status {
	key := m.Key(hash)
	if _, ok := m.Lookup(hash); ok {
		return Status{State: StateDone, Progress: 100}
	}

	m.mu.Lock()
	if j, ok := m.jobs[key]; ok {
		m.mu.Unlock()
		return j.status()
	}
	j := &job{key: key, source: source, state: StateQueued}
	m.jobs[key] = j
	m.queue = append(m.queue, j)
	m.mu.Unlock()

	m.dispatch()
	return j.status()
}

// dispatch 把排队任务交给空闲槽位
func (m *Manager) dispatch() {
	m.mu.Lock()
	for m.running < m.cfg.MaxJobs && len(m.queue) > 0 {
		j := m.queue[0]
		m.queue = m.queue[1:]
		m.running++
		j.mu.Lock()
		j.state = StateRunning
		j.mu.Unlock()
		utils.Go(func() { m.run(j) })
	}
	m.mu.Unlock()
}

// run 执行一次转码：probe 时长 → ffmpeg → 原子 rename
func (m *Manager) run(j *job) {
	defer func() {
		if r := recover(); r != nil {
			llog.L().Errorf("transcode panic: %v", r)
			m.finish(j, StateFailed, fmt.Sprintf("panic: %v", r))
		}
		m.mu.Lock()
		m.running--
		m.mu.Unlock()
		m.dispatch()
	}()

	dst := filepath.Join(m.cacheDir, j.key+".mp4")
	part := dst + ".part"
	_ = os.Remove(part)
	defer func() { _ = os.Remove(part) }() // 失败时清掉半成品

	duration := m.probeDuration(j.source)
	started := time.Now()
	if err := m.transcode(j, j.source, part, duration); err != nil {
		llog.L().Errorf("transcode failed: %s: %v", j.key, err)
		m.finish(j, StateFailed, err.Error())
		return
	}
	if err := os.Rename(part, dst); err != nil {
		m.finish(j, StateFailed, fmt.Sprintf("rename: %v", err))
		return
	}
	llog.L().Infof("transcode done: %s in %s", j.key, time.Since(started).Round(time.Second))
	m.finish(j, StateDone, "")
}

func (m *Manager) finish(j *job, state, errMsg string) {
	j.mu.Lock()
	j.state = state
	j.err = errMsg
	if state == StateDone {
		j.progress = 100
	}
	j.mu.Unlock()
}

// transcode 跑 ffmpeg，边跑边把 out_time 换算成百分比
func (m *Manager) transcode(j *job, src, dst string, duration time.Duration) error {
	cmd := exec.Command(m.cfg.FfmpegPath, buildArgs(src, dst, m.cfg)...)
	stderr := &tailBuffer{limit: 4096}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		elapsed, done, ok := parseProgressLine(scanner.Text())
		if !ok {
			continue
		}
		if done {
			break
		}
		if elapsed > 0 {
			j.mu.Lock()
			j.progress = progressPercent(elapsed, duration)
			j.mu.Unlock()
		}
	}
	_, _ = io.Copy(io.Discard, stdout)

	if err := cmd.Wait(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return err
		}
		return fmt.Errorf("%v: %s", err, lastLines(msg, 3))
	}
	return nil
}

// probeDuration 用 ffprobe 读总时长（拿不到返回 0，进度就退化成 0 直到完成）
func (m *Manager) probeDuration(src string) time.Duration {
	if m.cfg.FfprobePath == "" {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, m.cfg.FfprobePath,
		"-v", "error", "-show_entries", "format=duration",
		"-of", "default=nw=1:nk=1", src).Output()
	if err != nil {
		llog.L().Warnf("transcode: ffprobe duration failed: %v", err)
		return 0
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}

// cleanupPartials 清掉上次残留的半成品
func (m *Manager) cleanupPartials() {
	matches, err := filepath.Glob(filepath.Join(m.cacheDir, "*.part"))
	if err != nil {
		return
	}
	for _, path := range matches {
		_ = os.Remove(path)
	}
}

// tailBuffer 只保留最后 limit 字节，用来把 ffmpeg 的错误尾巴带进日志
type tailBuffer struct {
	buf   []byte
	limit int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.limit {
		b.buf = b.buf[len(b.buf)-b.limit:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string { return string(b.buf) }

// lastLines 取最后 n 行非空内容
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}
