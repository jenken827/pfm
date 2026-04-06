import 'dart:async';
import 'dart:io';

import 'package:equatable/equatable.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:path/path.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/proto/native/enum.pb.dart';
import 'package:pfm/services/services.dart';
import 'package:uuid/uuid.dart';

part 'transfer_event.dart';
part 'transfer_state.dart';

/// 传输 Bloc - 管理上传和下载任务
class TransferBloc extends Bloc<TransferEvent, TransferState> {
  TransferBloc(this._bgService) : super(const TransferState()) {
    on<TransferUploadSubscribeEvent>(_onUploadSubscribe);
    on<TransferDownloadSubscribeEvent>(_onDownloadSubscribe);
    on<TransferRefreshEvent>(_onRefresh);
    on<TransferClearCompletedEvent>(_onClearCompleted);
    on<TransferClearFailedEvent>(_onClearFailed);
    on<TransferPauseEvent>(_onPause);
    on<TransferResumeEvent>(_onResume);
    on<TransferCancelEvent>(_onCancel);
    on<TransferRemoveEvent>(_onRemove);
    on<TransferAddUploadEvent>(_onAddUpload);
  }

  final BackgroundService _bgService;

  Future<void> _onUploadSubscribe(
    TransferUploadSubscribeEvent event,
    Emitter<TransferState> emit,
  ) async {
    await emit.forEach(
      _bgService.uploadTaskStream,
      onData: (tasks) {
        return state.copyWith(
          uploadTasks: tasks,
          fakeStatus: state.fakeStatus + 1,
        );
      },
    );
  }

  Future<void> _onDownloadSubscribe(
    TransferDownloadSubscribeEvent event,
    Emitter<TransferState> emit,
  ) async {
    // await emit.forEach(
    //   _bgService.downloadTaskStream,
    //   onData: (tasks) {
    //     return state.copyWith(downloadTasks: tasks);
    //   },
    // );
  }

  /// 刷新传输列表
  Future<void> _onRefresh(
    TransferRefreshEvent event,
    Emitter<TransferState> emit,
  ) async {
    if (state.isUpload) {
      final tasks = await _bgService.storage.getAllUploadTasksAsync();
      emit(
        state.copyWith(uploadTasks: tasks, fakeStatus: state.fakeStatus + 1),
      );
    }
  }

  /// 清除已完成的传输任务
  Future<void> _onClearCompleted(
    TransferClearCompletedEvent event,
    Emitter<TransferState> emit,
  ) async {
    if (state.isUpload) {
      await _bgService.storage.clearUploadCompleted();
      final tasks = await _bgService.storage.getAllUploadTasksAsync();
      emit(
        state.copyWith(uploadTasks: tasks, fakeStatus: state.fakeStatus + 1),
      );
    }
  }

  /// 清除失败的传输任务
  Future<void> _onClearFailed(
    TransferClearFailedEvent event,

    Emitter<TransferState> emit,
  ) async {
    if (state.isUpload) {
      await _bgService.storage.clearUploadFailed();
      final tasks = await _bgService.storage.getAllUploadTasksAsync();
      emit(
        state.copyWith(uploadTasks: tasks, fakeStatus: state.fakeStatus + 1),
      );
    }
  }

  /// 暂停所有上传
  Future<void> _onPause(
    TransferPauseEvent event,
    Emitter<TransferState> emit,
  ) async {
    if (state.isUpload) {
      await _bgService.storage.pauseUploadTasks(event.taskId);
      final tasks = await _bgService.storage.getAllUploadTasksAsync();
      emit(
        state.copyWith(uploadTasks: tasks, fakeStatus: state.fakeStatus + 1),
      );
    }
  }

  /// 恢复所有上传
  Future<void> _onResume(
    TransferResumeEvent event,
    Emitter<TransferState> emit,
  ) async {
    if (state.isUpload) {
      await _bgService.resumeUploadTasks(event.taskId);

      final tasks = await _bgService.storage.getAllUploadTasksAsync();

      emit(
        state.copyWith(uploadTasks: tasks, fakeStatus: state.fakeStatus + 1),
      );
    }
  }

  /// 取消所有上传
  Future<void> _onCancel(
    TransferCancelEvent event,
    Emitter<TransferState> emit,
  ) async {
    if (state.isUpload) {
      await _bgService.storage.cancelUploadTask(event.taskId);
      final tasks = await _bgService.storage.getAllUploadTasksAsync();
      emit(
        state.copyWith(uploadTasks: tasks, fakeStatus: state.fakeStatus + 1),
      );
    }
  }

  /// 删除
  Future<void> _onRemove(
    TransferRemoveEvent event,
    Emitter<TransferState> emit,
  ) async {
    if (state.isUpload) {
      await _bgService.storage.deleteUploadTask(event.taskId);
      final tasks = await _bgService.storage.getAllUploadTasksAsync();
      emit(
        state.copyWith(uploadTasks: tasks, fakeStatus: state.fakeStatus + 1),
      );
    }
  }

  Future<void> _onAddUpload(
    TransferAddUploadEvent event,

    Emitter<TransferState> emit,
  ) async {
    try {
      final fileName = basename(event.filePath);

      // 获取文件信息
      final file = File(event.filePath);
      if (!await file.exists()) {
        getLogger().error('文件不存在：${event.filePath}');
        return;
      }

      final stat = await file.stat();
      final fileSize = stat.size;

      // 创建持久化记录（Hash 会在后台计算）
      final record = UploadTaskRecord(
        id: Uuid().v8(),
        filepath: event.filePath,
        filename: fileName,
        fileSize: fileSize,
        parentId: event.parentId,
        status: UploadStatus.PENDING,
        tags: event.tags,
        overwrite: event.overwrite,
      );

      // 调度后台任务（包含 Hash 计算）
      await _bgService.scheduleNewUploadTask(record);

      getLogger().info(
        'Scheduled upload task: ${record.id}, hash will be computed in background',
      );
    } catch (e) {
      getLogger().error('Start upload failed: $e');
    }
  }
}
