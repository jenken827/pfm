package transcode

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 转码状态
const (
	// StateQueued 已入队，等空闲槽位
	StateQueued = "queued"
	// StateRunning 正在转码
	StateRunning = "running"
	// StateDone 已就绪（调用方应改播缓存文件）
	StateDone = "done"
	// StateFailed 失败（Error 里有原因）
	StateFailed = "failed"
)

// Status 一次转码请求的快照
type Status struct {
	State    string `json:"state"`
	Progress int    `json:"progress"` // 0-100（未知时长时为 0，直到完成）
	Error    string `json:"error,omitempty"`
}

// buildArgs 组装 ffmpeg 参数
//
// 目标：一段能在弱网（2~4Mbps）上稳定播放的 H.264/AAC MP4。
//   - scale=-2:min(N,ih)：长边不超过 N，宽度按比例取偶数（-2），不放大源
//   - capped CRF：crf 保画质，maxrate/bufsize 兜住码率上限
//   - yuv420p：输出 8bit 4:2:0 limited range，兼容性最好
//     （手机拍的 yuvj420p 全范围素材在部分设备上会退化成软解，转一遍顺带修掉）
//   - +faststart：moov 前置，边下边播
func buildArgs(src, dst string, cfg Config) []string {
	return []string{
		"-hide_banner", "-nostdin", "-y",
		"-i", src,
		"-map", "0:v:0",
		"-map", "0:a:0?",
		"-vf", fmt.Sprintf("scale=-2:min(%d\\,ih)", cfg.MaxHeight),
		"-c:v", "libx264",
		"-preset", cfg.Preset,
		"-crf", "23",
		"-maxrate", fmt.Sprintf("%dk", cfg.VideoKbps),
		"-bufsize", fmt.Sprintf("%dk", cfg.VideoKbps*2),
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", fmt.Sprintf("%dk", cfg.AudioKbps),
		"-ac", "2",
		"-movflags", "+faststart",
		"-progress", "pipe:1",
		"-nostats",
		// 输出先写 *.mp4.part 再 rename，扩展名不是 .mp4，
		// 必须显式指定封装格式，否则 ffmpeg 报「Unable to find a suitable output format」
		"-f", "mp4",
		dst,
	}
}

// parseProgressLine 解析 ffmpeg `-progress pipe:1` 的一行，返回已输出时长与是否结束。
//
// ffmpeg 的输出形如：
//
//	frame=123
//	out_time_us=5120000
//	out_time=00:00:05.120000
//	progress=continue
//
// 注意 out_time_ms 在 ffmpeg 里实际是微秒（历史遗留），所以优先用 out_time_us，
// 没有就退回解析 out_time 文本，避免算错进度。
func parseProgressLine(line string) (elapsed time.Duration, done bool, ok bool) {
	line = strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(line, "out_time_us="):
		us, err := strconv.ParseInt(strings.TrimPrefix(line, "out_time_us="), 10, 64)
		if err != nil || us < 0 {
			return 0, false, false
		}
		return time.Duration(us) * time.Microsecond, false, true
	case strings.HasPrefix(line, "out_time="):
		value := strings.TrimPrefix(line, "out_time=")
		d, err := parseClock(value)
		if err != nil {
			return 0, false, false
		}
		return d, false, true
	case strings.HasPrefix(line, "progress="):
		return 0, strings.TrimPrefix(line, "progress=") == "end", true
	}
	return 0, false, false
}

// parseClock 解析 HH:MM:SS.ffffff
func parseClock(value string) (time.Duration, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid clock %q", value)
	}
	hours, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	minutes, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, err
	}
	seconds, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return 0, err
	}
	total := float64(hours*3600+minutes*60) + seconds
	return time.Duration(total * float64(time.Second)), nil
}

// progressPercent 把已输出时长换算成百分比（0-100），总时长未知时返回 0
func progressPercent(elapsed, total time.Duration) int {
	if total <= 0 || elapsed <= 0 {
		return 0
	}
	if elapsed >= total {
		return 99 // 100 留给「转码完成」，避免进度条显示 100% 却还在收尾
	}
	return int(float64(elapsed) / float64(total) * 100)
}
