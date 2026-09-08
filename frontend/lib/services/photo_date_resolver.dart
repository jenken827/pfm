import 'dart:io' as io;
import 'dart:typed_data';

import 'package:exif/exif.dart' as exif;
import 'package:path/path.dart' as p;
import 'package:pfm/proto/native/file.pb.dart';

/// 文件名解析规则类型（与后端 internal/service/photo_date.go 常量一致）
class PhotoDateRuleTypes {
  static const prefixSegmentDate = 'prefix_segment_date';
  static const prefixCompactDate = 'prefix_compact_date';
  static const prefixTimestamp = 'prefix_timestamp';
  static const leadingTimestamp = 'leading_timestamp';
}

/// 图片拍摄时间解析器（相册备份按"年/月"归档用）
///
/// 解析顺序：
/// 1. EXIF DateTimeOriginal（只读文件头，大小由配置 exifHeadReadLimit 决定，
///    默认 256KB，覆盖绝大多数 JPEG 的 EXIF 段）
/// 2. 文件名规则（按配置 filenameRules 顺序逐条尝试，先命中先返回）
/// 3. 媒体库时间（photo_manager 的 createDateTime）作为最终兜底
/// 全部失败返回 null，调用方走原有编号子目录逻辑
///
/// 规则表来源：默认内置（与后端默认一致）；相册备份前会尝试拉取服务端
/// config.photo_date（GetPhotoDateConfig RPC）覆盖，拉取失败回退内置默认。
class PhotoDateResolver {
  /// EXIF 解析默认读取的文件头上限（EXIF APP1 段几乎总在文件头部）
  static const int defaultExifReadLimit = 256 * 1024;

  /// 时间戳防误判默认阈值（与后端 defaultTsMinYear / defaultTsMaxYearOffset 一致）
  static const int defaultTsMinYear = 2000;
  static const int defaultTsMaxYearOffset = 1;

  /// 内置默认配置（与后端 defaultPhotoDateRules 保持一致，变更需同步）
  static PhotoDateConfig defaultConfig() {
    return PhotoDateConfig(
      exifEnabled: true,
      exifHeadReadLimit: defaultExifReadLimit,
      tsMinYear: defaultTsMinYear,
      tsMaxYearOffset: defaultTsMaxYearOffset,
      filenameRules: [
        // IMG_ / Screenshot_ / PANO_ / VID_ 前缀，"_" 分隔第二段为日期
        PhotoDateFilenameRule(
          type: PhotoDateRuleTypes.prefixSegmentDate,
          prefixes: ['IMG_', 'Screenshot_', 'PANO_', 'VID_'],
          segmentIndex: 1,
        ),
        // IMG 前缀直接跟 8 位 YYYYMMDD（如 IMG20150908xxx.jpg）
        PhotoDateFilenameRule(
          type: PhotoDateRuleTypes.prefixCompactDate,
          prefixes: ['IMG'],
        ),
        // mmexport / wx_camera_ / microMsg. 前缀后跟 10 位时间戳
        PhotoDateFilenameRule(
          type: PhotoDateRuleTypes.prefixTimestamp,
          prefixes: ['mmexport', 'wx_camera_', 'microMsg.'],
        ),
        // 文件名开头连续数字（前 10 位为秒级时间戳）
        PhotoDateFilenameRule(
          type: PhotoDateRuleTypes.leadingTimestamp,
        ),
      ],
    );
  }

  /// 综合解析：EXIF → 文件名规则 → 媒体库时间
  /// [config] 为 null 时使用内置默认配置
  static Future<DateTime?> resolveFromFile(
    String filePath, {
    DateTime? mediaDate,
    PhotoDateConfig? config,
  }) async {
    final cfg = config ?? defaultConfig();
    if (cfg.exifEnabled) {
      final fromExif = await readExifDate(filePath, cfg);
      if (fromExif != null) return fromExif;
    }

    final fromName = parseFromFilename(p.basename(filePath), cfg);
    if (fromName != null) return fromName;

    return mediaDate;
  }

  /// 读取文件头 EXIF DateTimeOriginal
  static Future<DateTime?> readExifDate(
    String filePath,
    PhotoDateConfig cfg,
  ) async {
    try {
      final file = io.File(filePath);
      if (!await file.exists()) return null;

      final len = await file.length();
      if (len <= 0) return null;
      final readLimit = cfg.exifHeadReadLimit > 0
          ? cfg.exifHeadReadLimit
          : defaultExifReadLimit;
      final readLen = len < readLimit ? len : readLimit;

      final bytes = await file
          .openRead(0, readLen)
          .fold<Uint8List>(Uint8List(0), (acc, chunk) {
        final merged = Uint8List(acc.length + chunk.length)
          ..setRange(0, acc.length, acc)
          ..setRange(acc.length, acc.length + chunk.length, chunk);
        return merged;
      });

      final data = await exif.readExifFromBytes(bytes);
      // DateTimeOriginal 在 EXIF 段（部分文件同时存在 Image 段，优先 EXIF）
      final tag =
          data['EXIF DateTimeOriginal'] ?? data['Image DateTimeOriginal'];
      if (tag == null) return null;
      return parseExifDateTime(tag.printable);
    } catch (_) {
      // 非图片 / EXIF 缺失 / 读取失败：一律走文件名解析
      return null;
    }
  }

  /// 解析 EXIF 日期字符串，如 "2026:07:16 22:35:34" 或 "2026-07-16 22:35:34"
  static DateTime? parseExifDateTime(String? s) {
    if (s == null || s.trim().isEmpty) return null;
    final m = RegExp(
      r'^(\d{4})[\-:](\d{2})[\-:](\d{2})[ T](\d{2}):(\d{2})(?::(\d{2}))?',
    ).firstMatch(s.trim());
    if (m == null) return null;

    final year = int.parse(m.group(1)!);
    final month = int.parse(m.group(2)!);
    final day = int.parse(m.group(3)!);
    final hour = int.parse(m.group(4)!);
    final minute = int.parse(m.group(5)!);
    final second = m.group(6) != null ? int.parse(m.group(6)!) : 0;

    return _safeDateTime(year, month, day, hour, minute, second);
  }

  /// 按规则表顺序从文件名解析拍摄日期（配置规则整体替换默认表；
  /// 未知规则类型跳过，保证向前兼容新服务端配置）
  /// [config] 为 null 时使用内置默认规则
  static DateTime? parseFromFilename(
    String filename, [
    PhotoDateConfig? config,
  ]) {
    return parseFromFilenameWithConfig(filename, config ?? defaultConfig());
  }

  /// 按规则表顺序从文件名解析拍摄日期（显式传配置）
  static DateTime? parseFromFilenameWithConfig(
    String filename,
    PhotoDateConfig cfg,
  ) {
    if (filename.isEmpty) return null;

    final tsMinYear = cfg.tsMinYear > 0 ? cfg.tsMinYear : defaultTsMinYear;
    final tsMaxYearOffset = cfg.tsMaxYearOffset > 0
        ? cfg.tsMaxYearOffset
        : defaultTsMaxYearOffset;

    for (final rule in cfg.filenameRules) {
      if (rule.type.isEmpty) continue;
      DateTime? t;
      if (rule.type == PhotoDateRuleTypes.prefixSegmentDate) {
        t = _parsePrefixSegmentDate(filename, rule);
      } else if (rule.type == PhotoDateRuleTypes.prefixCompactDate) {
        t = _parsePrefixCompactDate(filename, rule);
      } else if (rule.type == PhotoDateRuleTypes.prefixTimestamp) {
        t = _parsePrefixTimestamp(filename, rule, tsMinYear, tsMaxYearOffset);
      } else if (rule.type == PhotoDateRuleTypes.leadingTimestamp) {
        t = _parseLeadingTimestamp(filename, tsMinYear, tsMaxYearOffset);
      } else {
        // 未知规则类型：跳过（后端同样行为，保证向前兼容新服务端配置）
        continue;
      }
      if (t != null) return t;
    }
    return null;
  }

  /// 前缀 + "_" 分隔取 segmentIndex 段（如 IMG_ / Screenshot_ / PANO_ / VID_）
  static DateTime? _parsePrefixSegmentDate(
    String filename,
    PhotoDateFilenameRule rule,
  ) {
    for (final prefix in rule.prefixes) {
      if (!filename.startsWith(prefix)) continue;
      final parts = filename.split('_');
      if (parts.length > rule.segmentIndex) {
        final d = _parseDateField(parts[rule.segmentIndex]);
        if (d != null) return d;
      }
    }
    return null;
  }

  /// 前缀后紧跟 8 位 YYYYMMDD（如 IMG20150908xxx.jpg）
  static DateTime? _parsePrefixCompactDate(
    String filename,
    PhotoDateFilenameRule rule,
  ) {
    for (final prefix in rule.prefixes) {
      if (!filename.startsWith(prefix)) continue;
      final rest = filename.substring(prefix.length);
      if (rest.length >= 8) {
        final d = _parseCompactDate(rest.substring(0, 8));
        if (d != null) return d;
      }
    }
    return null;
  }

  /// 前缀后取前 10 位时间戳（如 mmexport / wx_camera_ / microMsg.）
  static DateTime? _parsePrefixTimestamp(
    String filename,
    PhotoDateFilenameRule rule,
    int tsMinYear,
    int tsMaxYearOffset,
  ) {
    for (final prefix in rule.prefixes) {
      if (!filename.startsWith(prefix)) continue;
      final t = _parseTimestampFromDigits(
        filename.substring(prefix.length),
        tsMinYear,
        tsMaxYearOffset,
      );
      if (t != null) return t;
    }
    return null;
  }

  /// 文件名开头连续数字，前 10 位为秒级时间戳
  static DateTime? _parseLeadingTimestamp(
    String filename,
    int tsMinYear,
    int tsMaxYearOffset,
  ) {
    return _parseTimestampFromDigits(filename, tsMinYear, tsMaxYearOffset);
  }

  /// 解析日期字段：支持 YYYYMMDD 与 YYYY-MM-DD 两种格式
  /// 虚线日期在字段内搜索（不锚定整串），兼容 "2018-12-31"、
  /// "2018-12-31-21-37-41"、"2018-12-31-xxx.jpg" 等变体（Samsung 等厂商截图命名）
  static DateTime? _parseDateField(String s) {
    final compact = _parseCompactDate(s);
    if (compact != null) return compact;

    final dashed =
        RegExp(r'(\d{4})-(\d{2})-(\d{2})').firstMatch(s.trim());
    if (dashed != null) {
      return _safeDateTime(
        int.parse(dashed.group(1)!),
        int.parse(dashed.group(2)!),
        int.parse(dashed.group(3)!),
        0,
        0,
        0,
      );
    }
    return null;
  }

  /// 解析 YYYYMMDD 紧凑日期，非法日期返回 null
  /// 兼容入口：等价于规则引擎内部使用的同名校验（后端 parseCompactDate）
  static DateTime? parseCompactDate(String s) => _parseCompactDate(s);

  /// 解析 YYYYMMDD 紧凑日期，非法日期返回 null
  static DateTime? _parseCompactDate(String s) {
    final m = RegExp(r'^(\d{4})(\d{2})(\d{2})$').firstMatch(s.trim());
    if (m == null) return null;
    return _safeDateTime(
      int.parse(m.group(1)!),
      int.parse(m.group(2)!),
      int.parse(m.group(3)!),
      0,
      0,
      0,
    );
  }

  /// 从字符串开头的连续数字中解析 10 位秒级时间戳
  ///
  /// 防误判校验：
  /// - 时间戳前两位必须 > 12（否则可能是其他数字编号，如 01xxx）
  /// - 且 <= 当前时间戳前两位（如当前为 17，则 18 开头不可能是时间戳）
  /// - 解析后的年份必须在 tsMinYear ~ 当前年份+tsMaxYearOffset 之间
  static DateTime? _parseTimestampFromDigits(
    String s,
    int tsMinYear,
    int tsMaxYearOffset,
  ) {
    final m = RegExp(r'^(\d+)').firstMatch(s);
    if (m == null) return null;
    final digits = m.group(1)!;
    if (digits.length < 10) return null;

    final tsStr = digits.substring(0, 10);
    final firstTwo = int.parse(tsStr.substring(0, 2));
    final nowSec = DateTime.now().millisecondsSinceEpoch ~/ 1000;
    final curFirstTwo = int.parse(nowSec.toString().substring(0, 2));

    // 前两位必须在 (12, 当前前两位] 区间
    if (firstTwo <= 12 || firstTwo > curFirstTwo) return null;

    final sec = int.tryParse(tsStr);
    if (sec == null) return null;

    final dt = DateTime.fromMillisecondsSinceEpoch(sec * 1000);
    final now = DateTime.now();
    final maxYear = now.year + tsMaxYearOffset;
    if (dt.year < tsMinYear || dt.year > maxYear) return null;
    return dt;
  }

  /// 构造合法 DateTime，非法（如 2月30日）返回 null
  static DateTime? _safeDateTime(
    int year,
    int month,
    int day,
    int hour,
    int minute,
    int second,
  ) {
    if (month < 1 || month > 12 || day < 1 || day > 31) return null;
    final dt = DateTime(year, month, day, hour, minute, second);
    // DateTime 会自动归一化（如 2月30日 → 3月2日），校验回读是否一致
    if (dt.year != year || dt.month != month || dt.day != day) return null;
    return dt;
  }
}
