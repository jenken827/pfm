import 'dart:async';
import 'dart:convert';
import 'dart:io' as io;
import 'dart:math';

import 'package:http/http.dart' as http;
import 'package:pfm/models/models.dart';
import 'package:pfm/services/services.dart';

/// 下载文件核心逻辑
///
/// 支持：
/// - 断点续传（从 downloadedBytes 恢复）
/// - 多线程并发下载（默认 3 线程，通过 HTTP Range 实现）
/// - 小文件单线程直接下载
/// - Telegram 大文件预下载（202 响应处理）
Future<void> downloadFile({
  required String taskId,
  required TransferStorage storage,
  required String baseUrl,
  required CasdoorToken token,
}) async {
  final l = getLogger();

  // 获取任务记录
  final task = await storage.getDownloadTaskAsync(taskId);
  if (task == null) {
    l.warning('download record not found for taskId: $taskId');
    return;
  }

  http.Client? client;

  try {
    // 构建下载 URL
    String downloadUrl;

    // 检查是否是历史版本下载（格式：history:{historyId}）
    if (task.fileId.startsWith('history:')) {
      final historyId = task.fileId.substring(8); // 移除 'history:' 前缀
      downloadUrl = '$baseUrl/api/file/history/download?id=$historyId';
    } else {
      downloadUrl = '$baseUrl/api/file/stream/download?file_id=${task.fileId}';
      if (task.providerId != null) {
        downloadUrl += '&provider_id=${task.providerId}';
      }
    }

    final headers = <String, String>{
      'Authorization': 'Bearer ${token.accessToken}',
    };

    // 创建 HTTP 客户端
    client = http.Client();

    // 更新状态为下载中
    await storage.updateDownloadTask(
      task.id,
      status: DownloadStatus.downloading,
    );

    // 预下载检查：Telegram 大文件场景下，后端可能返回 202 等待缓存
    final preCheck = await _checkPreDownload(
      client: client,
      downloadUrl: downloadUrl,
      headers: headers,
    );
    if (preCheck != null) {
      l.info('Pre-download detected: $preCheck, task: ${task.id}');
      await storage.updateDownloadTask(
        task.id,
        status: DownloadStatus.predownload,
        errorMessage: preCheck,
      );
      return;
    }

    // 判断使用单线程还是多线程
    const smallFileThreshold = 50 * 1024 * 1024; // 50MB

    if (task.fileSize <= smallFileThreshold) {
      // 小文件：单线程下载
      await _downloadSingleThread(
        task: task,
        storage: storage,
        downloadUrl: downloadUrl,
        headers: headers,
        client: client,
      );
    } else {
      // 大文件：多线程并发下载
      await _downloadMultiThread(
        task: task,
        storage: storage,
        downloadUrl: downloadUrl,
        headers: headers,
        client: client,
      );
    }

    l.info('Download completed for task: ${task.id}');
  } catch (e) {
    l.error('Download failed: $e');
    final tt = await storage.getDownloadTaskAsync(taskId);
    if (tt != null) {
      await storage.updateDownloadTask(
        taskId,
        status: DownloadStatus.failed,
        errorMessage: e.toString(),
      );
    }
  } finally {
    client?.close();
  }
}

/// 检查是否需要预下载
///
/// 对于 Telegram 大文件（>20MB），后端可能返回 202 + {"status": "pre_download", ...}
/// 表示文件正在缓存中，需要前端更新状态并提示用户手动重试。
///
/// 返回 null 表示正常下载，返回字符串表示预下载原因（cache_status 描述）
Future<String?> _checkPreDownload({
  required http.Client client,
  required String downloadUrl,
  required Map<String, String> headers,
}) async {
  // 发送 HEAD 请求探测，不下载 body
  final resp = await client.head(Uri.parse(downloadUrl), headers: headers);
  if (resp.statusCode != 200 && resp.statusCode != 206) {
    // 非正常响应，尝试读取 body（可能是 202 JSON）
    if (resp.statusCode == 202 || resp.body.isNotEmpty) {
      try {
        final body = jsonDecode(resp.body) as Map<String, dynamic>;
        if (body['status'] == 'pre_download') {
          final cacheStatus = body['cache_status'];
          final providerId = body['provider_id'];
          return 'provider=$providerId, cache_status=$cacheStatus';
        }
      } catch (_) {
        // JSON 解析失败，忽略
      }
    }
    // 其他非正常状态码，抛出异常走原有失败流程
    throw Exception('HTTP ${resp.statusCode}: ${resp.reasonPhrase}');
  }
  return null;
}

/// 单线程下载（用于小文件）
Future<void> _downloadSingleThread({
  required DownloadTaskRecord task,
  required TransferStorage storage,
  required String downloadUrl,
  required Map<String, String> headers,
  required http.Client client,
}) async {
  final l = getLogger();

  // 打开本地文件（不存在则创建）
  final file = io.File(task.savePath);
  if (!await file.exists()) {
    await file.create(recursive: true);
  }

  io.RandomAccessFile raf = await file.open(mode: io.FileMode.writeOnly);

  try {
    int downloadedBytes = task.downloadedBytes;
    int deltaBytes = 0;
    Timer? syncTimer;

    // 已下载完则直接完成
    if (downloadedBytes >= task.fileSize) {
      await storage.updateDownloadTask(
        task.id,
        status: DownloadStatus.completed,
        downloadedBytes: task.fileSize,
      );
      return;
    }

    // 设置 Range 从断点开始
    final rangeHeaders = {...headers, 'Range': 'bytes=$downloadedBytes-'};

    syncTimer = Timer.periodic(Duration(seconds: 3), (t) {
      storage.updateDownloadTask(
        task.id,
        speed: deltaBytes ~/ 3,
        downloadedBytes: downloadedBytes,
      );
      deltaBytes = 0;
    });

    final response = await client.get(
      Uri.parse(downloadUrl),
      headers: rangeHeaders,
    );

    if (response.statusCode != 200 && response.statusCode != 206) {
      throw Exception('HTTP ${response.statusCode}: ${response.reasonPhrase}');
    }

    final bodyBytes = response.bodyBytes;

    // Seek 到起始位置并写入
    await raf.setPosition(downloadedBytes);
    await raf.writeFrom(bodyBytes);
    downloadedBytes += bodyBytes.length;
    deltaBytes += bodyBytes.length;

    // 检查是否被取消或暂停
    final currentTask = await storage.getDownloadTaskAsync(task.id);
    if (currentTask == null ||
        currentTask.status == DownloadStatus.canceled ||
        currentTask.status == DownloadStatus.paused) {
      l.info('Download cancelled or paused');
      return;
    }

    // 更新完成状态
    await storage.updateDownloadTask(
      task.id,
      status: DownloadStatus.completed,
      downloadedBytes: downloadedBytes,
    );

    syncTimer.cancel();
  } finally {
    await raf.close();
  }
}

/// 多线程并发下载（用于大文件）
Future<void> _downloadMultiThread({
  required DownloadTaskRecord task,
  required TransferStorage storage,
  required String downloadUrl,
  required Map<String, String> headers,
  required http.Client client,
}) async {
  final file = io.File(task.savePath);
  if (!await file.exists()) {
    await file.create(recursive: true);
  }

  // 计算总分片数
  final chunkSize = task.chunkSize;
  final totalChunks = (task.fileSize + chunkSize - 1) ~/ chunkSize;

  // 更新总分片数
  if (task.chunkTotal != totalChunks) {
    await storage.updateDownloadTask(task.id, chunkTotal: totalChunks);
  }

  io.RandomAccessFile raf = await file.open(mode: io.FileMode.writeOnly);

  try {
    int downloadedBytes = task.downloadedBytes;
    int deltaBytes = 0;
    Timer? syncTimer;

    // 已下载完则直接完成
    if (downloadedBytes >= task.fileSize) {
      await storage.updateDownloadTask(
        task.id,
        status: DownloadStatus.completed,
        downloadedBytes: task.fileSize,
      );
      return;
    }

    syncTimer = Timer.periodic(Duration(seconds: 3), (t) {
      storage.updateDownloadTask(
        task.id,
        speed: deltaBytes ~/ 3,
        downloadedBytes: downloadedBytes,
      );
      deltaBytes = 0;
    });

    // 当前写入位置（受保护，需要锁）
    int currentOffset = downloadedBytes;
    final offsetLock = _Lock();

    // 使用多个线程并发下载
    final futures = <Future>[];
    for (int i = 0; i < task.workerCount; i++) {
      futures.add(
        _downloadWorker(
          workerIndex: i,
          task: task,
          storage: storage,
          downloadUrl: downloadUrl,
          headers: headers,
          client: client,
          raf: raf,
          getCurrentOffset: () async =>
              offsetLock.synchronized(() async => currentOffset),
          setCurrentOffset: (offset) async => offsetLock.synchronized(() async {
            currentOffset = offset;
          }),
          onProgress: (bytes) async {
            deltaBytes += bytes;
            downloadedBytes = await offsetLock.synchronized(
              () async => currentOffset,
            );
          },
        ),
      );
    }

    await Future.wait(futures);
    syncTimer.cancel();
  } finally {
    await raf.close();
  }
}

/// 简单的锁实现，用于保护 currentOffset
class _Lock {
  bool _locked = false;
  final _queue = <Completer<void>>[];

  Future<T> _synchronized<T>(Future<T> Function() action) async {
    while (_locked) {
      final completer = Completer<void>();
      _queue.add(completer);
      await completer.future;
    }
    _locked = true;
    try {
      return await action();
    } finally {
      _locked = false;
      if (_queue.isNotEmpty) {
        _queue.removeAt(0).complete();
      }
    }
  }

  Future<R> synchronized<R>(Future<R> Function() action) =>
      _synchronized(action);
}

/// 单个下载 worker 线程
Future<void> _downloadWorker({
  required int workerIndex,
  required DownloadTaskRecord task,
  required TransferStorage storage,
  required String downloadUrl,
  required Map<String, String> headers,
  required http.Client client,
  required io.RandomAccessFile raf,
  required Future<int> Function() getCurrentOffset,
  required Future<void> Function(int offset) setCurrentOffset,
  required Future<void> Function(int bytes) onProgress,
}) async {
  final l = getLogger();
  final chunkSize = task.chunkSize;

  while (true) {
    // 获取下一个待下载的分片偏移量
    final currentOffset = await getCurrentOffset();
    if (currentOffset >= task.fileSize) break;

    // 检查是否被取消或暂停
    final currentTask = await storage.getDownloadTaskAsync(task.id);
    if (currentTask == null ||
        currentTask.status == DownloadStatus.canceled ||
        currentTask.status == DownloadStatus.paused) {
      l.info('Download cancelled or paused, worker $workerIndex stopping');
      return;
    }

    // 每个 worker 跳跃式分配：worker 0 下载 0,3,6...，worker 1 下载 1,4,7...，worker 2 下载 2,5,8...
    // 计算本 worker 本次下载的起始偏移量
    int offset = currentOffset + workerIndex * chunkSize;

    // 如果起始位置超过文件大小，跳过
    if (offset >= task.fileSize) break;

    // 计算结束位置
    int end = min(offset + chunkSize - 1, task.fileSize - 1);

    // 更新全局偏移量（跳过分片大小 * workerCount）
    final nextOffset = currentOffset + chunkSize * task.workerCount;
    await setCurrentOffset(
      nextOffset > task.fileSize ? task.fileSize : nextOffset,
    );

    // 发送 Range 请求
    final rangeHeaders = {...headers, 'Range': 'bytes=$offset-$end'};

    try {
      final response = await client.get(
        Uri.parse(downloadUrl),
        headers: rangeHeaders,
      );

      if (response.statusCode != 200 && response.statusCode != 206) {
        l.error('Worker $workerIndex: HTTP ${response.statusCode}');
        throw Exception('HTTP ${response.statusCode}');
      }

      final bodyBytes = response.bodyBytes;

      // 写入文件对应位置
      await raf.setPosition(offset);
      await raf.writeFrom(bodyBytes);
      await onProgress(bodyBytes.length);
    } catch (e) {
      l.error('Worker $workerIndex download error: $e');
      // 本次分片失败，继续下一个
      continue;
    }
  }

  l.info('Worker $workerIndex finished');
}
