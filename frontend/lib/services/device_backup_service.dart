import 'dart:async';
import 'dart:convert';
import 'dart:io';

// ignore: implementation_imports
import 'package:blake3_dart/src/blake3_compact.dart';
import 'package:flutter/services.dart';
import 'package:get_it/get_it.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';
import 'package:permission_handler/permission_handler.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/proto/native/enum.pb.dart' hide BackupFileStatus;
import 'package:pfm/services/services.dart';
import 'package:pfm/utils/platform.dart';
import 'package:talker_flutter/talker_flutter.dart';
import 'package:uuid/uuid.dart';

/// 短信/通话记录备份类型
enum DeviceBackupType {
  sms,
  callLog,
}

/// 短信/通话备份状态
enum DeviceBackupStatus {
  idle,
  loading,
  exporting,
  uploading,
  completed,
  skipped, // hash 相同，跳过上传
  error,
  noPermission,
}

/// 短信/通话备份结果
class DeviceBackupResult {
  final DeviceBackupStatus status;
  final int totalCount;
  final int exportedCount;
  final String? filePath;
  final String? errorMessage;
  final DateTime? lastBackupTime;

  const DeviceBackupResult({
    this.status = DeviceBackupStatus.idle,
    this.totalCount = 0,
    this.exportedCount = 0,
    this.filePath,
    this.errorMessage,
    this.lastBackupTime,
  });
}

/// 短信与通话记录备份服务（任务9）
///
/// 数据源为 Android 原生 MethodChannel（pfm/device_backup），
/// 导出为文本/XML 后走现有后台上传队列，逻辑与联系人备份一致：
/// 导出内容 hash 与上次一致则跳过上传。
class DeviceBackupService {
  static DeviceBackupService? _instance;

  final LocalStorage _localStorage;
  late final Talker _logger;

  /// 原生通道
  static const _channel = MethodChannel('pfm/device_backup');

  /// 各类型独立状态与配置
  final Map<DeviceBackupType, DeviceBackupResult> _results = {};
  final Map<DeviceBackupType, DeviceBackupConfig> _configs = {};

  final _statusControllers =
      Map<DeviceBackupType, StreamController<DeviceBackupResult>>.fromEntries(
        DeviceBackupType.values.map(
          (t) => MapEntry(t, StreamController<DeviceBackupResult>.broadcast()),
        ),
      );

  final Set<DeviceBackupType> _isBackingUp = {};

  DeviceBackupService._(this._localStorage) : _logger = getLogger();

  static Future<DeviceBackupService> instance() async {
    if (_instance != null) return _instance!;
    final localStorage = GetIt.I.get<LocalStorage>();
    _instance = DeviceBackupService._(localStorage);
    await _instance!._loadConfigs();
    return _instance!;
  }

  DeviceBackupResult result(DeviceBackupType type) =>
      _results[type] ?? const DeviceBackupResult();

  DeviceBackupConfig config(DeviceBackupType type) =>
      _configs[type] ?? const DeviceBackupConfig();

  Stream<DeviceBackupResult> statusStream(DeviceBackupType type) =>
      _statusControllers[type]!.stream;

  /// 加载持久化的配置
  Future<void> _loadConfigs() async {
    for (final type in DeviceBackupType.values) {
      final key = _configKey(type);
      final saved = _settingsBoxGet(key);
      if (saved != null) {
        _configs[type] = DeviceBackupConfig.fromJson(
          Map<String, dynamic>.from(saved),
        );
        _results[type] = DeviceBackupResult(
          lastBackupTime: _configs[type]!.lastBackupTime,
        );
      }
    }
  }

  Map<dynamic, dynamic>? _settingsBoxGet(String key) =>
      _localStorage.settingsBox.get(key) as Map<dynamic, dynamic>?;

  String _configKey(DeviceBackupType type) =>
      type == DeviceBackupType.sms
          ? 'smsBackupConfig'
          : 'callLogBackupConfig';

  Future<void> _saveConfig(DeviceBackupType type) async {
    final cfg = _configs[type];
    if (cfg != null) {
      await _localStorage.settingsBox.put(_configKey(type), cfg.toJson());
    }
  }

  /// 更新目标文件夹
  Future<void> setTargetFolder(
    DeviceBackupType type,
    String parentId,
    String folderName,
  ) async {
    final current = _configs[type] ?? const DeviceBackupConfig();
    _configs[type] = current.copyWith(
      targetParentId: parentId,
      targetFolderName: folderName,
    );
    await _saveConfig(type);
  }

  /// 检查并请求系统权限
  Future<bool> requestPermission(DeviceBackupType type) async {
    if (isDesktop()) return false;

    final permission = type == DeviceBackupType.sms
        ? Permission.sms
        : Permission.phone;
    final status = await permission.request();
    if (status.isGranted) return true;

    _updateStatus(
      type,
      status: DeviceBackupStatus.noPermission,
      errorMessage: type == DeviceBackupType.sms
          ? '需要短信读取权限才能备份短信（设置中授予"短信"权限）'
          : '需要通话记录读取权限才能备份通话记录（设置中授予"电话"权限）',
    );
    return false;
  }

  /// 执行备份
  Future<DeviceBackupResult> startBackup(DeviceBackupType type) async {
    if (isDesktop()) {
      _updateStatus(
        type,
        status: DeviceBackupStatus.error,
        errorMessage: '短信/通话记录备份仅在 Android 端可用',
      );
      return result(type);
    }

    if (_isBackingUp.contains(type)) {
      _logger.info('Backup already in progress: $type');
      return result(type);
    }
    _isBackingUp.add(type);

    try {
      // 1. 检查权限
      final hasPermission = await requestPermission(type);
      if (!hasPermission) return result(type);

      // 2. 读取系统数据（原生通道返回 JSON 字符串数组）
      _updateStatus(type, status: DeviceBackupStatus.loading);
      final rawList = await _readFromNative(type);
      _logger.info('Loaded ${rawList.length} records for $type');

      if (rawList.isEmpty) {
        _updateStatus(
          type,
          status: DeviceBackupStatus.completed,
          totalCount: 0,
          exportedCount: 0,
        );
        return result(type);
      }

      _updateStatus(
        type,
        status: DeviceBackupStatus.exporting,
        totalCount: rawList.length,
      );

      // 3. 序列化为导出文件
      final content = _buildExportContent(type, rawList);
      _updateStatus(type, exportedCount: rawList.length);

      // 4. 计算 hash（内容去重：相同内容跳过上传）
      final contentBytes = Uint8List.fromList(utf8.encode(content));
      final hasher = HashContext.unkeyed();
      hasher.update(contentBytes);
      final hashOutput = Uint8List(32);
      hasher.finalize(hashOutput);
      final hash = hashOutput
          .map((b) => b.toRadixString(16).padLeft(2, '0'))
          .join();

      final prevConfig = _configs[type];
      if (hash == prevConfig?.lastBackupHash) {
        _logger.info('$type content unchanged, skipping upload');
        _updateStatus(
          type,
          status: DeviceBackupStatus.skipped,
          lastBackupTime: prevConfig?.lastBackupTime,
          filePath: prevConfig?.lastBackupFilePath,
        );
        return result(type);
      }

      // 5. 写入带日期后缀的临时文件（避免覆盖旧备份）
      final tempDir = await getTemporaryDirectory();
      final now = DateTime.now();
      final dateSuffix =
          '${now.year}${now.month.toString().padLeft(2, '0')}${now.day.toString().padLeft(2, '0')}_${now.hour.toString().padLeft(2, '0')}${now.minute.toString().padLeft(2, '0')}';
      final ext = type == DeviceBackupType.sms ? 'xml' : 'csv';
      final prefix = type == DeviceBackupType.sms ? 'sms' : 'call_log';
      final fileName = '${prefix}_backup_$dateSuffix.$ext';
      final filePath = p.join(tempDir.path, fileName);
      final file = File(filePath);
      await file.writeAsString(content);

      _logger.info('Exported $type to: $filePath');
      _updateStatus(type, filePath: filePath);

      // 6. 上传到服务器（走现有后台上传队列）
      _updateStatus(type, status: DeviceBackupStatus.uploading);
      await _uploadBackup(type, file);

      // 7. 更新配置
      _configs[type] = (prevConfig ?? const DeviceBackupConfig()).copyWith(
        lastBackupHash: hash,
        lastBackupTime: now,
        lastBackupFilePath: filePath,
      );
      await _saveConfig(type);

      _updateStatus(type, status: DeviceBackupStatus.completed, lastBackupTime: now);
      return result(type);
    } catch (e) {
      _logger.error('$type backup failed', e);
      _updateStatus(
        type,
        status: DeviceBackupStatus.error,
        errorMessage: e.toString(),
      );
      return result(type);
    } finally {
      _isBackingUp.remove(type);
    }
  }

  /// 调用原生通道读取数据，返回 JSON 解码后的记录列表
  Future<List<Map<String, dynamic>>> _readFromNative(
    DeviceBackupType type,
  ) async {
    final method = type == DeviceBackupType.sms ? 'readSms' : 'readCallLog';
    final raw = await _channel.invokeMethod<List<dynamic>>(method);
    if (raw == null) return const [];
    return raw
        .map((e) => jsonDecode(e.toString()) as Map<String, dynamic>)
        .toList();
  }

  /// 构建导出文件内容
  String _buildExportContent(
    DeviceBackupType type,
    List<Map<String, dynamic>> records,
  ) {
    if (type == DeviceBackupType.sms) {
      return _buildSmsXml(records);
    }
    return _buildCallLogCsv(records);
  }

  /// 短信导出为 XML（格式与 SMS Backup & Restore 兼容，便于恢复）
  String _buildSmsXml(List<Map<String, dynamic>> records) {
    final buf = StringBuffer();
    buf.writeln('<?xml version="1.0" encoding="UTF-8"?>');
    buf.writeln('<smses count="${records.length}">');
    for (final r in records) {
      final address = _xmlEscape((r['address'] ?? '').toString());
      final body = _xmlEscape((r['body'] ?? '').toString());
      buf.writeln(
        '  <sms address="$address" date="${r['date']}" type="${r['type']}" '
        'read="${r['read']}" body="$body" />',
      );
    }
    buf.writeln('</smses>');
    return buf.toString();
  }

  /// 通话记录导出为 CSV（Excel 可直接打开）
  String _buildCallLogCsv(List<Map<String, dynamic>> records) {
    const typeNames = {
      1: '来电',
      2: '去电',
      3: '未接',
    };
    final buf = StringBuffer();
    buf.writeln('时间,号码,姓名,类型,时长(秒)');
    for (final r in records) {
      final dateRaw = r['date'];
      final date = dateRaw is int
          ? DateTime.fromMillisecondsSinceEpoch(dateRaw)
              .toLocal()
              .toIso8601String()
          : '';
      final number = _csvEscape((r['number'] ?? '').toString());
      final name = _csvEscape((r['name'] ?? '').toString());
      final typeName = typeNames[r['type']] ?? '未知';
      buf.writeln(
        '${_csvEscape(date)},$number,$name,$typeName,${r['duration'] ?? 0}',
      );
    }
    return buf.toString();
  }

  /// 上传备份文件到服务器
  Future<void> _uploadBackup(
    DeviceBackupType type,
    File file,
  ) async {
    final fileName = p.basename(file.path);
    final fileSize = await file.length();
    final content = await file.readAsBytes();

    final hasher = HashContext.unkeyed();
    hasher.update(Uint8List.fromList(content));
    final hashOutput = Uint8List(32);
    hasher.finalize(hashOutput);
    final fileHash = hashOutput
        .map((b) => b.toRadixString(16).padLeft(2, '0'))
        .join();

    final task = UploadTaskRecord(
      id: const Uuid().v4(),
      filename: fileName,
      filepath: file.path,
      fileSize: fileSize,
      fileHash: fileHash,
      parentId: _configs[type]?.targetParentId ?? '',
      status: UploadStatus.PENDING,
      overwrite: false, // 文件名带日期，不需要覆盖
      createdAt: DateTime.now(),
    );

    final bgService = GetIt.I.get<BackgroundService>();
    await bgService.scheduleNewUploadTask(task);

    _logger.info('Scheduled $type backup upload: $fileName');
  }

  void _updateStatus(
    DeviceBackupType type, {
    DeviceBackupStatus? status,
    int? totalCount,
    int? exportedCount,
    String? filePath,
    String? errorMessage,
    DateTime? lastBackupTime,
  }) {
    final prev = _results[type] ?? const DeviceBackupResult();
    _results[type] = DeviceBackupResult(
      status: status ?? prev.status,
      totalCount: totalCount ?? prev.totalCount,
      exportedCount: exportedCount ?? prev.exportedCount,
      filePath: filePath ?? prev.filePath,
      errorMessage: errorMessage ?? prev.errorMessage,
      lastBackupTime: lastBackupTime ?? prev.lastBackupTime,
    );
    _statusControllers[type]!.add(_results[type]!);
  }

  void dispose() {
    for (final c in _statusControllers.values) {
      c.close();
    }
  }
}

String _xmlEscape(String s) => s
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&apos;');

String _csvEscape(String s) {
  if (s.contains(',') || s.contains('"') || s.contains('\n')) {
    return '"${s.replaceAll('"', '""')}"';
  }
  return s;
}
