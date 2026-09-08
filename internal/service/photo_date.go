package service

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"jenken827.com/jenken827/pfm/internal/config"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// 文件名解析规则类型（与 proto PhotoDateFilenameRule.type 取值一致）
const (
	// prefixSegmentDate: 前缀+分隔符取 SegmentIndex 段，段内按 YYYYMMDD 或 YYYY-MM-DD 解析
	ruleTypePrefixSegmentDate = "prefix_segment_date"
	// prefixCompactDate: 前缀后紧跟 8 位 YYYYMMDD
	ruleTypePrefixCompactDate = "prefix_compact_date"
	// prefixTimestamp: 前缀后紧跟 10 位秒级时间戳
	ruleTypePrefixTimestamp = "prefix_timestamp"
	// leadingTimestamp: 文件名开头连续数字，前 10 位为秒级时间戳
	ruleTypeLeadingTimestamp = "leading_timestamp"
)

// 默认规则常量
const (
	defaultExifHeadReadLimit = 262144 // 256KB，EXIF APP1 段几乎总在文件头部
	defaultTsMinYear         = 2000
	defaultTsMaxYearOffset   = 1 // 允许解析到当前年份 + 1（时区差异留余量）
)

// photoDateOptions 一次解析使用的规则与阈值（两端默认保持一致，
// 服务端可通过 config.photo_date 覆盖后经 GetPhotoDateConfig 下发）
type photoDateOptions struct {
	filenameRules   []*native.PhotoDateFilenameRule
	tsMinYear       int
	tsMaxYearOffset int
}

// defaultPhotoDateRules 内置默认文件名规则（顺序敏感，先命中先返回）
var defaultPhotoDateRules = []*native.PhotoDateFilenameRule{
	// IMG_ / Screenshot_ / PANO_ / VID_ 前缀，"_" 分隔第二段为日期
	{
		Type:         ptrStr(ruleTypePrefixSegmentDate),
		Prefixes:     []string{"IMG_", "Screenshot_", "PANO_", "VID_"},
		SegmentIndex: ptrU32(1),
	},
	// IMG 前缀直接跟 8 位 YYYYMMDD（如 IMG20150908xxx.jpg）
	{
		Type:     ptrStr(ruleTypePrefixCompactDate),
		Prefixes: []string{"IMG"},
	},
	// mmexport / wx_camera_ / microMsg. 前缀后跟 10 位时间戳
	{
		Type:     ptrStr(ruleTypePrefixTimestamp),
		Prefixes: []string{"mmexport", "wx_camera_", "microMsg."},
	},
	// 文件名开头连续数字（前 10 位为秒级时间戳）
	{Type: ptrStr(ruleTypeLeadingTimestamp)},
}

func ptrStr(s string) *string { return &s }
func ptrU32(v uint32) *uint32 { return &v }

// defaultPhotoDateOptions 内置默认解析选项
func defaultPhotoDateOptions() photoDateOptions {
	return photoDateOptions{
		filenameRules:   defaultPhotoDateRules,
		tsMinYear:       defaultTsMinYear,
		tsMaxYearOffset: defaultTsMaxYearOffset,
	}
}

// photoDateOptionsFromConfig 从服务端配置构造解析选项；未配置（零值）的
// 项回退到内置默认，保证与前端默认规则一致
func photoDateOptionsFromConfig(cfg models.PhotoDateConfig) photoDateOptions {
	opts := defaultPhotoDateOptions()
	if len(cfg.FilenameRules) > 0 {
		rules := make([]*native.PhotoDateFilenameRule, 0, len(cfg.FilenameRules))
		for _, r := range cfg.FilenameRules {
			rules = append(rules, &native.PhotoDateFilenameRule{
				Type:         &r.Type,
				Prefixes:     r.Prefixes,
				SegmentIndex: &r.SegmentIndex,
			})
		}
		opts.filenameRules = rules
	}
	if cfg.TsMinYear != 0 {
		opts.tsMinYear = int(cfg.TsMinYear)
	}
	if cfg.TsMaxYearOffset != 0 {
		opts.tsMaxYearOffset = int(cfg.TsMaxYearOffset)
	}
	return opts
}

// ParsePhotoDateFromFilename 从文件名解析拍摄日期（使用默认规则）
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
	return parseFilenameWithOptions(filename, defaultPhotoDateOptions())
}

// ParsePhotoDateFromFilenameWithConfig 使用服务端配置的规则解析文件名拍摄日期
func ParsePhotoDateFromFilenameWithConfig(filename string, cfg models.PhotoDateConfig) *time.Time {
	return parseFilenameWithOptions(filename, photoDateOptionsFromConfig(cfg))
}

// parseFilenameWithOptions 按规则表顺序解析，首个命中的规则即返回
func parseFilenameWithOptions(filename string, opts photoDateOptions) *time.Time {
	for _, rule := range opts.filenameRules {
		if rule == nil || rule.Type == nil {
			continue
		}
		var t *time.Time
		switch *rule.Type {
		case ruleTypePrefixSegmentDate:
			t = parsePrefixSegmentDate(filename, rule)
		case ruleTypePrefixCompactDate:
			t = parsePrefixCompactDate(filename, rule)
		case ruleTypePrefixTimestamp:
			t = parsePrefixTimestamp(filename, rule, opts)
		case ruleTypeLeadingTimestamp:
			t = parseTimestampFromDigitsOpts(filename, opts)
		default:
			// 未知规则类型：跳过，向前兼容（前端老版本遇到新类型也跳过）
			continue
		}
		if t != nil {
			return t
		}
	}
	return nil
}

// parsePrefixSegmentDate 前缀 + "_" 分隔取 SegmentIndex 段（如 IMG_ / Screenshot_ / PANO_ / VID_）
func parsePrefixSegmentDate(filename string, rule *native.PhotoDateFilenameRule) *time.Time {
	for _, prefix := range rule.Prefixes {
		if !strings.HasPrefix(filename, prefix) {
			continue
		}
		parts := strings.Split(filename, "_")
		idx := int(rule.GetSegmentIndex())
		if len(parts) > idx {
			if d := parseDateField(parts[idx]); d != nil {
				return d
			}
		}
	}
	return nil
}

// parsePrefixCompactDate 前缀后紧跟 8 位 YYYYMMDD（如 IMG20150908xxx.jpg）
func parsePrefixCompactDate(filename string, rule *native.PhotoDateFilenameRule) *time.Time {
	for _, prefix := range rule.Prefixes {
		if !strings.HasPrefix(filename, prefix) {
			continue
		}
		rest := filename[len(prefix):]
		if len(rest) >= 8 {
			if d := parseCompactDate(rest[:8]); d != nil {
				return d
			}
		}
	}
	return nil
}

// parsePrefixTimestamp 前缀后取前 10 位时间戳（如 mmexport / wx_camera_ / microMsg.）
func parsePrefixTimestamp(filename string, rule *native.PhotoDateFilenameRule, opts photoDateOptions) *time.Time {
	for _, prefix := range rule.Prefixes {
		if !strings.HasPrefix(filename, prefix) {
			continue
		}
		if t := parseTimestampFromDigitsOpts(filename[len(prefix):], opts); t != nil {
			return t
		}
	}
	return nil
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

// parseTimestampFromDigits 从字符串开头连续数字中解析 10 位秒级时间戳（默认阈值）
func parseTimestampFromDigits(s string) *time.Time {
	return parseTimestampFromDigitsOpts(s, defaultPhotoDateOptions())
}

// parseTimestampFromDigitsOpts 校验：
// 前两位 > 12 且 <= 当前时间戳前两位；年份 tsMinYear ~ 当前年份+tsMaxYearOffset
func parseTimestampFromDigitsOpts(s string, opts photoDateOptions) *time.Time {
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
	maxYear := now.Year() + opts.tsMaxYearOffset
	if dt.Year() < opts.tsMinYear || dt.Year() > maxYear {
		return nil
	}
	return &dt
}

// GetPhotoDateConfig 返回照片日期解析配置（相册备份按"年/月"归档用）
// 服务端 config.photo_date 为权威源：配置的规则整体替换默认规则表；
// 未配置项回退内置默认，保证与前端内置默认一致。
func GetPhotoDateConfig() *native.PhotoDateConfig {
	cfg := config.GetConfig().PhotoDate
	opts := photoDateOptionsFromConfig(cfg)

	// EXIF 开关/读取上限：仅前端相册备份消费
	exifEnabled := true
	if cfg.ExifEnabled != nil {
		exifEnabled = *cfg.ExifEnabled
	}
	headLimit := uint32(defaultExifHeadReadLimit)
	if cfg.ExifHeadReadLimit != 0 {
		headLimit = cfg.ExifHeadReadLimit
	}

	return &native.PhotoDateConfig{
		ExifEnabled:       &exifEnabled,
		ExifHeadReadLimit: &headLimit,
		FilenameRules:     opts.filenameRules,
		TsMinYear:         ptrU32(uint32(opts.tsMinYear)),
		TsMaxYearOffset:   ptrU32(uint32(opts.tsMaxYearOffset)),
	}
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
