import 'dart:async';
import 'dart:io' as io;
import 'dart:math';
import 'dart:typed_data';

import 'package:pfm/api/api.dart';
import 'package:pfm/proto/native/enum.pb.dart';
import 'package:pfm/services/services.dart';

// 直接导入 blake3_dart 内部类以支持增量哈希
// ignore: implementation_imports
import 'package:blake3_dart/src/blake3_compact.dart';

Future<void> uploadFile({
  required String taskId,
  required Sqlite3Storage storage,
  required FmRepository fmRepo,
}) async {
  final l = getLogger();
  // 获取任务记录
  final task = await storage.getUploadTaskAsync(taskId);
  if (task == null) {
    l.warning('upload record not found for taskId: $taskId');
    return;
  }
  io.RandomAccessFile? raf;
  Timer? syncTimer;
  try {
    // 计算文件哈希
    if (task.fileHash == null || task.fileHash!.isEmpty) {
      await storage.updateUploadTask(task.id, status: UploadStatus.HASHING);
      final fileHash = await _computeFileHash(task.filepath);
      await storage.updateUploadTask(task.id, fileHash: fileHash);
      task.fileHash = fileHash;
    }

    // 更新状态为上传中
    await storage.updateUploadTask(task.id, status: UploadStatus.UPLOADING);

    // 检查文件大小，决定使用小文件上传还是分片上传
    const smallFileThreshold = 10 * 1024 * 1024; // 10MB
    final fileSize = task.fileSize;

    if (fileSize <= smallFileThreshold) {
      // 小文件上传：直接读取整个文件并发送
      l.info('Using small file upload for file size: $fileSize bytes');
      
      final file = io.File(task.filepath);
      final content = await file.readAsBytes();
      
      final result = await fmRepo.uploadSmall(
        name: task.filename,
        parentId: task.parentId,
        size: fileSize,
        hash: task.fileHash!,
        content: content,
        overwrite: false,
        onSendProgress: (sent, total) {
          storage.updateUploadTask(task.id, uploadedBytes: sent);
        },
      );

      if (!result.success) {
        await storage.updateUploadTask(
          task.id,
          status: UploadStatus.FAILED,
          errorMessage: result.message,
        );
        l.error('Small file upload failed: ${result.message}');
        return;
      }

      // 检查是否秒传
      if (result.data?.exists == true) {
        l.info('File uploaded instantly (instant upload)');
      }

      await storage.updateUploadTask(task.id, status: UploadStatus.COMPLETED);
      l.info('Small file upload completed successfully');
      return;
    }

    // 分片上传逻辑（大文件）
    List<int> uploadedChunks = [];
    // 初始化上传
    if (task.uploadId == null || task.uploadId!.isEmpty) {
      final result = await fmRepo.initUpload(
        name: task.filename,
        size: task.fileSize,
        hash: task.fileHash!,
        parentId: task.parentId,
      );
      if (!result.success) {
        await storage.updateUploadTask(
          task.id,
          status: UploadStatus.FAILED,
          errorMessage: result.message,
        );
        l.error('InitUpload failed: ${result.message}');
        return;
      }
      final data = result.data!;

      final uploadId = data.uploadId.toString();
      l.info(
        'InitUpload result: uploadId=$uploadId, '
        'chunkTotal=${data.chunkTotal}, chunkSize=${data.chunkSize}, '
        'exists=${data.exists}, uploadedChunks=${data.uploadedChunks}',
      );

      // 检查是否秒传（exists 表示文件已存在）
      if (data.exists) {
        await storage.updateUploadTask(task.id, status: UploadStatus.COMPLETED);
        return;
      }
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
    final chunkSize = task.chunkSize;
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
            ? task.fileSize - chunkSize * (totalChunks - 1)
            : chunkSize;
      }
    }

    if (pendingChunks.isNotEmpty) {
      //执行上传
      l.info(
        'Starting single-threaded chunk upload: ${pendingChunks.length} chunks to upload',
      );

      raf = await file.open();
      syncTimer = Timer.periodic(Duration(seconds: 1), (t) {
        // 定期更新速度
        storage.updateUploadTask(
          task.id,
          speed: deltaUploadedBytes,
          uploadedBytes: max(realUploadedBytes, uploadedBytes),
        );
        deltaUploadedBytes = 0;
      });

      for (final chunkIndex in pendingChunks) {
        // 检查是否被取消或暂停或删除
        final currentTask = await storage.getUploadTaskAsync(task.id);
        if (currentTask == null ||
            currentTask.status == UploadStatus.CANCELED ||
            currentTask.status == UploadStatus.PAUSED) {
          l.info('Upload cancelled or paused, stopping upload');
          return;
        }

        // 读取分片数据
        final offset = chunkIndex * chunkSize;
        final remainingBytes = task.fileSize - offset;
        final currentChunkSize = remainingBytes > chunkSize
            ? chunkSize
            : remainingBytes;

        await raf.setPosition(offset);
        final chunkData = await raf.read(currentChunkSize);

        // 计算分片哈希
        final chunkHash = _computeBlake3Hash(chunkData);

        int lastUploadedBytes = 0;
        int initUploadedBytes = realUploadedBytes;
        // 上传分片
        final result = await fmRepo.uploadChunk(
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
                ? chunkSize
                : task.fileSize - chunkSize * (totalChunks - 1);
            if (sent < cs) {
              realUploadedBytes = initUploadedBytes + sent;
            } else if (realUploadedBytes < initUploadedBytes + cs) {
              realUploadedBytes = initUploadedBytes + cs;
            }
          },
        );

        if (!result.success) {
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
    final result = await fmRepo.completeUpload(uploadId: int.parse(task.uploadId!));
    if (!result.success) {
      await storage.updateUploadTask(
        task.id,
        status: UploadStatus.FAILED,
        errorMessage: result.message,
      );
      getLogger().error('CompleteUpload failed: ${result.message}');
      return;
    }

    await storage.updateUploadTask(task.id, status: UploadStatus.COMPLETED);

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
