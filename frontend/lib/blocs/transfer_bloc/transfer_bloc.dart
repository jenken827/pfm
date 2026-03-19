import 'dart:async';

import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:pfm/blocs/upload_bloc/upload_bloc.dart';
import 'package:pfm/blocs/upload_bloc/upload_event.dart';
import 'package:pfm/blocs/upload_bloc/upload_state.dart';
import 'transfer_event.dart';
import 'transfer_state.dart';

/// 传输 Bloc - 管理上传和下载任务
class TransferBloc extends Bloc<TransferEvent, TransferState> {
  final UploadBloc _uploadBloc;
  StreamSubscription<UploadState>? _uploadSubscription;

  TransferBloc({
    required UploadBloc uploadBloc,
  })  : _uploadBloc = uploadBloc,
        super(const TransferState()) {
    on<TransferRefreshEvent>(_onRefresh);
    on<TransferClearCompletedEvent>(_onClearCompleted);
    on<TransferClearFailedEvent>(_onClearFailed);
    on<TransferPauseAllUploadsEvent>(_onPauseAllUploads);
    on<TransferResumeAllUploadsEvent>(_onResumeAllUploads);
    on<TransferCancelAllUploadsEvent>(_onCancelAllUploads);

    // 监听 UploadBloc 状态变化
    _uploadSubscription = _uploadBloc.stream.listen((uploadState) {
      _syncUploadTasks(uploadState);
    });

    // 初始化时同步当前状态
    _syncUploadTasks(_uploadBloc.state);
  }

  /// 同步上传任务状态
  void _syncUploadTasks(UploadState uploadState) {
    emit(state.copyWith(uploadTasks: uploadState.tasks));
  }

  /// 刷新传输列表
  Future<void> _onRefresh(
    TransferRefreshEvent event,
    Emitter<TransferState> emit,
  ) async {
    // 重新同步上传任务
    _syncUploadTasks(_uploadBloc.state);
  }

  /// 清除已完成的传输任务
  Future<void> _onClearCompleted(
    TransferClearCompletedEvent event,
    Emitter<TransferState> emit,
  ) async {
    _uploadBloc.add(const UploadClearCompletedEvent());
  }

  /// 清除失败的传输任务
  Future<void> _onClearFailed(
    TransferClearFailedEvent event,
    Emitter<TransferState> emit,
  ) async {
    final failedTaskIds = state.uploadTasks
        .where((t) => t.isFailed)
        .map((t) => t.taskId)
        .toList();

    for (final taskId in failedTaskIds) {
      _uploadBloc.add(UploadRemoveEvent(taskId));
    }
  }

  /// 暂停所有上传
  Future<void> _onPauseAllUploads(
    TransferPauseAllUploadsEvent event,
    Emitter<TransferState> emit,
  ) async {
    for (final task in state.uploadTasks.where((t) => t.isUploading)) {
      _uploadBloc.add(UploadPauseEvent(task.taskId));
    }
  }

  /// 恢复所有上传
  Future<void> _onResumeAllUploads(
    TransferResumeAllUploadsEvent event,
    Emitter<TransferState> emit,
  ) async {
    for (final task in state.uploadTasks.where((t) => t.isPaused)) {
      _uploadBloc.add(UploadResumeEvent(task.taskId));
    }
  }

  /// 取消所有上传
  Future<void> _onCancelAllUploads(
    TransferCancelAllUploadsEvent event,
    Emitter<TransferState> emit,
  ) async {
    for (final task in state.uploadTasks.where((t) => t.isUploading || t.isPaused)) {
      _uploadBloc.add(UploadCancelEvent(task.taskId));
    }
  }

  @override
  Future<void> close() {
    _uploadSubscription?.cancel();
    return super.close();
  }
}
