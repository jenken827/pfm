import 'dart:convert';

/// 备份文件内容与设备标识的纯逻辑（不依赖 Flutter 插件，便于单测）。
///
/// 短信、通话记录**按设备独有**：两台手机的短信/通话记录互不相干，
/// 备份到服务器后必须能分辨来自哪台设备，否则多设备备份会混作一谈。
/// 因此文件名与文件内容里都要带设备信息；通讯录是跨设备同步的同一份数据，
/// 不需要（也**不能**加，否则换机/改名会改变 hash 导致重复上传）。

/// 设备信息由**类型**与**标识**两段组成，二者来源与用途不同：
///
/// | 段 | 例 | 来源 | 用途 |
/// | --- | --- | --- | --- |
/// | 类型 | `华为畅享60X` | 厂商市场名 `ro.config.marketing_name` | 说明这是什么型号 |
/// | 标识 | `huaweufhfg` | 设备设置里的「设备名称」 | 区分**同型号**的多台设备 |
///
/// 两段都要：只有类型无法区分同型号的两台手机；只有标识（用户随便改的名字）
/// 又看不出是什么设备。合成 `{类型}_{标识}` 才同时满足"看得懂"与"能区分"。

/// 每一段 sanitize 后的最大字符数。
/// 设备名用户可随意改名（可能整句中文），过长会撑爆文件名，故截断。
const int kDeviceLabelMaxChars = 24;

/// 设备信息的兜底值（两段都取不到、非 Android 端）
const String kDeviceLabelFallback = 'unknown_device';

/// 短信/通话记录备份 hash 规则版本号。
///
/// 与通讯录的 `kContactHashSchemeVersion` 同理：一旦导出字段集合、
/// 设备标识等参与 hash 的内容规则变化，**必须**递增此值，否则新规则下
/// 算出的 hash 可能与旧备份相同而被误判为「内容无变动」跳过上传。
///
/// v1 → v2：导出内容与文件名新增设备标识。
/// v2 → v3：设备标识拆分为「类型_标识」两段。
const String kDeviceBackupHashSchemeVersion = 'v3';

/// 每批参与内容拼装的记录条数（控制 StringBuffer 峰值，避免一次性加载全部记录）
const int kDeviceBackupChunkSize = 500;

/// 把设备标识清洗成可安全用于文件名与 CSV 单元格的形式。
///
/// 系统设备名是用户自由文本，可能含空格、斜杠（破坏路径）、逗号/引号
/// （破坏 CSV 列）、换行（破坏 CSV 行）及控制字符，一律替换为 `_`；
/// 同时截断过长名称并去掉首尾 `_`/`.`（避免空壳名或隐藏文件）。
String sanitizeDeviceLabel(String raw) {
  // \\ 在 Dart 字面量里表示单个反斜杠，正则字符类中需再转义一次
  final cleaned = raw
      .replaceAll(RegExp(r'[\\/:*?"<>|,\s\u0000-\u001F\u007F]'), '_')
      .replaceAll(RegExp('_+'), '_');
  var result = cleaned;
  if (result.length > kDeviceLabelMaxChars) {
    result = result.substring(0, kDeviceLabelMaxChars);
  }
  result = result.replaceAll(RegExp(r'^[_.]+|[_.]+$'), '');
  return result.isEmpty ? kDeviceLabelFallback : result;
}

/// 把设备类型与设备标识合成一段"设备信息"，用于文件名与文件头。
///
/// 形如 `华为畅享60X_huaweufhfg`。两段各自 sanitize 后再拼接：
/// 分别清洗能保住各自的长度上限，拼完再整体截断会把标识挤掉。
///
/// 某一段取不到时只输出另一段（不填占位符）—— `华为畅享60X` 仍比
/// `华为畅享60X_unknown_device` 干净；两段都取不到才用 [kDeviceLabelFallback]，
/// 因为文件名不能没有设备段（否则多设备无从区分）。
String buildDeviceInfo({required String deviceType, required String deviceId}) {
  final type = _sanitizeSegment(deviceType);
  final id = _sanitizeSegment(deviceId);
  if (type.isEmpty && id.isEmpty) return kDeviceLabelFallback;
  if (type.isEmpty) return id;
  if (id.isEmpty) return type;
  return '${type}_$id';
}

/// 清洗单个设备信息段；为空表示"这段没有值"，不套兜底值
String _sanitizeSegment(String raw) {
  final cleaned = sanitizeDeviceLabel(raw);
  return cleaned == kDeviceLabelFallback ? '' : cleaned;
}

/// 生成备份文件名：`{prefix}_backup_{设备信息}_{yyyyMMdd_HHmm}.{ext}`
///
/// 形如 `sms_backup_华为畅享60X_huaweufhfg_20260921_0805.xml`。
/// 设备信息放在时间戳**之前**：同一设备的历次备份在按文件名排序时能聚在
/// 一起，用户一眼能看出哪个文件来自哪台设备。
String buildDeviceBackupFileName({
  required String prefix,
  required String deviceInfo,
  required DateTime now,
  required String extension,
}) {
  // 这里再 sanitize 一次：调用方可能直接传原始值
  final device = sanitizeDeviceLabel(deviceInfo);
  final ts =
      '${now.year}${_two(now.month)}${_two(now.day)}_${_two(now.hour)}${_two(now.minute)}';
  return '${prefix}_backup_${device}_$ts.$extension';
}

String _two(int v) => v.toString().padLeft(2, '0');

/// 构建备份文件内容（短信 → XML，通话记录 → CSV），均带设备标识。
///
/// 记录以 JSON 字符串形式逐条传入（原生通道的原始格式），逐条解析而非
/// 一次性反序列化整个数组 —— 避免解码出的对象图与文本同时驻留内存。
///
/// [includeHeader] 为 false 时不输出文件头/`<smses>` 开标签（后续分块），
/// 使调用方可以按 [kDeviceBackupChunkSize] 分批拼接出与一次性生成
/// **逐字节相同**的内容（[isLastChunk] 为 true 时补 `</smses>`）。
/// 分块是为了限制手机端内存峰值，单元测试用一次性调用断言同一结果。
String buildDeviceBackupContent({
  required bool isSms,
  required String deviceInfo,
  required Iterable<String> rawJsonRecords,
  bool includeHeader = true,
  bool isLastChunk = true,
  int? totalCount,
}) {
  final device = sanitizeDeviceLabel(deviceInfo);
  return isSms
      ? _buildSmsXml(
          device,
          rawJsonRecords,
          includeHeader: includeHeader,
          isLastChunk: isLastChunk,
          totalCount: totalCount,
        )
      : _buildCallLogCsv(
          device,
          rawJsonRecords,
          includeHeader: includeHeader,
        );
}

/// 短信导出为 XML（格式与 SMS Backup & Restore 兼容，便于恢复）。
///
/// 设备信息放在注释里：SMS Backup & Restore 只解析 `<smses>` 并忽略注释，
/// 既不破坏恢复兼容性，用文本编辑器打开又能一眼看到来源设备。
///
/// `count` 属性需要记录总条数，而分块写入时当前块的条数不等于总数，
/// 故由调用方通过 [totalCount] 传入；未传时退化为"本块条数"（一次性调用场景）。
String _buildSmsXml(
  String deviceInfo,
  Iterable<String> rawJsonRecords, {
  required bool includeHeader,
  required bool isLastChunk,
  int? totalCount,
}) {
  final records = StringBuffer();
  var count = 0;
  for (final raw in rawJsonRecords) {
    final r = jsonDecode(raw) as Map<String, dynamic>;
    count++;
    final address = _xmlEscape((r['address'] ?? '').toString());
    final text = _xmlEscape((r['body'] ?? '').toString());
    records.writeln(
      '  <sms address="$address" date="${r['date']}" type="${r['type']}" '
      'read="${r['read']}" body="$text" />',
    );
  }

  final buf = StringBuffer();
  if (includeHeader) {
    buf.writeln('<?xml version="1.0" encoding="UTF-8"?>');
    // 设备信息必须 XML 转义：sanitize 只保证"能进文件名"，
    // 而 & 这类字符在文件名里合法、在 XML 里非法（会破坏整份 XML）
    buf.writeln('<!-- backup_device="${_xmlEscape(deviceInfo)}" -->');
    buf.writeln('<smses count="${totalCount ?? count}">');
  }
  buf.write(records);
  if (isLastChunk) buf.writeln('</smses>');
  return buf.toString();
}

/// 通话记录导出为 CSV（Excel 可直接打开），设备信息作为表头注释行。
String _buildCallLogCsv(
  String deviceInfo,
  Iterable<String> rawJsonRecords, {
  required bool includeHeader,
}) {
  const typeNames = {1: '来电', 2: '去电', 3: '未接'};
  final buf = StringBuffer();
  if (includeHeader) {
    // CSV 注释行按 CSV 规则转义引号：设备信息里的引号已被 sanitize 成
    // 下划线，但这里仍走一次转义以防将来 sanitize 规则放宽
    buf.writeln(
      '# backup_device="${_csvEscape(deviceInfo)}"',
    );
    buf.writeln('时间,号码,姓名,类型,时长(秒)');
  }
  for (final raw in rawJsonRecords) {
    final r = jsonDecode(raw) as Map<String, dynamic>;
    final dateRaw = r['date'];
    final date = dateRaw is int
        ? DateTime.fromMillisecondsSinceEpoch(
            dateRaw,
          ).toLocal().toIso8601String()
        : '';
    final number = _csvEscape((r['number'] ?? '').toString());
    final name = _csvEscape((r['name'] ?? '').toString());
    final typeName = typeNames[r['type']] ?? '未知';
    buf.writeln(
      '${_csvEscape(date)},$number,$name,$typeName,${r['duration'] ?? 0}',
    );
  }
  return buf.toString();
}

String _xmlEscape(String s) => s
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&apos;');

String _csvEscape(String s) {
  if (s.contains(',') || s.contains('"') || s.contains('\n')) {
    return '"${s.replaceAll('"', '""')}"';
  }
  return s;
}
