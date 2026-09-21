import 'dart:async';
import 'dart:convert';
// 注意：本服务刻意不导入 dart:io —— 录音目录属 media_rw 组，
// 路径/目录 API 一律被系统拒绝，全部访问改走原生 MediaStore 通道

import 'package:flutter/services.dart';
import 'package:get_it/get_it.dart';
import 'package:path/path.dart' as p;
import 'package:pfm/models/models.dart';
// proto 与 client_backup_service 各有一个 BackupFileStatus（枚举撞名）；
// 这里要的是 client_backup_service 的 Dart 枚举（peding/stale...），故 hide proto 那个，
// 与 device_backup_service.dart 同一处理
import 'package:pfm/proto/native/enum.pb.dart' hide BackupFileStatus;
import 'package:pfm/services/services.dart';
import 'package:pfm/utils/platform.dart';
import 'package:talker_flutter/talker_flutter.dart';
import 'package:uuid/uuid.dart';

/// 通话录音备份状态
enum CallRecordingStatus {
  idle,
  scanning,
  uploading,
  completed,
  error,
  noPermission,
  notConfigured,
}

/// 通话录音备份结果
class CallRecordingBackupResult {
  final CallRecordingStatus status;
  final int totalFiles;
  final int pendingFiles;
  final int uploadedFiles;
  final String? errorMessage;
  final DateTime? lastRunAt;

  const CallRecordingBackupResult({
    this.status = CallRecordingStatus.idle,
    this.totalFiles = 0,
    this.pendingFiles = 0,
    this.uploadedFiles = 0,
    this.errorMessage,
    this.lastRunAt,
  });
}

/// 增量扫描结果
///
/// `toBackup` 是本次需要上传的文件全路径列表，调用方据此建上传任务。
class CallRecordingScanResult {
  final List<CallRecordingCandidate> all;
  final List<CallRecordingCandidate> toBackup;

  const CallRecordingScanResult({required this.all, required this.toBackup});
}

/// 单个录音文件候选
class CallRecordingCandidate {
  /// SAF 授权目录下的 content:// uri：导出时就按它读内容
  final String safUri;

  /// 展示用名称（**不可直接读**，仅用于显示/日志）
  final String absPath;

  /// 相对源目录的路径，作为 [backup_file_records.relative_path] 的稳定主键
  final String relativePath;
  final int fileSize;
  final int modifiedAt;
  final CallRecordingName name;

  const CallRecordingCandidate({
    required this.safUri,
    required this.absPath,
    required this.relativePath,
    required this.fileSize,
    required this.modifiedAt,
    required this.name,
  });

  String get displayName => p.basename(absPath);
}

/// 通话录音备份服务
///
/// 通话录音与短信/通话记录不同：ROM 已把录音写成磁盘文件，本服务只负责
/// **搬运**——扫描源目录 → 与上次记录比对找出新增/变更 → 批量建上传任务，
/// 全程复用现有后台上传队列与 [backup_file_records] 断点记录。
///
/// 与「自动备份」([ClientBackupService]) 同属本地目录备份，但录音场景更窄：
/// 单一固定目录、只认音频扩展名、不递归子目录（ROM 不会在录音目录下再建层级）。
class CallRecordingBackupService {
  static CallRecordingBackupService? _instance;

  static const _configKey = 'callRecordingBackupConfig';

  /// 录音扫描/导出通道。**必须走它而非 Directory.list()**：
  /// 录音目录属组是 media_rw(1023)，本应用不在该组，路径访问会被系统拒绝
  /// （实测扫描恒为 0，尽管目录里有 438 个文件）。
  static const _recordingChannel = MethodChannel('pfm/call_recording');

  /// 配置 ID：录音备份是单例任务，用固定 ID 复用 backup_file_records 表
  static const configId = 'call_recording_backup';

  /// 每批写入的上传任务数（与相册备份一致，避免一次性建 438 个任务卡住 UI）
  static const _batchSize = 50;

  final LocalStorage _localStorage;
  late final Talker _logger;
  final _uuid = const Uuid();

  CallRecordingBackupConfig _config = const CallRecordingBackupConfig();
  CallRecordingBackupResult _result = const CallRecordingBackupResult();

  final _controller = StreamController<CallRecordingBackupResult>.broadcast();
  Stream<CallRecordingBackupResult> get statusStream => _controller.stream;

  bool _isRunning = false;

  CallRecordingBackupService._(this._localStorage) : _logger = getLogger();

  static Future<CallRecordingBackupService> instance() async {
    if (_instance != null) return _instance!;
    final storage = GetIt.I.get<LocalStorage>();
    _instance = CallRecordingBackupService._(storage);
    await _instance!._loadConfig();
    return _instance!;
  }

  CallRecordingBackupConfig get config => _config;
  CallRecordingBackupResult get result => _result;
  bool get isRunning => _isRunning;

  Future<void> _loadConfig() async {
    final saved = _localStorage.settingsBox.get(_configKey);
    if (saved != null) {
      _config = CallRecordingBackupConfig.fromJson(
        Map<String, dynamic>.from(saved as Map),
      );
      _result = CallRecordingBackupResult(lastRunAt: _config.lastRunAt);
    }
  }

  Future<void> _saveConfig() async {
    await _localStorage.settingsBox.put(_configKey, _config.toJson());
  }

  /// 设置录音源目录
  Future<void> setSourceDir(String dir) async {
    _config = _config.copyWith(sourceDir: dir);
    await _saveConfig();
    _emit(_result);
  }

  /// 设置服务端目标文件夹
  Future<void> setTargetFolder(String parentId, String folderName) async {
    _config = _config.copyWith(
      targetParentId: parentId,
      targetFolderName: folderName,
    );
    await _saveConfig();
    _emit(_result);
  }

  /// 探测可能的录音目录，返回第一个存在的（绝对路径）。
  ///
  /// 各 ROM 录音目录位置不统一且无标准 API，这里用候选表快速命中，
  /// 探测不到则返回 null，由 UI 引导用户手动选择。
  /// 当前是否已获得录音目录的 SAF 授权（授权可能被撤销/目录被删）。
  Future<bool> hasDirectoryAccess() async {
    if (isDesktop()) return false;
    try {
      final ok = await _recordingChannel.invokeMethod<bool>(
        'hasPersistedPermission',
      );
      return ok ?? false;
    } on PlatformException catch (e) {
      _logger.error('hasPersistedPermission failed', e);
      return false;
    }
  }

  /// 拉起系统目录选择器让用户授权录音目录（SAF）。
  ///
  /// 返回 true 表示授权成功；false 表示用户取消或失败。
  /// 授权会持久化（takePersistableUriPermission），重启后仍有效。
  Future<bool> pickDirectory() async {
    if (isDesktop()) return false;
    try {
      final uri = await _recordingChannel.invokeMethod<String>('pickDirectory');
      if (uri == null || uri.isEmpty) return false;
      // 记录展示名：tree uri 本身不可读，这里只存一个易读的标识
      _config = _config.copyWith(sourceDir: 'Sounds/CallRecord');
      await _saveConfig();
      _emit(_result);
      return true;
    } on PlatformException catch (e) {
      _logger.error('pickDirectory failed', e);
      _emit(
        CallRecordingBackupResult(
          status: CallRecordingStatus.error,
          errorMessage: '选择录音目录失败：${e.message}',
          lastRunAt: _config.lastRunAt,
        ),
      );
      return false;
    }
  }

  /// 扫描录音目录并与上次记录比对，得出需要上传的文件。
  ///
  /// 只读，不修改任何记录——便于 UI 先展示"将备份 N 个"再让用户确认执行。
  Future<CallRecordingScanResult> scan() async {
    // 先确认 SAF 授权仍有效：可能被用户撤销或目录被删，
    // 此时必须重新授权，否则扫描只会静默返回空、无从判断原因
    if (!await hasDirectoryAccess()) {
      _emit(
        CallRecordingBackupResult(
          status: CallRecordingStatus.noPermission,
          errorMessage: '请先授权录音目录（点击上方"录音目录"，选择手机的录音文件夹）',
          lastRunAt: _config.lastRunAt,
        ),
      );
      return const CallRecordingScanResult(all: [], toBackup: []);
    }

    _emit(
      CallRecordingBackupResult(
        status: CallRecordingStatus.scanning,
        lastRunAt: _config.lastRunAt,
      ),
    );

    final storage = await Sqlite3Storage.instance();
    final all = <CallRecordingCandidate>[];

    // 走 MediaStore 查询而非 Directory.list()：录音目录属组为 media_rw(1023)，
    // 本应用不在该组，路径访问会被系统拒绝（实测恒为 0 条）。
    List<dynamic> rawItems;
    try {
      final res = await _recordingChannel.invokeMethod<List<dynamic>>(
        'scanRecordings',
      );
      rawItems = res ?? const [];
    } on PlatformException catch (e) {
      _logger.error('scanRecordings failed', e);
      _emit(
        CallRecordingBackupResult(
          status: CallRecordingStatus.error,
          errorMessage: '读取录音目录失败：${e.message}',
          lastRunAt: _config.lastRunAt,
        ),
      );
      return const CallRecordingScanResult(all: [], toBackup: []);
    }

    for (final item in rawItems) {
      // 原生侧返回的是 List<String>，每个元素是**一条 JSON 字符串**
      // （逐条序列化以避免大对象一次性转换），故这里必须逐个 jsonDecode。
      // 曾漏掉这一步直接 `item as Map`，导致
      // "type 'String' is not a subtype of type 'Map<dynamic, dynamic>'"
      final m = _decodeJsonMap(item);
      final name = (m['name'] ?? '').toString();
      if (!isCallRecordingFile(name)) continue;
      all.add(
        CallRecordingCandidate(
          safUri: (m['uri'] ?? '').toString(),
          absPath: name,
          relativePath: name,
          fileSize: (m['size'] as num?)?.toInt() ?? 0,
          modifiedAt: (((m['modified'] as num?)?.toInt() ?? 0) / 1000).floor(),
          name: parseCallRecordingName(name),
        ),
      );
    }
    // 按通话时间倒序（无时间的排最后），便于 UI 展示最新的在前
    all.sort((a, b) {
      final ta = a.name.recordedAt;
      final tb = b.name.recordedAt;
      if (ta == null && tb == null) return a.displayName.compareTo(b.displayName);
      if (ta == null) return 1;
      if (tb == null) return -1;
      return tb.compareTo(ta);
    });

    final toBackup = <CallRecordingCandidate>[];
    for (final candidate in all) {
      final record = await storage.getBackupFileRecord(
        configId,
        candidate.relativePath,
      );
      final need = shouldBackupRecording(
        hasRecord: record != null,
        recordStatus: record?['status'] as int?,
        recordModifiedAt: record?['modified_at'] as int?,
        recordFileSize: record?['file_size'] as int?,
        currentModifiedAt: candidate.modifiedAt,
        currentFileSize: candidate.fileSize,
      );
      if (need) toBackup.add(candidate);
    }

    _logger.info(
      'CallRecording: scanned ${all.length}, ${toBackup.length} need backup',
    );
    return CallRecordingScanResult(all: all, toBackup: toBackup);
  }

  /// 执行备份：扫描 + 批量建上传任务。
  Future<CallRecordingBackupResult> startBackup() async {
    if (isDesktop()) {
      _emit(
        CallRecordingBackupResult(
          status: CallRecordingStatus.error,
          errorMessage: '通话录音备份仅在移动端可用',
        ),
      );
      return _result;
    }

    if (!_config.isConfigured) {
      _emit(
        CallRecordingBackupResult(
          status: CallRecordingStatus.notConfigured,
          errorMessage: '请先选择录音目录与备份目标文件夹',
        ),
      );
      return _result;
    }

    if (_isRunning) {
      _logger.info('Call recording backup already in progress');
      return _result;
    }
    _isRunning = true;

    try {
      if (!await hasDirectoryAccess()) {
        _emit(
          CallRecordingBackupResult(
            status: CallRecordingStatus.noPermission,
            errorMessage: '请先授权录音目录（点击上方"录音目录"，选择手机的录音文件夹）',
            lastRunAt: _config.lastRunAt,
          ),
        );
        return _result;
      }

      final scanResult = await scan();
      if (scanResult.toBackup.isEmpty) {
        _config = _config.copyWith(
          lastRunAt: DateTime.now(),
          lastScanCount: scanResult.all.length,
          lastBackupCount: 0,
          lastBackupSize: 0,
        );
        await _saveConfig();
        // 无新文件时正是回收空间的好时机：删掉已上传完成的副本，
        // 未完成的留着下次复用（不清空目录，见 cleanupUploadedCopies 注释）
        await cleanupUploadedCopies();
        _emit(
          CallRecordingBackupResult(
            status: CallRecordingStatus.completed,
            totalFiles: scanResult.all.length,
            pendingFiles: 0,
            uploadedFiles: 0,
            lastRunAt: _config.lastRunAt,
          ),
        );
        return _result;
      }

      _emit(
        CallRecordingBackupResult(
          status: CallRecordingStatus.uploading,
          totalFiles: scanResult.all.length,
          pendingFiles: scanResult.toBackup.length,
          uploadedFiles: 0,
          lastRunAt: _config.lastRunAt,
        ),
      );

      final storage = await Sqlite3Storage.instance();
      final bgService = await BackgroundService.instance();
      final now = DateTime.now();

      // 先把录音复制到应用私有目录。
      //
      // 必须复制：原始路径位于 media_rw 属组目录，本应用读不了（见通道注释），
      // 而上传流程是按**文件路径**读内容（io.File(filepath)），直接用原路径必失败。
      // 复制后私有副本可读；副本在上传完成后清理（见 _pruneExportedCopies）。
      final exported = await _exportToPrivateDir(scanResult.toBackup);
      if (exported.isEmpty) {
        _emit(
          CallRecordingBackupResult(
            status: CallRecordingStatus.error,
            errorMessage: '无法读取录音文件（已扫描到 ${scanResult.all.length} 个，'
                '但导出副本失败，请确认已授予音频/存储读取权限）',
            totalFiles: scanResult.all.length,
            lastRunAt: _config.lastRunAt,
          ),
        );
        return _result;
      }

      // 分批建任务并落 backup_file_records，避免一次性 400+ 条阻塞 UI；
      // 记录先写 PENDING，上传完成由上传流程回写状态（与相册备份一致）
      var created = 0;
      final batch = <UploadTaskRecord>[];
      final records = <Map<String, dynamic>>[];

      for (final candidate in scanResult.toBackup) {
        // 用私有副本路径而非原始路径：原路径读不了
        final localPath = exported[candidate.relativePath];
        if (localPath == null) continue;

        batch.add(
          UploadTaskRecord(
            id: _uuid.v4(),
            filepath: localPath,
            filename: candidate.displayName,
            fileSize: candidate.fileSize,
            parentId: _config.targetParentId,
            status: UploadStatus.PENDING,
            // 约定 `configId|relativePath`：上传完成后
            // BackgroundService._syncBackupRecordOnComplete 据此把
            // backup_file_records 置为 completed。不设 tags 会导致
            // 记录永远停在 pending，进而每次都被判定为"需重传"
            tags: '$configId|${candidate.relativePath}',
            // 文件名带号码与时间，不同录音不会同名；同名才覆盖
            overwrite: true,
            createdAt: now,
          ),
        );
        records.add({
          'id': _uuid.v4(),
          'config_id': configId,
          'relative_path': candidate.relativePath,
          'file_size': candidate.fileSize,
          'modified_at': candidate.modifiedAt,
          'status': BackupFileStatus.pending.index,
          'created_at': now.toIso8601String(),
        });

        if (batch.length >= _batchSize) {
          await _flushBatch(storage, bgService, batch, records);
          created += batch.length;
          batch.clear();
          records.clear();
        }
      }
      if (batch.isNotEmpty) {
        await _flushBatch(storage, bgService, batch, records);
        created += batch.length;
      }

      final totalSize = scanResult.toBackup.fold<int>(
        0,
        (sum, c) => sum + c.fileSize,
      );
      _config = _config.copyWith(
        lastRunAt: now,
        lastScanCount: scanResult.all.length,
        lastBackupCount: scanResult.toBackup.length,
        lastBackupSize: totalSize,
      );
      await _saveConfig();

      // 触发上传（批量插入后由上传服务接管）
      await bgService.scheduleFirstPendingUpload();

      // 回收上一轮已完成上传的副本；本轮刚导出的会被保留供复用
      await cleanupUploadedCopies();

      _emit(
        CallRecordingBackupResult(
          status: CallRecordingStatus.completed,
          totalFiles: scanResult.all.length,
          pendingFiles: scanResult.toBackup.length,
          uploadedFiles: created,
          lastRunAt: _config.lastRunAt,
        ),
      );
      return _result;
    } catch (e) {
      _logger.error('Call recording backup failed', e);
      _emit(
        CallRecordingBackupResult(
          status: CallRecordingStatus.error,
          errorMessage: e.toString(),
          lastRunAt: _config.lastRunAt,
        ),
      );
      return _result;
    } finally {
      _isRunning = false;
    }
  }

  /// 私有副本的绝对路径。
  ///
  /// 前缀不硬编码（那会依赖设备/用户路径），由原生的
  /// listExportedCopies 返回目录后再拼接；未取到时回退为空字符串，
  /// 此时调用方会走"重新导出"路径，仍能正确工作。
  String _exportPath(String name) =>
      _exportDirPrefix.isEmpty ? '' : '$_exportDirPrefix/$name';

  /// 原生上报的私有副本目录前缀（由 listExportedCopies 填充）
  String _exportDirPrefix = '';

  /// 列出已存在的私有副本，返回 {文件名: 大小}。
  ///
  /// 复用副本可避免每次备份都重拷全部文件（性能关键路径）。
  Future<Map<String, int>> _listExportedCopies() async {
    try {
      final res = await _recordingChannel.invokeMethod<List<dynamic>>(
        'listExportedCopies',
      );
      if (res == null) return const {};
      final out = <String, int>{};
      for (final item in res) {
        final m = _decodeJsonMap(item);
        final name = (m['name'] ?? '').toString();
        final size = (m['size'] as num?)?.toInt() ?? 0;
        final dir = (m['dir'] ?? '').toString();
        if (dir.isNotEmpty) _exportDirPrefix = dir;
        if (name.isNotEmpty) out[name] = size;
      }
      return out;
    } on PlatformException catch (e) {
      _logger.error('listExportedCopies failed', e);
      return const {};
    }
  }

  /// 把选中的录音复制到应用私有目录，返回 {原始文件名: 私有副本绝对路径}。
  ///
  /// 走原生通道按 MediaStore 的 _id 读取（content://），
  /// 因为原始路径属 media_rw 目录、本应用无权按路径读取。
  Future<Map<String, String>> _exportToPrivateDir(
    List<CallRecordingCandidate> candidates,
  ) async {
    if (candidates.isEmpty) return const {};
    try {
      // 复用已存在的副本：副本是"上传前的中转"，只要还在且大小对得上就无需重拷。
      // 不这样做的话，每次备份都要把全部文件重新复制一遍（真机 438 个约 20s+），
      // 这是"点备份很卡"的主因之一。
      final existing = await _listExportedCopies();
      final map = <String, String>{};
      final toCopy = <CallRecordingCandidate>[];
      for (final c in candidates) {
        final size = existing[c.relativePath];
        if (size != null && size == c.fileSize) {
          map[c.relativePath] = _exportPath(c.relativePath);
        } else {
          toCopy.add(c);
        }
      }
      if (map.isNotEmpty) {
        _logger.info(
          'CallRecording: reused ${map.length} existing copies, '
          'need copy ${toCopy.length}',
        );
      }

      for (final c in toCopy) {
        final res = await _recordingChannel.invokeMethod<String>(
          'exportRecording',
          {'uri': c.safUri, 'name': c.relativePath},
        );
        if (res == null || res.isEmpty) continue;
        final m = _decodeJsonMap(res);
        final name = (m['name'] ?? '').toString();
        final path = (m['uri'] ?? '').toString();
        if (name.isNotEmpty && path.isNotEmpty) map[name] = path;
      }
      if (map.length < candidates.length) {
        // 少导出意味着部分文件读取失败，如实记日志（不静默）
        _logger.info(
          'CallRecording: exported ${map.length}/${candidates.length} copies',
        );
      }
      return map;
    } on PlatformException catch (e) {
      _logger.error('exportRecording failed', e);
      return const {};
    }
  }

  /// 清理**已完成上传**的私有副本，避免撑爆应用私有空间。
  ///
  /// 刻意不做"全量清理"：副本现在会被复用来跳过重复复制（性能优化），
  /// 只有确认对应录音已上传完成（backup_file_records 里 status=completed）
  /// 才能删。若删早了导致上传失败，下次会重新导出，不会永久丢失。
  ///
  /// 该方法在每次备份结束、且没有进行中任务时调用。
  Future<void> cleanupUploadedCopies() async {
    try {
      final storage = await Sqlite3Storage.instance();
      final records = await storage.getBackupFileRecords(configId);
      final done = <String>{};
      for (final r in records) {
        final status = r['status'] as int?;
        final path = (r['relative_path'] ?? '').toString();
        if (status == BackupFileStatus.completed.index && path.isNotEmpty) {
          done.add(path);
        }
      }
      if (done.isEmpty) return;
      final res = await _recordingChannel.invokeMethod<int>(
        'cleanupExports',
        {'names': done.toList()},
      );
      if (res != null && res > 0) {
        _logger.info('CallRecording: cleaned $res uploaded copies');
      }
    } on PlatformException catch (e) {
      _logger.error('cleanupExports failed', e);
    }
  }

  /// 把选中的录音复制到应用私有目录，返回 {原始文件名: 私有副本绝对路径}。
  ///
  /// 走原生通道按 MediaStore 的 _id 读取（content://），
  /// 因为原始路径属 media_rw 目录、本应用无权按路径读取。

  /// 清理私有目录里的录音副本，避免撑爆应用私有空间。
  ///
  /// 副本是"上传队列读取前的临时中转"，上传完成后即可删。若删早了导致上传失败，
  /// 下次运行会重新导出（增量判定把 pending/failed 视为需重传），不会永久丢失。

  /// 写入一批上传任务与增量记录。
  ///
  /// 既有记录（曾上传、本次因变更需重传）走 UPDATE，避免 UNIQUE(config_id,
  /// relative_path) 冲突；新文件走 INSERT。
  Future<void> _flushBatch(
    Sqlite3Storage storage,
    BackgroundService bgService,
    List<UploadTaskRecord> batch,
    List<Map<String, dynamic>> records,
  ) async {
    await bgService.addUploadTasksBatch(batch);
    for (final record in records) {
      final existing = await storage.getBackupFileRecord(
        configId,
        record['relative_path'] as String,
      );
      if (existing != null) {
        await storage.updateBackupFileRecord(existing['id'] as String, {
          'file_size': record['file_size'],
          'modified_at': record['modified_at'],
          'status': record['status'],
          'message': null,
        });
      } else {
        await storage.addBackupFileRecord(record);
      }
    }
  }

  /// 把原生返回的一条记录解析为 Map。
  ///
  /// 原生侧统一返回"逐条 JSON 字符串"（避免大对象一次性转换），
  /// 调用方拿到的是 String 或已被平台解码的 Map，这里两者都接受 ——
  /// 曾因直接 `item as Map` 而抛出
  /// `type 'String' is not a subtype of type 'Map<dynamic, dynamic>'`，
  /// 统一走本函数可避免同类错误复发。
  Map<String, dynamic> _decodeJsonMap(Object? raw) {
    if (raw is Map) return Map<String, dynamic>.from(raw);
    final s = raw?.toString() ?? '';
    if (s.isEmpty) return const {};
    final decoded = jsonDecode(s);
    if (decoded is Map) return Map<String, dynamic>.from(decoded);
    return const {};
  }

  void _emit(CallRecordingBackupResult r) {
    _result = r;
    _controller.add(r);
  }

  void dispose() {
    _controller.close();
  }
}
