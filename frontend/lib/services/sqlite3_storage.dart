import 'dart:convert';
import 'dart:io';

import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';
import 'package:pfm/models/upload_task_record.dart';
import 'package:pfm/services/services.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

/// 存储服务 - 基于 SQLite 实现
///
/// 支持 WorkManager 后台隔离环境，使用 sqflite_common_ffi
/// 确保数据库操作在后台线程执行，不阻塞 UI
class Sqlite3Storage {
  static const String _dbName = 'pfm.db';
  static const int _dbVersion = 1;
  static const String _uploadTaskTableName = 'upload_tasks';
  static const String _downloadTaskTableName = 'download_tasks';

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
    await db.execute('''
      CREATE TABLE $_uploadTaskTableName (
        id TEXT PRIMARY KEY,
        file_path TEXT NOT NULL,
        file_name TEXT NOT NULL,
        file_size INTEGER NOT NULL,
        parent_id TEXT,
        file_hash TEXT,
        mime TEXT,
        file_type INTEGER,
        chunk_size INTEGER NOT NULL,
        chunk_total INTEGER NOT NULL,
        chunk_index INTEGER DEFAULT 0,
        uploaded_chunks TEXT DEFAULT '[]',
        status INTEGER DEFAULT 0,
        error_message TEXT,
        upload_id TEXT,
        file_id TEXT,
        created_at INTEGER NOT NULL,
        completed_at INTEGER,
        retry_count INTEGER DEFAULT 0,
        uploaded_bytes INTEGER DEFAULT 0,
        hash_progress REAL DEFAULT 0
      );
      CREATE TABLE $_downloadTaskTableName (
        id TEXT PRIMARY KEY,
        file_path TEXT NOT NULL,
        file_name TEXT NOT NULL,
        file_size INTEGER NOT NULL,       
        file_hash TEXT,       
        status INTEGER DEFAULT 0,
        error_message TEXT,  
        file_id TEXT,
        created_at INTEGER NOT NULL,
        completed_at INTEGER,
        retry_count INTEGER DEFAULT 0
      );

    ''');

    // 创建索引
    await db.execute(
      'CREATE INDEX idx_upload_tasks_status ON $_uploadTaskTableName (status)',
    );
    await db.execute(
      'CREATE INDEX idx_upload_tasks_created_at ON $_uploadTaskTableName (created_at)',
    );
    await db.execute(
      'CREATE INDEX idx_download_tasks_status ON $_downloadTaskTableName (status)',
    );
    await db.execute(
      'CREATE INDEX idx_download_tasks_created_at ON $_downloadTaskTableName (created_at)',
    );

    getLogger().info('Sqlite3Storage tables created');
  }

  /// 升级表
  Future<void> _onUpgrade(Database db, int oldVersion, int newVersion) async {
    getLogger().info(
      'Sqlite3Storage upgrading from $oldVersion to $newVersion',
    );
    // 未来版本升级逻辑
  }

  // /// 获取数据库连接
  // Future<Database> _getDb() async {
  //   if (_database != null && _database!.isOpen) {
  //     return _database!;
  //   }

  //   final dbPath = await _getDatabasePath();
  //   _database = await databaseFactoryFfi.openDatabase(
  //     dbPath,
  //     options: OpenDatabaseOptions(
  //       version: _dbVersion,
  //       onCreate: _onCreate,
  //       onUpgrade: _onUpgrade,
  //     ),
  //   );
  //   // 启用 WAL 模式
  //   await _database!.execute('PRAGMA journal_mode = WAL');
  //   await _database!.execute('PRAGMA busy_timeout = 30000');

  //   return _database!;
  // }

  /// 将记录转换为 Map
  Map<String, dynamic> _recordToMap(UploadTaskRecord record) {
    return {
      'id': record.id,
      'file_path': record.filePath,
      'file_name': record.fileName,
      'file_size': record.fileSize,
      'parent_id': record.parentId,
      'file_hash': record.fileHash,
      'mime': record.mime,
      'file_type': record.fileType,
      'chunk_size': record.chunkSize,
      'chunk_total': record.chunkTotal,
      'chunk_index': record.chunkIndex,
      'uploaded_chunks': jsonEncode(record.uploadedChunks),
      'status': record.status.index,
      'error_message': record.errorMessage,
      'upload_id': record.uploadId,
      'file_id': record.fileId,
      'created_at': record.createdAt.millisecondsSinceEpoch,
      'completed_at': record.completedAt?.millisecondsSinceEpoch,
      'retry_count': record.retryCount,
      'uploaded_bytes': record.uploadedBytes,
      'hash_progress': record.hashProgress,
    };
  }

  /// 将 Map 转换为记录
  UploadTaskRecord _mapToRecord(Map<String, dynamic> map) {
    return UploadTaskRecord(
      id: map['id'] as String,
      filePath: map['file_path'] as String,
      fileName: map['file_name'] as String,
      fileSize: map['file_size'] as int,
      parentId: map['parent_id'] as String?,
      fileHash: map['file_hash'] as String?,
      mime: map['mime'] as String?,
      fileType: map['file_type'] as int?,
      chunkSize: map['chunk_size'] as int,
      chunkTotal: map['chunk_total'] as int,
      chunkIndex: map['chunk_index'] as int? ?? 0,
      uploadedChunks: _parseUploadedChunks(map['uploaded_chunks'] as String?),
      status: UploadTaskStatusRecord.values[map['status'] as int? ?? 0],
      errorMessage: map['error_message'] as String?,
      uploadId: map['upload_id'] as String?,
      fileId: map['file_id'] as String?,
      createdAt: DateTime.fromMillisecondsSinceEpoch(
        map['created_at'] as int? ?? DateTime.now().millisecondsSinceEpoch,
      ),
      completedAt: map['completed_at'] != null
          ? DateTime.fromMillisecondsSinceEpoch(map['completed_at'] as int)
          : null,
      retryCount: map['retry_count'] as int? ?? 0,
      uploadedBytes: map['uploaded_bytes'] as int? ?? 0,
      hashProgress: map['hash_progress'] as double? ?? 0,
    );
  }

  /// 解析已上传分片列表
  List<int> _parseUploadedChunks(String? json) {
    if (json == null || json.isEmpty) return [];
    try {
      final list = jsonDecode(json) as List;
      return list.cast<int>();
    } catch (e) {
      return [];
    }
  }

  /// 获取所有任务
  List<UploadTaskRecord> getAllTasks() {
    // 同步方法：从缓存获取
    if (_database == null) return [];
    // 这里需要异步查询，返回空列表
    // 实际使用时应该调用 getAllTasksAsync
    return [];
  }

  /// 获取所有任务（异步）
  Future<List<UploadTaskRecord>> getAllUploadTasksAsync() async {
    final List<Map<String, dynamic>> maps = await _database!.query(
      _uploadTaskTableName,
      orderBy: 'created_at DESC',
    );
    return maps.map(_mapToRecord).toList();
  }

  /// 获取待处理的任务
  List<UploadTaskRecord> getPendingUploadTasks() {
    // 同步方法：从缓存获取
    return [];
  }

  /// 获取待处理的任务（异步）
  Future<List<UploadTaskRecord>> getPendingUploadTasksAsync() async {
    final List<Map<String, dynamic>> maps = await _database!.query(
      _uploadTaskTableName,
      where: 'status IN (?, ?, ?, ?)',
      whereArgs: [
        UploadTaskStatusRecord.pending.index,
        UploadTaskStatusRecord.uploading.index,
        UploadTaskStatusRecord.paused.index,
        UploadTaskStatusRecord.initializing.index,
      ],
      orderBy: 'created_at ASC',
    );
    return maps.map(_mapToRecord).toList();
  }

  /// 获取任务
  UploadTaskRecord? getUploadTask(String id) {
    // 同步方法：从缓存获取
    return null;
  }

  /// 获取任务（异步）
  Future<UploadTaskRecord?> getUploadTaskAsync(String id) async {
    final List<Map<String, dynamic>> maps = await _database!.query(
      _uploadTaskTableName,
      where: 'id = ?',
      whereArgs: [id],
      limit: 1,
    );
    if (maps.isEmpty) return null;
    return _mapToRecord(maps.first);
  }

  /// 保存任务
  Future<void> saveUploadTask(UploadTaskRecord task) async {
    await _withRetry(
      () => _database!.insert(
        _uploadTaskTableName,
        _recordToMap(task),
        conflictAlgorithm: ConflictAlgorithm.replace,
      ),
    );
    getLogger().debug('Saved task: ${task.id}, status: ${task.status}');
  }

  /// 更新任务字段
  Future<void> updateUploadTask(
    String id, {
    UploadTaskStatusRecord? status,
    int? chunkIndex,
    List<int>? uploadedChunks,
    String? errorMessage,
    String? uploadId,
    String? fileId,
    int? retryCount,
    int? uploadedBytes,
    String? fileHash,
    double? hashProgress,
  }) async {
    final values = <String, dynamic>{};

    if (status != null) {
      values['status'] = status.index;
      if (status == UploadTaskStatusRecord.completed) {
        values['completed_at'] = DateTime.now().millisecondsSinceEpoch;
      }
    }
    if (chunkIndex != null) values['chunk_index'] = chunkIndex;
    if (uploadedChunks != null) {
      values['uploaded_chunks'] = jsonEncode(uploadedChunks);
    }
    if (errorMessage != null) values['error_message'] = errorMessage;
    if (uploadId != null) values['upload_id'] = uploadId;
    if (fileId != null) values['file_id'] = fileId;
    if (retryCount != null) values['retry_count'] = retryCount;
    if (uploadedBytes != null) values['uploaded_bytes'] = uploadedBytes;
    if (fileHash != null) values['file_hash'] = fileHash;
    if (hashProgress != null) values['hash_progress'] = hashProgress;

    if (values.isEmpty) return;

    await _withRetry(
      () => _database!.update(
        _uploadTaskTableName,
        values,
        where: 'id = ?',
        whereArgs: [id],
      ),
    );
    getLogger().debug('Updated task: $id');
  }

  /// 删除任务
  Future<void> deleteTask(String id) async {
    await _withRetry(
      () => _database!.delete(
        _uploadTaskTableName,
        where: 'id = ?',
        whereArgs: [id],
      ),
    );
    getLogger().debug('Deleted task: $id');
  }

  /// 清除已完成的任务
  Future<void> clearCompleted() async {
    final count = await _withRetry(
      () => _database!.delete(
        _uploadTaskTableName,
        where: 'status = ?',
        whereArgs: [UploadTaskStatusRecord.completed.index],
      ),
    );
    getLogger().info('Cleared $count completed tasks');
  }

  /// 清除失败的任务
  Future<void> clearFailed() async {
    final count = await _withRetry(
      () => _database!.delete(
        _uploadTaskTableName,
        where: 'status = ?',
        whereArgs: [UploadTaskStatusRecord.failed.index],
      ),
    );
    getLogger().info('Cleared $count failed tasks');
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
