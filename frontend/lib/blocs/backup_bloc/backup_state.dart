part of 'backup_bloc.dart';

enum BackupStatus { initial, loading, success, failure }

class BackupState extends Equatable {
  final BackupStatus status;
  final List<BackupTask> tasks;
  final int total;
  final List<BackupFileRecord> files;
  final int filesTotal;
  final String? message;

  const BackupState({
    this.status = BackupStatus.initial,
    this.tasks = const [],
    this.total = 0,
    this.files = const [],
    this.filesTotal = 0,
    this.message,
  });

  BackupState copyWith({
    BackupStatus? status,
    List<BackupTask>? tasks,
    int? total,
    List<BackupFileRecord>? files,
    int? filesTotal,
    String? message,
  }) {
    return BackupState(
      status: status ?? this.status,
      tasks: tasks ?? this.tasks,
      total: total ?? this.total,
      files: files ?? this.files,
      filesTotal: filesTotal ?? this.filesTotal,
      message: message ?? this.message,
    );
  }

  @override
  List<Object?> get props => [status, tasks, total, files, filesTotal, message];
}
