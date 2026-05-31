import 'dart:async';
import 'dart:io';

import 'package:flutter_contacts/flutter_contacts.dart';
import 'package:get_it/get_it.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/proto/native/enum.pb.dart';
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
  late final Talker _logger;

  ContactBackupResult _result = const ContactBackupResult();

  final _statusController =
      StreamController<ContactBackupResult>.broadcast();
  Stream<ContactBackupResult> get statusStream => _statusController.stream;

  bool _isBackingUp = false;

  ContactBackupService._(this._storage) : _logger = getLogger();

  static Future<ContactBackupService> instance() async {
    if (_instance != null) return _instance!;
    final storage = await Sqlite3Storage.instance();
    _instance = ContactBackupService._(storage);
    return _instance!;
  }

  ContactBackupResult get result => _result;

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
  Future<ContactBackupResult> startBackup({
    String? targetParentId,
  }) async {
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

      // 保存到临时文件
      final tempDir = await getTemporaryDirectory();
      final timestamp = DateTime.now().millisecondsSinceEpoch;
      final filePath = p.join(tempDir.path, 'contacts_$timestamp.vcf');
      final file = File(filePath);
      await file.writeAsString(vCardContent);

      _logger.info('Exported contacts to: $filePath');
      _updateStatus(filePath: filePath);

      // 上传到服务器
      _updateStatus(status: ContactBackupStatus.uploading);
      await _uploadBackup(file, targetParentId ?? '');

      // 完成
      _updateStatus(
        status: ContactBackupStatus.completed,
        lastBackupTime: DateTime.now(),
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
  Future<void> _uploadBackup(File file, String parentId) async {
    final fileName = p.basename(file.path);
    final fileSize = await file.length();
    final content = await file.readAsBytes();

    final fileHash = content.hashCode.toRadixString(16);

    final task = UploadTaskRecord(
      id: const Uuid().v4(),
      filename: fileName,
      filepath: file.path,
      fileSize: fileSize,
      parentId: parentId,
      status: UploadStatus.PENDING,
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
