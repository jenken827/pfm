part of 'client_backup_bloc.dart';

abstract class ClientBackupEvent extends Equatable {
  const ClientBackupEvent();

  @override
  List<Object?> get props => [];
}

class ClientBackupLoadEvent extends ClientBackupEvent {
  const ClientBackupLoadEvent();
}

class ClientBackupCreateEvent extends ClientBackupEvent {
  final String id;
  final String name;
  final String sourcePath;
  final String targetDirId;
  final String strategy;
  final String? cronExpression;
  final String? ignoreRules;
  final bool recursive;
  final bool enabled;

  const ClientBackupCreateEvent({
    required this.id,
    required this.name,
    required this.sourcePath,
    required this.targetDirId,
    this.strategy = 'scheduled',
    this.cronExpression,
    this.ignoreRules,
    this.recursive = true,
    this.enabled = true,
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

class ClientBackupUpdateEvent extends ClientBackupEvent {
  final String id;
  final String? name;
  final String? sourcePath;
  final String? targetDirId;
  final String? strategy;
  final String? cronExpression;
  final String? ignoreRules;
  final bool? recursive;
  final bool? enabled;

  const ClientBackupUpdateEvent({
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

class ClientBackupDeleteEvent extends ClientBackupEvent {
  final List<String> ids;

  const ClientBackupDeleteEvent({required this.ids});

  @override
  List<Object?> get props => [ids];
}

class ClientBackupTriggerEvent extends ClientBackupEvent {
  final String id;

  const ClientBackupTriggerEvent({required this.id});

  @override
  List<Object?> get props => [id];
}
