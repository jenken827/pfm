import 'dart:async';
import 'dart:io';
import 'dart:typed_data';

import 'package:blake3_dart/src/blake3_compact.dart';
import 'package:flutter_contacts/flutter_contacts.dart';
import 'package:get_it/get_it.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/proto/native/enum.pb.dart' hide BackupFileStatus;
import 'package:pfm/services/services.dart';
import 'package:pfm/utils/platform.dart';
import 'package:talker_flutter/talker_flutter.dart';
import 'package:uuid/uuid.dart';

/// 通讯录备份状态
enum ContactBackupStatus {
  idle,
  loading,
  exporting,
  uploading,
  completed,
  skipped, // hash 相同，跳过上传
  error,
  noPermission,
}

/// 通讯录备份结果
class ContactBackupResult {
  final ContactBackupStatus status;
  final int totalContacts;
  final int exportedContacts;
  final String? filePath;
  final String? errorMessage;
  final DateTime? lastBackupTime;

  const ContactBackupResult({
    this.status = ContactBackupStatus.idle,
    this.totalContacts = 0,
    this.exportedContacts = 0,
    this.filePath,
    this.errorMessage,
    this.lastBackupTime,
  });
}

/// 通讯录备份服务
class ContactBackupService {
  static ContactBackupService? _instance;

  final Sqlite3Storage _storage;
  final LocalStorage _localStorage;
  late final Talker _logger;

  ContactBackupResult _result = const ContactBackupResult();
  ContactBackupConfig _config = const ContactBackupConfig();

  final _statusController =
      StreamController<ContactBackupResult>.broadcast();
  Stream<ContactBackupResult> get statusStream => _statusController.stream;

  bool _isBackingUp = false;

  ContactBackupService._(this._storage, this._localStorage)
      : _logger = getLogger();

  static Future<ContactBackupService> instance() async {
    if (_instance != null) return _instance!;
    final storage = await Sqlite3Storage.instance();
    final localStorage = GetIt.I.get<LocalStorage>();
    _instance = ContactBackupService._(storage, localStorage);
    await _instance!._loadConfig();
    return _instance!;
  }

  ContactBackupResult get result => _result;
  ContactBackupConfig get config => _config;

  /// 加载持久化的配置
  Future<void> _loadConfig() async {
    final saved = _localStorage.contactBackupConfig;
    if (saved != null) {
      _config = saved;
      _result = ContactBackupResult(
        lastBackupTime: saved.lastBackupTime,
      );
    }
  }

  /// 保存配置
  Future<void> _saveConfig() async {
    await _localStorage.saveContactBackupConfig(_config);
  }

  /// 更新目标文件夹
  Future<void> setTargetFolder(String parentId, String folderName) async {
    _config = _config.copyWith(
      targetParentId: parentId,
      targetFolderName: folderName,
    );
    await _saveConfig();
  }

  /// 检查并请求通讯录权限
  Future<bool> requestPermission() async {
    if (isDesktop()) return false;

    final permission =
        await FlutterContacts.permissions.request(PermissionType.read);
    if (permission == PermissionStatus.granted ||
        permission == PermissionStatus.limited) {
      return true;
    }

    _updateStatus(
      status: ContactBackupStatus.noPermission,
      errorMessage: '需要通讯录访问权限才能备份联系人',
    );
    return false;
  }

  /// 执行备份
  Future<ContactBackupResult> startBackup() async {
    if (isDesktop()) {
      _updateStatus(
        status: ContactBackupStatus.error,
        errorMessage: '通讯录备份仅在移动端（Android/iOS）可用',
      );
      return _result;
    }

    if (_isBackingUp) {
      _logger.info('Contact backup already in progress');
      return _result;
    }

    _isBackingUp = true;

    try {
      // 检查权限
      final hasPermission = await requestPermission();
      if (!hasPermission) return _result;

      // 加载联系人
      _updateStatus(status: ContactBackupStatus.loading);
      final contacts = await FlutterContacts.getAll();
      _logger.info('Loaded ${contacts.length} contacts');

      if (contacts.isEmpty) {
        _updateStatus(
          status: ContactBackupStatus.completed,
          totalContacts: 0,
          exportedContacts: 0,
        );
        return _result;
      }

      _updateStatus(
        status: ContactBackupStatus.exporting,
        totalContacts: contacts.length,
      );

      // 导出为 vCard
      final vCardContent = FlutterContacts.vCard.exportAll(contacts);
      _updateStatus(exportedContacts: contacts.length);

      // 计算 hash（使用 Blake3，与上传服务一致）
      // 去掉 REV 行（包含导出时间戳，每次不同），只比较联系人实际内容
      final contentForHash = vCardContent
          .split('\r\n')
          .where((line) => !line.startsWith('REV:'))
          .join('\r\n');
      final contentBytes = Uint8List.fromList(contentForHash.codeUnits);
      final hasher = HashContext.unkeyed();
      hasher.update(contentBytes);
      final output = Uint8List(32);
      hasher.finalize(output);
      final hash = output.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
      _logger.info('Exported contacts hash: $hash');

      // 对比 hash，如果相同则跳过上传
      if (hash == _config.lastBackupHash) {
        _logger.info('Contacts unchanged, skipping upload');
        _updateStatus(
          status: ContactBackupStatus.skipped,
          lastBackupTime: _config.lastBackupTime,
          filePath: _config.lastBackupFilePath,
        );
        return _result;
      }

      // 保存到固定文件名（替换旧文件）
      final tempDir = await getTemporaryDirectory();
      final filePath = p.join(tempDir.path, 'contacts_backup.vcf');
      final file = File(filePath);
      await file.writeAsString(vCardContent);

      // 删除旧的备份文件（如果路径不同）
      if (_config.lastBackupFilePath != null &&
          _config.lastBackupFilePath != filePath) {
        final oldFile = File(_config.lastBackupFilePath!);
        if (await oldFile.exists()) {
          await oldFile.delete();
          _logger.info('Deleted old backup file: ${_config.lastBackupFilePath}');
        }
      }

      _logger.info('Exported contacts to: $filePath');
      _updateStatus(filePath: filePath);

      // 上传到服务器
      _updateStatus(status: ContactBackupStatus.uploading);
      await _uploadBackup(file);

      // 更新配置
      final now = DateTime.now();
      _config = _config.copyWith(
        lastBackupHash: hash,
        lastBackupTime: now,
        lastBackupFilePath: filePath,
      );
      await _saveConfig();

      // 完成
      _updateStatus(
        status: ContactBackupStatus.completed,
        lastBackupTime: now,
      );

      return _result;
    } catch (e) {
      _logger.error('Contact backup failed', e);
      _updateStatus(
        status: ContactBackupStatus.error,
        errorMessage: e.toString(),
      );
      return _result;
    } finally {
      _isBackingUp = false;
    }
  }

  /// 上传备份文件到服务器
  Future<void> _uploadBackup(File file) async {
    final fileName = 'contacts_backup.vcf';
    final fileSize = await file.length();
    final content = await file.readAsBytes();

    // 使用 Blake3 作为文件 hash（与上传服务一致）
    final hasher = HashContext.unkeyed();
    hasher.update(Uint8List.fromList(content));
    final hashOutput = Uint8List(32);
    hasher.finalize(hashOutput);
    final fileHash = hashOutput.map((b) => b.toRadixString(16).padLeft(2, '0')).join();

    final task = UploadTaskRecord(
      id: const Uuid().v4(),
      filename: fileName,
      filepath: file.path,
      fileSize: fileSize,
      fileHash: fileHash,
      parentId: _config.targetParentId,
      status: UploadStatus.PENDING,
      overwrite: true, // 固定文件名，覆盖已有文件
      createdAt: DateTime.now(),
    );

    await _storage.addUploadTask(task);

    final bgService = GetIt.I.get<BackgroundService>();
    await bgService.scheduleNewUploadTask(task);

    _logger.info('Scheduled contact backup upload: $fileName');
  }

  void _updateStatus({
    ContactBackupStatus? status,
    int? totalContacts,
    int? exportedContacts,
    String? filePath,
    String? errorMessage,
    DateTime? lastBackupTime,
  }) {
    _result = ContactBackupResult(
      status: status ?? _result.status,
      totalContacts: totalContacts ?? _result.totalContacts,
      exportedContacts: exportedContacts ?? _result.exportedContacts,
      filePath: filePath ?? _result.filePath,
      errorMessage: errorMessage ?? _result.errorMessage,
      lastBackupTime: lastBackupTime ?? _result.lastBackupTime,
    );
    _statusController.add(_result);
  }

  void dispose() {
    _statusController.close();
  }
}
