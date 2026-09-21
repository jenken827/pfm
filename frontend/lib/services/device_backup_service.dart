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
import 'package:pfm/services/device_backup_content.dart';
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
///
/// **按设备标识**：短信与通话记录是本机独有数据（不像通讯录那样跨设备同步），
/// 多台设备备份到同一目录后必须能分辨来源，因此文件名与文件内容都会带上
/// 设备标识（见 [buildDeviceBackupFileName] / [buildDeviceBackupContent]）。
class DeviceBackupService {
  static DeviceBackupService? _instance;

  final LocalStorage _localStorage;
  late final Talker _logger;

  /// 原生通道：读取短信/通话记录
  static const _channel = MethodChannel('pfm/device_backup');

  /// 原生通道：读取设备标识（设备名/厂商型号）
  static const _deviceChannel = MethodChannel('pfm/device_info');

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

  /// 设备信息缓存（类型+标识，一次会话内设备不会变，避免每次备份都走通道）
  String? _deviceInfo;

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

  /// 本机设备信息（`{类型}_{标识}`，用于文件名与内容），带缓存
  Future<String> deviceInfo() async {
    if (_deviceInfo != null) return _deviceInfo!;
    _deviceInfo = isDesktop() ? kDeviceLabelFallback : await _readDeviceInfo();
    return _deviceInfo!;
  }

  /// 从原生通道读取设备类型与标识，合成一段设备信息；失败时退回兜底值
  Future<String> _readDeviceInfo() async {
    try {
      // 类型与标识分别取：类型用于"看得懂是什么设备"，
      // 标识用于"区分同型号的多台设备"，两者来源不同、都不可省
      final type = await _deviceChannel.invokeMethod<String>('getDeviceType');
      final id = await _deviceChannel.invokeMethod<String>('getDeviceId');
      return buildDeviceInfo(deviceType: type ?? '', deviceId: id ?? '');
    } catch (e) {
      // 取设备信息失败不能阻断备份：退回兜底标识继续导出
      _logger.error('Read device info failed', e);
      return kDeviceLabelFallback;
    }
  }

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

    // 目标目录必须已设置：后端 InitUpload 对空 parentId 直接返回
    // 「cannot upload file to root directory」，即在**上传阶段**才失败。
    // 放在这里提前拦截，避免用户白等一次导出+上传才发现没选目录。
    final targetId = _configs[type]?.targetParentId ?? '';
    if (targetId.isEmpty) {
      _updateStatus(
        type,
        status: DeviceBackupStatus.error,
        errorMessage: '请先选择备份目标文件夹（点击上方"备份到"设置）',
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

      // 3. 设备标识（文件名与内容都要带，避免多设备备份混淆）
      final deviceInfo = await this.deviceInfo();

      // 4. 写入带设备信息与日期后缀的临时文件
      final file = await _exportToFile(type, rawList, deviceInfo);
      _logger.info('Exported $type to: ${file.path}');

      // 5. 计算 hash（内容去重：相同内容跳过上传）。
      //    分块流式哈希，避免为算 hash 再多读一份完整内容进内存。
      final hash = await _hashFileChunked(
        file,
        prefix: '$kDeviceBackupHashSchemeVersion\n$deviceInfo\n',
        onProgress: (done) => _updateStatus(
          type,
          exportedCount: done.clamp(0, rawList.length),
        ),
      );
      _updateStatus(type, exportedCount: rawList.length);

      final prevConfig = _configs[type];
      if (hash == prevConfig?.lastBackupHash) {
        _logger.info('$type content unchanged, skipping upload');
        // 本次导出的文件既然不上传就删掉：否则每点一次"立即备份"都会
        // 在本地留下一份重复文件，只能靠 _pruneOldBackups 的 3 份上限兜底
        await _deleteQuietly(file);
        _updateStatus(
          type,
          status: DeviceBackupStatus.skipped,
          lastBackupTime: prevConfig?.lastBackupTime,
          filePath: prevConfig?.lastBackupFilePath,
        );
        return result(type);
      }

      _updateStatus(type, filePath: file.path);

      // 6. 上传到服务器（走现有后台上传队列）
      _updateStatus(type, status: DeviceBackupStatus.uploading);
      await _uploadBackup(type, file);

      // 7. 更新配置（记录设备标识，页面上显示"上次备份来自哪台设备"）
      final now = DateTime.now();
      _configs[type] = (prevConfig ?? const DeviceBackupConfig()).copyWith(
        lastBackupHash: hash,
        lastBackupTime: now,
        lastBackupFilePath: file.path,
        deviceLabel: deviceInfo,
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

  /// 导出到本地文件。
  ///
  /// 落在 Application Support 目录而非 getTemporaryDirectory()：后者在
  /// Android 上映射到 cache 目录，系统清缓存可能把还没被上传队列读走的
  /// 备份文件删掉（与通讯录备份同一考量）。
  Future<File> _exportToFile(
    DeviceBackupType type,
    List<String> rawList,
    String deviceInfo,
  ) async {
    final dir = await getApplicationSupportDirectory();
    final now = DateTime.now();
    final isSms = type == DeviceBackupType.sms;
    final file = File(
      p.join(
        dir.path,
        buildDeviceBackupFileName(
          prefix: isSms ? 'sms' : 'call_log',
          deviceInfo: deviceInfo,
          now: now,
          extension: isSms ? 'xml' : 'csv',
        ),
      ),
    );

    // 分块写入：短信可达数万条，把全部记录与整份 XML 同时留在内存里
    // 在手机上很容易触发 OOM。这里按固定条数分批拼装、逐批落盘，
    // 拼接结果与一次性生成完全相同（见 buildDeviceBackupContent 的约定）。
    final sink = file.openWrite();
    try {
      for (var start = 0; start < rawList.length; start += kDeviceBackupChunkSize) {
        final end = (start + kDeviceBackupChunkSize).clamp(0, rawList.length);
        final isLast = end >= rawList.length;
        sink.write(
          buildDeviceBackupContent(
            isSms: isSms,
            deviceInfo: deviceInfo,
            rawJsonRecords: rawList.sublist(start, end),
            // 文件头/表头只在第一批输出，`</smses>` 只在最后一批输出
            includeHeader: start == 0,
            isLastChunk: isLast,
            totalCount: rawList.length,
          ),
        );
        _updateStatus(type, exportedCount: end, totalCount: rawList.length);
      }
    } finally {
      await sink.flush();
      await sink.close();
    }

    // 上传队列要等后台 Timer 才读文件，故只做有界清理，避免本地无限堆积
    await _pruneOldBackups(dir, keep: _kMaxLocalBackups);
    return file;
  }

  /// 调用原生通道读取数据，返回 JSON 字符串列表（保持原生格式，延迟解析）
  Future<List<String>> _readFromNative(DeviceBackupType type) async {
    final method = type == DeviceBackupType.sms ? 'readSms' : 'readCallLog';
    final raw = await _channel.invokeMethod<List<dynamic>>(method);
    if (raw == null) return const [];
    return raw.map((e) => e.toString()).toList();
  }

  /// 逐块计算文件 hash（额外拼入规则版本号与设备标识）。
  ///
  /// 设备标识进 hash 是有意的：换个设备导出的记录内容必然不同，
  /// 但若沿用同一份配置（例如同目录重复备份），旧 hash 会导致新设备
  /// 的备份被误判为"内容未变"而跳过上传。
  Future<String> _hashFileChunked(
    File file, {
    required String prefix,
    void Function(int done)? onProgress,
  }) async {
    final hasher = HashContext.unkeyed();
    hasher.update(Uint8List.fromList(utf8.encode(prefix)));
    var done = 0;
    var sinceNotify = 0;
    await for (final chunk in file.openRead()) {
      // openRead 的块是 List<int>，blake3 要 Uint8List
      hasher.update(Uint8List.fromList(chunk));
      done += chunk.length;
      sinceNotify += chunk.length;
      // 进度回调节流：每 256KB 通知一次，避免高频 setState
      if (onProgress != null && sinceNotify >= 256 * 1024) {
        sinceNotify = 0;
        onProgress(done);
      }
    }
    final out = Uint8List(32);
    hasher.finalize(out);
    return out.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
  }

  /// 上传备份文件到服务器
  Future<void> _uploadBackup(
    DeviceBackupType type,
    File file,
  ) async {
    final fileName = p.basename(file.path);
    final fileSize = await file.length();

    // 文件 hash 复用同一套分块读取，避免整份内容进内存
    final fileHash = await _hashFileChunked(file, prefix: '');

    final task = UploadTaskRecord(
      id: const Uuid().v4(),
      filename: fileName,
      filepath: file.path,
      fileSize: fileSize,
      fileHash: fileHash,
      // 此处必非空（startBackup 已校验）；显式断言防止将来绕过校验
      parentId: _configs[type]!.targetParentId,
      status: UploadStatus.PENDING,
      overwrite: false, // 文件名带设备与日期，不需要覆盖
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

/// 本地保留的历史备份文件上限（按修改时间倒序保留，超出的删除）
const int _kMaxLocalBackups = 3;

/// 删除文件，失败只记日志不抛出（清理属尽力而为，不能影响备份结论）
Future<void> _deleteQuietly(File file) async {
  try {
    if (await file.exists()) await file.delete();
  } catch (_) {
    // 忽略：残留文件由 _pruneOldBackups 的有界清理兜底
  }
}

/// 只清理本服务自己生成的备份文件，不碰目录内其他文件
Future<void> _pruneOldBackups(Directory dir, {required int keep}) async {
  if (!await dir.exists()) return;
  final files = <File>[];
  await for (final e in dir.list()) {
    if (e is File) {
      final name = p.basename(e.path);
      if (name.startsWith('sms_backup_') || name.startsWith('call_log_backup_')) {
        files.add(e);
      }
    }
  }
  if (files.length <= keep) return;
  files.sort((a, b) => b.statSync().modified.compareTo(a.statSync().modified));
  for (final f in files.skip(keep)) {
    try {
      await f.delete();
    } catch (_) {
      // 清理失败不影响本次备份
    }
  }
}
