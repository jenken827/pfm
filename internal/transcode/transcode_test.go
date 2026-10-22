package transcode

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNormalizeConfig(t *testing.T) {
	cases := []struct {
		name string
		in   Config
		want Config
	}{
		{
			name: "全空回落默认",
			in:   Config{},
			want: Config{
				FfmpegPath:  "ffmpeg",
				FfprobePath: "ffprobe",
				MaxHeight:   DefaultMaxHeight,
				VideoKbps:   DefaultVideoKbps,
				AudioKbps:   DefaultAudioKbps,
				Preset:      DefaultPreset,
				MaxJobs:     DefaultMaxJobs,
			},
		},
		{
			name: "超出范围被夹住",
			in:   Config{MaxHeight: 4, VideoKbps: 99999, AudioKbps: 999, Preset: "insane", MaxJobs: 99},
			want: Config{
				FfmpegPath:  "ffmpeg",
				FfprobePath: "ffprobe",
				MaxHeight:   240, // < 240 回落到默认值 720 是另一条分支，这里按默认走
				VideoKbps:   20000,
				AudioKbps:   320,
				Preset:      DefaultPreset,
				MaxJobs:     4,
			},
		},
	}
	// MaxHeight 4 会先被 < 240 判断改写成默认 720
	cases[1].want.MaxHeight = DefaultMaxHeight

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeConfig(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("normalizeConfig = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestBuildArgs(t *testing.T) {
	cfg := normalizeConfig(Config{MaxHeight: 720, VideoKbps: 1500, AudioKbps: 96, Preset: "veryfast"})
	args := strings.Join(buildArgs("in.mp4", "out.mp4", cfg), " ")

	for _, want := range []string{
		"-vf scale=-2:min(720\\,ih)",
		"-c:v libx264",
		"-preset veryfast",
		"-maxrate 1500k",
		"-bufsize 3000k",
		"-pix_fmt yuv420p",
		"-c:a aac",
		"-b:a 96k",
		"-movflags +faststart",
		"-progress pipe:1",
		"-f mp4",
		"out.mp4",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q:\n%s", want, args)
		}
	}
	// 源地址必须在目标之前，否则 ffmpeg 会把输出当成输入
	if strings.Index(args, "in.mp4") > strings.Index(args, "-progress") {
		t.Errorf("source must precede output flags:\n%s", args)
	}
}

func TestParseProgressLine(t *testing.T) {
	cases := []struct {
		line    string
		elapsed time.Duration
		done    bool
		ok      bool
	}{
		{"out_time_us=5120000", 5120 * time.Millisecond, false, true},
		{"out_time=00:00:05.120000", 5120 * time.Millisecond, false, true},
		{"out_time=01:02:03.500000", time.Hour + 2*time.Minute + 3500*time.Millisecond, false, true},
		{"progress=end", 0, true, true},
		{"progress=continue", 0, false, true},
		{"frame=120", 0, false, false},
		{"", 0, false, false},
		{"out_time=N/A", 0, false, false},
	}
	for _, tc := range cases {
		elapsed, done, ok := parseProgressLine(tc.line)
		if ok != tc.ok || done != tc.done || elapsed != tc.elapsed {
			t.Errorf("parseProgressLine(%q) = (%v, %v, %v), want (%v, %v, %v)",
				tc.line, elapsed, done, ok, tc.elapsed, tc.done, tc.ok)
		}
	}
}

func TestProgressPercent(t *testing.T) {
	total := 40 * time.Second
	if got := progressPercent(0, total); got != 0 {
		t.Errorf("progressPercent(0) = %d, want 0", got)
	}
	if got := progressPercent(20*time.Second, total); got != 50 {
		t.Errorf("progressPercent(half) = %d, want 50", got)
	}
	if got := progressPercent(total, total); got != 99 {
		t.Errorf("progressPercent(full) = %d, want 99 (100 留给完成)", got)
	}
	if got := progressPercent(10*time.Second, 0); got != 0 {
		t.Errorf("progressPercent(unknown duration) = %d, want 0", got)
	}
}

func TestManagerKeyAndLookup(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{cfg: Config{MaxHeight: 720, VideoKbps: 1500, Preset: "veryfast"}, cacheDir: dir, jobs: map[string]*job{}}

	key := m.Key("abc123")
	if !strings.HasPrefix(key, "abc123_720p_1500k_veryfast") {
		t.Fatalf("Key = %q, want 参数拼进键（改配置后不复用旧产物）", key)
	}
	if _, ok := m.Lookup("abc123"); ok {
		t.Fatal("Lookup on empty cache should be false")
	}

	// 空文件不算就绪（rename 前的 0 字节不能当结果）
	path := m.CachePath("abc123")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Lookup("abc123"); ok {
		t.Fatal("empty cache file should not be treated as ready")
	}
	if err := os.WriteFile(path, []byte("fake mp4"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := m.Lookup("abc123"); !ok || got != path {
		t.Fatalf("Lookup = (%q, %v), want (%q, true)", got, ok, path)
	}
}

// TestTranscodeRealVideo 真跑一次 ffmpeg：720p 上限要生效、输出要能解码。
// 没装 ffmpeg/ffprobe 的环境直接跳过。
func TestTranscodeRealVideo(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}

	src := filepath.Join(t.TempDir(), "src.mp4")
	// 用 lavfi 造一段 640x480 / 2 秒的素材，不依赖外部文件
	gen := exec.Command("ffmpeg", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=640x480:rate=15:duration=2",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generate fixture: %v\n%s", err, out)
	}

	m := NewManager(Config{MaxHeight: 240, VideoKbps: 300, AudioKbps: 64, MaxJobs: 1}, t.TempDir())
	if m == nil {
		t.Skip("transcode manager disabled")
	}

	// 用内容哈希当键，跟调用方口径一致
	hash := "testfixture"
	status := m.Request(hash, src)
	if status.State != StateQueued && status.State != StateRunning {
		t.Fatalf("Request = %+v, want queued/running", status)
	}

	deadline := time.Now().Add(90 * time.Second)
	for {
		if _, ok := m.Lookup(hash); ok {
			break
		}
		if st := m.Status(hash); st.State == StateFailed {
			t.Fatalf("transcode failed: %s", st.Error)
		}
		if time.Now().After(deadline) {
			t.Fatalf("transcode timeout, last status: %+v", m.Status(hash))
		}
		time.Sleep(100 * time.Millisecond)
	}

	out, err := exec.Command("ffprobe", "-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=height,pix_fmt,codec_name",
		"-of", "default=nw=1", m.CachePath(hash)).CombinedOutput()
	if err != nil {
		t.Fatalf("probe output: %v\n%s", err, out)
	}
	got := string(out)
	if !strings.Contains(got, "height=240") {
		t.Errorf("output height should be capped at 240:\n%s", got)
	}
	if !strings.Contains(got, "pix_fmt=yuv420p") {
		t.Errorf("output should be yuv420p (limited range):\n%s", got)
	}
	if !strings.Contains(got, "codec_name=h264") {
		t.Errorf("output should be h264:\n%s", got)
	}
}
