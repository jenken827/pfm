// import 'dart:async';
// import 'dart:io';
// import 'dart:typed_data';

// import 'package:crypto/crypto.dart';
// import 'package:convert/convert.dart';
// import 'package:path/path.dart' as p;
// import 'package:pfm/api/api.dart';
// import 'package:pfm/proto/native/enum.pbenum.dart';
// import 'package:pfm/services/logger.dart';
// import 'package:talker_flutter/talker_flutter.dart';

// /// 上传任务状态
// enum UploadTaskStatus {
//   pending,
//   initializing,
//   uploading,
//   completing,
//   completed,
//   paused,
//   cancelled,
//   failed,
// }

// /// 上传任务信息
// class UploadTask {
//   final String id;
//   final String filePath;
//   final String fileName;
//   final int fileSize;
//   final String? parentId;
//   final int chunkSize;
//   final int chunkTotal;

//   UploadTaskStatus status;
//   String? uploadId;
//   String? fileHash;
//   int uploadedChunks;
//   Set<int> completedChunkIndexes;
//   String? errorMessage;
//   String? fileId;
//   double hashProgress; // 哈希计算进度 (0-100)
//   int uploadedBytes; // 已上传字节数
//   double speed; // 上传速度 (字节/秒)
//   double maxProgress; // 最大进度（防止回退）
//   DateTime? lastProgressUpdate; // 上次进度更新时间

//   UploadTask({
//     required this.id,
//     required this.filePath,
//     required this.fileName,
//     required this.fileSize,
//     this.parentId,
//     this.chunkSize = ChunkUploadService.defaultChunkSize,
//     required this.chunkTotal,
//     this.status = UploadTaskStatus.pending,
//     this.uploadId,
//     this.fileHash,
//     this.uploadedChunks = 0,
//     Set<int>? completedChunkIndexes,
//     this.errorMessage,
//     this.fileId,
//     this.hashProgress = 0,
//     this.uploadedBytes = 0,
//     this.speed = 0,
//     this.maxProgress = 0,
//     this.lastProgressUpdate,
//   }) : completedChunkIndexes = completedChunkIndexes ?? {};

//   /// 更新进度并计算速度
//   void updateProgress(int newUploadedChunks, int newUploadedBytes) {
//     final now = DateTime.now();
//     final currentProgress = chunkTotal > 0 ? (newUploadedChunks / chunkTotal) * 100 : 0.0;

//     // 更新最大进度
//     if (currentProgress > maxProgress) {
//       maxProgress = currentProgress;
//     }

//     // 计算速度
//     if (lastProgressUpdate != null) {
//       final deltaBytes = newUploadedBytes - uploadedBytes;
//       final deltaSeconds = now.difference(lastProgressUpdate!).inMilliseconds / 1000;
//       if (deltaSeconds > 0) {
//         speed = deltaBytes / deltaSeconds;
//       }
//     }

//     uploadedChunks = newUploadedChunks;
//     uploadedBytes = newUploadedBytes;
//     lastProgressUpdate = now;
//   }
// }

// /// 分片上传服务
// /// 支持大文件分片上传、多线程上传、断点续传
// class ChunkUploadService {
//   static const int defaultChunkSize = 10 * 1024 * 1024; // 10MB
//   static const int largeFileThreshold = 100 * 1024 * 1024; // 100MB
//   static const int maxConcurrentUploads = 5; // 最大并发上传数

//   final FmRepository _repository;
//   final Talker _logger = getLogger();

//   ChunkUploadService(this._repository);

//   /// 计算文件哈希（用于秒传和断点续传）
//   Future<String> calculateFileHash(String filePath) async {
//     final file = File(filePath);
//     if (!await file.exists()) {
//       throw Exception('文件不存在: $filePath');
//     }

//     final digest = await sha256.bind(file.openRead()).last;
//     return hex.encode(digest.bytes);
//   }

//   /// 计算分片哈希
//   String calculateChunkHash(Uint8List chunkData) {
//     final digest = sha256.convert(chunkData);
//     return hex.encode(digest.bytes);
//   }

//   /// 读取文件分片
//   Future<Uint8List> readChunk(String filePath, int chunkIndex, int chunkSize) async {
//     final file = File(filePath);
//     final offset = (chunkIndex - 1) * chunkSize;
    
//     final raf = await file.open(mode: FileMode.read);
//     await raf.setPosition(offset);
    
//     final remaining = await file.length() - offset;
//     final readLength = remaining > chunkSize ? chunkSize : remaining.toInt();
//     final bytes = await raf.read(readLength);
//     await raf.close();
    
//     return bytes;
//   }

//   /// 初始化上传任务
//   Future<UploadTask> initUploadTask({
//     required String filePath,
//     String? parentId,
//   }) async {
//     final file = File(filePath);
//     if (!await file.exists()) {
//       throw Exception('文件不存在: $filePath');
//     }

//     final fileName = p.basename(filePath);
//     final fileSize = await file.length();
//     final fileHash = await calculateFileHash(filePath);
    
//     // 计算分片数量
//     final chunkTotal = (fileSize + defaultChunkSize - 1) ~/ defaultChunkSize;
    
//     final taskId = DateTime.now().millisecondsSinceEpoch.toString();
    
//     final task = UploadTask(
//       id: taskId,
//       filePath: filePath,
//       fileName: fileName,
//       fileSize: fileSize,
//       parentId: parentId,
//       chunkSize: defaultChunkSize,
//       chunkTotal: chunkTotal,
//       fileHash: fileHash,
//     );

//     // 获取文件 MIME 类型
//     final mime = _getMimeType(fileName);
//     final fileType = _getFileType(fileName);

//     // 调用初始化上传接口
//     final result = await _repository.initUpload(
//       name: fileName,
//       parentId: parentId,
//       size: fileSize,
//       hash: fileHash,
//       mime: mime,
//       type: fileType,
//       chunkSize: defaultChunkSize,
//     );

//     if (!result.success) {
//       task.status = UploadTaskStatus.failed;
//       task.errorMessage = result.message;
//       return task;
//     }

//     final respData = result.data!;
    
//     // 检查是否秒传
//     if (respData.exists == true) {
//       task.status = UploadTaskStatus.completed;
//       task.fileId = respData.fileId;
//       _logger.info('文件秒传成功: $fileName');
//       return task;
//     }

//     // 断点续传：恢复已上传分片
//     task.uploadId = respData.uploadId;
//     task.uploadedChunks = respData.uploadedChunks.length;
//     task.completedChunkIndexes = Set<int>.from(respData.uploadedChunks);
    
//     if (task.completedChunkIndexes.isNotEmpty) {
//       _logger.info('断点续传: $fileName, 已上传分片: ${task.completedChunkIndexes}');
//     }

//     task.status = UploadTaskStatus.uploading;
//     return task;
//   }

//   /// 上传单个分片
//   Future<bool> uploadChunk(UploadTask task, int chunkIndex) async {
//     if (task.uploadId == null) {
//       throw Exception('上传任务未初始化');
//     }

//     // 检查分片是否已上传（断点续传）
//     if (task.completedChunkIndexes.contains(chunkIndex)) {
//       _logger.info('分片已上传，跳过: chunkIndex=$chunkIndex');
//       return true;
//     }

//     // 读取分片数据
//     final chunkData = await readChunk(task.filePath, chunkIndex, task.chunkSize);
//     final chunkHash = calculateChunkHash(chunkData);

//     // 上传分片
//     final result = await _repository.uploadChunk(
//       uploadId: task.uploadId!,
//       chunkIndex: chunkIndex,
//       chunkData: chunkData,
//       chunkHash: chunkHash,
//     );

//     if (result.success) {
//       task.completedChunkIndexes.add(chunkIndex);
//       task.uploadedChunks = result.data!.uploadedCount ?? task.completedChunkIndexes.length;
//       _logger.info('分片上传成功: chunkIndex=$chunkIndex, progress=${task.uploadedChunks}/${task.chunkTotal}');
//       return true;
//     } else {
//       _logger.error('分片上传失败: chunkIndex=$chunkIndex, error=${result.message}');
//       return false;
//     }
//   }

//   /// 多线程上传所有分片
//   Future<UploadTask> uploadAllChunks(
//     UploadTask task, {
//     void Function(UploadTask)? onProgress,
//     void Function(UploadTask)? onCompleted,
//     void Function(UploadTask, String)? onError,
//   }) async {
//     if (task.uploadId == null) {
//       task.status = UploadTaskStatus.failed;
//       task.errorMessage = '上传任务未初始化';
//       return task;
//     }

//     // 获取未上传的分片序号
//     final pendingChunks = <int>[];
//     for (var i = 1; i <= task.chunkTotal; i++) {
//       if (!task.completedChunkIndexes.contains(i)) {
//         pendingChunks.add(i);
//       }
//     }

//     if (pendingChunks.isEmpty) {
//       _logger.info('所有分片已上传完成');
//     } else {
//       _logger.info('开始上传 ${pendingChunks.length} 个分片');

//       // 使用并发上传
//       final semaphore = _Semaphore(maxConcurrentUploads);
//       final futures = <Future<void>>[];

//       for (final chunkIndex in pendingChunks) {
//         futures.add(
//           semaphore.acquire().then((_) async {
//             try {
//               final success = await uploadChunk(task, chunkIndex);
//               if (!success && task.status != UploadTaskStatus.cancelled) {
//                 task.status = UploadTaskStatus.failed;
//                 task.errorMessage = '分片上传失败: chunkIndex=$chunkIndex';
//                 onError?.call(task, task.errorMessage!);
//               }
//               onProgress?.call(task);
//             } finally {
//               semaphore.release();
//             }
//           }),
//         );
//       }

//       await Future.wait(futures);
//     }

//     if (task.status == UploadTaskStatus.failed || task.status == UploadTaskStatus.cancelled) {
//       return task;
//     }

//     // 完成上传
//     task.status = UploadTaskStatus.completing;
//     final completeResult = await _repository.completeUpload(uploadId: task.uploadId!);

//     if (completeResult.success) {
//       task.status = UploadTaskStatus.completed;
//       task.fileId = completeResult.data!.fileId;
//       _logger.info('上传完成: ${task.fileName}, fileId=${task.fileId}');
//       onCompleted?.call(task);
//     } else {
//       task.status = UploadTaskStatus.failed;
//       task.errorMessage = completeResult.message;
//       onError?.call(task, task.errorMessage!);
//     }

//     return task;
//   }

//   /// 取消上传
//   Future<void> cancelUpload(UploadTask task) async {
//     if (task.uploadId != null) {
//       await _repository.cancelUpload(uploadId: task.uploadId!);
//     }
//     task.status = UploadTaskStatus.cancelled;
//   }

//   /// 获取上传进度
//   Future<UploadTask> getUploadProgress(UploadTask task) async {
//     if (task.uploadId == null) return task;

//     final result = await _repository.getUploadProgress(uploadId: task.uploadId!);
//     if (result.success) {
//       task.uploadedChunks = result.data!.uploadedCount ?? 0;
//       task.completedChunkIndexes = Set<int>.from(result.data!.uploadedChunks);
//     }
//     return task;
//   }

//   /// 根据 MIME 类型判断文件类型
//   String _getMimeType(String fileName) {
//     final ext = p.extension(fileName).toLowerCase();
//     const mimeTypes = {
//       '.jpg': 'image/jpeg',
//       '.jpeg': 'image/jpeg',
//       '.png': 'image/png',
//       '.gif': 'image/gif',
//       '.webp': 'image/webp',
//       '.pdf': 'application/pdf',
//       '.doc': 'application/msword',
//       '.docx': 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
//       '.xls': 'application/vnd.ms-excel',
//       '.xlsx': 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
//       '.ppt': 'application/vnd.ms-powerpoint',
//       '.pptx': 'application/vnd.openxmlformats-officedocument.presentationml.presentation',
//       '.mp3': 'audio/mpeg',
//       '.mp4': 'video/mp4',
//       '.avi': 'video/x-msvideo',
//       '.mov': 'video/quicktime',
//       '.zip': 'application/zip',
//       '.rar': 'application/x-rar-compressed',
//       '.7z': 'application/x-7z-compressed',
//       '.txt': 'text/plain',
//       '.json': 'application/json',
//       '.xml': 'application/xml',
//     };
//     return mimeTypes[ext] ?? 'application/octet-stream';
//   }

//   /// 根据文件扩展名判断文件类型
//   FileType _getFileType(String fileName) {
//     final ext = p.extension(fileName).toLowerCase();
//     if (['.jpg', '.jpeg', '.png', '.gif', '.webp', '.bmp', '.ico', '.svg'].contains(ext)) {
//       return FileType.IMAGE;
//     }
//     if (['.mp4', '.avi', '.mov', '.wmv', '.flv', '.mkv', '.webm'].contains(ext)) {
//       return FileType.VIDEO;
//     }
//     if (['.mp3', '.wav', '.flac', '.aac', '.ogg', '.wma', '.m4a'].contains(ext)) {
//       return FileType.AUDIO;
//     }
//     if (['.pdf', '.doc', '.docx', '.xls', '.xlsx', '.ppt', '.pptx', '.txt', '.md'].contains(ext)) {
//       return FileType.DOCUMENT;
//     }
//     if (['.zip', '.rar', '.7z', '.tar', '.gz', '.bz2'].contains(ext)) {
//       return FileType.ARCHIVE;
//     }
//     return FileType.TYPE_UNSPECIFIED;
//   }
// }

// /// 信号量实现 - 用于控制并发数
// class _Semaphore {
//   final int _maxCount;
//   int _currentCount = 0;
//   final List<Completer<void>> _waitQueue = [];

//   _Semaphore(this._maxCount);

//   Future<void> acquire() async {
//     if (_currentCount < _maxCount) {
//       _currentCount++;
//       return;
//     }
//     final completer = Completer<void>();
//     _waitQueue.add(completer);
//     await completer.future;
//   }

//   void release() {
//     if (_waitQueue.isNotEmpty) {
//       final completer = _waitQueue.removeAt(0);
//       completer.complete();
//     } else {
//       _currentCount--;
//     }
//   }
// }
