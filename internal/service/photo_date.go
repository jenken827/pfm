package service

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ParsePhotoDateFromFilename 从文件名解析拍摄日期
// 规则与前端 PhotoDateResolver（frontend/lib/services/photo_date_resolver.dart）完全一致：
//   - IMG_20260811_063006.jpg / PANO_20170121_xxxx.jpg / VID_20150218_xxx.jpg
//     / Screenshot_20240109_xxx : "_" 分隔第二段为日期（YYYYMMDD 或 YYYY-MM-DD）
//   - IMG20150908xxx.jpg : "IMG" 前缀直接跟 8 位 YYYYMMDD
//   - mmexport1713701399029.jpg / wx_camera_1725958479520.jpg
//     / microMsg.1713701399029.jpg : 去前缀后取前 10 位时间戳
//   - 1784212534296.png : 开头连续数字的前 10 位为秒级时间戳
//
// 时间戳防误判：前两位必须 > 12 且 <= 当前时间戳前两位；解析年份 2000 ~ 当前年份+1。
// 无法解析返回 nil。
func ParsePhotoDateFromFilename(filename string) *time.Time {
	if filename == "" {
		return nil
	}

	// 规则 2/3：IMG_ / Screenshot_ / PANO_ / VID_ 前缀
	for _, prefix := range []string{"IMG_", "Screenshot_", "PANO_", "VID_"} {
		if strings.HasPrefix(filename, prefix) {
			parts := strings.Split(filename, "_")
			if len(parts) >= 2 {
				if t := parseDateField(parts[1]); t != nil {
					return t
				}
			}
		}
	}

	// 规则：IMG 前缀直接跟 8 位 YYYYMMDD（如 IMG20150908xxx.jpg）
	if strings.HasPrefix(filename, "IMG") && len(filename) >= 11 {
		if t := parseCompactDate(filename[3:11]); t != nil {
			return t
		}
	}

	// 规则 4/5：mmexport / wx_camera_ / microMsg. 前缀
	for _, prefix := range []string{"mmexport", "wx_camera_", "microMsg."} {
		if strings.HasPrefix(filename, prefix) {
			if t := parseTimestampFromDigits(filename[len(prefix):]); t != nil {
				return t
			}
		}
	}

	// 规则 1：开头连续数字
	return parseTimestampFromDigits(filename)
}

var compactDateRe = regexp.MustCompile(`^(\d{4})(\d{2})(\d{2})$`)
// 虚线日期在字段内搜索（不锚定整串）：兼容 "2018-12-31"、"2018-12-31-21-37-41"、
// "2018-12-31-xxx.jpg" 等变体（Samsung 等厂商截图命名）
var dashedDateRe = regexp.MustCompile(`(\d{4})-(\d{2})-(\d{2})`)

// parseDateField 解析日期字段：支持 YYYYMMDD 与 YYYY-MM-DD 两种格式
func parseDateField(s string) *time.Time {
	if t := parseCompactDate(s); t != nil {
		return t
	}
	m := dashedDateRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return nil
	}
	year, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	day, _ := strconv.Atoi(m[3])
	return safeDateTime(year, month, day, 0, 0, 0)
}

// parseCompactDate 解析 YYYYMMDD 紧凑日期，非法返回 nil
func parseCompactDate(s string) *time.Time {
	s = strings.TrimSpace(s)
	m := compactDateRe.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	year, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	day, _ := strconv.Atoi(m[3])
	return safeDateTime(year, month, day, 0, 0, 0)
}

var leadingDigitsRe = regexp.MustCompile(`^(\d+)`)

// parseTimestampFromDigits 从字符串开头连续数字中解析 10 位秒级时间戳
// 校验：前两位 > 12 且 <= 当前时间戳前两位；年份 2000 ~ 当前年份+1
func parseTimestampFromDigits(s string) *time.Time {
	m := leadingDigitsRe.FindStringSubmatch(s)
	if m == nil || len(m[1]) < 10 {
		return nil
	}
	tsStr := m[1][:10]
	firstTwo, err := strconv.Atoi(tsStr[:2])
	if err != nil {
		return nil
	}
	now := time.Now()
	nowFirstTwo, _ := strconv.Atoi(strconv.FormatInt(now.Unix(), 10)[:2])
	if firstTwo <= 12 || firstTwo > nowFirstTwo {
		return nil
	}
	sec, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return nil
	}
	dt := time.Unix(sec, 0)
	if dt.Year() < 2000 || dt.Year() > now.Year()+1 {
		return nil
	}
	return &dt
}

// safeDateTime 构造合法本地时间，非法日期（如 2月30日）返回 nil
func safeDateTime(year, month, day, hour, minute, second int) *time.Time {
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return nil
	}
	dt := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.Local)
	// time.Date 会自动归一化（如 2月30日 → 3月2日），校验回读是否一致
	if dt.Year() != year || int(dt.Month()) != month || dt.Day() != day {
		return nil
	}
	return &dt
}
