import 'dart:async';
import 'dart:io' as io;
import 'dart:math';
import 'dart:typed_data';

import 'package:pfm/api/api.dart';
import 'package:pfm/proto/native/enum.pb.dart' hide BackupFileStatus;
import 'package:pfm/services/services.dart';

// 直接导入 blake3_dart 内部类以支持增量哈希
// ignore: implementation_imports
import 'package:blake3_dart/src/blake3_compact.dart';

/// 带重试的异步操作执行器
/// 对网络超时等瞬时错误进行指数退避重试
Future<T> _retryOnTimeout<T>(
  Future<T> Function() action, {
  int maxRetries = 3,
  String operationName = 'operation',
}) async {
  int attempt = 0;
  while (true) {
    try {
      return await action();
    } catch (e) {
      attempt++;
      // 仅对超时和连接错误重试
      final isRetryable = e is TimeoutException ||
          (e is io.SocketException) ||
          (e.toString().contains('sendTimeout')) ||
          (e.toString().contains('receiveTimeout')) ||
          (e.toString().contains('connectionTimeout')) ||
          (e.toString().contains('Connection refused'));
      if (!isRetryable || attempt >= maxRetries) {
        rethrow;
      }
      final delay = Duration(seconds: attempt * 2);
      getLogger().warning(
        '$operationName failed (attempt $attempt/$maxRetries), '
        'retrying in ${delay.inSeconds}s: $e',
      );
      await Future.delayed(delay);
    }
  }
}

Future<void> uploadFile({
  required String taskId,
  required Sqlite3Storage storage,
  required FmRepository fmRepo,
  int chunkSize = 2 * 1024 * 1024,
  int smallFileThreshold = 5 * 1024 * 1024,
}) async {
  final l = getLogger();
  final startTime = DateTime.now();

  // 获取任务记录
  final task = await storage.getUploadTaskAsync(taskId);
  if (task == null) {
    l.warning('Upload task not found: $taskId');
    return;
  }

  l.info('[Upload] Start: ${task.filename} (${_formatSize(task.fileSize)}) taskId=$taskId');

  io.RandomAccessFile? raf;
  Timer? syncTimer;
  try {
    // 计算文件哈希
    if (task.fileHash == null || task.fileHash!.isEmpty) {
      l.info('[Upload] Computing hash for ${task.filename}...');
      await storage.updateUploadTask(task.id, status: UploadStatus.HASHING);
      final hashStart = DateTime.now();
      final fileHash = await _computeFileHash(task.filepath);
      final hashDuration = DateTime.now().difference(hashStart);
      l.info('[Upload] Hash computed in ${hashDuration.inMilliseconds}ms: ${fileHash.substring(0, 16)}...');
      await storage.updateUploadTask(task.id, fileHash: fileHash);
      task.fileHash = fileHash;
    }

    // 更新状态为上传中
    await storage.updateUploadTask(task.id, status: UploadStatus.UPLOADING);

    final fileSize = task.fileSize;

    if (fileSize <= smallFileThreshold) {
      // 小文件上传：先 initUpload 检查秒传，不秒传再 uploadSmall 传内容
      l.info('[Upload] Small file mode: ${task.filename} (${_formatSize(fileSize)})');

      // 先请求秒传检查
      final initResult = await _retryOnTimeout(
        () => fmRepo.initUpload(
          name: task.filename,
          size: task.fileSize,
          hash: task.fileHash!,
          parentId: task.parentId,
          overwrite: task.overwrite,
          tags: task.tags,
        ),
        operationName: 'initUpload(${task.filename})',
      );

      if (!initResult.success) {
        await storage.updateUploadTask(
          task.id,
          status: UploadStatus.FAILED,
          errorMessage: initResult.message,
        );
        l.error('[Upload] InitUpload FAILED: ${task.filename} - ${initResult.message}');
        return;
      }

      // 秒传成功
      if (initResult.data!.exists) {
        l.info('[Upload] Instant upload: ${task.filename}');
        await storage.updateUploadTask(
          task.id,
          status: UploadStatus.COMPLETED,
          uploadedBytes: fileSize,
        );
        final totalDuration = DateTime.now().difference(startTime);
        l.info('[Upload] COMPLETED (instant): ${task.filename} in ${totalDuration.inMilliseconds}ms');
        return;
      }

      // 不秒传，上传文件内容
      final file = io.File(task.filepath);
      final content = await file.readAsBytes();

      final uploadStart = DateTime.now();
      final result = await _retryOnTimeout(
        () => fmRepo.uploadSmall(
          name: task.filename,
          parentId: task.parentId,
          size: fileSize,
          hash: task.fileHash!,
          content: content,
          overwrite: task.overwrite,
          tags: task.tags,
          onSendProgress: (sent, total) {
            final capped = sent > fileSize ? fileSize : sent;
            storage.updateUploadTask(task.id, uploadedBytes: capped);
          },
        ),
        operationName: 'uploadSmall(${task.filename})',
      );

      final uploadDuration = DateTime.now().difference(uploadStart);

      if (!result.success) {
        await storage.updateUploadTask(
          task.id,
          status: UploadStatus.FAILED,
          errorMessage: result.message,
        );
        l.error('[Upload] FAILED: ${task.filename} after ${uploadDuration.inSeconds}s - ${result.message}');
        return;
      }

      await storage.updateUploadTask(
        task.id,
        status: UploadStatus.COMPLETED,
        uploadedBytes: fileSize,
      );

      final totalDuration = DateTime.now().difference(startTime);
      l.info('[Upload] COMPLETED: ${task.filename} in ${totalDuration.inSeconds}s');
      return;
    }

    // 分片上传逻辑（大文件）
    l.info('[Upload] Chunked upload mode: ${task.filename} (${_formatSize(fileSize)})');
    List<int> uploadedChunks = [];

    // 初始化上传
    if (task.uploadId == null || task.uploadId!.isEmpty) {
      l.info('[Upload] Initializing chunked upload for ${task.filename}...');
      final initStart = DateTime.now();

      final result = await _retryOnTimeout(
        () => fmRepo.initUpload(
          name: task.filename,
          size: task.fileSize,
          hash: task.fileHash!,
          parentId: task.parentId,
          chunkSize: chunkSize,
          overwrite: task.overwrite,
          tags: task.tags,
        ),
        operationName: 'initUpload(${task.filename})',
      );

      final initDuration = DateTime.now().difference(initStart);

      if (!result.success) {
        await storage.updateUploadTask(
          task.id,
          status: UploadStatus.FAILED,
          errorMessage: result.message,
        );
        l.error('[Upload] InitUpload FAILED: ${task.filename} after ${initDuration.inSeconds}s - ${result.message}');
        return;
      }
      final data = result.data!;

      // 先检查是否秒传（exists 表示文件已存在，此时 uploadId 为 null）
      if (data.exists) {
        l.info('[Upload] Instant upload: ${task.filename}');
        await storage.updateUploadTask(
          task.id,
          status: UploadStatus.COMPLETED,
          uploadedBytes: task.fileSize,
        );
        return;
      }

      final uploadId = data.uploadId.toString();
      l.info(
        '[Upload] InitUpload OK: ${task.filename} uploadId=$uploadId, '
        'chunks=${data.chunkTotal}, chunkSize=${_formatSize(data.chunkSize.toInt())} '
        '(${initDuration.inMilliseconds}ms)',
      );

      task.uploadId = uploadId;
      task.chunkTotal = data.chunkTotal;
      task.chunkSize = data.chunkSize.toInt();

      // 获取已上传的分片
      if (data.uploadedChunks.isNotEmpty) {
        await storage.updateUploadTask(
          task.id,
          uploadId: data.uploadId.toString(),
          chunkTotal: data.chunkTotal,
          chunkSize: data.chunkSize,
        );
        uploadedChunks = data.uploadedChunks;
      }
    }

    // 单线程顺序上传分片
    final file = io.File(task.filepath);
    final totalChunks = task.chunkTotal;
    final serverChunkSize = task.chunkSize;
    final bitmap = uploadedChunks;

    // 已上传字节数（从task中恢复）
    int uploadedBytes = task.uploadedBytes;

    // 实际上传字节数，如果某个分片上传失败，之前的上传字节数会被重置，所以需要一个实际的上传字节数
    int realUploadedBytes = 0;

    // 用于计算速度的字节差值
    int deltaUploadedBytes = 0;

    // 获取待上传的分片列表（从位图中查找未上传的分片）
    final pendingChunks = <int>[];
    for (int i = 0; i < totalChunks; i++) {
      final byteIndex = i ~/ 8;
      final bitIndex = i % 8;
      if (byteIndex >= bitmap.length ||
          (bitmap[byteIndex] & (1 << bitIndex)) == 0) {
        pendingChunks.add(i);
      } else {
        // 根据已上传的分片计算实际上传的字节数
        realUploadedBytes += i == totalChunks - 1
            ? task.fileSize - serverChunkSize * (totalChunks - 1)
            : serverChunkSize;
      }
    }

    if (pendingChunks.isNotEmpty) {
      //执行上传
      l.info('[Upload] ${task.filename}: ${pendingChunks.length} chunks to upload (${totalChunks - pendingChunks.length} already done)');

      raf = await file.open();
      final chunkUploadStart = DateTime.now();

      syncTimer = Timer.periodic(Duration(seconds: 3), (t) {
        storage.updateUploadTask(
          task.id,
          speed: deltaUploadedBytes ~/ 3,
          uploadedBytes: max(realUploadedBytes, uploadedBytes),
        );
        deltaUploadedBytes = 0;
      });

      for (int i = 0; i < pendingChunks.length; i++) {
        final chunkIndex = pendingChunks[i];
        // 检查是否被取消或暂停或删除
        final currentTask = await storage.getUploadTaskAsync(task.id);
        if (currentTask == null ||
            currentTask.status == UploadStatus.CANCELED ||
            currentTask.status == UploadStatus.PAUSED) {
          l.info('[Upload] ${task.filename}: cancelled or paused, stopping');
          return;
        }

        // 读取分片数据
        final offset = chunkIndex * serverChunkSize;
        final remainingBytes = task.fileSize - offset;
        final currentChunkSize = remainingBytes > serverChunkSize
            ? serverChunkSize
            : remainingBytes;

        await raf.setPosition(offset);
        final chunkData = await raf.read(currentChunkSize);

        // 计算分片哈希
        final chunkHash = _computeBlake3Hash(chunkData);

        int lastUploadedBytes = 0;
        int initUploadedBytes = realUploadedBytes;
        // 上传分片
        final result = await _retryOnTimeout(
          () => fmRepo.uploadChunk(
            uploadId: int.parse(task.uploadId!),
            chunkIndex: chunkIndex,
            chunkData: chunkData,
            chunkHash: chunkHash,
            onSendProgress: (sent, total) {
              //sent是已发送的字节数，不是从上次调用到本次调用的字节数，不能直接累加，需要计算差值
              // total是参数的总字节数，不是分片大小 total = chunkSize + otherParamsSize
              final delta = sent - lastUploadedBytes; //两次调用之间的差值
              lastUploadedBytes = sent;
              deltaUploadedBytes += delta;
              final cs = chunkIndex != totalChunks - 1
                  ? serverChunkSize
                  : task.fileSize - serverChunkSize * (totalChunks - 1);
              if (sent < cs) {
                realUploadedBytes = initUploadedBytes + sent;
              } else if (realUploadedBytes < initUploadedBytes + cs) {
                realUploadedBytes = initUploadedBytes + cs;
              }
            },
          ),
          operationName: 'uploadChunk(${task.filename}#$chunkIndex)',
        );

        if (!result.success) {
          // 后端异步处理窗口期：记录已被清理，视为已完成
          if (result.message != null &&
              result.message!.contains('record not found')) {
            getLogger().info(
              'Chunk $chunkIndex: record not found (already processing), treating as done',
            );
            await storage.updateUploadTask(
              task.id,
              status: UploadStatus.COMPLETED,
              uploadedBytes: task.fileSize,
            );
            return;
          }
          await storage.updateUploadTask(
            task.id,
            status: UploadStatus.FAILED,
            errorMessage: result.message,
          );
          getLogger().error(
            'Chunk $chunkIndex upload failed: ${result.message}',
          );
          // 单线程模式遇到错误直接停止
          return;
        }
      }
    }

    // 4. 完成上传
    final result = await _retryOnTimeout(
      () => fmRepo.completeUpload(uploadId: int.parse(task.uploadId!)),
      operationName: 'completeUpload(${task.filename})',
    );
    if (!result.success) {
      // 后端异步处理窗口期：记录已被清理，视为已完成
      if (result.message != null &&
          result.message!.contains('record not found')) {
        getLogger().info('CompleteUpload: record not found (already processing), treating as done');
        await storage.updateUploadTask(
          task.id,
          status: UploadStatus.COMPLETED,
          uploadedBytes: task.fileSize,
        );
        return;
      }
      await storage.updateUploadTask(
        task.id,
        status: UploadStatus.FAILED,
        errorMessage: result.message,
      );
      getLogger().error('CompleteUpload failed: ${result.message}');
      return;
    }

    // 更新完成状态，同时确保进度为100%
    await storage.updateUploadTask(
      task.id,
      status: UploadStatus.COMPLETED,
      uploadedBytes: task.fileSize,
    );

    getLogger().info('Upload completed successfully');
  } catch (e) {
    getLogger().error('Chunk upload failed: $e');
    final tt = await storage.getUploadTaskAsync(task.id);
    if (tt != null) {
      await storage.updateUploadTask(
        task.id,
        status: UploadStatus.FAILED,
        errorMessage: e.toString(),
      );
    }
    return;
  } finally {
    syncTimer?.cancel();
    raf?.close();
  }
}

/// 计算文件 Hash（使用 Blake3）
Future<String> _computeFileHash(String filePath) async {
  final file = io.File(filePath);
  final hasher = HashContext.unkeyed();

  getLogger().debug(
    'start hashing file ${DateTime.now().millisecondsSinceEpoch}',
  );
  await for (final chunk in file.openRead()) {
    hasher.update(Uint8List.fromList(chunk));
  }
  getLogger().debug(
    'end hashing file ${DateTime.now().millisecondsSinceEpoch}',
  );

  final output = Uint8List(32);
  hasher.finalize(output);
  return output.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
}

/// 计算数据的 Blake3 哈希
String _computeBlake3Hash(List<int> data) {
  return blake3Hex(Uint8List.fromList(data));
}

/// 格式化文件大小
String _formatSize(int bytes) {
  if (bytes < 1024) return '${bytes}B';
  if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)}KB';
  if (bytes < 1024 * 1024 * 1024) return '${(bytes / (1024 * 1024)).toStringAsFixed(1)}MB';
  return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(1)}GB';
}
