// import 'package:equatable/equatable.dart';
// import 'package:pfm/services/chunk_upload_service.dart';

// // 导出 UploadTaskStatus
// export 'package:pfm/services/chunk_upload_service.dart' show UploadTaskStatus;

// /// 上传状态
// enum UploadStatus {
//   initial,
//   loading,
//   hashing,  // 正在计算文件哈希
//   uploading,
//   paused,
//   completed,
//   failed,
// }

// /// 上传任务状态
// class UploadTaskState extends Equatable {
//   final String taskId;
//   final String filePath;
//   final String fileName;
//   final int fileSize;
//   final String? parentId;
//   final UploadTaskStatus status;
//   final int uploadedChunks;
//   final int totalChunks;
//   final String? errorMessage;
//   final String? fileId;
//   final double hashProgress; // 哈希计算进度 (0-100)
//   final int uploadedBytes; // 已上传字节数
//   final double speed; // 上传速度 (字节/秒)
//   final double maxProgress; // 最大进度（防止回退）

//   const UploadTaskState({
//     required this.taskId,
//     required this.filePath,
//     required this.fileName,
//     required this.fileSize,
//     this.parentId,
//     required this.status,
//     this.uploadedChunks = 0,
//     this.totalChunks = 1,
//     this.errorMessage,
//     this.fileId,
//     this.hashProgress = 0,
//     this.uploadedBytes = 0,
//     this.speed = 0,
//     this.maxProgress = 0,
//   });

//   /// 上传进度（0-100），取当前进度和最大进度中的最大值
//   double get progress {
//     if (totalChunks == 0) return 0;
//     final currentProgress = (uploadedChunks / totalChunks) * 100;
//     return currentProgress > maxProgress ? currentProgress : maxProgress;
//   }

//   /// 已上传大小（字节）
//   int get uploadedSize => uploadedBytes;

//   /// 格式化上传速度
//   String get formattedSpeed {
//     if (speed < 1024) {
//       return '${speed.toStringAsFixed(0)} B/s';
//     } else if (speed < 1024 * 1024) {
//       return '${(speed / 1024).toStringAsFixed(1)} KB/s';
//     } else {
//       return '${(speed / (1024 * 1024)).toStringAsFixed(2)} MB/s';
//     }
//   }

//   /// 是否上传中
//   bool get isUploading => status == UploadTaskStatus.uploading;

//   /// 是否已完成
//   bool get isCompleted => status == UploadTaskStatus.completed;

//   /// 是否失败
//   bool get isFailed => status == UploadTaskStatus.failed;

//   /// 是否暂停
//   bool get isPaused => status == UploadTaskStatus.paused;

//   /// 是否正在计算哈希
//   bool get isHashing => status == UploadTaskStatus.pending && hashProgress > 0 && hashProgress < 100;

//   UploadTaskState copyWith({
//     UploadTaskStatus? status,
//     int? uploadedChunks,
//     int? totalChunks,
//     String? errorMessage,
//     String? fileId,
//     double? hashProgress,
//     int? uploadedBytes,
//     double? speed,
//     double? maxProgress,
//   }) {
//     return UploadTaskState(
//       taskId: taskId,
//       filePath: filePath,
//       fileName: fileName,
//       fileSize: fileSize,
//       parentId: parentId,
//       status: status ?? this.status,
//       uploadedChunks: uploadedChunks ?? this.uploadedChunks,
//       totalChunks: totalChunks ?? this.totalChunks,
//       errorMessage: errorMessage ?? this.errorMessage,
//       fileId: fileId ?? this.fileId,
//       hashProgress: hashProgress ?? this.hashProgress,
//       uploadedBytes: uploadedBytes ?? this.uploadedBytes,
//       speed: speed ?? this.speed,
//       maxProgress: maxProgress ?? this.maxProgress,
//     );
//   }

//   @override
//   List<Object?> get props => [
//         taskId,
//         filePath,
//         fileName,
//         fileSize,
//         parentId,
//         status,
//         uploadedChunks,
//         totalChunks,
//         errorMessage,
//         fileId,
//         hashProgress,
//         uploadedBytes,
//         speed,
//         maxProgress,
//       ];
// }

// /// 整体上传状态
// class UploadState extends Equatable {
//   final UploadStatus status;
//   final List<UploadTaskState> tasks;
//   final String? errorMessage;

//   const UploadState({
//     this.status = UploadStatus.initial,
//     this.tasks = const [],
//     this.errorMessage,
//   });

//   /// 获取正在上传的任务
//   List<UploadTaskState> get uploadingTasks =>
//       tasks.where((t) => t.isUploading).toList();

//   /// 获取已完成的任务
//   List<UploadTaskState> get completedTasks =>
//       tasks.where((t) => t.isCompleted).toList();

//   /// 获取失败的任务
//   List<UploadTaskState> get failedTasks =>
//       tasks.where((t) => t.isFailed).toList();

//   /// 总上传进度
//   double get totalProgress {
//     if (tasks.isEmpty) return 0;
//     final totalProgress = tasks.fold<double>(0, (sum, t) => sum + t.progress);
//     return totalProgress / tasks.length;
//   }

//   /// 总上传速度
//   double get totalSpeed {
//     return tasks
//         .where((t) => t.isUploading)
//         .fold<double>(0, (sum, t) => sum + t.speed);
//   }

//   /// 格式化总上传速度
//   String get formattedTotalSpeed {
//     final speed = totalSpeed;
//     if (speed < 1024) {
//       return '${speed.toStringAsFixed(0)} B/s';
//     } else if (speed < 1024 * 1024) {
//       return '${(speed / 1024).toStringAsFixed(1)} KB/s';
//     } else {
//       return '${(speed / (1024 * 1024)).toStringAsFixed(2)} MB/s';
//     }
//   }

//   UploadState copyWith({
//     UploadStatus? status,
//     List<UploadTaskState>? tasks,
//     String? errorMessage,
//   }) {
//     return UploadState(
//       status: status ?? this.status,
//       tasks: tasks ?? this.tasks,
//       errorMessage: errorMessage ?? this.errorMessage,
//     );
//   }

//   @override
//   List<Object?> get props => [status, tasks, errorMessage];
// }
