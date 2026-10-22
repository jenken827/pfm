package thumbnail

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"jenken827.com/jenken827/pfm/internal/models"
)

// writeTestImage 生成一张 800x600 的 PNG，返回路径
func writeTestImage(t *testing.T, dir string) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	path := filepath.Join(dir, "src.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create test image: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode test image: %v", err)
	}
	return path
}

// decodeBounds 读取缩略图尺寸
func decodeBounds(t *testing.T, path string) (int, int) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open thumbnail %s: %v", path, err)
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatalf("decode thumbnail %s: %v", path, err)
	}
	return cfg.Width, cfg.Height
}

func TestConfigureNormalizesSizes(t *testing.T) {
	t.Cleanup(func() { Configure(models.DefaultThumbnailSmallSize, models.DefaultThumbnailLargeSize) })

	cases := []struct {
		name        string
		small       int
		large       int
		wantSmall   int
		wantLarge   int
		wantSizeLen int
	}{
		{"默认值", models.DefaultThumbnailSmallSize, models.DefaultThumbnailLargeSize, 200, 400, 2},
		{"自定义", 320, 640, 320, 640, 2},
		{"零值回落默认", 0, 0, 200, 400, 2},
		{"负数回落默认", -1, -100, 200, 400, 2},
		{"过小收敛到下界", 1, 8, minThumbnailSize, minThumbnailSize, 1},
		{"过大收敛到上界", 99999, 99998, maxThumbnailSize, maxThumbnailSize, 1},
		{"写反自动纠正", 600, 300, 300, 600, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			Configure(c.small, c.large)
			if got := SmallSize(); got.Width != c.wantSmall || got.Height != c.wantSmall {
				t.Fatalf("SmallSize = %+v, want %d", got, c.wantSmall)
			}
			if got := LargeSize(); got.Width != c.wantLarge || got.Height != c.wantLarge {
				t.Fatalf("LargeSize = %+v, want %d", got, c.wantLarge)
			}
			if got := len(Sizes()); got != c.wantSizeLen {
				t.Fatalf("len(Sizes()) = %d, want %d", got, c.wantSizeLen)
			}
		})
	}
}

func TestGenerateAllUsesConfiguredSizes(t *testing.T) {
	t.Cleanup(func() { Configure(models.DefaultThumbnailSmallSize, models.DefaultThumbnailLargeSize) })

	dir := t.TempDir()
	srcPath := writeTestImage(t, dir)
	g := NewGenerator(filepath.Join(dir, "thumbnails"))
	const hash = "deadbeef"

	Configure(300, 600)
	paths, err := g.GenerateAll(srcPath, hash)
	if err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	wantPaths := map[string]bool{
		filepath.Join(dir, "thumbnails", hash+"_300.jpg"): true,
		filepath.Join(dir, "thumbnails", hash+"_600.jpg"): true,
	}
	for size, path := range paths {
		if !wantPaths[path] {
			t.Fatalf("unexpected thumbnail path %s (size %d)", path, size.Width)
		}
		delete(wantPaths, path)
	}
	if len(wantPaths) != 0 {
		t.Fatalf("missing thumbnails: %v", wantPaths)
	}

	// 800x600 原图 -> 600 边长：按宽高比缩到 600x450；300 边长 -> 300x225
	if w, h := decodeBounds(t, paths[ThumbnailSize{Width: 600, Height: 600}]); w != 600 || h != 450 {
		t.Fatalf("600 thumbnail = %dx%d, want 600x450", w, h)
	}
	if w, h := decodeBounds(t, paths[ThumbnailSize{Width: 300, Height: 300}]); w != 300 || h != 225 {
		t.Fatalf("300 thumbnail = %dx%d, want 300x225", w, h)
	}

	// 改尺寸后旧文件仍在，DeleteAllThumbnails 要连旧尺寸一起清掉
	Configure(200, 400)
	if _, err := g.GenerateAll(srcPath, hash); err != nil {
		t.Fatalf("GenerateAll after reconfigure: %v", err)
	}
	if err := g.DeleteAllThumbnails(hash); err != nil {
		t.Fatalf("DeleteAllThumbnails: %v", err)
	}
	left, err := filepath.Glob(filepath.Join(dir, "thumbnails", hash+"_*.jpg"))
	if err != nil {
		t.Fatalf("glob thumbnails: %v", err)
	}
	if len(left) != 0 {
		t.Fatalf("thumbnails not fully deleted: %v", left)
	}
}

func TestGenerateAllSingleSizeWhenEqual(t *testing.T) {
	t.Cleanup(func() { Configure(models.DefaultThumbnailSmallSize, models.DefaultThumbnailLargeSize) })

	dir := t.TempDir()
	srcPath := writeTestImage(t, dir)
	g := NewGenerator(filepath.Join(dir, "thumbnails"))

	Configure(256, 256)
	paths, err := g.GenerateAll(srcPath, "cafe")
	if err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("len(paths) = %d, want 1", len(paths))
	}
}
