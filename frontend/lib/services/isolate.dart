import 'dart:io' as io;
import 'dart:isolate';
import 'dart:typed_data';

// 直接导入 blake3_dart 内部类以支持增量哈希
// ignore: implementation_imports
import 'package:blake3_dart/src/blake3_compact.dart';
import 'package:dio/dio.dart';
import 'package:fixnum/fixnum.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/services/services.dart';

/// Isolate 配置数据
class UploadIsolateConfig {
  final SendPort sendPort;
  final String taskId;
  final String filePath;
  final String fileName;
  final int fileSize;
  final String? parentId;
  final int chunkSize;
  final int chunkTotal;
  final String baseUrl;
  final String token;
  final String dbPath;

  UploadIsolateConfig({
    required this.sendPort,
    required this.taskId,
    required this.filePath,
    required this.fileName,
    required this.fileSize,
    this.parentId,
    required this.chunkSize,
    required this.chunkTotal,
    required this.baseUrl,
    required this.token,
    required this.dbPath,
  });
}

/// Isolate 入口点（桌面平台）
@pragma('vm:entry-point')
void uploadIsolateEntryPoint(UploadIsolateConfig config) async {
  // 预先初始化后台 Logger 单例
  initTalkerLogger();
  getLogger().info('Isolate started for task: ${config.taskId}');

  final sendPort = config.sendPort;

  try {
    // 使用主线程传递的数据库路径初始化存储
    final storage = await Sqlite3Storage.initForIsolate(config.dbPath);
    getLogger().info('Storage initialized with dbPath: ${config.dbPath}');

    final task = await storage.getUploadTaskAsync(config.taskId);

    if (task == null) {
      sendPort.send({'type': 'failed', 'error': 'Task not found'});
      return;
    }

    // 发送进度辅助函数
    void sendProgress(String stage, double progress) {
      sendPort.send({'type': 'progress', 'stage': stage, 'progress': progress});
    }

    // 1. 计算文件 Hash（使用 Blake3）
    sendProgress('hashing', 0);
    final fileHash = await _computeFileHash(config.filePath, (progress) {
      sendProgress('hashing', progress * 0.1); // 0-10%
    });
    task.fileHash = fileHash;
    await storage.saveUploadTask(task);
    sendProgress('hashing', 10);

    // 2. 初始化上传
    sendProgress('initializing', 10);
    final baseUrl = config.baseUrl;
    final token = config.token;

    if (baseUrl.isEmpty) {
      throw Exception('服务器地址未配置');
    }

    final dio = Dio(
      BaseOptions(
        baseUrl: baseUrl,
        headers: {
          'Authorization': 'Bearer $token',
          'Content-Type': 'application/json',
        },
        connectTimeout: const Duration(seconds: 30),
        receiveTimeout: const Duration(seconds: 60),
      ),
    );

    // 更新状态为上传中
    task.status = UploadTaskStatusRecord.uploading;
    await storage.saveUploadTask(task);

    if (task.uploadId == null || task.uploadId!.isEmpty) {
      // 使用 protobuf 请求
      final initReq = InitUploadReq()
        ..name = task.fileName
        ..size = Int64(task.fileSize)
        ..hash = task.fileHash ?? ''
        ..mime = task.mime ?? '';

      if (task.parentId != null) {
        initReq.parentId = task.parentId!;
      }

      final initResp = await dio.post(
        '/native.FileService/InitUpload',
        data: initReq.toProto3Json(),
      );
      getLogger().debug('InitUpload response: ${initResp.data}');

      // 使用 protobuf 解析响应
      final initData = extractRespData<InitUploadRespData>(
        initResp.data as Map<String, dynamic>,
        InitUploadRespData(),
      );

      final uploadId = initData.uploadId;
      getLogger().info(
        'InitUpload result: uploadId=$uploadId, '
        'chunkTotal=${initData.chunkTotal}, chunkSize=${initData.chunkSize}, '
        'exists=${initData.exists}, uploadedChunks=${initData.uploadedChunks}',
      );

      if (uploadId.isEmpty) {
        throw Exception('Failed to get upload ID');
      }

      task.uploadId = uploadId;
      // 更新分片信息
      if (initData.hasChunkTotal()) {
        task.chunkTotal = initData.chunkTotal;
      }
      if (initData.hasChunkSize()) {
        task.chunkSize = initData.chunkSize;
      }
      await storage.saveUploadTask(task);
      getLogger().debug(
        'Task saved with uploadId=$uploadId, chunkTotal=${task.chunkTotal}',
      );

      // 检查是否秒传（exists 表示文件已存在）
      if (initData.exists) {
        task.status = UploadTaskStatusRecord.completed;
        task.fileId = initData.fileId;
        await storage.saveUploadTask(task);
        sendPort.send({'type': 'completed'});
        return;
      }

      // 获取已上传的分片
      if (initData.uploadedChunks.isNotEmpty) {
        task.uploadedChunks = initData.uploadedChunks.toList();
        await storage.saveUploadTask(task);
      }
    }

    // 3. 上传分片
    final file = io.File(task.filePath);
    final uploadedChunksSet = Set<int>.from(task.uploadedChunks);
    final totalChunks = task.chunkTotal;
    final chunkSize = task.chunkSize;

    for (int i = 1; i <= totalChunks; i++) {
      // 检查是否被取消或暂停
      final currentTask = await storage.getUploadTaskAsync(task.id);
      if (currentTask?.status == UploadTaskStatusRecord.cancelled ||
          currentTask?.status == UploadTaskStatusRecord.paused) {
        return;
      }

      if (uploadedChunksSet.contains(i)) continue;

      final offset = (i - 1) * chunkSize;
      final remainingBytes = task.fileSize - offset;
      final currentChunkSize = remainingBytes > chunkSize
          ? chunkSize
          : remainingBytes;

      final raf = await file.open();
      await raf.setPosition(offset);
      final chunkData = await raf.read(currentChunkSize);
      await raf.close();

      final chunkHash = _computeBlake3Hash(chunkData);

      // 使用 protobuf 请求
      final uploadChunkReq = UploadChunkReq()
        ..uploadId = task.uploadId!
        ..chunkIndex = i
        ..chunkHash = chunkHash
        ..chunk = chunkData;

      getLogger().debug(
        'Uploading chunk $i/$totalChunks: uploadId=${task.uploadId}, '
        'chunkIndex=${uploadChunkReq.chunkIndex}, chunkSize=${chunkData.length}, '
        'chunkHash=$chunkHash',
      );

      try {
        final uploadResp = await dio.post(
          '/native.FileService/UploadChunk',
          data: uploadChunkReq.toProto3Json(),
        );
        getLogger().debug('Upload chunk $i response: ${uploadResp.data}');
      } catch (e) {
        getLogger().error('Upload chunk $i failed: $e');
        rethrow;
      }

      uploadedChunksSet.add(i);
      task.uploadedChunks = uploadedChunksSet.toList();
      await storage.saveUploadTask(task);

      // 更新进度 (10% - 100%)
      sendProgress('uploading', 10 + (i / totalChunks) * 90);
    }

    // 4. 完成上传
    final completeReq = CompleteUploadReq()..uploadId = task.uploadId!;
    final completeResp = await dio.post(
      '/native.FileService/CompleteUpload',
      data: completeReq.toProto3Json(),
    );
    final completeData = extractRespData<CompleteUploadRespData>(
      completeResp.data as Map<String, dynamic>,
      CompleteUploadRespData(),
    );

    task.status = UploadTaskStatusRecord.completed;
    task.fileId = completeData.fileId;
    task.completedAt = DateTime.now();
    await storage.saveUploadTask(task);

    sendPort.send({'type': 'completed'});
  } catch (e) {
    getLogger().error('Task ${config.taskId} failed: $e');
    sendPort.send({'type': 'failed', 'error': e.toString()});
  }
}

/// 计算文件 Hash（使用 Blake3）
Future<String> _computeFileHash(
  String filePath,
  void Function(double) onProgress,
) async {
  final file = io.File(filePath);
  final length = await file.length();
  final hasher = HashContext.unkeyed();

  var processed = 0;
  getLogger().debug(
    'start hashing file ${DateTime.now().millisecondsSinceEpoch}',
  );
  await for (final chunk in file.openRead()) {
    hasher.update(Uint8List.fromList(chunk));
    processed += chunk.length;
    onProgress(processed / length);
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
