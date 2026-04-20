import 'package:equatable/equatable.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/proto/native/backup.pb.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';

part 'backup_event.dart';
part 'backup_state.dart';

/// 备份任务 Bloc
class BackupBloc extends Bloc<BackupEvent, BackupState> {
  final FmRepository _repository;

  BackupBloc(this._repository) : super(const BackupState()) {
    on<BackupLoadEvent>(_onLoad);
    on<BackupCreateEvent>(_onCreate);
    on<BackupUpdateEvent>(_onUpdate);
    on<BackupDeleteEvent>(_onDelete);
    on<BackupTriggerEvent>(_onTrigger);
    on<BackupLoadFilesEvent>(_onLoadFiles);
  }

  Future<void> _onLoad(
    BackupLoadEvent event,
    Emitter<BackupState> emit,
  ) async {
    emit(state.copyWith(status: BackupStatus.loading));

    final result = await _repository.listBackupTasks();

    if (result.success && result.data != null) {
      emit(
        state.copyWith(
          status: BackupStatus.success,
          tasks: result.data!.tasks.toList(),
          total: result.data!.hasTotal() ? result.data!.total.toInt() : 0,
        ),
      );
    } else {
      emit(
        state.copyWith(
          status: BackupStatus.failure,
          message: result.message ?? '获取备份任务失败',
        ),
      );
    }
  }

  Future<void> _onCreate(
    BackupCreateEvent event,
    Emitter<BackupState> emit,
  ) async {
    final result = await _repository.createBackupTask(
      name: event.name,
      sourcePath: event.sourcePath,
      targetDirId: event.targetDirId,
      strategy: event.strategy,
      cronExpression: event.cronExpression,
      ignoreRules: event.ignoreRules,
      recursive: event.recursive,
      enabled: event.enabled,
    );

    if (result.success) {
      add(BackupLoadEvent());
    } else {
      emit(
        state.copyWith(
          status: BackupStatus.failure,
          message: result.message ?? '创建备份任务失败',
        ),
      );
    }
  }

  Future<void> _onUpdate(
    BackupUpdateEvent event,
    Emitter<BackupState> emit,
  ) async {
    final result = await _repository.updateBackupTask(
      id: event.id,
      name: event.name,
      sourcePath: event.sourcePath,
      targetDirId: event.targetDirId,
      strategy: event.strategy,
      cronExpression: event.cronExpression,
      ignoreRules: event.ignoreRules,
      recursive: event.recursive,
      enabled: event.enabled,
    );

    if (result.success) {
      add(BackupLoadEvent());
    } else {
      emit(
        state.copyWith(
          status: BackupStatus.failure,
          message: result.message ?? '更新备份任务失败',
        ),
      );
    }
  }

  Future<void> _onDelete(
    BackupDeleteEvent event,
    Emitter<BackupState> emit,
  ) async {
    final result = await _repository.deleteBackupTask(event.ids);

    if (result.success) {
      add(BackupLoadEvent());
    } else {
      emit(
        state.copyWith(
          status: BackupStatus.failure,
          message: result.message ?? '删除备份任务失败',
        ),
      );
    }
  }

  Future<void> _onTrigger(
    BackupTriggerEvent event,
    Emitter<BackupState> emit,
  ) async {
    final result = await _repository.triggerBackup(event.id);

    if (!result.success) {
      emit(
        state.copyWith(
          status: BackupStatus.failure,
          message: result.message ?? '触发备份失败',
        ),
      );
    }
  }

  Future<void> _onLoadFiles(
    BackupLoadFilesEvent event,
    Emitter<BackupState> emit,
  ) async {
    final result = await _repository.listBackupFiles(taskId: event.taskId);

    if (result.success && result.data != null) {
      emit(
        state.copyWith(
          files: result.data!.files.toList(),
          filesTotal: result.data!.hasTotal() ? result.data!.total.toInt() : 0,
        ),
      );
    }
  }
}
