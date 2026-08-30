import 'dart:io';
import 'dart:typed_data';

import 'package:exif/exif.dart' as exif;
import 'package:path/path.dart' as p;

/// 图片拍摄时间解析器（相册备份按"年/月"归档用）
///
/// 解析顺序：
/// 1. EXIF DateTimeOriginal（只读文件头 256KB，覆盖绝大多数 JPEG 的 EXIF 段）
/// 2. 文件名规则（微信/系统相机/截图的常见命名）
/// 3. 媒体库时间（photo_manager 的 createDateTime）作为最终兜底
/// 全部失败返回 null，调用方走原有编号子目录逻辑
class PhotoDateResolver {
  /// EXIF 解析读取的文件头上限（EXIF APP1 段几乎总在文件头部）
  static const int _exifReadLimit = 256 * 1024;

  /// 综合解析：EXIF → 文件名 → 媒体库时间
  static Future<DateTime?> resolveFromFile(
    String filePath, {
    DateTime? mediaDate,
  }) async {
    final fromExif = await readExifDate(filePath);
    if (fromExif != null) return fromExif;

    final fromName = parseFromFilename(p.basename(filePath));
    if (fromName != null) return fromName;

    return mediaDate;
  }

  /// 读取文件头 EXIF DateTimeOriginal
  static Future<DateTime?> readExifDate(String filePath) async {
    try {
      final file = File(filePath);
      if (!await file.exists()) return null;

      final len = await file.length();
      if (len <= 0) return null;
      final readLen = len < _exifReadLimit ? len : _exifReadLimit;

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
      r'^(\d{4})[:\-](\d{2})[:\-](\d{2})[ T](\d{2}):(\d{2})(?::(\d{2}))?',
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

  /// 从文件名解析拍摄日期（支持微信/系统相机/截图常见命名）
  ///
  /// 规则：
  /// - IMG_20260811_063006.jpg / PANO_20170121_xxxx.jpg / VID_20150218_xxx.jpg
  ///   / Screenshot_20240109_... : "_" 分隔第二段为日期（YYYYMMDD 或 YYYY-MM-DD）
  /// - IMG20150908xxx.jpg         : "IMG" 前缀直接跟 8 位 YYYYMMDD
  /// - mmexport1713701399029.jpg / wx_camera_1725958479520.jpg
  ///   / microMsg.1713701399029.jpg : 去前缀取前 10 位时间戳
  /// - 1784212534296.png            → 文件名开头 10 位时间戳
  static DateTime? parseFromFilename(String filename) {
    if (filename.isEmpty) return null;

    // 规则2/3：IMG_ / Screenshot_ / PANO_ / VID_ 前缀，"_" 分隔第二段为日期
    for (final prefix in const ['IMG_', 'Screenshot_', 'PANO_', 'VID_']) {
      if (filename.startsWith(prefix)) {
        final parts = filename.split('_');
        if (parts.length >= 2) {
          final d = parseDateField(parts[1]);
          if (d != null) return d;
        }
      }
    }

    // 规则：IMG 前缀直接跟 8 位 YYYYMMDD（如 IMG20150908xxx.jpg）
    if (filename.startsWith('IMG') && filename.length >= 11) {
      final d = parseCompactDate(filename.substring(3, 11));
      if (d != null) return d;
    }

    // 规则4/5：mmexport / wx_camera_ / microMsg. 前缀，去掉前缀后取前 10 位时间戳
    for (final prefix in const ['mmexport', 'wx_camera_', 'microMsg.']) {
      if (filename.startsWith(prefix)) {
        final rest = filename.substring(prefix.length);
        final ts = parseTimestampFromDigits(rest);
        if (ts != null) return ts;
      }
    }

    // 规则1：文件名开头为连续数字，前 10 位即毫秒时间戳
    final ts = parseTimestampFromDigits(filename);
    if (ts != null) return ts;

    return null;
  }

  /// 解析日期字段：支持 YYYYMMDD 与 YYYY-MM-DD 两种格式
  /// 虚线日期在字段内搜索（不锚定整串），兼容 "2018-12-31"、
  /// "2018-12-31-21-37-41"、"2018-12-31-xxx.jpg" 等变体（Samsung 等厂商截图命名）
  static DateTime? parseDateField(String s) {
    final compact = parseCompactDate(s);
    if (compact != null) return compact;

    final dashed = RegExp(r'(\d{4})-(\d{2})-(\d{2})').firstMatch(s.trim());
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
  static DateTime? parseCompactDate(String s) {
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
  /// 防误判校验（用户指定规则）：
  /// - 时间戳前两位必须 > 12（否则可能是其他数字编号，如 01xxx）
  /// - 且 <= 当前时间戳前两位（如当前为 17，则 18 开头不可能是时间戳）
  /// - 解析后的年份必须在 2000 ~ 当前年份+1 之间
  static DateTime? parseTimestampFromDigits(String s) {
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
    if (dt.year < 2000 || dt.year > now.year + 1) return null;
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
