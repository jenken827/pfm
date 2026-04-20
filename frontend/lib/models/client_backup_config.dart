/// 客户端自动备份配置
///
/// 存储在 Hive 中，配置存储在客户端本地
class ClientBackupConfig {
  /// 配置ID (UUID)
  String id;

  /// 任务名称
  String name;

  /// 本地源路径（客户端文件系统路径）
  String sourcePath;

  /// 服务端目标目录ID
  String targetDirId;

  /// 备份策略：scheduled / watch（仅桌面端支持 watch）
  String strategy;

  /// Cron 表达式（仅 scheduled 策略）
  String? cronExpression;

  /// 过滤规则（兼容 .gitignore 语法）
  String? ignoreRules;

  /// 是否包含子目录
  bool recursive;

  /// 是否启用
  bool enabled;

  /// 上次执行时间
  DateTime? lastRunAt;

  /// 上次扫描到的文件数
  int lastScanCount;

  /// 上次需要备份的文件数
  int lastBackupCount;

  /// 上次备份的文件总大小
  int lastBackupSize;

  /// 创建时间
  DateTime createdAt;

  /// 更新时间
  DateTime updatedAt;

  ClientBackupConfig({
    required this.id,
    required this.name,
    required this.sourcePath,
    required this.targetDirId,
    this.strategy = 'scheduled',
    this.cronExpression,
    this.ignoreRules,
    this.recursive = true,
    this.enabled = true,
    this.lastRunAt,
    this.lastScanCount = 0,
    this.lastBackupCount = 0,
    this.lastBackupSize = 0,
    DateTime? createdAt,
    DateTime? updatedAt,
  })  : createdAt = createdAt ?? DateTime.now(),
        updatedAt = updatedAt ?? DateTime.now();

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'sourcePath': sourcePath,
        'targetDirId': targetDirId,
        'strategy': strategy,
        'cronExpression': cronExpression,
        'ignoreRules': ignoreRules,
        'recursive': recursive,
        'enabled': enabled,
        'lastRunAt': lastRunAt?.toIso8601String(),
        'lastScanCount': lastScanCount,
        'lastBackupCount': lastBackupCount,
        'lastBackupSize': lastBackupSize,
        'createdAt': createdAt.toIso8601String(),
        'updatedAt': updatedAt.toIso8601String(),
      };

  static ClientBackupConfig fromJson(Map<String, dynamic> json) =>
      ClientBackupConfig(
        id: json['id'],
        name: json['name'],
        sourcePath: json['sourcePath'],
        targetDirId: json['targetDirId'],
        strategy: json['strategy'] ?? 'scheduled',
        cronExpression: json['cronExpression'],
        ignoreRules: json['ignoreRules'],
        recursive: json['recursive'] ?? true,
        enabled: json['enabled'] ?? true,
        lastRunAt: json['lastRunAt'] != null
            ? DateTime.parse(json['lastRunAt'])
            : null,
        lastScanCount: json['lastScanCount'] ?? 0,
        lastBackupCount: json['lastBackupCount'] ?? 0,
        lastBackupSize: json['lastBackupSize'] ?? 0,
        createdAt: json['createdAt'] != null
            ? DateTime.parse(json['createdAt'])
            : DateTime.now(),
        updatedAt: json['updatedAt'] != null
            ? DateTime.parse(json['updatedAt'])
            : DateTime.now(),
      );

  ClientBackupConfig copyWith({
    String? name,
    String? sourcePath,
    String? targetDirId,
    String? strategy,
    String? cronExpression,
    String? ignoreRules,
    bool? recursive,
    bool? enabled,
    DateTime? lastRunAt,
    int? lastScanCount,
    int? lastBackupCount,
    int? lastBackupSize,
  }) {
    return ClientBackupConfig(
      id: id,
      name: name ?? this.name,
      sourcePath: sourcePath ?? this.sourcePath,
      targetDirId: targetDirId ?? this.targetDirId,
      strategy: strategy ?? this.strategy,
      cronExpression: cronExpression ?? this.cronExpression,
      ignoreRules: ignoreRules ?? this.ignoreRules,
      recursive: recursive ?? this.recursive,
      enabled: enabled ?? this.enabled,
      lastRunAt: lastRunAt ?? this.lastRunAt,
      lastScanCount: lastScanCount ?? this.lastScanCount,
      lastBackupCount: lastBackupCount ?? this.lastBackupCount,
      lastBackupSize: lastBackupSize ?? this.lastBackupSize,
      createdAt: createdAt,
      updatedAt: DateTime.now(),
    );
  }
}
