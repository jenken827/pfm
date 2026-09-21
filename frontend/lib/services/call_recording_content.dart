/// 通话录音备份的纯逻辑（不依赖 Flutter 插件，便于单测）。
///
/// 与短信/通话记录（查 ContentProvider 导出文本）不同，通话录音是**磁盘上
/// 已经存在的二进制文件**：ROM 在通话结束时写入录音目录，本功能只需把它们
/// 搬上服务器。因此这里只做两件事：识别文件是否像录音、从文件名还原出
/// 通话对象与时间（用于展示与排序）。
///
/// **刻意不带设备标识**：录音文件名自带号码与时间戳，跨设备基本不重名；
/// 加了反而会让换机/改名改变判定结果导致整批重传。这与短信/通话记录
/// （按设备独有、必须标识）是有意的区别。
///
/// 文件名形态来自真机实测（华为 STG-AL00，/sdcard/Sounds/CallRecord）：
/// ```
/// 181 3574 1189_20260612104625.amr   ← 号码被 ROM 按 xxxx xxxx xxxx 分段
/// 0571 2895 8484_20240626134601.amr  ← 固话区号
/// 移动@10086_20260906162304.amr       ← 联系人名@号码（10086 之类短号）
/// ```
library;

/// 录音文件扩展名白名单。
///
/// 各 ROM 录音编码不同：华为用 AMR，小米/部分机型用 M4A、AAC、3GP。
/// 只收音频容器，避免把目录里的缩略图、日志等杂项当录音上传。
const Set<String> kCallRecordingExtensions = {
  'amr',
  'm4a',
  'aac',
  'mp3',
  '3gp',
  'ogg',
  'wav',
};

/// 录音文件名解析结果
class CallRecordingName {
  /// 通话对象原始串（号码或"联系人名@号码"）
  final String party;

  /// 通话时间；文件名里没有合法时间戳时为 null
  final DateTime? recordedAt;

  const CallRecordingName({required this.party, this.recordedAt});
}

/// ROM 时间戳形如 `20260612104625`（yyyyMMddHHmmss）
final RegExp _timestampRe = RegExp(r'(\d{14})$');

/// 判断扩展名是否属于录音
bool isCallRecordingFile(String fileName) {
  final dot = fileName.lastIndexOf('.');
  if (dot <= 0 || dot == fileName.length - 1) return false;
  return kCallRecordingExtensions.contains(
    fileName.substring(dot + 1).toLowerCase(),
  );
}

/// 解析录音文件名：`{通话对象}_{yyyyMMddHHmmss}.{ext}`。
///
/// 解析失败（没有时间戳后缀）也照常返回一个 party=主干的记录 —— 备份不该
/// 因为"文件名不合 ROM 规范"就漏掉真实录音，只是排序会退化。调用方可据
/// [CallRecordingName.recordedAt] 是否为 null 决定是否按文件名排序。
CallRecordingName parseCallRecordingName(String fileName) {
  // 去掉扩展名
  final dot = fileName.lastIndexOf('.');
  final stem = dot > 0 ? fileName.substring(0, dot) : fileName;

  final m = _timestampRe.firstMatch(stem);
  if (m == null) {
    return CallRecordingName(party: stem.trim());
  }

  // 时间戳前的分隔符（通常还有下划线）一并去掉
  var party = stem.substring(0, m.start);
  if (party.endsWith('_')) party = party.substring(0, party.length - 1);

  return CallRecordingName(
    party: party.trim(),
    recordedAt: _parseTimestamp(m.group(1)!),
  );
}

/// 把 `yyyyMMddHHmmss` 解析为本地时间；月份等字段越界时返回 null。
///
/// 用手工解析而非 DateTime.parse：后者会静默接受 13 月这类非法值并进位，
/// 而"文件名里的时间戳"本就可能是号码的一部分（例如纯数字号码恰好 14 位），
/// 严格校验才能避免把号码误当时间。
DateTime? _parseTimestamp(String s) {
  final year = int.parse(s.substring(0, 4));
  final month = int.parse(s.substring(4, 6));
  final day = int.parse(s.substring(6, 8));
  final hour = int.parse(s.substring(8, 10));
  final minute = int.parse(s.substring(10, 12));
  final second = int.parse(s.substring(12, 14));

  if (month < 1 || month > 12) return null;
  if (day < 1 || day > 31) return null;
  if (hour > 23 || minute > 59 || second > 59) return null;

  final dt = DateTime(year, month, day, hour, minute, second);
  // DateTime 构造器会对越界日期进位（如 2 月 31 日 → 3 月 2 日），
  // 回读三个字段即可发现，避免接受"看起来解析成功"的假时间
  if (dt.year != year || dt.month != month || dt.day != day) return null;
  return dt;
}

/// 已知的录音目录候选（相对外部存储根目录）。
///
/// 各 ROM 位置不统一且**没有标准 API**，因此 UI 上仍提供"手动选目录"作为
/// 兜底；这里只是把常见路径列出来，用于首次进入时自动探测/预填，
/// 省去用户手动翻找。
const List<String> kCallRecordingDirCandidates = [
  'Sounds/CallRecord', // 华为/荣耀（实测）
  'Recordings/Call', // 部分 AOSP/三星
  'Recordings',
  'CallRecord',
  'MIUI/sound_recorder/call_rec', // 小米/红米
  'record/call',
  'PhoneRecord',
  'Call',
];

/// 判断一个录音文件本次是否需要上传（增量判定）。
///
/// 抽成纯函数是为了可单测 —— 这个判定有"漏传"风险，必须锁死：
///
/// - [hasRecord] 为 false：新文件，必传。
/// - [recordStatus] 为 failed/pending/uploading：上次没传成功，**必须重试**。
///   这三种状态下 size 与 mtime 通常都没变，若只比对 size/mtime 会永远跳过它，
///   录音就永远上不去服务器。
/// - stale：上传后文件又变了，需重传。
/// - size 或 mtime 变化：需重传。
/// - 其余（completed/skipped 且未变化）：跳过。
bool shouldBackupRecording({
  required bool hasRecord,
  required int? recordStatus,
  required int? recordModifiedAt,
  required int? recordFileSize,
  required int currentModifiedAt,
  required int currentFileSize,
}) {
  if (!hasRecord) return true;

  switch (recordStatus) {
    case kBackupFileStatusStale:
    case kBackupFileStatusFailed:
      return true;
    // 注意：pending / uploading **不再**无条件重传。
    //
    // 曾经的规则是"这两个状态没传成功，必须重试"，初衷是防止漏传，
    // 但实践中每个文件入队时都会先写 pending，若上传流程正常完成会回写
    // completed；一旦回写链路缺失（例如任务没带 tags），pending 永不消除，
    // 就会把**已成功上传**的文件也每次重传 —— 真机上表现为"点一次备份
    // 重传全部 438 个"。漏传由 failed 状态覆盖，无需 pending 兜底。
  }

  if (recordModifiedAt != null && currentModifiedAt != recordModifiedAt) {
    return true;
  }
  if (recordFileSize != null && currentFileSize != recordFileSize) {
    return true;
  }
  return false;
}

/// [BackupFileStatus] 各状态的整数值。
///
/// 这里用常量而不是直接引用枚举：本文件是"纯逻辑"层，不依赖 services，
/// 以免引入 Flutter/GetIt 依赖导致单测必须起容器。
/// **数值必须与 client_backup_service.dart 的 BackupFileStatus 保持一致**，
/// 并有单测对此做断言。
const int kBackupFileStatusPending = 0;
const int kBackupFileStatusUploading = 1;
const int kBackupFileStatusCompleted = 2;
const int kBackupFileStatusFailed = 3;
const int kBackupFileStatusSkipped = 4;
const int kBackupFileStatusStale = 5;
