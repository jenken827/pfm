import 'dart:io';

import 'package:get_it/get_it.dart';
import 'package:path/path.dart' as p;
import 'package:pfm/models/models.dart';
import 'package:pfm/services/services.dart';
import 'package:pfm/proto/native/enum.pb.dart';
import 'package:uuid/uuid.dart';

/// 备份文件状态
enum BackupFileStatus {
  pending, // 等待上传
  uploading, // 上传中
  completed, // 已完成
  failed, // 失败
  skipped, // 已跳过（未修改）
  stale, // 上传后文件又变了
}

/// 客户端自动备份服务
///
/// 扫描本地目录 → 增量判断 → 创建 UploadTask → 走现有上传流程
class ClientBackupService {
  ClientBackupService._();
  static final ClientBackupService _instance = ClientBackupService._();
  static ClientBackupService get instance => _instance;

  static const _uuid = Uuid();

  /// 客户端备份配置的 Hive box key
  static const _configsKey = 'client_backup_configs';

  /// 获取所有客户端备份配置
  List<ClientBackupConfig> getConfigs() {
    final box = GetIt.I<LocalStorage>().settingsBox;
    final data = box.get(_configsKey);
    if (data == null) return [];
    if (data is List) {
      return data
          .map(
            (e) => ClientBackupConfig.fromJson(
              Map<String, dynamic>.from(e as Map),
            ),
          )
          .toList();
    }
    return [];
  }

  /// 保存所有配置
  Future<void> saveConfigs(List<ClientBackupConfig> configs) async {
    final box = GetIt.I<LocalStorage>().settingsBox;
    await box.put(_configsKey, configs.map((c) => c.toJson()).toList());
  }

  /// 添加配置
  Future<ClientBackupConfig> addConfig(ClientBackupConfig config) async {
    final configs = getConfigs();
    configs.add(config);
    await saveConfigs(configs);
    return config;
  }

  /// 更新配置
  Future<void> updateConfig(ClientBackupConfig config) async {
    final configs = getConfigs();
    final idx = configs.indexWhere((c) => c.id == config.id);
    if (idx >= 0) {
      configs[idx] = config;
      await saveConfigs(configs);
    }
  }

  /// 删除配置
  Future<void> deleteConfig(String configId) async {
    final configs = getConfigs();
    configs.removeWhere((c) => c.id == configId);
    await saveConfigs(configs);
    // 同时删除 SQLite 中的备份记录
    final storage = await Sqlite3Storage.instance();
    await storage.deleteBackupRecordsByConfig(configId);
  }

  /// 执行一次备份扫描并创建上传任务
  ///
  /// 返回 (扫描文件数, 需要备份文件数, 新建上传任务数)
  Future<(int, int, int)> executeBackup(String configId) async {
    final configs = getConfigs();
    final config = configs.firstWhere((c) => c.id == configId);
    if (!config.enabled) return (0, 0, 0);

    final storage = await Sqlite3Storage.instance();
    final transferStorage = await TransferStorage.instance();
    final l = getLogger();

    // 1. 扫描本地目录
    final files = await _scanDirectory(config);
    l.info(
      'ClientBackup: scanned ${files.length} files from ${config.sourcePath}',
    );

    // 2. 增量判断
    final toBackup = <_BackupCandidate>[];
    for (final file in files) {
      final record = await storage.getBackupFileRecord(
        configId,
        file.relativePath,
      );
      if (record == null) {
        // 新文件
        toBackup.add(file);
      } else {
        final status = record['status'] as int?;
        final modifiedAt = record['modified_at'] as int?;
        final fileSize = record['file_size'] as int?;
        if (status == BackupFileStatus.stale.index) {
          // STALE 状态需要重新备份
          toBackup.add(file);
        } else if (modifiedAt != null && file.modifiedAt != modifiedAt) {
          // 修改时间变化
          toBackup.add(file);
        } else if (fileSize != null && file.fileSize != fileSize) {
          // 文件大小变化
          toBackup.add(file);
        }
        // 否则：已备份且未修改，跳过
      }
    }

    l.info('ClientBackup: ${toBackup.length} files need backup');

    // 3. 为需要备份的文件创建 UploadTaskRecord
    int createdCount = 0;
    for (final candidate in toBackup) {
      try {
        // 检查文件是否还存在
        final file = File(candidate.absPath);
        if (!await file.exists()) continue;

        final taskId = _uuid.v4();
        final fileName = p.basename(candidate.absPath);

        // 创建 UploadTaskRecord，走现有上传流程
        final uploadTask = UploadTaskRecord(
          id: taskId,
          filepath: candidate.absPath,
          filename: fileName,
          fileSize: candidate.fileSize,
          parentId: config.targetDirId,
          status: UploadStatus.PENDING,
          overwrite: true,
        );

        await transferStorage.addUploadTask(uploadTask);

        // 创建/更新备份文件记录
        final existingRecord = await storage.getBackupFileRecord(
          configId,
          candidate.relativePath,
        );
        if (existingRecord != null) {
          await storage.updateBackupFileRecord(existingRecord['id'] as String, {
            'file_size': candidate.fileSize,
            'modified_at': candidate.modifiedAt,
            'status': BackupFileStatus.pending.index,
            'message': null,
          });
        } else {
          await storage.addBackupFileRecord({
            'id': _uuid.v4(),
            'config_id': configId,
            'relative_path': candidate.relativePath,
            'file_size': candidate.fileSize,
            'modified_at': candidate.modifiedAt,
            'status': BackupFileStatus.pending.index,
            'created_at': DateTime.now().toIso8601String(),
          });
        }

        // 通过 BackgroundService 调度上传
        final bgService = await BackgroundService.instance();
        bgService.scheduleNewUploadTask(uploadTask);
        createdCount++;
      } catch (e) {
        l.error(
          'ClientBackup: failed to create upload task for ${candidate.relativePath}: $e',
        );
      }
    }

    // 4. 更新配置的统计信息
    final updatedConfig = config.copyWith(
      lastRunAt: DateTime.now(),
      lastScanCount: files.length,
      lastBackupCount: toBackup.length,
      lastBackupSize: toBackup.fold<int>(0, (sum, f) => sum + f.fileSize),
    );
    await updateConfig(updatedConfig);

    return (files.length, toBackup.length, createdCount);
  }

  /// 扫描本地目录
  Future<List<_BackupCandidate>> _scanDirectory(
    ClientBackupConfig config,
  ) async {
    final dir = Directory(config.sourcePath);
    if (!await dir.exists()) return [];

    // 解析忽略规则
    List<String> ignorePatterns = [];
    if (config.ignoreRules != null && config.ignoreRules!.isNotEmpty) {
      ignorePatterns = config.ignoreRules!
          .split('\n')
          .where((l) => l.trim().isNotEmpty)
          .toList();
    }

    final results = <_BackupCandidate>[];

    await for (final entity in dir.list(recursive: config.recursive)) {
      if (entity is File) {
        final relativePath = p.relative(entity.path, from: config.sourcePath);

        // 检查忽略规则
        if (_matchesIgnoreRules(relativePath, ignorePatterns)) continue;

        try {
          final stat = await entity.stat();
          results.add(
            _BackupCandidate(
              relativePath: relativePath,
              absPath: entity.path,
              fileSize: stat.size,
              modifiedAt: stat.modified.millisecondsSinceEpoch ~/ 1000,
            ),
          );
        } catch (_) {
          // 文件无法访问，跳过
        }
      }
    }

    return results;
  }

  /// 简单的忽略规则匹配（支持 * 通配符）
  bool _matchesIgnoreRules(String relativePath, List<String> patterns) {
    for (final pattern in patterns) {
      final trimmed = pattern.trim();
      if (trimmed.isEmpty || trimmed.startsWith('#')) continue;

      // 简单的通配符匹配
      if (trimmed.contains('*')) {
        final regex = RegExp(
          trimmed.replaceAll('.', r'\.').replaceAll('*', '.*'),
        );
        if (regex.hasMatch(relativePath)) return true;
      } else {
        if (relativePath == trimmed || relativePath.endsWith('/$trimmed')) {
          return true;
        }
      }
    }
    return false;
  }
}

class _BackupCandidate {
  final String relativePath;
  final String absPath;
  final int fileSize;
  final int modifiedAt;

  _BackupCandidate({
    required this.relativePath,
    required this.absPath,
    required this.fileSize,
    required this.modifiedAt,
  });
}
