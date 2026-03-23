import 'dart:io' as io;

import 'package:dio/dio.dart';
import 'package:fixnum/fixnum.dart';
import 'package:flutter/foundation.dart';
import 'package:path/path.dart' as p;
import 'package:pfm/api/api.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/proto/native/common.pb.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/services/services.dart';
import 'package:protobuf/protobuf.dart';
import 'package:talker_flutter/talker_flutter.dart';

// 条件导入 WorkManager（仅移动平台）
import 'package:workmanager/workmanager.dart'
    if (dart.library.html) ''
    if (dart.library.io) 'package:workmanager/workmanager.dart';

// 直接导入 blake3_dart 内部类以支持增量哈希
// ignore: implementation_imports
import 'package:blake3_dart/src/blake3_compact.dart';

/// WorkManager 任务名称
const String kUploadTaskName = 'upload_task';

/// WorkManager 后台任务处理
///
/// 此文件包含所有 WorkManager 相关代码，独立于 BackgroundUploadService
/// 确保在后台隔离环境中正确初始化 Logger 和数据库连接

/// 后台任务回调调度器
///
/// 注意：此函数在 WorkManager 的后台隔离环境中执行，
/// 不能使用 UI 线程的 GetIt 实例或任何依赖它的对象
@pragma('vm:entry-point')
void callbackDispatcher() {
  // 仅在移动平台执行
  if (kIsWeb) return;

  Workmanager().executeTask((task, inputData) async {
    final taskId = inputData?['taskId'] as String?;
    if (taskId == null) {
      return Future.value(false);
    }

    // 预先初始化后台 Logger 单例
    initTalkerLogger();
    final logger = getLogger();
    logger.info('WorkManager task started: $taskId');

    try {
      // 初始化存储（在后台隔离环境中）
      final dbPath = await _getDatabasePath();
      final storage = await Sqlite3Storage.initForIsolate(dbPath);
      final taskRecord = await storage.getUploadTaskAsync(taskId);

      if (taskRecord == null) {
        logger.warning('Task not found: $taskId');
        return Future.value(false);
      }

      // 执行上传
      await _executeUploadTask(taskRecord, storage, logger);

      return Future.value(true);
    } catch (e, stackTrace) {
      getLogger().error('Background upload failed: $e', stackTrace);
      return Future.value(false);
    }
  });
}

/// 执行上传任务
Future<void> _executeUploadTask(
  UploadTaskRecord task,
  Sqlite3Storage storage,
  Talker logger,
) async {
  // 1. 计算 Hash（移动平台后台任务中计算）
  // if (task.fileHash == null || task.fileHash!.isEmpty) {
  //   task.status = UploadTaskStatus.initializing;
  //   await storage.saveUploadTask(task);

  //   final file = io.File(task.filePath);
  //   if (!await file.exists()) {
  //     task.status = UploadTaskStatus.failed;
  //     task.errorMessage = '文件不存在';
  //     await storage.saveUploadTask(task);
  //     return;
  //   }

  //   logger.info('Computing file hash: ${task.fileName}');
  //   final fileHash = await _computeFileHash(task.filePath);
  //   task.fileHash = fileHash;
  //   await storage.saveUploadTask(task);
  // }

  // final file = io.File(task.filePath);
  // if (!await file.exists()) {
  //   task.status = UploadTaskStatus.failed;
  //   task.errorMessage = '文件不存在';
  //   await storage.saveUploadTask(task);
  //   return;
  // }

  // // 更新状态为上传中
  // task.status = UploadTaskStatus.uploading;
  // await storage.saveUploadTask(task);

  // try {
  //   // 获取服务器配置（从 SharedPreferences 或环境变量）
  //   final baseUrl = await _getBaseUrl();
  //   final token = await _getToken();

  //   if (baseUrl.isEmpty) {
  //     throw Exception('服务器地址未配置');
  //   }

  //   final dio = Dio(
  //     BaseOptions(
  //       baseUrl: baseUrl,
  //       headers: {
  //         'Authorization': 'Bearer $token',
  //         'Content-Type': 'application/json',
  //       },
  //       connectTimeout: const Duration(seconds: 30),
  //       receiveTimeout: const Duration(seconds: 60),
  //     ),
  //   );

  //   // 初始化上传
  //   if (task.uploadId == null || task.uploadId!.isEmpty) {
  //     logger.info('Initializing upload: ${task.fileName}');

  //     // 使用 protobuf 请求
  //     final initReq = InitUploadReq()
  //       ..name = task.fileName
  //       ..size = Int64(task.fileSize)
  //       ..hash = task.fileHash ?? ''
  //       ..mime = task.mime ?? '';

  //     if (task.parentId != null) {
  //       initReq.parentId = task.parentId!;
  //     }

  //     final initResp = await dio.post(
  //       '/native.FileService/InitUpload',
  //       data: initReq.toProto3Json(),
  //     );

  //     // 使用 protobuf 解析响应
  //     final initData = extractRespData<InitUploadRespData>(
  //       initResp.data as Map<String, dynamic>,
  //       InitUploadRespData(),
  //     );

  //     final uploadId = initData.uploadId;
  //     if (uploadId.isEmpty) {
  //       throw Exception('Failed to get upload ID');
  //     }

  //     task.uploadId = uploadId;
  //     // 更新分片信息
  //     if (initData.hasChunkTotal()) {
  //       task.chunkTotal = initData.chunkTotal;
  //     }
  //     if (initData.hasChunkSize()) {
  //       task.chunkSize = initData.chunkSize;
  //     }
  //     await storage.saveUploadTask(task);

  //     // 检查是否秒传（exists 表示文件已存在）
  //     if (initData.exists) {
  //       task.status = UploadTaskStatusRecord.completed;
  //       task.fileId = initData.fileId;
  //       await storage.saveUploadTask(task);
  //       logger.info('Instant upload completed: ${task.id}');
  //       return;
  //     }

  //     // 获取已上传的分片
  //     if (initData.uploadedChunks.isNotEmpty) {
  //       task.uploadedChunks = initData.uploadedChunks.toList();
  //       await storage.saveUploadTask(task);
  //     }
  //   }

  //   // 上传分片
  //   final uploadedChunksSet = Set<int>.from(task.uploadedChunks);
  //   final chunkSize = task.chunkSize;
  //   final totalChunks = task.chunkTotal;

  //   logger.info(
  //     'Uploading chunks: ${uploadedChunksSet.length}/$totalChunks completed',
  //   );

  //   for (int i = 1; i <= totalChunks; i++) {
  //     // 检查是否被取消或暂停
  //     final currentTask = await storage.getUploadTaskAsync(task.id);
  //     if (currentTask?.status == UploadTaskStatusRecord.cancelled ||
  //         currentTask?.status == UploadTaskStatusRecord.paused) {
  //       logger.info('Task paused or cancelled: ${task.id}');
  //       return;
  //     }

  //     if (uploadedChunksSet.contains(i)) continue;

  //     final offset = (i - 1) * chunkSize;
  //     final remainingBytes = task.fileSize - offset;
  //     final currentChunkSize = remainingBytes > chunkSize
  //         ? chunkSize
  //         : remainingBytes;

  //     // 读取分片数据
  //     final raf = await file.open();
  //     await raf.setPosition(offset);
  //     final chunkData = await raf.read(currentChunkSize);
  //     await raf.close();

  //     // 计算分片哈希
  //     final chunkHash = _computeBlake3Hash(chunkData);

  //     // 使用 protobuf 请求
  //     final uploadChunkReq = UploadChunkReq()
  //       ..uploadId = task.uploadId!
  //       ..chunkIndex = i
  //       ..chunkHash = chunkHash
  //       ..chunk = chunkData;

  //     // 上传分片
  //     await dio.post(
  //       '/native.FileService/UploadChunk',
  //       data: uploadChunkReq.toProto3Json(),
  //     );

  //     uploadedChunksSet.add(i);
  //     task.uploadedChunks = uploadedChunksSet.toList();
  //     await storage.saveUploadTask(task);

  //     logger.debug('Chunk $i/$totalChunks uploaded');
  //   }

  //   // 完成上传
  //   logger.info('Completing upload: ${task.id}');
  //   final completeReq = CompleteUploadReq()..uploadId = task.uploadId!;
  //   final completeResp = await dio.post(
  //     '/native.FileService/CompleteUpload',
  //     data: completeReq.toProto3Json(),
  //   );

  //   final completeData = extractRespData<CompleteUploadRespData>(
  //     completeResp.data as Map<String, dynamic>,
  //     CompleteUploadRespData(),
  //   );

  //   task.status = UploadTaskStatusRecord.completed;
  //   task.fileId = completeData.fileId;
  //   task.completedAt = DateTime.now();
  //   await storage.saveUploadTask(task);

  //   logger.info('Upload completed: ${task.id}');
  // } catch (e) {
  //   logger.error('Upload failed: $e');
  //   task.status = UploadTaskStatusRecord.failed;
  //   task.errorMessage = e.toString();
  //   task.retryCount = task.retryCount + 1;
  //   await storage.saveUploadTask(task);
  // }
}

/// 计算文件 Hash（使用 Blake3）
Future<String> _computeFileHash(String filePath) async {
  final file = io.File(filePath);
  final hasher = HashContext.unkeyed();

  await for (final chunk in file.openRead()) {
    hasher.update(Uint8List.fromList(chunk));
  }

  final output = Uint8List(32);
  hasher.finalize(output);
  return output.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
}

/// 计算数据的 Blake3 哈希
String _computeBlake3Hash(List<int> data) {
  return blake3Hex(Uint8List.fromList(data));
}

/// 获取数据库路径（返回完整文件路径）
Future<String> _getDatabasePath() async {
  const dbName = 'upload_tasks.db';

  // 尝试从环境变量获取完整路径
  final envPath = io.Platform.environment['PFM_DB_PATH'];
  if (envPath != null) {
    return envPath;
  }

  // 默认使用当前工作目录下的 .pfm/data
  final dbDir = p.join(io.Directory.current.path, '.pfm', 'data');
  final dir = io.Directory(dbDir);
  if (!await dir.exists()) {
    await dir.create(recursive: true);
  }
  return p.join(dbDir, dbName);
}

/// 获取服务器地址
///
/// 在后台环境中，从 SharedPreferences 或配置文件读取
Future<String> _getBaseUrl() async {
  // 尝试从环境变量获取
  final envUrl = io.Platform.environment['PFM_SERVER_URL'];
  if (envUrl != null) {
    return envUrl;
  }

  // TODO: 从 SharedPreferences 读取
  // 这里需要实现从持久化存储读取配置的逻辑
  return '';
}

/// 获取认证 Token
///
/// 在后台环境中，从安全存储读取
Future<String> _getToken() async {
  // 尝试从环境变量获取
  final envToken = io.Platform.environment['PFM_TOKEN'];
  if (envToken != null) {
    return envToken;
  }

  // TODO: 从安全存储读取
  // 这里需要实现从安全存储读取 Token 的逻辑
  return '';
}
