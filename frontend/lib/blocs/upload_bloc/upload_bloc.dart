// import 'dart:async';
// import 'dart:io';

// import 'package:flutter_bloc/flutter_bloc.dart';
// import 'package:path/path.dart' as p;
// import 'package:pfm/api/api.dart';
// import 'package:pfm/models/upload_task_record.dart';
// import 'package:pfm/services/background_service.dart';
// import 'package:pfm/services/chunk_upload_service.dart';
// import 'package:pfm/services/logger.dart';
// import 'upload_event.dart';
// import 'upload_state.dart';

// /// 上传 Bloc - 支持前后台切换
// class UploadBloc extends Bloc<UploadEvent, UploadState> {
//   final BackgroundService _backgroundService;
//   final Map<String, UploadTask> _tasks = {};
//   Timer? _syncTimer;

//   UploadBloc({
//     required FmRepository repository,
//     required BackgroundService backgroundService,
//   }) : _backgroundService = backgroundService,
//        super(const UploadState()) {
//     on<UploadStartEvent>(_onStartUpload);
//     on<UploadPauseEvent>(_onPauseUpload);
//     on<UploadResumeEvent>(_onResumeUpload);
//     on<UploadCancelEvent>(_onCancelUpload);
//     on<UploadRetryEvent>(_onRetryUpload);
//     on<UploadRemoveEvent>(_onRemoveUpload);
//     on<UploadClearCompletedEvent>(_onClearCompleted);
//     on<_UploadTaskUpdatedEvent>(_onTaskUpdated);

//     // 监听存储变化，同步状态
//     _startStorageSync();

//     // 初始化时加载未完成的任务
//     _loadPendingTasks();
//   }

//   /// 开始同步
//   void _startStorageSync() {
//     _syncTimer = Timer.periodic(const Duration(milliseconds: 500), (_) async {
//       await _syncFromStorage();
//     });
//   }

//   /// 从存储同步任务状态
//   Future<void> _syncFromStorage() async {
//     final storageTasks = await _backgroundService.storage
//         .getAllUploadTasksAsync();

//     for (final record in storageTasks) {
//       final existingTask = _tasks[record.id];
//       if (existingTask == null) {
//         // 新任务，添加到内存
//         final task = _convertRecordToTask(record);
//         _tasks[record.id] = task;
//       } else {
//         // 更新现有任务状态
//         existingTask.status = _convertStatus(record.status);
//         existingTask.uploadedChunks = record.uploadedChunks.length;
//         existingTask.uploadedBytes = record.uploadedBytes;
//         existingTask.speed = record.speed;
//         existingTask.maxProgress = record.maxProgress;
//         existingTask.errorMessage = record.errorMessage;
//         existingTask.fileId = record.fileId;
//         existingTask.uploadId = record.uploadId;
//         existingTask.fileHash = record.fileHash;
//         existingTask.hashProgress = record.hashProgress;
//       }
//     }

//     // 更新 UI 状态
//     add(const _UploadTaskUpdatedEvent());
//   }

//   /// 加载待处理任务
//   Future<void> _loadPendingTasks() async {
//     final pendingRecords = await _backgroundService.storage
//         .getPendingUploadTasksAsync();

//     for (final record in pendingRecords) {
//       final task = _convertRecordToTask(record);
//       _tasks[record.id] = task;
//     }

//     add(const _UploadTaskUpdatedEvent());
//   }

//   /// 开始上传
//   Future<void> _onStartUpload(
//     UploadStartEvent event,
//     Emitter<UploadState> emit,
//   ) async {
//     try {
//       final fileName = p.basename(event.filePath);

//       // 获取文件信息
//       final file = File(event.filePath);
//       if (!await file.exists()) {
//         emit(
//           state.copyWith(status: UploadStatus.failed, errorMessage: '文件不存在'),
//         );
//         return;
//       }

//       final stat = await file.stat();
//       final fileSize = stat.size;
//       final chunkSize = ChunkUploadService.defaultChunkSize;
//       final chunkTotal = (fileSize / chunkSize).ceil().clamp(1, 999999);

//       // 创建持久化记录（Hash 会在后台计算）
//       final record = UploadTaskRecord(
//         id: DateTime.now().millisecondsSinceEpoch.toString(),
//         filePath: event.filePath,
//         fileName: fileName,
//         fileSize: fileSize,
//         parentId: event.parentId,
//         chunkSize: chunkSize,
//         chunkTotal: chunkTotal,
//         status: UploadTaskStatus.pending,
//         mime: _getMimeFromFileName(fileName),
//       );

//       // 调度后台任务（包含 Hash 计算）
//       await _backgroundService.scheduleUpload(record);

//       // 添加到内存（状态为 initializing）
//       final task = _convertRecordToTask(record);
//       task.status = UploadTaskStatus.initializing;
//       _tasks[record.id] = task;

//       add(const _UploadTaskUpdatedEvent());

//       getLogger().info(
//         'Scheduled upload task: ${record.id}, hash will be computed in background',
//       );
//     } catch (e) {
//       getLogger().error('Start upload failed: $e');
//       emit(
//         state.copyWith(status: UploadStatus.failed, errorMessage: e.toString()),
//       );
//     }
//   }

//   /// 根据文件名获取 MIME 类型
//   String? _getMimeFromFileName(String fileName) {
//     final ext = fileName.split('.').last.toLowerCase();
//     switch (ext) {
//       case 'jpg':
//       case 'jpeg':
//         return 'image/jpeg';
//       case 'png':
//         return 'image/png';
//       case 'gif':
//         return 'image/gif';
//       case 'webp':
//         return 'image/webp';
//       case 'mp4':
//         return 'video/mp4';
//       case 'mp3':
//         return 'audio/mpeg';
//       case 'pdf':
//         return 'application/pdf';
//       case 'doc':
//         return 'application/msword';
//       case 'docx':
//         return 'application/vnd.openxmlformats-officedocument.wordprocessingml.document';
//       case 'xls':
//         return 'application/vnd.ms-excel';
//       case 'xlsx':
//         return 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet';
//       case 'zip':
//         return 'application/zip';
//       default:
//         return 'application/octet-stream';
//     }
//   }

//   /// 暂停上传
//   Future<void> _onPauseUpload(
//     UploadPauseEvent event,
//     Emitter<UploadState> emit,
//   ) async {
//     await _backgroundService.pauseUpload(event.taskId);
//     _syncFromStorage();
//   }

//   /// 恢复上传
//   Future<void> _onResumeUpload(
//     UploadResumeEvent event,
//     Emitter<UploadState> emit,
//   ) async {
//     await _backgroundService.resumeUpload(event.taskId);
//     _syncFromStorage();
//   }

//   /// 取消上传
//   Future<void> _onCancelUpload(
//     UploadCancelEvent event,
//     Emitter<UploadState> emit,
//   ) async {
//     await _backgroundService.cancelUpload(event.taskId);
//     _syncFromStorage();
//   }

//   /// 重试上传
//   Future<void> _onRetryUpload(
//     UploadRetryEvent event,
//     Emitter<UploadState> emit,
//   ) async {
//     final record = await _backgroundService.storage.getUploadTaskAsync(
//       event.taskId,
//     );
//     if (record != null && record.canRetry) {
//       // 重置状态并重新调度
//       record.status = UploadTaskStatusRecord.pending;
//       record.errorMessage = null;
//       record.retryCount = record.retryCount + 1;
//       await _backgroundService.storage.saveUploadTask(record);

//       await _backgroundService.resumeUpload(event.taskId);
//       await _syncFromStorage();
//     }
//   }

//   /// 移除上传任务
//   Future<void> _onRemoveUpload(
//     UploadRemoveEvent event,
//     Emitter<UploadState> emit,
//   ) async {
//     await _backgroundService.cancelUpload(event.taskId);
//     await _backgroundService.storage.deleteTask(event.taskId);
//     _tasks.remove(event.taskId);
//     add(const _UploadTaskUpdatedEvent());
//   }

//   /// 清除已完成的任务
//   Future<void> _onClearCompleted(
//     UploadClearCompletedEvent event,
//     Emitter<UploadState> emit,
//   ) async {
//     await _backgroundService.storage.clearCompleted();

//     _tasks.removeWhere((_, task) => task.status == UploadTaskStatus.completed);
//     add(const _UploadTaskUpdatedEvent());
//   }

//   /// 任务更新事件处理
//   void _onTaskUpdated(
//     _UploadTaskUpdatedEvent event,
//     Emitter<UploadState> emit,
//   ) {
//     final taskStates = _tasks.values
//         .map((t) => _convertTaskToState(t))
//         .toList();

//     final newState = state.copyWith(
//       status: taskStates.any((t) => t.isUploading)
//           ? UploadStatus.uploading
//           : UploadStatus.initial,
//       tasks: taskStates,
//     );

//     emit(newState);
//   }

//   /// 转换持久化记录为任务对象
//   UploadTask _convertRecordToTask(UploadTaskRecord record) {
//     return UploadTask(
//       id: record.id,
//       filePath: record.filePath,
//       fileName: record.fileName,
//       fileSize: record.fileSize,
//       parentId: record.parentId,
//       fileHash: record.fileHash,
//       chunkSize: record.chunkSize,
//       chunkTotal: record.chunkTotal,
//       uploadedChunks: record.uploadedChunks.length,
//       status: _convertStatus(record.status),
//       errorMessage: record.errorMessage,
//       uploadId: record.uploadId,
//       fileId: record.fileId,
//       hashProgress: record.hashProgress,
//     );
//   }

//   /// 转换状态枚举
//   UploadTaskStatus _convertStatus(UploadTaskStatusRecord status) {
//     switch (status) {
//       case UploadTaskStatusRecord.pending:
//         return UploadTaskStatus.pending;
//       case UploadTaskStatusRecord.initializing:
//         return UploadTaskStatus.initializing;
//       case UploadTaskStatusRecord.uploading:
//         return UploadTaskStatus.uploading;
//       case UploadTaskStatusRecord.paused:
//         return UploadTaskStatus.paused;
//       case UploadTaskStatusRecord.completing:
//         return UploadTaskStatus.completing;
//       case UploadTaskStatusRecord.completed:
//         return UploadTaskStatus.completed;
//       case UploadTaskStatusRecord.failed:
//         return UploadTaskStatus.failed;
//       case UploadTaskStatusRecord.cancelled:
//         return UploadTaskStatus.cancelled;
//     }
//   }

//   /// 转换任务为状态对象
//   UploadTaskState _convertTaskToState(UploadTask task) {
//     return UploadTaskState(
//       taskId: task.id,
//       filePath: task.filePath,
//       fileName: task.fileName,
//       fileSize: task.fileSize,
//       parentId: task.parentId,
//       status: task.status,
//       uploadedChunks: task.uploadedChunks,
//       totalChunks: task.chunkTotal,
//       errorMessage: task.errorMessage,
//       fileId: task.fileId,
//       hashProgress: task.hashProgress,
//       uploadedBytes: task.uploadedBytes,
//       speed: task.speed,
//       maxProgress: task.maxProgress,
//     );
//   }

//   @override
//   Future<void> close() {
//     _syncTimer?.cancel();
//     return super.close();
//   }
// }

// /// 内部事件：任务更新
// class _UploadTaskUpdatedEvent extends UploadEvent {
//   const _UploadTaskUpdatedEvent();
// }
