import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/proto/native/enum.pbenum.dart' hide BackupFileStatus;
import 'package:pfm/services/services.dart';
import 'package:talker_flutter/talker_flutter.dart';

/// 后台服务(上传，下载)
///
/// 使用 Timer 定期调度上传/下载任务，直接在主 Isolate 执行（I/O 密集型不阻塞 UI）。
class BackgroundService {
  static BackgroundService? _instance;
  final Sqlite3Storage _storage;
  late final TransferStorage _transferStorage;
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

  /// 公开的计数 getter（供 UI 使用）
  /// uploadingCount 用 _runningUploads.length，避免统计到旧的卡住任务
  int get uploadingCount => _runningUploads.length;
  int get pendingCount => _pendingCount;
  int get completedCount => _completedCount;
  int get failedCount => _failedCount;
  int get downloadingCount => _runningDownloads.length;

  /// 取消所有传输任务（退出登录/Token 失效时调用）
  /// 注意：不停 timer，因为登录后需要它驱动 UI 刷新。
  /// timer 内有 isLoggedIn guard，未登录时会自动跳过调度。
  void cancelAllTransfers() {
    _logger.info('Cancelling all transfers');
    _runningUploads.clear();
    _runningDownloads.clear();
    _scheduledUploadIds.clear();
    _scheduledDownloadIds.clear();
    _pendingCount = 0;
    _completedCount = 0;
    _failedCount = 0;
    notifyUploadTasksChanged();
    notifyDownloadTasksChanged();
  }

  /// 已调度但尚未开始执行的任务 ID（防止重复调度）
  final Map<String, DateTime> _scheduledUploadIds = {};
  final Map<String, DateTime> _scheduledDownloadIds = {};

  /// 当前正在执行的任务 ID（用于并发控制）
  final Set<String> _runningUploads = {};
  final Set<String> _runningDownloads = {};

  /// timer 是否正在执行（防止重入）
  bool _timerRunning = false;

  /// 上传/下载任务计数（从数据库获取，用于 UI 状态统计）
  int _pendingCount = 0;
  int _completedCount = 0;
  int _failedCount = 0;

  BackgroundService._(this._storage) : _logger = getLogger();

  /// 获取存储实例
  Sqlite3Storage get storage => _storage;

  /// 获取传输任务存储实例（Hive）
  TransferStorage get transferStorage => _transferStorage;

  /// 获取单例实例
  static Future<BackgroundService> instance() async {
    if (_instance != null) {
      return _instance!;
    }
    final storage = await Sqlite3Storage.instance();
    _instance = BackgroundService._(storage);
    _instance!._transferStorage = await TransferStorage.instance();

    // 启动时重置卡在 UPLOADING/HASHING 状态的旧任务为 PENDING
    await _instance!._resetStuckTasks();

    _instance!._syncTimer = Timer.periodic(Duration(seconds: 3), (timer) async {
      await _instance!._timerHandler(timer);
    });

    return _instance!;
  }

  /// 重置卡在 UPLOADING/HASHING 状态的旧任务（app 崩溃/中断导致）
  Future<void> _resetStuckTasks() async {
    final stuckCount = await _transferStorage.resetStuckUploadTasks();
    if (stuckCount > 0) {
      _logger.info('Reset $stuckCount stuck UPLOADING tasks to PENDING');
    }
  }

  Future<void> _timerHandler(Timer timer) async {
    // 防止重入：上一次还没执行完就跳过
    if (_timerRunning) {
      _logger.debug('Timer: skipped (previous still running)');
      return;
    }
    _timerRunning = true;
    try {
      await _timerHandlerInner(timer);
    } finally {
      _timerRunning = false;
    }
  }

  Future<void> _timerHandlerInner(Timer timer) async {
    // 未登录时跳过调度（退出登录/Token 失效后 timer 保持运行但不调度）
    final ls = GetIt.I.get<LocalStorage>();
    if (!ls.isLoggedIn) {
      _logger.debug('Timer: not logged in, skipping');
      return;
    }

    // 一次查询获取所有计数（减少数据库锁竞争）
    final counts = await _transferStorage.countAllUploadTasks();
    final uploadingCount = counts['uploading'] ?? 0;
    final pendingCount = counts['pending'] ?? 0;
    final completedCount = counts['completed'] ?? 0;
    final failedCount = counts['failed'] ?? 0;

    // 更新计数缓存
    _pendingCount = pendingCount;
    _completedCount = completedCount;
    _failedCount = failedCount;

    _logger.info(
      'Timer: uploading=$uploadingCount, pending=$pendingCount, '
      'downloading=$downloadingCount, running=${_runningUploads.length}/${_runningDownloads.length}',
    );

    // 上传调度（直接在主 Isolate 运行，上传是 I/O 密集型不阻塞 UI）
    {
      final canScheduleUpload = _maxUploadConcurrency - _runningUploads.length;
      _logger.info(
        'Timer: canScheduleUpload=$canScheduleUpload (max=$_maxUploadConcurrency, running=${_runningUploads.length})',
      );

      if (canScheduleUpload > 0 && pendingCount > 0) {
        final pendingTasks = await _transferStorage.getUploadTasksByStatus(
          UploadStatus.PENDING,
          limit: canScheduleUpload,
        );

        _logger.info(
          'Timer: found ${pendingTasks.length} pending tasks to schedule',
        );

        for (final task in pendingTasks.take(canScheduleUpload)) {
          _logger.info('Timer: starting upload ${task.id} (${task.filename})');
          _runningUploads.add(task.id);
          _runUpload(task.id); // fire-and-forget, finally 块会清理
        }
      }

      // 预下载轮询：检查 Telegram 大文件缓存状态，完成后自动触发下载
      // 必须在下载调度之前执行，这样轮询改为 pending 的任务能被立即调度
      await _pollPredownloadTasks();

      // 下载调度
      final canScheduleDownload =
          _maxDownloadConcurrency - _runningDownloads.length;
      if (canScheduleDownload > 0) {
        final pendingDownloads = await _transferStorage
            .getDownloadTasksByStatus(
              DownloadStatus.pending,
              limit: canScheduleDownload,
            );
        if (pendingDownloads.isNotEmpty) {
          for (final task in pendingDownloads.take(canScheduleDownload)) {
            _logger.info('Timer: starting download ${task.id}');
            _runningDownloads.add(task.id);
            _runDownload(task.id); // fire-and-forget
          }
        }
      }
    }

    // 定期通知 UI 更新（使用分页加载的任务）
    final recentUploads = await _transferStorage.getAllUploadTasksAsync(
      limit: 100,
    );
    final recentDownloads = await _transferStorage.getAllDownloadTasksAsync(
      limit: 100,
    );
    _uploadTaskController.add(recentUploads);
    _downloadTaskController.add(recentDownloads);

    if (uploadingCount == 0 &&
        pendingCount == 0 &&
        downloadingCount == 0 &&
        _runningUploads.isEmpty &&
        _runningDownloads.isEmpty) {
      // 还有预下载任务时不停止 timer
      final predownloadCount = await _transferStorage
          .countDownloadTasksByStatus(DownloadStatus.predownload);
      if (predownloadCount > 0) {
        getLogger().debug(
          'timer: $predownloadCount predownload tasks, keeping timer',
        );
        return;
      }
      getLogger().debug('timer: no active tasks, stopping timer');
      timer.cancel();
      _syncTimer = null;
    }
  }

  /// 直接在主 Isolate 执行上传（I/O 密集型，不阻塞 UI）
  Future<void> _runUpload(String taskId) async {
    // 有任务开始，启动前台服务保活
    _updateForegroundNotification();
    try {
      final fmRepo = FmRepository();
      await uploadFile(
        taskId: taskId,
        storage: _transferStorage,
        fmRepo: fmRepo,
        chunkSize: GetIt.I.get<LocalStorage>().uploadChunkSize,
        smallFileThreshold: GetIt.I.get<LocalStorage>().smallFileThreshold,
      );

      // 上传完成后，回填 backup_file_records（仅相册备份任务有 tags）
      final task = await _transferStorage.getUploadTaskAsync(taskId);
      if (task != null && task.tags != null && task.tags!.isNotEmpty) {
        if (task.status == UploadStatus.COMPLETED) {
          await _syncBackupRecordOnComplete(task);
        } else if (task.status == UploadStatus.FAILED) {
          // 失败也回写，否则记录停在 pending 会被后续增量判定当成"需重传"，
          // 但实际上从未成功过 —— 两者语义不同，必须区分
          await _syncBackupRecordOnFailed(task);
        }
      }
    } catch (e, s) {
      _logger.error('Upload failed: $taskId', e, s);
      // 异常时确保状态更新为 FAILED，避免卡在 UPLOADING
      try {
        await _transferStorage.updateUploadTask(
          taskId,
          status: UploadStatus.FAILED,
          errorMessage: e.toString(),
        );
        // 同步更新 backup_file_records 状态
        final task = await _transferStorage.getUploadTaskAsync(taskId);
        if (task != null && task.tags != null && task.tags!.isNotEmpty) {
          await _syncBackupRecordOnFailed(task);
        }
      } catch (_) {}
    } finally {
      _runningUploads.remove(taskId);
      _updateForegroundNotification();
      await notifyUploadTasksChanged();
    }
  }

  /// 解析 tags 得到 (configId, relativePath)。
  ///
  /// 约定：`configId|relativePath` 表示非相册备份（如通话录音）；
  /// 不含 `|` 时按相册备份处理（历史行为：tags 直接是 asset id）。
  /// 之所以复用 tags 而不新增字段：上传任务表结构已固定，
  /// 且回写逻辑只需这两个值，避免改表与迁移。
  (String, String) _parseBackupTag(String tags) {
    final i = tags.indexOf('|');
    if (i < 0) return (Sqlite3Storage.albumBackupConfigId, tags);
    return (tags.substring(0, i), tags.substring(i + 1));
  }

  /// 上传成功后回填 backup_file_records
  Future<void> _syncBackupRecordOnComplete(UploadTaskRecord task) async {
    try {
      final (configId, relativePath) = _parseBackupTag(task.tags!);
      final record = await _storage.getBackupFileRecord(configId, relativePath);
      if (record != null) {
        await _storage.updateBackupFileRecord(record['id'] as String, {
          'file_hash': task.fileHash,
          'status': BackupFileStatus.completed.index,
          'uploaded_at': DateTime.now().toIso8601String(),
        });
        _logger.info(
          'Backup record updated to completed for asset: ${task.tags}',
        );
      }
    } catch (e) {
      _logger.warning('Failed to sync backup record on complete: $e');
    }
  }

  /// 上传失败后更新 backup_file_records 状态
  Future<void> _syncBackupRecordOnFailed(UploadTaskRecord task) async {
    try {
      final (configId, relativePath) = _parseBackupTag(task.tags!);
      final record = await _storage.getBackupFileRecord(configId, relativePath);
      if (record != null) {
        await _storage.updateBackupFileRecord(record['id'] as String, {
          'status': BackupFileStatus.failed.index,
          'message': task.errorMessage,
        });
        _logger.info('Backup record updated to failed for asset: ${task.tags}');
      }
    } catch (e) {
      _logger.warning('Failed to sync backup record on failure: $e');
    }
  }

  /// 直接在主 Isolate 执行下载
  Future<void> _runDownload(String taskId) async {
    _updateForegroundNotification();
    try {
      final ls = GetIt.I.get<LocalStorage>();
      await downloadFile(
        taskId: taskId,
        storage: _transferStorage,
        baseUrl: ls.downloadBaseUrl,
        token: ls.token!,
        // 阶段 2：直链需要走 REST 接口取链，取不到会自动回退代理下载（行为不变）
        repository: FmRepository(),
      );
    } catch (e, s) {
      _logger.error('Download failed: $taskId', e, s);
      // 异常时确保状态更新为 FAILED
      try {
        await _transferStorage.updateDownloadTask(
          taskId,
          status: DownloadStatus.failed,
          errorMessage: e.toString(),
        );
      } catch (_) {}
    } finally {
      _runningDownloads.remove(taskId);
      _updateForegroundNotification();
      await notifyDownloadTasksChanged();
    }
  }

  /// 轮询预下载任务的缓存状态
  ///
  /// 检查所有 predownload 状态的下载任务，查询后端缓存状态。
  /// 如果缓存完成（CACHED），自动将任务状态改为 pending，由 timer 调度下载。
  Future<void> _pollPredownloadTasks() async {
    final predownloadTasks = await _transferStorage.getDownloadTasksByStatus(
      DownloadStatus.predownload,
      limit: 10,
    );
    if (predownloadTasks.isEmpty) return;

    final fmRepo = FmRepository();

    for (final task in predownloadTasks) {
      if (task.fileHash == null || task.providerId == null) {
        _logger.warning(
          'Predownload task missing fileHash/providerId: ${task.id}',
        );
        continue;
      }

      try {
        final result = await fmRepo.getFileCacheStatus(
          fileHash: task.fileHash!,
          providerId: task.providerId!,
        );

        if (result.success && result.data != null) {
          final cacheStatus = result.data!.cacheStatus;
          _logger.info('Poll predownload ${task.id}: cacheStatus=$cacheStatus');

          if (cacheStatus == CacheStatus.CACHED) {
            // 缓存完成，自动触发下载
            _logger.info('Cache ready for ${task.id}, resuming download');
            await _transferStorage.updateDownloadTask(
              task.id,
              status: DownloadStatus.pending,
              errorMessage: null,
            );
          } else if (cacheStatus == CacheStatus.UNCACHED) {
            // 缓存失败，标记为失败
            _logger.warning('Cache failed for ${task.id}');
            await _transferStorage.updateDownloadTask(
              task.id,
              status: DownloadStatus.failed,
              errorMessage: '缓存失败，请重试',
            );
          }
          // CACHING 状态继续轮询
        }
      } catch (e) {
        _logger.error('Poll predownload failed for ${task.id}: $e');
      }
    }
  }

  /// 更新前台服务通知，无任务时停止服务
  void _updateForegroundNotification() {
    final total = _runningUploads.length + _runningDownloads.length;
    if (total > 0) {
      ForegroundServiceManager.start();
      ForegroundServiceManager.updateNotification(
        uploading: _runningUploads.length,
        downloading: _runningDownloads.length,
        completed: _completedCount,
        total: _pendingCount + _completedCount + _failedCount,
      );
    } else {
      ForegroundServiceManager.stop();
    }
  }

  /// 手动触发UI更新（用于任务完成时确保UI刷新）
  Future<void> notifyUploadTasksChanged() async {
    final uploadTasks = await _transferStorage.getAllUploadTasksAsync();
    _uploadTaskController.add(uploadTasks);
  }

  /// 手动触发下载UI更新
  Future<void> notifyDownloadTasksChanged() async {
    final downloadTasks = await _transferStorage.getAllDownloadTasksAsync();
    _downloadTaskController.add(downloadTasks);
  }

  /// 初始化后台服务
  static Future<void> initialize() async {
    if (kIsWeb) {
      getLogger().info('Web platform - background upload not supported');
      return;
    }
    getLogger().info('Background service initialized (timer-based)');
  }

  // 新增上传任务
  Future<void> scheduleNewUploadTask(UploadTaskRecord task) async {
    _logger.info('Adding new upload task: ${task.id}');
    // 保存新任务记录
    task.status = UploadStatus.PENDING;
    await _transferStorage.addUploadTask(task);
    await scheduleUpload(task.id, true);
  }

  /// 批量新增上传任务（真正的批量 INSERT，在事务中执行）
  /// 调用方应最后调用 scheduleFirstPendingUpload() 触发处理
  Future<void> addUploadTasksBatch(List<UploadTaskRecord> tasks) async {
    if (tasks.isEmpty) return;

    // 确保所有任务状态为 PENDING
    for (final task in tasks) {
      task.status = UploadStatus.PENDING;
    }

    // 在单个事务中批量插入，比逐条 INSERT 快几十倍
    await _transferStorage.addUploadTasksBatch(tasks);
    _logger.info('Batch added ${tasks.length} upload tasks to database');
  }

  /// 调度待处理的上传任务（用于批量添加后触发处理）
  Future<void> scheduleFirstPendingUpload() async {
    _logger.info(
      'scheduleFirstPendingUpload called, ensuring timer is running',
    );
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
    final taskIds = await _transferStorage.resumeUploadTasks(taskId);
    //重新调度
    for (final tid in taskIds) {
      await scheduleUpload(tid, true);
    }
  }

  /// 重命名并重试上传任务
  Future<void> renameAndRetryUpload(String taskId, String newFilename) async {
    _logger.info('Renaming and retrying upload task: $taskId -> $newFilename');

    // 获取任务
    final task = await _transferStorage.getUploadTaskAsync(taskId);
    if (task == null) {
      _logger.warning('Task not found: $taskId');
      return;
    }

    // 更新文件名并重置状态
    await _transferStorage.updateUploadTask(
      taskId,
      filename: newFilename,
      status: UploadStatus.PENDING,
      errorMessage: null,
      retryCount: 0,
    );

    // 重新调度上传
    await scheduleUpload(taskId, true);
    _logger.info('Renamed and rescheduled upload task: $taskId');
  }

  /// 调度上传任务
  Future<void> scheduleUpload(String taskId, bool isNew) async {
    _logger.info('scheduleUpload: $taskId');
    _ensureTimerRunning();
  }

  /// 新增下载任务
  Future<void> scheduleNewDownloadTask(DownloadTaskRecord task) async {
    _logger.info('Adding new download task: ${task.id}');
    task.status = DownloadStatus.pending;
    await _transferStorage.addDownloadTask(task);
    await scheduleDownload(task.id, true);
  }

  /// 恢复下载任务
  Future<void> resumeDownloadTasks(String? taskId) async {
    _logger.info('Resuming download tasks');
    final taskIds = await _transferStorage.resumeDownloadTasks(taskId);
    for (final tid in taskIds) {
      await scheduleDownload(tid, true);
    }
  }

  /// 调度下载任务
  Future<void> scheduleDownload(String taskId, bool isNew) async {
    _logger.info('scheduleDownload: $taskId');
    _ensureTimerRunning();
  }

  /// 取消上传任务
  Future<void> cancelUpload(String taskId) async {
    _logger.info('Cancelling upload task: $taskId');

    // 更新任务状态
    await _transferStorage.updateUploadTask(
      taskId,
      status: UploadStatus.CANCELED,
    );
  }

  /// 暂停上传任务
  Future<void> pauseUpload(String taskId) async {
    _logger.info('Pausing upload task: $taskId');
    await _transferStorage.updateUploadTask(
      taskId,
      status: UploadStatus.PAUSED,
    );
  }

  /// 恢复上传任务（只重置状态，由 _timerHandler 调度，不超出并发限制）
  Future<void> resumeUpload(String taskId) async {
    _logger.info('Resuming upload task: $taskId');
    await _transferStorage.resumeUploadTasks(taskId);
    // 确保 timer 运行，由 timer 调度 pending 任务
    _syncTimer ??= Timer.periodic(Duration(seconds: 3), (timer) async {
      await _timerHandler(timer);
    });
  }

  /// 清理已完成的任务
  Future<void> cleanup() async {
    await _transferStorage.clearUploadCompleted();
    await _transferStorage.clearDownloadCompleted();
  }

  /// 取消下载任务
  Future<void> cancelDownload(String taskId) async {
    _logger.info('Cancelling download task: $taskId');
    await _transferStorage.updateDownloadTask(
      taskId,
      status: DownloadStatus.canceled,
    );
  }

  /// 暂停下载任务
  Future<void> pauseDownload(String taskId) async {
    _logger.info('Pausing download task: $taskId');
    await _transferStorage.updateDownloadTask(
      taskId,
      status: DownloadStatus.paused,
    );
  }

  /// 恢复下载任务
  Future<void> resumeDownload(String taskId) async {
    _logger.info('Resuming download task: $taskId');
    final task = await _transferStorage.getDownloadTaskAsync(taskId);
    if (task == null) return;
    await _transferStorage.resumeDownloadTasks(taskId);
    await scheduleDownload(taskId, true);
  }

  /// 重试下载任务（只重置状态，由 _timerHandler 调度，不超出并发限制）
  Future<void> retryDownload(String taskId) async {
    _logger.info('Retrying download task: $taskId');
    await _transferStorage.updateDownloadTask(
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
