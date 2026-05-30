import 'dart:async';
import 'dart:io';

import 'package:contacts_service/contacts_service.dart';
import 'package:get_it/get_it.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';
import 'package:permission_handler/permission_handler.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/proto/native/enum.pb.dart';
import 'package:pfm/services/services.dart';
import 'package:pfm/utils/platform.dart';
import 'package:talker_flutter/talker_flutter.dart';
import 'package:uuid/uuid.dart';

/// 通讯录备份状态
enum ContactBackupStatus {
  idle, // 空闲
  loading, // 加载联系人中
  exporting, // 导出中
  uploading, // 上传中
  completed, // 完成
  error, // 错误
  noPermission, // 无权限
}

/// 通讯录备份结果
class ContactBackupResult {
  final ContactBackupStatus status;
  final int totalContacts; // 总联系人数
  final int exportedContacts; // 已导出数
  final String? filePath; // 备份文件路径
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
///
/// 功能：
/// - 读取系统通讯录
/// - 导出为 vCard (.vcf) 格式
/// - 上传到服务器
class ContactBackupService {
  static ContactBackupService? _instance;

  final Sqlite3Storage _storage;
  late final Talker _logger;

  // 备份状态
  ContactBackupResult _result = const ContactBackupResult();

  // 状态控制器
  final _statusController =
      StreamController<ContactBackupResult>.broadcast();
  Stream<ContactBackupResult> get statusStream => _statusController.stream;

  // 是否正在备份
  bool _isBackingUp = false;

  ContactBackupService._(this._storage) : _logger = getLogger();

  /// 获取单例实例
  static Future<ContactBackupService> instance() async {
    if (_instance != null) return _instance!;
    final storage = await Sqlite3Storage.instance();
    _instance = ContactBackupService._(storage);
    return _instance!;
  }

  /// 获取当前结果
  ContactBackupResult get result => _result;

  /// 检查并请求通讯录权限
  Future<bool> requestPermission() async {
    if (isDesktop()) return false;
    final status = await Permission.contacts.request();
    if (status.isGranted) {
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
      final contacts = await ContactsService.getContacts();
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
      final vCardContent = _exportToVCard(contacts);
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

  /// 导出联系人为 vCard 格式
  String _exportToVCard(Iterable<Contact> contacts) {
    final buffer = StringBuffer();

    for (final contact in contacts) {
      buffer.writeln('BEGIN:VCARD');
      buffer.writeln('VERSION:3.0');

      // 姓名
      final givenName = contact.givenName ?? '';
      final familyName = contact.familyName ?? '';
      if (givenName.isNotEmpty || familyName.isNotEmpty) {
        buffer.writeln('N:$familyName;$givenName;;;');
        buffer.writeln('FN:$givenName $familyName'.trim());
      }

      // 昵称
      if (contact.displayName != null && contact.displayName!.isNotEmpty) {
        buffer.writeln('NICKNAME:${contact.displayName}');
      }

      // 电话号码
      if (contact.phones != null) {
        for (final phone in contact.phones!) {
          final value = phone.value ?? '';
          if (value.isNotEmpty) {
            final label = phone.label ?? 'CELL';
            buffer.writeln('TEL;TYPE=$label:$value');
          }
        }
      }

      // 邮箱
      if (contact.emails != null) {
        for (final email in contact.emails!) {
          final value = email.value ?? '';
          if (value.isNotEmpty) {
            final label = email.label ?? 'HOME';
            buffer.writeln('EMAIL;TYPE=$label:$value');
          }
        }
      }

      // 公司/职位
      if (contact.company != null && contact.company!.isNotEmpty) {
        buffer.writeln('ORG:${contact.company}');
      }
      if (contact.jobTitle != null && contact.jobTitle!.isNotEmpty) {
        buffer.writeln('TITLE:${contact.jobTitle}');
      }

      // 备注（contacts_service 0.6.3 不支持 note 属性，跳过）

      // 地址
      if (contact.postalAddresses != null) {
        for (final addr in contact.postalAddresses!) {
          final street = addr.street ?? '';
          final city = addr.city ?? '';
          final region = addr.region ?? '';
          final postcode = addr.postcode ?? '';
          final country = addr.country ?? '';
          if (street.isNotEmpty ||
              city.isNotEmpty ||
              region.isNotEmpty ||
              postcode.isNotEmpty ||
              country.isNotEmpty) {
            buffer.writeln(
              'ADR;TYPE=HOME:;;$street;$city;$region;$postcode;$country',
            );
          }
        }
      }

      buffer.writeln('END:VCARD');
      buffer.writeln();
    }

    return buffer.toString();
  }

  /// 上传备份文件到服务器
  Future<void> _uploadBackup(File file, String parentId) async {
    final fileName = p.basename(file.path);
    final fileSize = await file.length();
    final content = await file.readAsBytes();

    // 计算文件哈希
    final fileHash = content.hashCode.toRadixString(16);

    // 创建上传任务
    final task = UploadTaskRecord(
      id: const Uuid().v4(),
      filename: fileName,
      filepath: file.path,
      fileSize: fileSize,
      parentId: parentId,
      status: UploadStatus.PENDING,
      createdAt: DateTime.now(),
    );

    // 保存到数据库
    await _storage.addUploadTask(task);

    // 调度上传任务
    final bgService = GetIt.I.get<BackgroundService>();
    await bgService.scheduleNewUploadTask(task);

    _logger.info('Scheduled contact backup upload: $fileName');
  }

  /// 更新状态
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

  /// 清理资源
  void dispose() {
    _statusController.close();
  }
}
