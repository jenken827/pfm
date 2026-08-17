part of 'backup_bloc.dart';

abstract class BackupEvent extends Equatable {
  const BackupEvent();

  @override
  List<Object?> get props => [];
}

/// 加载备份任务列表
class BackupLoadEvent extends BackupEvent {}

/// 创建备份任务
class BackupCreateEvent extends BackupEvent {
  final String name;
  final String sourcePath;
  final String targetDirId;
  final BackupStrategy? strategy;
  final String? cronExpression;
  final String? ignoreRules;
  final bool? recursive;
  final bool? enabled;

  const BackupCreateEvent({
    required this.name,
    required this.sourcePath,
    required this.targetDirId,
    this.strategy,
    this.cronExpression,
    this.ignoreRules,
    this.recursive,
    this.enabled,
  });

  @override
  List<Object?> get props => [
    name,
    sourcePath,
    targetDirId,
    strategy,
    cronExpression,
    ignoreRules,
    recursive,
    enabled,
  ];
}

/// 更新备份任务
class BackupUpdateEvent extends BackupEvent {
  final int id;
  final String? name;
  final String? sourcePath;
  final String? targetDirId;
  final BackupStrategy? strategy;
  final String? cronExpression;
  final String? ignoreRules;
  final bool? recursive;
  final bool? enabled;

  const BackupUpdateEvent({
    required this.id,
    this.name,
    this.sourcePath,
    this.targetDirId,
    this.strategy,
    this.cronExpression,
    this.ignoreRules,
    this.recursive,
    this.enabled,
  });

  @override
  List<Object?> get props => [
    id,
    name,
    sourcePath,
    targetDirId,
    strategy,
    cronExpression,
    ignoreRules,
    recursive,
    enabled,
  ];
}

/// 删除备份任务
class BackupDeleteEvent extends BackupEvent {
  final List<int> ids;

  const BackupDeleteEvent({required this.ids});

  @override
  List<Object?> get props => [ids];
}

/// 手动触发备份
class BackupTriggerEvent extends BackupEvent {
  final int id;

  const BackupTriggerEvent({required this.id});

  @override
  List<Object?> get props => [id];
}

/// 加载备份文件记录
class BackupLoadFilesEvent extends BackupEvent {
  final int taskId;

  const BackupLoadFilesEvent({required this.taskId});

  @override
  List<Object?> get props => [taskId];
}
