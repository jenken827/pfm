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
    on<TransferAddBatchUploadEvent>(_onAddBatchUpload);
    on<TransferAddDownloadEvent>(_onAddDownload);
    on<TransferTabChangedEvent>(_onTabChanged);
    on<TransferRetryEvent>(_onRetry);
  }

  final BackgroundService _bgService;

  Future<void> _onUploadSubscribe(
    TransferUploadSubscribeEvent event,
    Emitter<TransferState> emit,
  ) async {
    await emit.forEach(
      _bgService.uploadTaskStream,
      onData: (tasks) {
        final newCompletedCount =
            tasks.where((t) => t.status == UploadStatus.COMPLETED).length;
        final oldCompletedCount = state.completedUploadCountSnapshot;
        final hasNewCompleted = newCompletedCount > oldCompletedCount;

        return state.copyWith(
          uploadTasks: tasks,
          fakeStatus: state.fakeStatus + 1,
          // 只有当有新的上传完成时才更新快照
          completedUploadCountSnapshot:
              hasNewCompleted ? newCompletedCount : oldCompletedCount,
        );
      },
    );
  }

  Future<void> _onDownloadSubscribe(
    TransferDownloadSubscribeEvent event,
    Emitter<TransferState> emit,
  ) async {
    await emit.forEach(
      _bgService.downloadTaskStream,
      onData: (records) {
        final tasks = records.map(_recordToState).toList();
        return state.copyWith(downloadTasks: tasks);
      },
    );
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
    } else {
      final records = await _bgService.storage.getAllDownloadTasksAsync();
      final tasks = records.map(_recordToState).toList();
      emit(state.copyWith(downloadTasks: tasks));
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
    } else {
      await _bgService.storage.clearDownloadCompleted();
      final records = await _bgService.storage.getAllDownloadTasksAsync();
      final tasks = records.map(_recordToState).toList();
      emit(state.copyWith(downloadTasks: tasks));
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
    } else {
      // 下载暂不支持单独清除失败，后续可按需添加
    }
  }

  /// 暂停传输
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
    } else {
      await _bgService.pauseDownload(event.taskId!);
      final records = await _bgService.storage.getAllDownloadTasksAsync();
      final tasks = records.map(_recordToState).toList();
      emit(state.copyWith(downloadTasks: tasks));
    }
  }

  /// 恢复传输
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
    } else {
      await _bgService.resumeDownload(event.taskId!);
      final records = await _bgService.storage.getAllDownloadTasksAsync();
      final tasks = records.map(_recordToState).toList();
      emit(state.copyWith(downloadTasks: tasks));
    }
  }

  /// 取消传输
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
    } else {
      await _bgService.cancelDownload(event.taskId!);
      final records = await _bgService.storage.getAllDownloadTasksAsync();
      final tasks = records.map(_recordToState).toList();
      emit(state.copyWith(downloadTasks: tasks));
    }
  }

  /// 删除传输记录
  Future<void> _onRemove(
    TransferRemoveEvent event,
    Emitter<TransferState> emit,
  ) async {
    getLogger().debug(
      '_onRemove: taskId: ${event.taskId}, isUpload: ${state.isUpload}',
    );
    if (state.isUpload) {
      await _bgService.storage.deleteUploadTask(event.taskId);
      final tasks = await _bgService.storage.getAllUploadTasksAsync();
      emit(
        state.copyWith(uploadTasks: tasks, fakeStatus: state.fakeStatus + 1),
      );
    } else {
      await _bgService.storage.deleteDownloadTask(event.taskId);

      final records = await _bgService.storage.getAllDownloadTasksAsync();
      final tasks = records.map(_recordToState).toList();
      emit(state.copyWith(downloadTasks: tasks));
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

  Future<void> _onAddBatchUpload(
    TransferAddBatchUploadEvent event,
    Emitter<TransferState> emit,
  ) async {
    try {
      final records = <UploadTaskRecord>[];

      for (final filePath in event.files) {
        final file = File(filePath);
        if (!await file.exists()) {
          getLogger().warning('文件不存在，跳过：$filePath');
          continue;
        }

        final stat = await file.stat();
        records.add(UploadTaskRecord(
          id: Uuid().v8(),
          filepath: filePath,
          filename: basename(filePath),
          fileSize: stat.size,
          parentId: event.parentId,
          status: UploadStatus.PENDING,
          tags: event.tags,
          overwrite: event.overwrite,
        ));
      }

      if (records.isEmpty) return;

      // 批量写入数据库，只调度第一个任务
      await _bgService.addUploadTasksBatch(records);
      await _bgService.scheduleFirstPendingUpload();

      getLogger().info(
        'Batch added ${records.length} upload tasks',
      );
    } catch (e) {
      getLogger().error('Batch upload failed: $e');
    }
  }

  Future<void> _onAddDownload(
    TransferAddDownloadEvent event,
    Emitter<TransferState> emit,
  ) async {
    try {
      // 检查是否有未完成的下载任务（用于断点续传）
      final existingTask = await _bgService.storage
          .getUncompletedDownloadTaskByFileId(event.fileId);

      if (existingTask != null) {
        // 存在未完成的任务，恢复下载而不是创建新任务
        getLogger().info(
          'Found uncompleted download task for file ${event.fileId}, '
          'resuming from ${existingTask.downloadedBytes} bytes',
        );

        // 重试任务
        _bgService.retryDownload(existingTask.id);
        return;
      }

      // 没有未完成的任务，创建新任务
      final record = DownloadTaskRecord(
        id: Uuid().v8(),
        fileId: event.fileId,
        filename: event.filename,
        fileSize: event.fileSize,
        savePath: event.savePath,
        fileHash: event.fileHash,
        status: DownloadStatus.pending,
        providerId: event.providerId,
      );

      await _bgService.scheduleNewDownloadTask(record);
      getLogger().info('Scheduled download task: ${record.id}');
    } catch (e) {
      getLogger().error('Start download failed: $e');
    }
  }

  void _onTabChanged(
    TransferTabChangedEvent event,
    Emitter<TransferState> emit,
  ) {
    emit(state.copyWith(currentTab: event.tab));
  }

  void _onRetry(TransferRetryEvent event, Emitter<TransferState> emit) {
    _bgService.retryDownload(event.taskId);
  }

  /// DownloadTaskRecord -> DownloadTaskState 转换
  DownloadTaskState _recordToState(DownloadTaskRecord record) {
    return DownloadTaskState(
      taskId: record.id,
      fileName: record.filename,
      fileId: record.fileId,
      fileSize: record.fileSize,
      downloadedSize: record.downloadedBytes,
      status: _toDownloadTaskStatus(record.status),
      errorMessage: record.errorMessage,
      localPath: record.savePath,
    );
  }

  DownloadTaskStatus _toDownloadTaskStatus(DownloadStatus status) {
    switch (status) {
      case DownloadStatus.pending:
        return DownloadTaskStatus.pending;
      case DownloadStatus.downloading:
        return DownloadTaskStatus.downloading;
      case DownloadStatus.completed:
        return DownloadTaskStatus.completed;
      case DownloadStatus.failed:
        return DownloadTaskStatus.failed;
      case DownloadStatus.canceled:
        return DownloadTaskStatus.cancelled;
      case DownloadStatus.paused:
        return DownloadTaskStatus.paused;
      case DownloadStatus.predownload:
        return DownloadTaskStatus.preDownload;
    }
  }
}
