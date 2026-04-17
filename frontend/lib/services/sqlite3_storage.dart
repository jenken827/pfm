import 'dart:io';

import 'package:fixnum/fixnum.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/proto/native/enum.pb.dart';
import 'package:pfm/services/services.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

/// 存储服务 - 基于 SQLite 实现
///
/// 支持 WorkManager 后台隔离环境，使用 sqflite_common_ffi
/// 确保数据库操作在后台线程执行，不阻塞 UI
class Sqlite3Storage {
  static const String _dbName = 'pfm.db';
  static const int _dbVersion = 1;
  static const String uploadTaskTableName = 'upload_tasks';
  static const String downloadTaskTableName = 'download_tasks';

  static Sqlite3Storage? _instance;
  static String? _dbPath;
  static Database? _database;

  /// 数据库操作重试配置
  static const int _maxRetries = 3;
  static const Duration _retryDelay = Duration(milliseconds: 100);

  /// 带重试的数据库操作
  static Future<T> _withRetry<T>(Future<T> Function() operation) async {
    int attempts = 0;
    while (true) {
      try {
        return await operation();
      } catch (e) {
        if (e.toString().contains('database is locked') &&
            attempts < _maxRetries) {
          attempts++;
          getLogger().debug(
            'Database locked, retrying ($attempts/$_maxRetries)...',
          );
          await Future.delayed(_retryDelay * attempts);
          continue;
        }
        rethrow;
      }
    }
  }

  Sqlite3Storage._();

  /// 获取单例实例
  static Future<Sqlite3Storage> instance() async {
    if (_instance == null) {
      _instance = Sqlite3Storage._();
      await _instance!._init();
    }
    return _instance!;
  }

  /// 获取数据库路径（支持后台隔离环境）
  static Future<String> _getDatabasePath() async {
    if (_dbPath != null) return _dbPath!;

    final dbDir = await getApplicationSupportDirectory();
    getLogger().info('sqlite3_storage: dbDir: ${dbDir.path}');
    // 默认使用应用文档目录
    final dbPath = p.join(dbDir.path, '.pfm', 'data');
    if (!await Directory(dbPath).exists()) {
      await Directory(dbPath).create(recursive: true);
    }
    _dbPath = p.join(dbPath, _dbName);
    return _dbPath!;
  }

  /// 获取当前数据库路径
  ///
  /// 用于传递给 Isolate，确保使用相同的数据库
  Future<String> getDatabasePath() async {
    return await _getDatabasePath();
  }

  /// 设置数据库路径（用于后台隔离环境初始化）
  ///
  /// 接受完整的数据库文件路径，而不是目录路径
  static void setDatabasePath(String fullPath) {
    _dbPath = fullPath;
  }

  /// 初始化数据库
  Future<void> _init() async {
    // 初始化 sqflite_ffi
    sqfliteFfiInit();
    final dbPath = await _getDatabasePath();
    _database = await databaseFactoryFfi.openDatabase(
      dbPath,
      options: OpenDatabaseOptions(
        version: _dbVersion,
        onCreate: _onCreate,
        onUpgrade: _onUpgrade,
      ),
    );
    // 启用 WAL 模式
    await _database!.execute('PRAGMA journal_mode = WAL');
    await _database!.execute('PRAGMA busy_timeout = 30000');

    getLogger().info('UploadTaskStorage initialized with WAL mode');
  }

  /// 创建表
  Future<void> _onCreate(Database db, int version) async {
    await db.execute(UploadTaskRecord.createSql());
    await db.execute(DownloadTaskRecord.createSql());

    getLogger().info('Sqlite3Storage tables created');
  }

  /// 升级表
  Future<void> _onUpgrade(Database db, int oldVersion, int newVersion) async {
    getLogger().info(
      'Sqlite3Storage upgrading from $oldVersion to $newVersion',
    );
    // 未来版本升级逻辑
  }

  /// 获取所有任务（异步）
  Future<List<UploadTaskRecord>> getAllUploadTasksAsync() async {
    final List<Map<String, dynamic>> maps = await _database!.query(
      uploadTaskTableName,
      orderBy: 'created_at DESC',
    );
    return maps.map((map) => UploadTaskRecord.fromJson(map)).toList();
  }

  /// 获取待处理的上传任务（异步）
  Future<List<UploadTaskRecord>> getPendingUploadTasksAsync() async {
    final List<Map<String, dynamic>> maps = await _database!.query(
      uploadTaskTableName,
      where: 'status = ?',
      whereArgs: [UploadStatus.PENDING.value],
      orderBy: 'created_at ASC',
    );
    return maps.map((map) => UploadTaskRecord.fromJson(map)).toList();
  }

  /// 获取暂停的上传任务
  Future<List<UploadTaskRecord>> getPausedUploadTasksAsync() async {
    final List<Map<String, dynamic>> maps = await _database!.query(
      uploadTaskTableName,
      where: 'status = ?',
      whereArgs: [UploadStatus.PAUSED.value],
      orderBy: 'created_at ASC',
    );
    return maps.map((map) => UploadTaskRecord.fromJson(map)).toList();
  }

  /// 获取下一个待处理的上传任务（异步）
  Future<UploadTaskRecord?> getNextPendingUploadTaskAsync() async {
    final List<Map<String, dynamic>> maps = await _database!.query(
      uploadTaskTableName,
      where: 'status = ?',
      whereArgs: [UploadStatus.PENDING.value],
      orderBy: 'created_at ASC',
      limit: 1,
    );
    if (maps.isEmpty) return null;
    return UploadTaskRecord.fromJson(maps.first);
  }

  /// 获取指定任务
  Future<UploadTaskRecord?> getUploadTaskAsync(String id) async {
    final List<Map<String, dynamic>> maps = await _database!.query(
      uploadTaskTableName,
      where: 'id = ?',
      whereArgs: [id],
      limit: 1,
    );
    if (maps.isEmpty) return null;
    return UploadTaskRecord.fromJson(maps.first);
  }

  /// 新增上传任务
  Future<void> addUploadTask(UploadTaskRecord task) async {
    getLogger().info('add new task  $task');
    final mp = task.toJson();
    getLogger().info('add new task json $mp');
    await _withRetry(
      () => _database!.insert(
        uploadTaskTableName,
        task.toJson(),
        conflictAlgorithm: ConflictAlgorithm.replace,
      ),
    );
    getLogger().debug('Saved task: ${task.id}, status: ${task.status}');
  }

  /// 更新上传任务
  Future<void> updateUploadTask(
    String id, {
    UploadStatus? status,
    String? errorMessage,
    int? chunkTotal,
    Int64? chunkSize,
    String? uploadId,
    int? retryCount,
    int? uploadedBytes,
    String? fileHash,
    int? speed,
  }) async {
    final values = <String, dynamic>{};

    if (status != null) {
      values['status'] = status.value;
      if (status == UploadStatus.COMPLETED) {
        values['completed_at'] = DateTime.now().toIso8601String();
      }
    }
    if (errorMessage != null) values['error_message'] = errorMessage;
    if (uploadId != null) values['upload_id'] = uploadId;
    if (retryCount != null) values['retry_count'] = retryCount;
    if (uploadedBytes != null) values['uploaded_bytes'] = uploadedBytes;
    if (fileHash != null) values['file_hash'] = fileHash;
    if (speed != null) values['speed'] = speed;
    if (chunkTotal != null) values['chunk_total'] = chunkTotal;
    if (chunkSize != null) values['chunk_size'] = chunkSize.toInt();

    if (values.isEmpty) return;

    await _withRetry(
      () => _database!.update(
        uploadTaskTableName,
        values,
        where: 'id = ?',
        whereArgs: [id],
      ),
    );
  }

  /// 删除任务
  Future<void> deleteUploadTask(String? id) async {
    final where = id == null ? '' : 'id=?';
    final whereArgs = id == null ? [] : [id];
    await _withRetry(
      () => _database!.delete(
        uploadTaskTableName,
        where: where,
        whereArgs: whereArgs,
      ),
    );
    getLogger().debug('Deleted task: $id');
  }

  /// 清除已完成的任务
  Future<void> clearUploadCompleted() async {
    final count = await _withRetry(
      () => _database!.delete(
        uploadTaskTableName,
        where: 'status = ?',
        whereArgs: [UploadStatus.COMPLETED.value],
      ),
    );
    getLogger().info('Cleared $count completed tasks');
  }

  /// 清除失败的任务
  Future<void> clearUploadFailed() async {
    final count = await _withRetry(
      () => _database!.delete(
        uploadTaskTableName,
        where: 'status = ?',
        whereArgs: [UploadStatus.FAILED.value],
      ),
    );
    getLogger().info('Cleared $count failed tasks');
  }

  /// 暂停任务
  Future<void> pauseUploadTasks(String? taskId) async {
    final where = taskId == null ? 'status in (?, ?)' : 'id=?';
    final whereArgs = taskId == null
        ? [
            UploadStatus.UPLOADING.value,
            UploadStatus.PENDING.value,
            UploadStatus.HASHING.value,
          ]
        : [taskId];
    await _withRetry(
      () => _database!.update(
        uploadTaskTableName,
        {'status': UploadStatus.PAUSED.value},
        where: where,
        whereArgs: whereArgs,
      ),
    );
  }

  // 恢复任务
  Future<List<String>> resumeUploadTasks(String? taskId) async {
    List<String> taskIds = [];
    String where;
    List<dynamic> whereArgs;
    if (taskId == null) {
      final tasks = await getPausedUploadTasksAsync();
      taskIds = tasks.map((t) => t.id).toList();
      where = 'status=?';
      whereArgs = [UploadStatus.PAUSED.value];
    } else {
      taskIds.add(taskId);
      where = 'id = ?';
      whereArgs = [taskId];
    }

    await _withRetry(
      () => _database!.update(
        uploadTaskTableName,
        {'status': UploadStatus.PENDING.value},
        where: where,
        whereArgs: whereArgs,
      ),
    );

    return taskIds;
  }

  Future<void> cancelUploadTask(String? taskId) async {
    final where = taskId == null ? 'status in (?, ?)' : 'id=?';
    final whereArgs = taskId == null
        ? [
            UploadStatus.UPLOADING.value,
            UploadStatus.PENDING.value,
            UploadStatus.HASHING.value,
          ]
        : [taskId];
    await _withRetry(
      () => _database!.update(
        uploadTaskTableName,
        {'status': UploadStatus.CANCELED.value},
        where: where,
        whereArgs: whereArgs,
      ),
    );
  }

  // ========== 下载任务 CRUD ==========

  /// 获取所有下载任务
  Future<List<DownloadTaskRecord>> getAllDownloadTasksAsync() async {
    final List<Map<String, dynamic>> maps = await _database!.query(
      downloadTaskTableName,
      orderBy: 'created_at DESC',
    );
    return maps.map((map) => DownloadTaskRecord.fromJson(map)).toList();
  }

  /// 获取指定下载任务
  Future<DownloadTaskRecord?> getDownloadTaskAsync(String id) async {
    final List<Map<String, dynamic>> maps = await _database!.query(
      downloadTaskTableName,
      where: 'id = ?',
      whereArgs: [id],
      limit: 1,
    );
    if (maps.isEmpty) return null;
    return DownloadTaskRecord.fromJson(maps.first);
  }

  /// 根据 fileId 获取未完成的下载任务
  ///
  /// 用于断点续传：如果存在未完成的下载任务，返回该任务记录
  Future<DownloadTaskRecord?> getUncompletedDownloadTaskByFileId(
    String fileId,
  ) async {
    final List<Map<String, dynamic>> maps = await _database!.query(
      downloadTaskTableName,
      where: 'file_id = ? AND status IN (?, ?, ?, ?)',
      whereArgs: [
        fileId,
        DownloadStatus.pending.index,
        DownloadStatus.downloading.index,
        DownloadStatus.paused.index,
        DownloadStatus.predownload.index,
      ],
      limit: 1,
      orderBy: 'created_at DESC',
    );
    if (maps.isEmpty) return null;
    return DownloadTaskRecord.fromJson(maps.first);
  }

  /// 新增下载任务
  Future<void> addDownloadTask(DownloadTaskRecord task) async {
    getLogger().info('add download task: ${task.id}');
    await _withRetry(
      () => _database!.insert(
        downloadTaskTableName,
        task.toJson(),
        conflictAlgorithm: ConflictAlgorithm.replace,
      ),
    );
  }

  /// 更新下载任务
  Future<void> updateDownloadTask(
    String id, {
    DownloadStatus? status,
    String? errorMessage,
    int? downloadedBytes,
    int? speed,
    int? chunkTotal,
  }) async {
    final values = <String, dynamic>{};

    if (status != null) {
      values['status'] = status.index;
      if (status == DownloadStatus.completed) {
        values['completed_at'] = DateTime.now().toIso8601String();
      }
    }
    if (errorMessage != null) values['error_message'] = errorMessage;
    if (downloadedBytes != null) values['downloaded_bytes'] = downloadedBytes;
    if (speed != null) values['speed'] = speed;
    if (chunkTotal != null) values['chunk_total'] = chunkTotal;

    if (values.isEmpty) return;

    await _withRetry(
      () => _database!.update(
        downloadTaskTableName,
        values,
        where: 'id = ?',
        whereArgs: [id],
      ),
    );
  }

  /// 删除下载任务
  Future<void> deleteDownloadTask(String? id) async {
    final where = id == null ? '' : 'id=?';
    final whereArgs = id == null ? [] : [id];
    await _withRetry(
      () => _database!.delete(
        downloadTaskTableName,
        where: where,
        whereArgs: whereArgs,
      ),
    );
    getLogger().debug('Deleted download task: $id');
  }

  /// 获取待处理的下载任务
  Future<List<DownloadTaskRecord>> getPendingDownloadTasksAsync() async {
    final List<Map<String, dynamic>> maps = await _database!.query(
      downloadTaskTableName,
      where: 'status = ?',
      whereArgs: [DownloadStatus.pending.index],
      orderBy: 'created_at ASC',
    );
    return maps.map((map) => DownloadTaskRecord.fromJson(map)).toList();
  }

  /// 获取暂停的下载任务
  Future<List<DownloadTaskRecord>> getPausedDownloadTasksAsync() async {
    final List<Map<String, dynamic>> maps = await _database!.query(
      downloadTaskTableName,
      where: 'status = ?',
      whereArgs: [DownloadStatus.paused.index],
      orderBy: 'created_at ASC',
    );
    return maps.map((map) => DownloadTaskRecord.fromJson(map)).toList();
  }

  /// 获取下一个待处理的下载任务
  Future<DownloadTaskRecord?> getNextPendingDownloadTaskAsync() async {
    final List<Map<String, dynamic>> maps = await _database!.query(
      downloadTaskTableName,
      where: 'status = ?',
      whereArgs: [DownloadStatus.pending.index],
      orderBy: 'created_at ASC',
      limit: 1,
    );
    if (maps.isEmpty) return null;
    return DownloadTaskRecord.fromJson(maps.first);
  }

  /// 清除已完成的下载任务
  Future<void> clearDownloadCompleted() async {
    final count = await _withRetry(
      () => _database!.delete(
        downloadTaskTableName,
        where: 'status = ?',
        whereArgs: [DownloadStatus.completed.index],
      ),
    );
    getLogger().info('Cleared $count completed download tasks');
  }

  /// 暂停下载任务
  Future<void> pauseDownloadTasks(String? taskId) async {
    final where = taskId == null ? 'status IN (?, ?)' : 'id=?';
    final whereArgs = taskId == null
        ? [DownloadStatus.downloading.index, DownloadStatus.pending.index]
        : [taskId];
    await _withRetry(
      () => _database!.update(
        downloadTaskTableName,
        {'status': DownloadStatus.paused.index},
        where: where,
        whereArgs: whereArgs,
      ),
    );
  }

  /// 恢复下载任务
  Future<List<String>> resumeDownloadTasks(String? taskId) async {
    List<String> taskIds = [];
    String where;
    List<dynamic> whereArgs;
    if (taskId == null) {
      final tasks = await getPausedDownloadTasksAsync();
      taskIds = tasks.map((t) => t.id).toList();
      where = 'status=?';
      whereArgs = [DownloadStatus.paused.index];
    } else {
      taskIds.add(taskId);
      where = 'id=?';
      whereArgs = [taskId];
    }

    await _withRetry(
      () => _database!.update(
        downloadTaskTableName,
        {'status': DownloadStatus.pending.index},
        where: where,
        whereArgs: whereArgs,
      ),
    );

    return taskIds;
  }

  /// 取消下载任务
  Future<void> cancelDownloadTask(String? taskId) async {
    final where = taskId == null ? 'status IN (?, ?)' : 'id=?';
    final whereArgs = taskId == null
        ? [DownloadStatus.downloading.index, DownloadStatus.pending.index]
        : [taskId];
    await _withRetry(
      () => _database!.update(
        downloadTaskTableName,
        {'status': DownloadStatus.canceled.index},
        where: where,
        whereArgs: whereArgs,
      ),
    );
  }

  /// 关闭数据库连接
  Future<void> close() async {
    await _database?.close();
    _database = null;
  }

  /// 在后台隔离环境中初始化存储
  ///
  /// 用于 WorkManager 回调中初始化，确保数据库路径正确
  static Future<Sqlite3Storage> initForIsolate(String basePath) async {
    setDatabasePath(basePath);

    if (_instance == null) {
      _instance = Sqlite3Storage._();
      await _instance!._init();
    }
    return _instance!;
  }
}
