import 'dart:async';
import 'dart:isolate';

import 'package:flutter/foundation.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/models/upload_task_record.dart';
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

  //  final  _downloadTaskController = StreamController<List<UploadTaskRecord>>.broadcast();
  //  Stream<List<UploadTaskRecord>> get uploadTaskStream => _uploadTaskController.stream;

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
    getLogger().debug(
      'background_service timer ----------, active uploads ${activeUploads.length},total ${uploadTasks.length}',
    );

    _uploadTaskController.add(uploadTasks);
    if (activeUploads.isEmpty) {
      getLogger().debug(
        'background_service timer, no active uploads,stop timer',
      );
      timer.cancel();
      _syncTimer = null;
    }
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
      await _scheduleWithWorkManager(taskId);
    } else if (isDesktopPlatform) {
      await _scheduleWithIsolate(taskId);
    }
  }

  /// 使用 WorkManager 调度任务（移动平台）
  /// workmanager任务只需要注册一次，后续由系统管理，即使后面应用关闭，系统也会自动调度，不需要重复注册
  Future<void> _scheduleWithWorkManager(String taskId) async {
    await Workmanager().registerOneOffTask(
      taskId,
      taskId,
      inputData: {'taskId': taskId, 'taskType': TaskType.upload},
      constraints: Constraints(networkType: NetworkType.connected),
    );

    _logger.info('Scheduled upload task with WorkManager: $taskId');
  }

  /// 使用 Isolate 调度任务（桌面平台）
  Future<void> _scheduleWithIsolate(String taskId) async {
    _logger.info('Starting upload task in background isolate: $taskId');

    final ii = await _getAvailableUploadIsolate();
    if (ii == null) {
      _logger.error('No available upload isolate');
      return;
    }
    ii.sendPort!.send(
      IsolateCommand(taskType: TaskType.upload, taskId: taskId),
    );
  }

  // 获取可用的Isolate
  Future<IsolateInfo?> _getAvailableUploadIsolate() async {
    for (final isolateInfo in _uploadIsolates.entries) {
      if (isolateInfo.value.status == IsolateStatus.ready ||
          isolateInfo.value.status == IsolateStatus.completed) {
        return isolateInfo.value;
      }
    }
    if (_uploadIsolates.length >= _maxIsolateCount) {
      return null;
    }
    // 创建新的Isolate

    final ci = await _createIsolate();
    return ci;
  }

  Future<IsolateInfo> _createIsolate() async {
    // 创建接收端口
    final receivePort = ReceivePort();

    // 获取数据库路径（确保使用与主线程相同的路径)
    final dbPath = await _storage.getDatabasePath();

    // 获取服务器配置（在主线程中获取，传递给 Isolate）
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
      ),
    );
    final ii = IsolateInfo(i: isolate, receivePort: receivePort);
    _uploadIsolates[id] = ii;

    final waitReady = Completer<void>.sync();
    receivePort.listen(
      (message) async => await _handleIsolateMessage(message, waitReady),
    );
    // 等待Isolate准备就绪
    await waitReady.future;

    return ii;
  }

  Future<void> _handleIsolateMessage(
    dynamic message,
    Completer<void> waitReady,
  ) async {
    final msg = message as IsolateMessage;
    final isUpload = msg.taskType == TaskType.upload;

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
      final task = await _storage.getNextPendingUploadTaskAsync();
      //TODO: 处理下载任务
      if (task != null) {
        await _scheduleWithIsolate(task.id);
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
  }

  void close() {
    _syncTimer?.cancel();
    _syncTimer = null;
    _uploadTaskController.close();
  }
}
