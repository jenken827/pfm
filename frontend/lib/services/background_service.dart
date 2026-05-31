import 'dart:async';
import 'dart:isolate';

import 'package:flutter/foundation.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/services/services.dart';
import 'package:pfm/utils/platform.dart';
import 'package:talker_flutter/talker_flutter.dart';
import 'package:uuid/uuid.dart';

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

  // 同步定时器，用来同步上传/下载任务
  Timer? _syncTimer;

  final _uploadTaskController =
      StreamController<List<UploadTaskRecord>>.broadcast();
  Stream<List<UploadTaskRecord>> get uploadTaskStream =>
      _uploadTaskController.stream;

  final _downloadTaskController =
      StreamController<List<DownloadTaskRecord>>.broadcast();
  Stream<List<DownloadTaskRecord>> get downloadTaskStream =>
      _downloadTaskController.stream;

  /// 桌面平台的 Isolate 管理
  /// 最多5个进程
  static const int _maxIsolateCount = 5;

  final Map<String, IsolateInfo> _uploadIsolates = {};

  final Map<String, IsolateInfo> _downloadIsolates = {};

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
    if (_instance != null) {
      return _instance!;
    }
    final storage = await Sqlite3Storage.instance();
    _instance = BackgroundService._(storage);

    _instance!._syncTimer = Timer.periodic(Duration(seconds: 1), (timer) async {
      await _instance!._timerHandler(timer);
    });

    return _instance!;
  }

  Future<void> _timerHandler(Timer timer) async {
    final uploadTasks = await storage.getAllUploadTasksAsync();
    final activeUploads = uploadTasks.where((task) => task.isActive).toList();
    final downloadTasks = await storage.getAllDownloadTasksAsync();
    final activeDownloads = downloadTasks
        .where((task) => task.isActive)
        .toList();

    getLogger().debug(
      'background_service timer: active uploads ${activeUploads.length}, '
      'active downloads ${activeDownloads.length}',
    );

    _uploadTaskController.add(uploadTasks);
    _downloadTaskController.add(downloadTasks);

    if (activeUploads.isEmpty && activeDownloads.isEmpty) {
      getLogger().debug(
        'background_service timer: no active tasks, stop timer',
      );
      timer.cancel();
      _syncTimer = null;
    }
  }

  /// 手动触发UI更新（用于任务完成时确保UI刷新）
  Future<void> notifyUploadTasksChanged() async {
    final uploadTasks = await storage.getAllUploadTasksAsync();
    _uploadTaskController.add(uploadTasks);
  }

  /// 手动触发下载UI更新
  Future<void> notifyDownloadTasksChanged() async {
    final downloadTasks = await storage.getAllDownloadTasksAsync();
    _downloadTaskController.add(downloadTasks);
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

  // 新增上传任务
  Future<void> scheduleNewUploadTask(UploadTaskRecord task) async {
    _logger.info('Adding new upload task: ${task.id}');
    // 保存新任务记录
    task.status = UploadStatus.PENDING;
    await _storage.addUploadTask(task);
    await scheduleUpload(task.id, true);
  }

  // 恢复上传任务
  Future<void> resumeUploadTasks(String? taskId) async {
    _logger.info('Resuming all upload tasks');
    final taskIds = await _storage.resumeUploadTasks(taskId);
    //重新调度
    for (final tid in taskIds) {
      await scheduleUpload(tid, true);
    }
  }

  /// 调度上传任务
  ///
  /// 根据平台选择不同的后台调度方式：
  /// isNew 表示是否为新的调度，针对移动平台，如果任务已经注册过了，应用再次启动初始化时，不用再注册一次
  /// 其它情况，比如新增任务，或者之前用户暂停了，恢复任务，都需要重新注册一次
  Future<void> scheduleUpload(String taskId, bool isNew) async {
    _logger.info('Scheduling upload task: $taskId');
    _syncTimer ??= Timer.periodic(Duration(seconds: 1), (timer) async {
      await _timerHandler(timer);
    });

    if (isWorkManagerSupported && isNew) {
      await _scheduleWithWorkManager(taskId, TaskType.upload);
    } else if (isDesktopPlatform) {
      await _scheduleWithIsolate(taskId, TaskType.upload);
    }
  }

  /// 新增下载任务
  Future<void> scheduleNewDownloadTask(DownloadTaskRecord task) async {
    _logger.info('Adding new download task: ${task.id}');
    task.status = DownloadStatus.pending;
    await _storage.addDownloadTask(task);
    await scheduleDownload(task.id, true);
  }

  /// 恢复下载任务
  Future<void> resumeDownloadTasks(String? taskId) async {
    _logger.info('Resuming download tasks');
    final taskIds = await _storage.resumeDownloadTasks(taskId);
    for (final tid in taskIds) {
      await scheduleDownload(tid, true);
    }
  }

  /// 调度下载任务
  Future<void> scheduleDownload(String taskId, bool isNew) async {
    _logger.info('Scheduling download task: $taskId');
    _syncTimer ??= Timer.periodic(Duration(seconds: 1), (timer) async {
      await _timerHandler(timer);
    });

    if (isWorkManagerSupported && isNew) {
      await _scheduleWithWorkManager(taskId, TaskType.download);
    } else if (isDesktopPlatform) {
      await _scheduleWithIsolate(taskId, TaskType.download);
    }
  }

  /// 使用 Isolate 调度任务（桌面平台）
  Future<void> _scheduleWithIsolate(String taskId, TaskType taskType) async {
    _logger.info('Starting $taskType task in background isolate: $taskId');

    final ii = await _getAvailableIsolate(taskType);
    if (ii == null) {
      _logger.error('No available isolate for $taskType');
      return;
    }

    _logger.info('Sending command to isolate, sendPort=${ii.sendPort}');
    ii.sendPort!.send(IsolateCommand(taskType: taskType, taskId: taskId));
  }

  // 获取可用的Isolate（upload和download各有一组，隔离不可共用）
  Future<IsolateInfo?> _getAvailableIsolate(TaskType taskType) async {
    final isolates = taskType == TaskType.upload
        ? _uploadIsolates
        : _downloadIsolates;

    // 1. 先从自己的池里找空闲的
    for (final isolateInfo in isolates.entries) {
      if (isolateInfo.value.status == IsolateStatus.ready ||
          isolateInfo.value.status == IsolateStatus.completed) {
        return isolateInfo.value;
      }
    }

    // 2. 如果自己的池未满，直接创建
    if (isolates.length < _maxIsolateCount) {
      return await _createIsolate(taskType);
    }

    // 3. 自己的池已达上限，无法创建
    _logger.error('No available isolate for $taskType, pool exhausted');
    return null;
  }

  Future<IsolateInfo> _createIsolate(TaskType taskType) async {
    // 创建接收端口
    final receivePort = ReceivePort();
    final waitReady = Completer<void>.sync();
    receivePort.listen(
      (message) async =>
          await _handleIsolateMessage(message, waitReady, taskType),
    );

    // 获取数据库路径
    final dbPath = await _storage.getDatabasePath();

    // 获取服务器配置
    final ls = GetIt.I.get<LocalStorage>();

    final id = Uuid().v8();

    // 启动 Isolate
    final isolate = await Isolate.spawn(
      isolateEntryPoint,
      IsolateConfig(
        id: id,
        sendPort: receivePort.sendPort,
        baseUrl: ls.fileServer!,
        token: ls.token!,
        dbPath: dbPath,
        taskType: taskType,
      ),
    );

    final isolates = taskType == TaskType.upload
        ? _uploadIsolates
        : _downloadIsolates;
    final ii = IsolateInfo(i: isolate, receivePort: receivePort);
    isolates[id] = ii;
    await waitReady.future;
    _logger.debug('isolate create success and ready---');
    return ii;
  }

  /// 使用 WorkManager 调度任务（移动平台）
  /// workmanager任务只需要注册一次，后续由系统管理，即使后面应用关闭，系统也会自动调度，不需要重复注册
  Future<void> _scheduleWithWorkManager(
    String taskId,
    TaskType taskType,
  ) async {
    final ls = GetIt.I.get<LocalStorage>();
    final dbPath = await _storage.getDatabasePath();

    final params = WmTaskParam(
      taskId: taskId,
      taskType: taskType,
      baseUrl: ls.fileServer!,
      token: ls.token!,
      dbPath: dbPath,
    );

    await Workmanager().registerOneOffTask(
      taskId,
      taskId,
      inputData: params.toJson(),
      constraints: Constraints(networkType: NetworkType.connected),
    );

    _logger.info('Scheduled $taskType task with WorkManager: $taskId');
  }

  Future<void> _handleIsolateMessage(
    dynamic message,
    Completer<void> waitReady,
    TaskType taskType,
  ) async {
    final msg = message as IsolateMessage;
    final isUpload = msg.taskType == TaskType.upload;
    _logger.debug(
      'main isolate receive message from isolate, tasktype: ${msg.taskType},status: ${msg.status}, sendport: ${msg.sendPort},isolateId:${msg.isolateId}',
    );

    if (msg.sendPort != null) {
      // 将内部发送端口保存到隔离信息中
      final isolateInfo = isUpload
          ? _uploadIsolates[msg.isolateId]
          : _downloadIsolates[msg.isolateId];
      if (isolateInfo != null) {
        isolateInfo.sendPort = msg.sendPort;
        isolateInfo.status = IsolateStatus.ready;
        waitReady.complete();
      }
      return;
    }
    // 处理其它消息
    if (msg.status == IsolateStatus.executing) {
      final isolateInfo = isUpload
          ? _uploadIsolates[msg.isolateId]
          : _downloadIsolates[msg.isolateId];

      isolateInfo?.status = IsolateStatus.executing;
      return;
    }
    // 任务执行完毕，尝试获取下一个任务
    if (msg.status == IsolateStatus.completed) {
      final isolateInfo = isUpload
          ? _uploadIsolates[msg.isolateId]
          : _downloadIsolates[msg.isolateId];

      isolateInfo?.status = IsolateStatus.completed;

      // 任务完成后通知UI更新
      if (isUpload) {
        await notifyUploadTasksChanged();
        final task = await _storage.getNextPendingUploadTaskAsync();
        if (task != null) {
          await _scheduleWithIsolate(task.id, TaskType.upload);
        }
      } else {
        await notifyDownloadTasksChanged();
        final task = await _storage.getNextPendingDownloadTaskAsync();
        if (task != null) {
          await _scheduleWithIsolate(task.id, TaskType.download);
        }
      }
    }
  }

  /// 取消上传任务
  Future<void> cancelUpload(String taskId) async {
    _logger.info('Cancelling upload task: $taskId');

    // 更新任务状态
    await _storage.updateUploadTask(taskId, status: UploadStatus.CANCELED);
  }

  /// 暂停上传任务
  Future<void> pauseUpload(String taskId) async {
    _logger.info('Pausing upload task: $taskId');
    await _storage.updateUploadTask(taskId, status: UploadStatus.PAUSED);
  }

  /// 恢复上传任务
  Future<void> resumeUpload(String taskId) async {
    _logger.info('Resuming upload task: $taskId');

    final task = await _storage.getUploadTaskAsync(taskId);
    if (task == null) return;
    await scheduleUpload(taskId, true);
  }

  /// 清理已完成的任务
  Future<void> cleanup() async {
    await _storage.clearUploadCompleted();
    await _storage.clearDownloadCompleted();
  }

  /// 取消下载任务
  Future<void> cancelDownload(String taskId) async {
    _logger.info('Cancelling download task: $taskId');
    await _storage.updateDownloadTask(taskId, status: DownloadStatus.canceled);
  }

  /// 暂停下载任务
  Future<void> pauseDownload(String taskId) async {
    _logger.info('Pausing download task: $taskId');
    await _storage.updateDownloadTask(taskId, status: DownloadStatus.paused);
  }

  /// 恢复下载任务
  Future<void> resumeDownload(String taskId) async {
    _logger.info('Resuming download task: $taskId');
    final task = await _storage.getDownloadTaskAsync(taskId);
    if (task == null) return;
    await scheduleDownload(taskId, true);
  }

  /// 重试下载任务（将失败任务重置为待调度状态）
  Future<void> retryDownload(String taskId) async {
    _logger.info('Retrying download task: $taskId');
    await _storage.updateDownloadTask(
      taskId,
      status: DownloadStatus.pending,
      errorMessage: null,
      downloadedBytes: 0,
    );
    await scheduleDownload(taskId, true);
  }

  void close() {
    _syncTimer?.cancel();
    _syncTimer = null;
    _uploadTaskController.close();
    _downloadTaskController.close();
  }
}
