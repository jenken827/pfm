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

  const DeviceBackupConfig({
    this.targetParentId = '',
    this.targetFolderName = '',
    this.lastBackupHash,
    this.lastBackupTime,
    this.lastBackupFilePath,
  });

  DeviceBackupConfig copyWith({
    String? targetParentId,
    String? targetFolderName,
    String? lastBackupHash,
    DateTime? lastBackupTime,
    String? lastBackupFilePath,
  }) {
    return DeviceBackupConfig(
      targetParentId: targetParentId ?? this.targetParentId,
      targetFolderName: targetFolderName ?? this.targetFolderName,
      lastBackupHash: lastBackupHash ?? this.lastBackupHash,
      lastBackupTime: lastBackupTime ?? this.lastBackupTime,
      lastBackupFilePath: lastBackupFilePath ?? this.lastBackupFilePath,
    );
  }

  Map<String, dynamic> toJson() => {
    'targetParentId': targetParentId,
    'targetFolderName': targetFolderName,
    'lastBackupHash': lastBackupHash,
    'lastBackupTime': lastBackupTime?.toIso8601String(),
    'lastBackupFilePath': lastBackupFilePath,
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
    );
  }
}
