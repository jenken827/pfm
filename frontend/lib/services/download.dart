import 'dart:async';
import 'dart:convert';
import 'dart:io' as io;
import 'dart:math';

import 'package:http/http.dart' as http;
import 'package:pfm/models/models.dart';
import 'package:pfm/services/services.dart';
import 'package:pfm/utils/utils.dart';

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
  required AuthToken token,
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
      downloadUrl =
          '$baseUrl/api/file/history/download?id=$historyId'; // baseUrl already resolved to transferUrl in caller
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

    // 预下载检查：Telegram 大文件场景下，后端可能返回 202 等待缓存
    final preCheck = await _checkPreDownload(
      client: client,
      downloadUrl: downloadUrl,
      headers: headers,
    );
    if (preCheck != null) {
      l.info('Pre-download detected: ${preCheck.message}, task: ${task.id}');
      await storage.updateDownloadTask(
        task.id,
        status: DownloadStatus.predownload,
        errorMessage: preCheck.message,
        providerId: preCheck.providerId,
      );
      return;
    }

    // 更新状态为下载中
    await storage.updateDownloadTask(
      task.id,
      status: DownloadStatus.downloading,
    );

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

/// 预下载检查结果
class PreDownloadResult {
  final String message;
  final int providerId;
  final String cacheStatus;

  PreDownloadResult({
    required this.message,
    required this.providerId,
    required this.cacheStatus,
  });
}

/// 检查是否需要预下载
///
/// 对于 Telegram 大文件（>20MB），后端可能返回 202 + {"status": "pre_download", ...}
/// 表示文件正在缓存中，需要前端更新状态并提示用户手动重试。
///
/// 返回 null 表示正常下载，返回 PreDownloadResult 表示需要预下载
Future<PreDownloadResult?> _checkPreDownload({
  required http.Client client,
  required String downloadUrl,
  required Map<String, String> headers,
}) async {
  // 用 GET + Range: bytes=0-0 探测（HEAD 请求没有 body，无法解析 202 JSON）
  final resp = await client.get(
    Uri.parse(downloadUrl),
    headers: {...headers, 'Range': 'bytes=0-0'},
  );
  if (resp.statusCode == 202) {
    // 202 = 预下载中，解析 body
    try {
      final body = jsonDecode(resp.body) as Map<String, dynamic>;
      if (body['status'] == 'pre_download') {
        final cacheStatus = body['cache_status'];
        final providerId = body['provider_id'] as int;
        return PreDownloadResult(
          message: 'provider=$providerId, cache_status=$cacheStatus',
          providerId: providerId,
          cacheStatus: cacheStatus.toString(),
        );
      }
    } catch (_) {
      // JSON 解析失败
    }
  }
  if (resp.statusCode != 200 && resp.statusCode != 206) {
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
      await _publishIfNeeded(storage: storage, task: task);
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

    final request = http.Request('GET', Uri.parse(downloadUrl));
    request.headers.addAll(rangeHeaders);
    final response = await client.send(request);

    if (response.statusCode != 200 && response.statusCode != 206) {
      throw Exception('HTTP ${response.statusCode}: ${response.reasonPhrase}');
    }

    // 流式写入，不一次性读入内存
    await raf.setPosition(downloadedBytes);
    await for (final chunk in response.stream) {
      await raf.writeFrom(chunk);
      downloadedBytes += chunk.length;
      deltaBytes += chunk.length;

      // 检查是否被取消或暂停
      final currentTask = await storage.getDownloadTaskAsync(task.id);
      if (currentTask == null ||
          currentTask.status == DownloadStatus.canceled ||
          currentTask.status == DownloadStatus.paused) {
        l.info('Download cancelled or paused');
        syncTimer.cancel();
        return;
      }
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

  // 发布必须在文件句柄关闭之后：Native 侧要读整份文件，句柄未刷盘会读到不完整内容
  await _publishIfNeeded(storage: storage, task: task);
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

    // 写入锁：保护 setPosition + writeFrom 的原子性
    final writeLock = _Lock();

    // 每个 worker 独立追踪自己的 chunk index，无需共享 currentOffset
    // worker 0 下载 chunk 0, 3, 6, 9...
    // worker 1 下载 chunk 1, 4, 7, 10...
    // worker 2 下载 chunk 2, 5, 8, 11...
    final futures = <Future>[];
    for (int i = 0; i < task.workerCount; i++) {
      futures.add(
        _downloadWorker(
          workerIndex: i,
          workerCount: task.workerCount,
          chunkSize: chunkSize,
          fileSize: task.fileSize,
          taskId: task.id,
          storage: storage,
          downloadUrl: downloadUrl,
          headers: headers,
          client: client,
          raf: raf,
          writeLock: writeLock,
          onProgress: (bytes) async {
            deltaBytes += bytes;
            downloadedBytes += bytes;
          },
        ),
      );
    }

    await Future.wait(futures);

    // 更新完成状态
    await storage.updateDownloadTask(
      task.id,
      status: DownloadStatus.completed,
      downloadedBytes: task.fileSize,
    );

    syncTimer.cancel();
  } finally {
    await raf.close();
  }

  // 发布必须在文件句柄关闭之后：Native 侧要读整份文件，句柄未刷盘会读到不完整内容
  await _publishIfNeeded(storage: storage, task: task);
}

/// 简单的锁实现，用于保护写操作
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

/// 单个下载 worker
///
/// 每个 worker 独立追踪 chunk index：
/// - worker 0: chunk 0, 3, 6, 9, ...
/// - worker 1: chunk 1, 4, 7, 10, ...
/// - worker 2: chunk 2, 5, 8, 11, ...
Future<void> _downloadWorker({
  required int workerIndex,
  required int workerCount,
  required int chunkSize,
  required int fileSize,
  required String taskId,
  required TransferStorage storage,
  required String downloadUrl,
  required Map<String, String> headers,
  required http.Client client,
  required io.RandomAccessFile raf,
  required _Lock writeLock,
  required Future<void> Function(int bytes) onProgress,
}) async {
  final l = getLogger();

  // 从断点恢复：计算本 worker 应该从哪个 chunk 开始
  // downloadedBytes 可能不为 0（断点续传），需要跳过已下载的 chunk
  int chunkIndex = workerIndex;

  while (true) {
    // 计算本 chunk 的起止位置
    int offset = chunkIndex * chunkSize;
    if (offset >= fileSize) break;

    int end = min(offset + chunkSize - 1, fileSize - 1);

    // 检查是否被取消或暂停
    final currentTask = await storage.getDownloadTaskAsync(taskId);
    if (currentTask == null ||
        currentTask.status == DownloadStatus.canceled ||
        currentTask.status == DownloadStatus.paused) {
      l.info('Download cancelled or paused, worker $workerIndex stopping');
      return;
    }

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

      // 写入文件对应位置（需要锁保护，setPosition 和 writeFrom 不能被其他 worker 打断）
      await writeLock.synchronized(() async {
        await raf.setPosition(offset);
        await raf.writeFrom(bodyBytes);
      });
      await onProgress(bodyBytes.length);
    } catch (e) {
      l.error('Worker $workerIndex download error: $e');
      // 本次分片失败，继续下一个
    }

    // 跳到下一个属于本 worker 的 chunk
    chunkIndex += workerCount;
  }

  l.info('Worker $workerIndex finished');
}

/// 下载完成后的「发布到公共下载目录」步骤（仅 Android 生效）。
///
/// 分区存储下应用私有目录对其他应用不可见，因此完成后把文件发布到系统「下载」，
/// 由原生侧经 MediaStore 代写（无需 MANAGE_EXTERNAL_STORAGE）。
///
/// 成功则把任务记录的 savePath 指向公共路径并删除私有残留；
/// 失败仅记录日志并保留私有文件 —— 下载本身已成功，不得因此把任务标为 failed。
Future<void> _publishIfNeeded({
  required TransferStorage storage,
  required DownloadTaskRecord task,
}) async {
  final l = getLogger();
  if (!DownloadDir.publishNeeded) return;

  final localPath = task.savePath;
  final src = io.File(localPath);
  if (!await src.exists()) {
    l.warning('publish skipped, local file missing: $localPath');
    return;
  }

  final published = await DownloadDir.publish(
    localPath: localPath,
    fileName: task.filename,
  );
  if (published == null) {
    l.warning('publish failed, keeping private copy at $localPath');
    return;
  }

  // 先更新记录再删源：即使删源失败，savePath 也已指向可见的公共副本。
  // contentUri 一并落库：Android 上「打开文件」必须用它（open_file 对公共目录
  // 路径会要求 MANAGE_EXTERNAL_STORAGE，而本应用不申请该权限）。
  await storage.updateDownloadTask(
    task.id,
    savePath: published.path,
    contentUri: published.uri,
    // uri 为空（API < 29）时显式置空，避免旧记录残留失效 uri
    clearContentUri: published.uri == null,
  );
  try {
    await src.delete();
  } catch (e) {
    l.warning('failed to delete private copy $localPath: $e');
  }
}
