import 'dart:async';
import 'dart:io';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:get_it/get_it.dart';
import 'package:path/path.dart' as p;
import 'package:pfm/api/api.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/proto/native/enum.pb.dart' hide BackupFileStatus;
import 'package:pfm/services/services.dart';
import 'package:photo_manager/photo_manager.dart';
import 'package:talker_flutter/talker_flutter.dart';
import 'package:uuid/uuid.dart';

/// 相册自动备份配置
class AlbumBackupConfig {
  final bool enabled; // 是否开启自动备份
  final bool wifiOnly; // 仅 WiFi 备份
  final List<String> backupAlbumIds; // 备份的相册 ID 列表
  final bool backupVideos; // 是否备份视频
  final String targetParentId; // 服务器目标文件夹 ID
  final String targetFolderName; // 目标文件夹名称（显示用）

  const AlbumBackupConfig({
    this.enabled = false,
    this.wifiOnly = true,
    this.backupAlbumIds = const [],
    this.backupVideos = false,
    this.targetParentId = '',
    this.targetFolderName = '',
  });

  Map<String, dynamic> toJson() => {
    'enabled': enabled,
    'wifiOnly': wifiOnly,
    'backupAlbumIds': backupAlbumIds,
    'backupVideos': backupVideos,
    'targetParentId': targetParentId,
    'targetFolderName': targetFolderName,
  };

  factory AlbumBackupConfig.fromJson(Map<String, dynamic> json) {
    return AlbumBackupConfig(
      enabled: json['enabled'] ?? false,
      wifiOnly: json['wifiOnly'] ?? true,
      backupAlbumIds: List<String>.from(json['backupAlbumIds'] ?? []),
      backupVideos: json['backupVideos'] ?? false,
      targetParentId: json['targetParentId'] ?? '',
      targetFolderName: json['targetFolderName'] ?? '',
    );
  }

  AlbumBackupConfig copyWith({
    bool? enabled,
    bool? wifiOnly,
    List<String>? backupAlbumIds,
    bool? backupVideos,
    String? targetParentId,
    String? targetFolderName,
  }) {
    return AlbumBackupConfig(
      enabled: enabled ?? this.enabled,
      wifiOnly: wifiOnly ?? this.wifiOnly,
      backupAlbumIds: backupAlbumIds ?? this.backupAlbumIds,
      backupVideos: backupVideos ?? this.backupVideos,
      targetParentId: targetParentId ?? this.targetParentId,
      targetFolderName: targetFolderName ?? this.targetFolderName,
    );
  }
}

/// 相册备份状态
enum AlbumBackupStatus {
  idle, // 空闲
  scanning, // 扫描相册中
  uploading, // 上传中
  completed, // 完成
  error, // 错误
  noPermission, // 无权限
  noNetwork, // 无网络
  waitingForWifi, // 等待 WiFi
}

/// 相册备份结果
class AlbumBackupResult {
  final AlbumBackupStatus status;
  final int totalPhotos; // 总照片数
  final int newPhotos; // 新照片数
  final int uploadedPhotos; // 已上传数
  final int failedPhotos; // 失败数
  final String? errorMessage;
  final DateTime? lastBackupTime;

  const AlbumBackupResult({
    this.status = AlbumBackupStatus.idle,
    this.totalPhotos = 0,
    this.newPhotos = 0,
    this.uploadedPhotos = 0,
    this.failedPhotos = 0,
    this.errorMessage,
    this.lastBackupTime,
  });
}

/// 相册自动备份服务
///
/// 功能：
/// - 扫描设备相册，获取所有照片/视频
/// - 对比本地记录，找出新增文件
/// - 根据网络状态决定是否上传
/// - 记录已备份文件，支持增量同步
class AlbumBackupService {
  static AlbumBackupService? _instance;

  final Sqlite3Storage _storage;
  final Connectivity _connectivity = Connectivity();
  late final Talker _logger;

  // 备份配置
  AlbumBackupConfig _config = const AlbumBackupConfig();

  // 备份状态
  AlbumBackupStatus _status = AlbumBackupStatus.idle;
  AlbumBackupResult _result = const AlbumBackupResult();

  // 状态控制器
  final _statusController = StreamController<AlbumBackupResult>.broadcast();
  Stream<AlbumBackupResult> get statusStream => _statusController.stream;

  // 是否正在备份
  bool _isBackingUp = false;

  AlbumBackupService._(this._storage) : _logger = getLogger();

  /// 获取单例实例
  static Future<AlbumBackupService> instance() async {
    if (_instance != null) return _instance!;
    final storage = await Sqlite3Storage.instance();
    _instance = AlbumBackupService._(storage);
    await _instance!._loadConfig();
    return _instance!;
  }

  /// 加载备份配置
  Future<void> _loadConfig() async {
    final ls = GetIt.I.get<LocalStorage>();
    final configJson = ls.settingsBox.get('albumBackupConfig');
    if (configJson != null) {
      _config = AlbumBackupConfig.fromJson(
        Map<String, dynamic>.from(configJson),
      );
    }
  }

  /// 保存备份配置
  Future<void> saveConfig(AlbumBackupConfig config) async {
    _config = config;
    final ls = GetIt.I.get<LocalStorage>();
    await ls.settingsBox.put('albumBackupConfig', config.toJson());
  }

  /// 获取当前配置
  AlbumBackupConfig get config => _config;

  /// 获取当前状态
  AlbumBackupStatus get status => _status;

  /// 获取当前结果
  AlbumBackupResult get result => _result;

  /// 检查并请求相册权限
  Future<bool> requestPermission() async {
    final permission = await PhotoManager.requestPermissionExtend();
    if (permission.isAuth) {
      return true;
    }

    _updateStatus(
      status: AlbumBackupStatus.noPermission,
      errorMessage: '需要相册访问权限才能备份照片',
    );
    return false;
  }

  /// 获取所有相册列表
  Future<List<AssetPathEntity>> getAlbums() async {
    final hasPermission = await requestPermission();
    if (!hasPermission) return [];

    return await PhotoManager.getAssetPathList(
      type: RequestType.image,
      hasAll: true,
    );
  }

  /// 检查网络状态
  Future<bool> _checkNetwork() async {
    final connectivityResult = await _connectivity.checkConnectivity();

    if (connectivityResult.contains(ConnectivityResult.none)) {
      _updateStatus(status: AlbumBackupStatus.noNetwork, errorMessage: '无网络连接');
      return false;
    }

    if (_config.wifiOnly &&
        !connectivityResult.contains(ConnectivityResult.wifi)) {
      _updateStatus(
        status: AlbumBackupStatus.waitingForWifi,
        errorMessage: '等待 WiFi 连接',
      );
      return false;
    }

    return true;
  }

  /// 自动触发备份（app 启动、回到前台时调用）
  /// 检查配置条件后自动执行，不重复触发
  Future<void> autoTrigger() async {
    if (_isBackingUp) return;
    if (!_config.enabled) return;
    if (_config.targetParentId.isEmpty) return;
    if (_config.backupAlbumIds.isEmpty) return;

    _logger.info('Album backup auto-triggered');
    await startBackup();
  }

  /// 执行备份
  /// [force] 为 true 时跳过已备份检查，强制重新备份所有照片
  Future<AlbumBackupResult> startBackup({bool force = false}) async {
    if (_isBackingUp) {
      _logger.info('Backup already in progress');
      return _result;
    }

    _isBackingUp = true;

    try {
      // 检查配置
      if (!_config.enabled) {
        _updateStatus(status: AlbumBackupStatus.idle, errorMessage: '自动备份未启用');
        return _result;
      }

      // 检查权限
      final hasPermission = await requestPermission();
      if (!hasPermission) return _result;

      // 检查网络
      final hasNetwork = await _checkNetwork();
      if (!hasNetwork) return _result;

      // 开始扫描
      _updateStatus(status: AlbumBackupStatus.scanning);

      // 获取要备份的照片
      final photos = await _getPhotosToBackup(force: force);
      _logger.info('Found ${photos.length} photos to backup');

      if (photos.isEmpty) {
        _updateStatus(
          status: AlbumBackupStatus.completed,
          totalPhotos: 0,
          newPhotos: 0,
        );
        return _result;
      }

      // 开始上传
      _updateStatus(
        status: AlbumBackupStatus.uploading,
        totalPhotos: photos.length,
        newPhotos: photos.length,
      );

      // 边写边传：每攒够一批就写库并调度上传，不等全部扫描完
      final bgService = GetIt.I.get<BackgroundService>();
      const batchSize = 50;
      var batch = <UploadTaskRecord>[];

      // 获取有空位的子目录 ID
      String targetFolderId = _config.targetParentId;
      try {
        final repo = FmRepository();
        final result = await repo.getOrCreateSubfolder(
          parentId: _config.targetParentId,
          prefix: '00',
        );
        if (result.success && result.data != null) {
          targetFolderId = result.data!;
          _logger.info('Using subfolder: $targetFolderId');
        } else {
          _logger.warning('Failed to get subfolder, using root: ${result.message}');
        }
      } catch (e) {
        _logger.warning('Failed to get subfolder, using root: $e');
      }
      var backupRecords = <Map<String, dynamic>>[];
      int failed = 0;
      int created = 0;
      bool uploadStarted = false;

      for (int i = 0; i < photos.length; i++) {
        final photo = photos[i];
        try {
          // 优先用 relativePath + title 构造路径（避免慢速 originFile 调用）
          String filePath;
          int fileSize;
          
          if (photo.relativePath != null && photo.title != null) {
            filePath = '${photo.relativePath}${photo.title}';
            // 尝试用 File 获取大小，失败则用 originFile
            try {
              fileSize = await File(filePath).length();
            } catch (_) {
              final file = await photo.originFile;
              if (file == null) {
                _logger.warning('Cannot get file for photo: ${photo.id}');
                failed++;
                continue;
              }
              filePath = file.path;
              fileSize = await file.length();
            }
          } else {
            final file = await photo.originFile;
            if (file == null) {
              _logger.warning('Cannot get file for photo: ${photo.id}');
              failed++;
              continue;
            }
            filePath = file.path;
            fileSize = await file.length();
          }

          final fileName = photo.title ?? p.basename(filePath);
          final now = DateTime.now();

          batch.add(UploadTaskRecord(
            id: const Uuid().v4(),
            filename: fileName,
            filepath: filePath,
            fileSize: fileSize,
            parentId: targetFolderId,
            status: UploadStatus.PENDING,
            tags: photo.id,  // 存储资产 ID
            createdAt: now,
          ));

          // 同步写 backup_file_records（用 asset ID 作为 relative_path）
          backupRecords.add({
            'id': const Uuid().v4(),
            'config_id': Sqlite3Storage.albumBackupConfigId,
            'relative_path': photo.id,
            'file_size': fileSize,
            'modified_at': photo.createDateTime.millisecondsSinceEpoch,
            'status': BackupFileStatus.pending.index,
            'created_at': now.toIso8601String(),
          });

          // 每 batchSize 个写一次库并调度上传
          if (batch.length >= batchSize) {
            await bgService.addUploadTasksBatch(batch);
            for (final record in backupRecords) {
              await _storage.addBackupFileRecord(record);
            }
            created += batch.length;
            _logger.info('Batch wrote ${batch.length} tasks (total: $created)');
            if (!uploadStarted) {
              await bgService.scheduleFirstPendingUpload();
              uploadStarted = true;
              _logger.info('Upload started after first batch');
            }
            _updateStatus(uploadedPhotos: created);
            batch = [];
            backupRecords = [];
            await Future.delayed(Duration.zero); // 让出主线程
          }
        } catch (e) {
          _logger.error('Failed to create task for photo: ${photo.id}', e);
          failed++;
        }
      }

      // 写入剩余的
      if (batch.isNotEmpty) {
        await bgService.addUploadTasksBatch(batch);
        for (final record in backupRecords) {
          await _storage.addBackupFileRecord(record);
        }
        created += batch.length;
        _logger.info('Final batch wrote ${batch.length} tasks (total: $created)');
      }

      // 确保上传已启动
      if (!uploadStarted && created > 0) {
        await bgService.scheduleFirstPendingUpload();
      }

      _updateStatus(uploadedPhotos: created);

      // 完成
      _updateStatus(
        status: AlbumBackupStatus.completed,
        uploadedPhotos: created,
        failedPhotos: failed,
        lastBackupTime: DateTime.now(),
      );

      return _result;
    } catch (e) {
      _logger.error('Backup failed', e);
      _updateStatus(
        status: AlbumBackupStatus.error,
        errorMessage: e.toString(),
      );
      return _result;
    } finally {
      _isBackingUp = false;
    }
  }

  /// 获取需要备份的照片列表
  /// [force] 为 true 时跳过已备份检查，返回所有照片
  Future<List<AssetEntity>> _getPhotosToBackup({bool force = false}) async {
    // 确定要查询的相册
    List<AssetPathEntity> albums;
    if (_config.backupAlbumIds.isEmpty) {
      // 备份所有相册
      albums = await PhotoManager.getAssetPathList(
        type: _config.backupVideos ? RequestType.common : RequestType.image,
        hasAll: true,
      );
    } else {
      // 备份指定相册
      albums = [];
      for (final albumId in _config.backupAlbumIds) {
        final allAlbums = await PhotoManager.getAssetPathList(
          type: RequestType.image,
          hasAll: true,
        );
        final album = allAlbums.where((a) => a.id == albumId).firstOrNull;
        if (album != null) {
          albums.add(album);
        }
      }
    }

    if (albums.isEmpty) return [];

    // 获取所有照片
    final allPhotos = <AssetEntity>[];
    for (final album in albums) {
      final photos = await album.getAssetListRange(
        start: 0,
        end: await album.assetCountAsync,
      );
      allPhotos.addAll(photos);
    }

    // 从 backup_file_records 表获取已备份的 asset ID（不受 upload_tasks 清理影响）
    // force 模式下跳过检查，强制重新备份所有照片
    if (force) {
      _logger.info('Force mode: skipping backup check, will re-backup all ${allPhotos.length} photos');
      return allPhotos;
    }

    final backedUpAssetIds = await _storage.getCompletedAlbumBackupAssetIds();

    final photosToBackup = allPhotos
        .where((photo) => !backedUpAssetIds.contains(photo.id))
        .toList();

    return photosToBackup;
  }

  /// 创建上传任务记录（不调度）
  Future<UploadTaskRecord> _createUploadTask(AssetEntity photo, {String? targetFolderId}) async {
    // 获取文件
    final file = await photo.originFile;
    if (file == null) {
      throw Exception('Cannot get file for photo: ${photo.id}');
    }

    // 获取文件信息
    final fileName = p.basename(file.path);
    final fileSize = await file.length();

    // 创建上传任务（tags 存储资产 ID 用于去重）
    return UploadTaskRecord(
      id: const Uuid().v4(),
      filename: fileName,
      filepath: file.path,
      fileSize: fileSize,
      parentId: targetFolderId ?? _config.targetParentId,
      status: UploadStatus.PENDING,
      tags: photo.id,  // 存储资产 ID
      createdAt: DateTime.now(),
    );
  }

  /// 更新状态
  void _updateStatus({
    AlbumBackupStatus? status,
    int? totalPhotos,
    int? newPhotos,
    int? uploadedPhotos,
    int? failedPhotos,
    String? errorMessage,
    DateTime? lastBackupTime,
  }) {
    _result = AlbumBackupResult(
      status: status ?? _result.status,
      totalPhotos: totalPhotos ?? _result.totalPhotos,
      newPhotos: newPhotos ?? _result.newPhotos,
      uploadedPhotos: uploadedPhotos ?? _result.uploadedPhotos,
      failedPhotos: failedPhotos ?? _result.failedPhotos,
      errorMessage: errorMessage ?? _result.errorMessage,
      lastBackupTime: lastBackupTime ?? _result.lastBackupTime,
    );
    _statusController.add(_result);
  }

  /// 清理资源
  void dispose() {
    _statusController.close();
  }
}
