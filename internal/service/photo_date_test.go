package service

import (
	"testing"
	"time"
)

func TestParsePhotoDateFromFilename(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		want     string // "2006-01-02 15:04:05"，空表示期望 nil
	}{
		// 规则1：毫秒时间戳命名
		{"毫秒时间戳", "1784212534296.png", "2026-07-16 22:35:34"},
		// 规则2/3：IMG_ / Screenshot_
		{"相机命名", "IMG_20260811_063006.jpg", "2026-08-11 00:00:00"},
		{"截图命名", "Screenshot_20240109_213741_1_com.tencent.mm.jpg", "2024-01-09 00:00:00"},
		// 新增规则：PANO_ / VID_ / IMG直接跟日期 / 虚线日期
		{"全景命名", "PANO_20170121_xxxx.jpg", "2017-01-21 00:00:00"},
		{"视频命名", "VID_20150218_xxx.jpg", "2015-02-18 00:00:00"},
		{"IMG直接跟日期", "IMG20150908xxx.jpg", "2015-09-08 00:00:00"},
		{"虚线日期截图", "Screenshot_2018-12-31-xxx.jpg", "2018-12-31 00:00:00"},
		{"虚线日期带时间", "Screenshot_2018-12-31-21-37-41_xxx.png", "2018-12-31 00:00:00"},
		// 规则4/5：mmexport / wx_camera_
		{"微信转发", "mmexport1713701399029.jpg", "2024-04-21 20:09:59"},
		{"微信拍摄", "wx_camera_1725958479520.jpg", "2024-09-10 16:54:39"},
		{"microMsg前缀", "microMsg.1713701399029.jpg", "2024-04-21 20:09:59"},
		{"视频扩展名同样解析", "mmexport1713701399029.mp4", "2024-04-21 20:09:59"},
		// 防误判
		{"前两位12拒绝", "12345678901.jpg", ""},
		{"前两位大于当前拒绝", "1955555555555.jpg", ""},
		{"少于10位数字拒绝", "12345.jpg", ""},
		// 无法解析
		{"普通文件名", "document.pdf", ""},
		{"带空格文件名", "my vacation photo.jpg", ""},
		{"空字符串", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParsePhotoDateFromFilename(tc.filename)
			if tc.want == "" {
				if got != nil {
					t.Fatalf("ParsePhotoDateFromFilename(%q) = %v, want nil", tc.filename, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("ParsePhotoDateFromFilename(%q) = nil, want %s", tc.filename, tc.want)
			}
			want, err := time.ParseInLocation("2006-01-02 15:04:05", tc.want, time.Local)
			if err != nil {
				t.Fatalf("bad test want: %v", err)
			}
			if !got.Equal(want) {
				t.Fatalf("ParsePhotoDateFromFilename(%q) = %v, want %v", tc.filename, got, want)
			}
		})
	}
}
