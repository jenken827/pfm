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

  /// 上传并发数（从配置读取）
  int get _maxUploadConcurrency {
    final ls = GetIt.I.get<LocalStorage>();
    return ls.uploadConcurrency;
  }

  /// 下载并发数（从配置读取）
  int get _maxDownloadConcurrency {
    final ls = GetIt.I.get<LocalStorage>();
    return ls.downloadConcurrency;
  }

  /// 已调度但尚未开始执行的任务 ID（防止重复调度）
  final Set<String> _scheduledUploadIds = {};
  final Set<String> _scheduledDownloadIds = {};

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

    _instance!._syncTimer = Timer.periodic(Duration(seconds: 3), (timer) async {
      await _instance!._timerHandler(timer);
    });

    return _instance!;
  }

  Future<void> _timerHandler(Timer timer) async {
    // 使用计数方法，不加载全部任务到内存
    final uploadingCount = await storage.countUploadingTasks();
    final pendingCount = await storage.countPendingUploadTasks();
    final downloadingCount = await storage.countDownloadingTasks();
    final activeDownloadCount = await storage.countActiveDownloadTasks();

    _logger.info(
      'Timer: uploading=$uploadingCount, pending=$pendingCount, '
      'downloading=$downloadingCount, scheduled=${_scheduledUploadIds.length}/${_scheduledDownloadIds.length}',
    );

    // 移动端并发调度：只加载需要调度的 pending 任务
    if (isWorkManagerSupported) {
      // 上传调度
      final canScheduleUpload = _maxUploadConcurrency - uploadingCount - _scheduledUploadIds.length;
      _logger.info('Timer: canScheduleUpload=$canScheduleUpload (max=${_maxUploadConcurrency}, uploading=$uploadingCount, scheduled=${_scheduledUploadIds.length})');

      if (canScheduleUpload > 0 && pendingCount > 0) {
        // 只加载需要调度的任务（排除已调度的）
        final pendingTasks = await storage.getUploadTasksByStatus(
          UploadStatus.PENDING,
          limit: canScheduleUpload,
        );
        final toSchedule = pendingTasks
            .where((t) => !_scheduledUploadIds.contains(t.id))
            .take(canScheduleUpload)
            .toList();

        _logger.info('Timer: found ${pendingTasks.length} pending tasks, ${toSchedule.length} to schedule');

        for (final task in toSchedule) {
          _logger.info('Timer: scheduling upload ${task.id} (${task.filename})');
          _scheduledUploadIds.add(task.id);
          try {
            await _scheduleWithWorkManager(task.id, TaskType.upload);
            _logger.info('Timer: successfully scheduled ${task.id}');
          } catch (e) {
            _logger.error('Timer: failed to schedule ${task.id}: $e');
            _scheduledUploadIds.remove(task.id);
          }
        }
      }

      // 清理已开始执行或已结束的调度记录
      _scheduledUploadIds.removeWhere((id) {
        // 检查任务是否还在 pending 状态
        return !_scheduledUploadIds.contains(id); // 简化：如果不在已调度列表中就清理
      });
      // 更准确的清理：查询已调度任务的当前状态
      final scheduledToRemove = <String>[];
      for (final id in _scheduledUploadIds) {
        final task = await storage.getUploadTaskAsync(id);
        if (task == null || task.status != UploadStatus.PENDING) {
          scheduledToRemove.add(id);
        }
      }
      _scheduledUploadIds.removeAll(scheduledToRemove);

      // 下载调度
      final canScheduleDownload = _maxDownloadConcurrency - downloadingCount - _scheduledDownloadIds.length;
      if (canScheduleDownload > 0 && activeDownloadCount > downloadingCount) {
        final pendingDownloads = await storage.getDownloadTasksByStatus(
          DownloadStatus.pending,
          limit: canScheduleDownload,
        );
        final toSchedule = pendingDownloads
            .where((t) => !_scheduledDownloadIds.contains(t.id))
            .take(canScheduleDownload)
            .toList();

        for (final task in toSchedule) {
          getLogger().info('Scheduling download: ${task.id} ($downloadingCount/${_maxDownloadConcurrency} active)');
          _scheduledDownloadIds.add(task.id);
          await _scheduleWithWorkManager(task.id, TaskType.download);
        }
      }

      // 清理已开始执行或已结束的下载调度记录
      final downloadToRemove = <String>[];
      for (final id in _scheduledDownloadIds) {
        final task = await storage.getDownloadTaskAsync(id);
        if (task == null || task.status != DownloadStatus.pending) {
          downloadToRemove.add(id);
        }
      }
      _scheduledDownloadIds.removeAll(downloadToRemove);
    }

    // 定期通知 UI 更新（使用分页加载的任务）
    final recentUploads = await storage.getAllUploadTasksAsync(limit: 100);
    final recentDownloads = await storage.getAllDownloadTasksAsync(limit: 100);
    _uploadTaskController.add(recentUploads);
    _downloadTaskController.add(recentDownloads);

    if (uploadingCount == 0 && pendingCount == 0 && downloadingCount == 0) {
      getLogger().debug('timer: no active tasks, stopping timer');
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

  /// 批量新增上传任务（仅写入数据库，不逐个调度 WorkManager）
  /// 调用方应最后调用 scheduleFirstPendingUpload() 触发处理
  Future<void> addUploadTasksBatch(List<UploadTaskRecord> tasks) async {
    for (final task in tasks) {
      task.status = UploadStatus.PENDING;
      await _storage.addUploadTask(task);
    }
    _logger.info('Batch added ${tasks.length} upload tasks to database');
  }

  /// 调度待处理的上传任务（用于批量添加后触发处理）
  Future<void> scheduleFirstPendingUpload() async {
    _logger.info('scheduleFirstPendingUpload called, ensuring timer is running');
    // 确保 timer 运行，由 timer 统一调度任务
    _ensureTimerRunning();
  }

  /// 确保 timer 正在运行
  void _ensureTimerRunning() {
    if (_syncTimer == null || !_syncTimer!.isActive) {
      _syncTimer?.cancel();
      _syncTimer = Timer.periodic(Duration(seconds: 3), (timer) async {
        await _timerHandler(timer);
      });
      _logger.info('Timer started');
    } else {
      _logger.info('Timer already active');
    }
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
        chunkSize: ls.uploadChunkSize,
        smallFileThreshold: ls.smallFileThreshold,
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
  /// 使用 WorkManager 调度任务（移动平台）
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
      chunkSize: ls.uploadChunkSize,
      smallFileThreshold: ls.smallFileThreshold,
    );

    final inputData = params.toJson();
    // 不记录 inputData，包含 token 敏感信息
    _logger.info('Scheduling ${taskType} task with WorkManager: $taskId');

    await Workmanager().registerOneOffTask(
      taskId,
      taskId,
      inputData: inputData,
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

  /// 恢复上传任务（只重置状态，由 _timerHandler 调度，不超出并发限制）
  Future<void> resumeUpload(String taskId) async {
    _logger.info('Resuming upload task: $taskId');
    await _storage.resumeUploadTasks(taskId);
    // 确保 timer 运行，由 timer 调度 pending 任务
    _syncTimer ??= Timer.periodic(Duration(seconds: 3), (timer) async {
      await _timerHandler(timer);
    });
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

  /// 重试下载任务（只重置状态，由 _timerHandler 调度，不超出并发限制）
  Future<void> retryDownload(String taskId) async {
    _logger.info('Retrying download task: $taskId');
    await _storage.updateDownloadTask(
      taskId,
      status: DownloadStatus.pending,
      errorMessage: null,
      downloadedBytes: 0,
    );
    // 确保 timer 运行，由 timer 调度 pending 任务
    _syncTimer ??= Timer.periodic(Duration(seconds: 3), (timer) async {
      await _timerHandler(timer);
    });
  }

  void close() {
    _syncTimer?.cancel();
    _syncTimer = null;
    _uploadTaskController.close();
    _downloadTaskController.close();
  }
}
