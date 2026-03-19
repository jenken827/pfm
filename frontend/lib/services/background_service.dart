import 'dart:async';
import 'dart:isolate';

import 'package:flutter/foundation.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/models/upload_task_record.dart';
import 'package:pfm/services/services.dart';
import 'package:pfm/utils/platform.dart';
import 'package:talker_flutter/talker_flutter.dart';

// 条件导入 WorkManager（仅移动平台）
import 'package:workmanager/workmanager.dart'
    if (dart.library.html) ''
    if (dart.library.io) 'package:workmanager/workmanager.dart';

/// 后台服务(上传，下载)
///
/// 平台支持：
/// - Android/iOS: 使用 WorkManager 实现真正的后台执行
/// - Windows/macOS/Linux: 使用 Isolate 实现应用内后台执行
/// - Web: 不支持后台任务
class BackgroundService {
  static BackgroundService? _instance;
  final Sqlite3Storage _storage;
  late final Talker _logger;

  /// 桌面平台的 Isolate 管理
  final Map<String, Isolate> _isolates = {};
  final Map<String, ReceivePort> _receivePorts = {};

  /// 进度回调
  final Map<String, void Function(double)> _progressCallbacks = {};

  /// 是否支持 WorkManager（仅移动平台）
  static bool get isWorkManagerSupported => isMobile();

  /// 是否支持后台任务
  static bool get isBackgroundTaskSupported => !isWeb();

  /// 是否为桌面平台
  static bool get isDesktopPlatform => isDesktop();

  BackgroundService._(this._storage) : _logger = getLogger();

  /// 获取存储实例
  Sqlite3Storage get storage => _storage;

  /// 获取单例实例
  static Future<BackgroundService> instance() async {
    if (_instance == null) {
      final storage = await Sqlite3Storage.instance();
      _instance = BackgroundService._(storage);
    }
    return _instance!;
  }

  /// 初始化后台服务
  ///
  /// 移动平台：初始化 WorkManager
  /// 桌面平台：无需特殊初始化
  static Future<void> initialize() async {
    if (kIsWeb) {
      getLogger().info('Web platform - background upload not supported');
      return;
    }

    if (isWorkManagerSupported) {
      // 移动平台：初始化 WorkManager
      // callbackDispatcher 在 workmanager_upload_task.dart 中定义
      await Workmanager().initialize(callbackDispatcher);
      getLogger().info('WorkManager initialized (mobile)');
    } else if (isDesktopPlatform) {
      // 桌面平台：无需特殊初始化
      getLogger().info(
        'Desktop platform - using Isolate for background upload',
      );
    }
  }

  /// 调度上传任务
  ///
  /// 根据平台选择不同的后台调度方式：
  Future<void> scheduleUpload(UploadTaskRecord task) async {
    _logger.info('Scheduling upload task: ${task.id}');

    // 保存任务记录
    await _storage.saveUploadTask(task);

    if (isWorkManagerSupported) {
      await _scheduleWithWorkManager(task);
    } else if (isDesktopPlatform) {
      await _scheduleWithIsolate(task);
    }
  }

  /// 使用 WorkManager 调度任务（移动平台）
  Future<void> _scheduleWithWorkManager(UploadTaskRecord task) async {
    await Workmanager().registerOneOffTask(
      kUploadTaskName,
      '${kUploadTaskName}_${task.id}',
      inputData: {'taskId': task.id},
      constraints: Constraints(networkType: NetworkType.connected),
    );
    _logger.info('Scheduled upload task with WorkManager: ${task.id}');
  }

  /// 使用 Isolate 调度任务（桌面平台）
  Future<void> _scheduleWithIsolate(UploadTaskRecord task) async {
    _logger.info('Starting upload task in background isolate: ${task.id}');

    // 创建接收端口
    final receivePort = ReceivePort();
    _receivePorts[task.id] = receivePort;

    // 获取数据库路径（确保使用与主线程相同的路径)
    final dbPath = await _storage.getDatabasePath();

    // 获取服务器配置（在主线程中获取，传递给 Isolate）
    final baseUrl = _getBaseUrl();
    final token = _getToken();

    // 启动 Isolate
    final isolate = await Isolate.spawn(
      uploadIsolateEntryPoint,
      UploadIsolateConfig(
        sendPort: receivePort.sendPort,
        taskId: task.id,
        filePath: task.filePath,
        fileName: task.fileName,
        fileSize: task.fileSize,
        parentId: task.parentId,
        chunkSize: task.chunkSize,
        chunkTotal: task.chunkTotal,
        baseUrl: baseUrl,
        token: token,
        dbPath: dbPath,
      ),
    );

    _isolates[task.id] = isolate;
    _receivePorts[task.id] = receivePort;

    // 监听进度
    receivePort.listen((message) {
      _handleIsolateMessage(task.id, message);
    });
  }

  /// 处理 Isolate 消息
  void _handleIsolateMessage(String taskId, dynamic message) {
    if (message is! Map) return;

    final type = message['type'] as String?;
    switch (type) {
      case 'progress':
        final progress = message['progress'] as double?;
        final stage = message['stage'] as String?;
        if (progress != null) {
          _logger.debug('Task $taskId progress: $progress%, stage: $stage');
          _progressCallbacks[taskId]?.call(progress);
          // 更新 hash 进度
          if (stage == 'hashing') {
            _storage.updateUploadTask(taskId, hashProgress: progress * 10);
          }
        }
        break;
      case 'completed':
        _logger.info('Task $taskId completed');
        _progressCallbacks[taskId]?.call(100);
        break;
      case 'failed':
        final error = message['error'] as String?;
        _logger.error('Task $taskId failed: $error');
        break;
    }
  }

  String _getBaseUrl() {
    return GetIt.I.get<LocalStorage>().fileServer ?? '';
  }

  String _getToken() {
    return GetIt.I.get<LocalStorage>().token?.accessToken ?? '';
  }

  /// 取消上传任务
  Future<void> cancelUpload(String taskId) async {
    _logger.info('Cancelling upload task: $taskId');

    // 更新任务状态
    await _storage.updateUploadTask(
      taskId,
      status: UploadTaskStatusRecord.cancelled,
    );

    // 杀死 Isolate
    _isolates[taskId]?.kill(priority: Isolate.immediate);
    _isolates.remove(taskId);
    _receivePorts[taskId]?.close();
    _receivePorts.remove(taskId);
    _progressCallbacks.remove(taskId);
  }

  /// 暂停上传任务
  Future<void> pauseUpload(String taskId) async {
    _logger.info('Pausing upload task: $taskId');
    await _storage.updateUploadTask(
      taskId,
      status: UploadTaskStatusRecord.paused,
    );
  }

  /// 恢复上传任务
  Future<void> resumeUpload(String taskId) async {
    _logger.info('Resuming upload task: $taskId');

    final task = _storage.getUploadTask(taskId);
    if (task == null) return;

    task.status = UploadTaskStatusRecord.pending;
    await _storage.saveUploadTask(task);

    if (isDesktopPlatform) {
      await _scheduleWithIsolate(task);
    }
  }

  /// 清理已完成的任务
  Future<void> cleanup() async {
    _isolates.forEach((key, isolate) {
      isolate.kill(priority: Isolate.immediate);
    });
    _isolates.clear();

    _receivePorts.forEach((key, port) {
      port.close();
    });
    _receivePorts.clear();

    _progressCallbacks.clear();
  }
}
