/// 短信/通话记录备份配置（任务9，两类备份共用同一结构，按类型分别存储）
class DeviceBackupConfig {
  /// 目标文件夹 ID
  final String targetParentId;

  /// 目标文件夹名称（用于显示）
  final String targetFolderName;

  /// 上次备份的文件 hash
  final String? lastBackupHash;

  /// 上次备份时间
  final DateTime? lastBackupTime;

  /// 上次备份的本地文件路径
  final String? lastBackupFilePath;

  /// 上次备份的来源设备标识（设备名/厂商型号）。
  ///
  /// 短信与通话记录是本机独有数据，多设备备份到同一目录会混在一起，
  /// 页面上靠它告诉用户"上次备份来自哪台设备"；同时它也参与导出的
  /// 文件名与内容（见 DeviceBackupService）。老版本无此字段时为空。
  final String? deviceLabel;

  const DeviceBackupConfig({
    this.targetParentId = '',
    this.targetFolderName = '',
    this.lastBackupHash,
    this.lastBackupTime,
    this.lastBackupFilePath,
    this.deviceLabel,
  });

  DeviceBackupConfig copyWith({
    String? targetParentId,
    String? targetFolderName,
    String? lastBackupHash,
    DateTime? lastBackupTime,
    String? lastBackupFilePath,
    String? deviceLabel,
  }) {
    return DeviceBackupConfig(
      targetParentId: targetParentId ?? this.targetParentId,
      targetFolderName: targetFolderName ?? this.targetFolderName,
      lastBackupHash: lastBackupHash ?? this.lastBackupHash,
      lastBackupTime: lastBackupTime ?? this.lastBackupTime,
      lastBackupFilePath: lastBackupFilePath ?? this.lastBackupFilePath,
      deviceLabel: deviceLabel ?? this.deviceLabel,
    );
  }

  Map<String, dynamic> toJson() => {
    'targetParentId': targetParentId,
    'targetFolderName': targetFolderName,
    'lastBackupHash': lastBackupHash,
    'lastBackupTime': lastBackupTime?.toIso8601String(),
    'lastBackupFilePath': lastBackupFilePath,
    'deviceLabel': deviceLabel,
  };

  factory DeviceBackupConfig.fromJson(Map<String, dynamic> json) {
    return DeviceBackupConfig(
      targetParentId: json['targetParentId'] ?? '',
      targetFolderName: json['targetFolderName'] ?? '',
      lastBackupHash: json['lastBackupHash'],
      lastBackupTime: json['lastBackupTime'] != null
          ? DateTime.parse(json['lastBackupTime'])
          : null,
      lastBackupFilePath: json['lastBackupFilePath'],
      deviceLabel: json['deviceLabel'],
    );
  }
}
