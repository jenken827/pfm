/// 通话录音备份配置（本地）
///
/// 与短信/通话记录备份（[DeviceBackupConfig]，导出后可不管本地）不同，
/// 录音是**磁盘上已有的文件**，备份是"搬运"：需要记住源目录以便下次增量扫描，
/// 并用 [BackupFileStatus] 记录每个文件的上传状态实现断点续传。
class CallRecordingBackupConfig {
  /// 录音源目录（绝对路径，如 /storage/emulated/0/Sounds/CallRecord）。
  /// 各 ROM 位置不统一且无标准 API，由用户选择或自动探测预填。
  final String sourceDir;

  /// 服务端目标文件夹 ID
  final String targetParentId;

  /// 目标文件夹名称（用于显示）
  final String targetFolderName;

  /// 上次执行时间
  final DateTime? lastRunAt;

  /// 上次扫描到的文件数
  final int lastScanCount;

  /// 上次需要上传的文件数
  final int lastBackupCount;

  /// 上次需要上传的字节数
  final int lastBackupSize;

  const CallRecordingBackupConfig({
    this.sourceDir = '',
    this.targetParentId = '',
    this.targetFolderName = '',
    this.lastRunAt,
    this.lastScanCount = 0,
    this.lastBackupCount = 0,
    this.lastBackupSize = 0,
  });

  /// 是否已配置完整（源目录与目标目录都已选）
  bool get isConfigured => sourceDir.isNotEmpty && targetParentId.isNotEmpty;

  CallRecordingBackupConfig copyWith({
    String? sourceDir,
    String? targetParentId,
    String? targetFolderName,
    DateTime? lastRunAt,
    int? lastScanCount,
    int? lastBackupCount,
    int? lastBackupSize,
  }) {
    return CallRecordingBackupConfig(
      sourceDir: sourceDir ?? this.sourceDir,
      targetParentId: targetParentId ?? this.targetParentId,
      targetFolderName: targetFolderName ?? this.targetFolderName,
      lastRunAt: lastRunAt ?? this.lastRunAt,
      lastScanCount: lastScanCount ?? this.lastScanCount,
      lastBackupCount: lastBackupCount ?? this.lastBackupCount,
      lastBackupSize: lastBackupSize ?? this.lastBackupSize,
    );
  }

  Map<String, dynamic> toJson() => {
    'sourceDir': sourceDir,
    'targetParentId': targetParentId,
    'targetFolderName': targetFolderName,
    'lastRunAt': lastRunAt?.toIso8601String(),
    'lastScanCount': lastScanCount,
    'lastBackupCount': lastBackupCount,
    'lastBackupSize': lastBackupSize,
  };

  factory CallRecordingBackupConfig.fromJson(Map<String, dynamic> json) {
    return CallRecordingBackupConfig(
      sourceDir: json['sourceDir'] ?? '',
      targetParentId: json['targetParentId'] ?? '',
      targetFolderName: json['targetFolderName'] ?? '',
      lastRunAt: json['lastRunAt'] != null
          ? DateTime.parse(json['lastRunAt'])
          : null,
      lastScanCount: json['lastScanCount'] ?? 0,
      lastBackupCount: json['lastBackupCount'] ?? 0,
      lastBackupSize: json['lastBackupSize'] ?? 0,
    );
  }
}
