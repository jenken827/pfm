import 'dart:async';
import 'dart:io';

import 'package:flutter_contacts/flutter_contacts.dart';
import 'package:get_it/get_it.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';
import 'package:pfm/models/contact_merge_plan.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/proto/native/enum.pb.dart' hide BackupFileStatus;
import 'package:pfm/services/contact_merge.dart';
import 'package:pfm/services/contact_restore.dart';
import 'package:pfm/services/contact_undo_service.dart';
import 'package:pfm/services/file_hasher.dart';
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

/// 通讯录备份 hash 规则版本号。
///
/// 一旦导出字段集合或清洗规则变化（例如新增字段、改 REV 过滤方式），
/// **必须**递增此值：它参与 hash 计算，从而保证规则变更后 hash 必然不同、
/// 不会把"新规则下的数据"误判为"与旧备份相同"而跳过上传。
const String kContactHashSchemeVersion = 'v2';

/// 本地保留的历史备份文件上限（按修改时间倒序保留，超出的删除）。
///
/// 文件需存活到后台上传读取完毕，无法立即删除，因此用有界保留避免无限堆积。
/// 服务器端才是真正的备份归档，本地这份只是上传前的暂存。
const int kMaxLocalBackups = 3;

/// 通讯录备份服务
class ContactBackupService {
  static ContactBackupService? _instance;

  final LocalStorage _localStorage;
  late final Talker _logger;

  ContactBackupResult _result = const ContactBackupResult();
  ContactBackupConfig _config = const ContactBackupConfig();

  final _statusController = StreamController<ContactBackupResult>.broadcast();
  Stream<ContactBackupResult> get statusStream => _statusController.stream;

  bool _isBackingUp = false;

  ContactBackupService._(this._localStorage) : _logger = getLogger();

  static Future<ContactBackupService> instance() async {
    if (_instance != null) return _instance!;
    final localStorage = GetIt.I.get<LocalStorage>();
    _instance = ContactBackupService._(localStorage);
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
      _result = ContactBackupResult(lastBackupTime: saved.lastBackupTime);
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

    final permission = await FlutterContacts.permissions.request(
      PermissionType.read,
    );
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
      //
      // 必须显式指定 properties！FlutterContacts.getAll() 默认用
      // ContactProperties.none（crud_api.dart:53 `properties ?? ContactProperties.none`），
      // 即只取 id 和 displayName，phones/emails/addresses 全是空列表 —— 导出的 vCard
      // 会只有姓名、没有任何 TEL 行，且全程无报错（静默数据丢失）。
      //
      // 用 allProperties 而非 all：后者额外取缩略图+全尺寸头像，头像以 base64 进 vCard
      // 会让体积暴涨，且换头像即触发整份重传（hash 是逐字节算的）。
      _updateStatus(status: ContactBackupStatus.loading);
      final contacts = await FlutterContacts.getAll(
        properties: ContactProperties.allProperties,
      );
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

      // 防御性校验：确认真的取到了号码。
      //
      // 曾经的线上事故：getAll() 漏传 properties → 拿到的全是只有姓名的空壳对象，
      // 导出的 vCard 一个 TEL 都没有，却"备份成功"上报，静默丢数据。
      // 若一台设备**所有**联系人的 phones 都为空，几乎不可能是真实情况
      // （手机通讯录通常至少有一个号码），此时宁可报错也不能上传空备份。
      _assertContactDataPresent(contacts);

      // 导出为 vCard
      final vCardContent = FlutterContacts.vCard.exportAll(contacts);
      _updateStatus(exportedContacts: contacts.length);

      // 二次校验：导出的文本里必须真的有 TEL 行。
      // 前面查的是对象，这里查的是最终落地内容 —— 只有这一步能真正保证
      // "用户下载到的 .vcf 里有号码"。
      if (!vCardContent.contains('TEL')) {
        throw StateError(
          '导出的 vCard 不含任何 TEL 行（${contacts.length} 个联系人），'
          '疑似字段获取失败，已中止上传以免产生无号码的无效备份',
        );
      }

      // 计算 hash（使用 Blake3，与上传服务一致）
      // 去掉 REV 行（包含导出时间戳，每次不同），只比较联系人实际内容。
      // 前缀 hash 规则版本号：一旦导出字段集合/规则变化，hash 必然改变，
      // 避免"数据维度变了但 hash 规则没变"导致新数据被误判为未变更而跳过上传。
      final cleanedVCard = vCardContent
          .split('\r\n')
          .where((line) => !line.startsWith('REV:'))
          .join('\r\n');
      final contentForHash = '$kContactHashSchemeVersion\n$cleanedVCard';
      final hash = FileHasher.hashBytes(contentForHash.codeUnits);
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

      // 保存到带日期后缀的文件名（避免覆盖旧备份）
      //
      // 用 Application Support 目录而非 getTemporaryDirectory()：
      // 后者在 Android 上映射到 getCacheDir()（/data/user/0/<pkg>/cache），
      // 系统在存储紧张时会清理、用户在设置里点「清除缓存」也会清空，
      // 而本文件要等到 BackgroundService 的 3 秒 Timer 才被读取上传
      // （upload.dart 后续按 task.filepath 读整份内容）——落在 cache 里有被
      // 提前清掉导致上传失败的风险。Application Support 不受此影响。
      final backupDir = await getApplicationSupportDirectory();
      final now = DateTime.now();
      final dateSuffix =
          '${now.year}${now.month.toString().padLeft(2, '0')}${now.day.toString().padLeft(2, '0')}_${now.hour.toString().padLeft(2, '0')}${now.minute.toString().padLeft(2, '0')}';
      final fileName = 'contacts_backup_$dateSuffix.vcf';
      final filePath = p.join(backupDir.path, fileName);
      final file = File(filePath);
      await file.writeAsString(vCardContent);

      _logger.info('Exported contacts to: $filePath');
      _updateStatus(filePath: filePath);

      // 备份文件要存活到后台上传读完（见 _uploadBackup 注释），不能立即删，
      // 故这里只做有界清理，避免历次备份在本地无限堆积。
      await _pruneOldBackups(backupDir, keep: kMaxLocalBackups);

      // 上传到服务器
      _updateStatus(status: ContactBackupStatus.uploading);
      await _uploadBackup(file);

      // 更新配置
      _config = _config.copyWith(
        lastBackupHash: hash,
        lastBackupTime: now,
        lastBackupFilePath: filePath,
      );
      await _saveConfig();

      // 完成
      _updateStatus(status: ContactBackupStatus.completed, lastBackupTime: now);

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
    // 从文件路径提取带日期后缀的文件名
    final fileName = p.basename(file.path);
    final fileSize = await file.length();
    final content = await file.readAsBytes();

    // 使用 Blake3 作为文件 hash（与上传服务一致；vCard 只有几百 KB，内存里算）
    final fileHash = FileHasher.hashBytes(content);

    final task = UploadTaskRecord(
      id: const Uuid().v4(),
      filename: fileName,
      filepath: file.path,
      fileSize: fileSize,
      fileHash: fileHash,
      parentId: _config.targetParentId,
      status: UploadStatus.PENDING,
      overwrite: false, // 文件名带日期，不需要覆盖
      createdAt: DateTime.now(),
    );

    final bgService = GetIt.I.get<BackgroundService>();
    await bgService.scheduleNewUploadTask(task);

    _logger.info('Scheduled contact backup upload: $fileName');
  }

  // ============================================================
  // 去重扫描（只读，不申请写权限、不改任何数据）
  // ============================================================

  /// 扫描重复联系人，返回全部重复组（含合并预览）。
  ///
  /// **只读**：仅申请读权限，不修改通讯录。用于让用户在真正执行前
  /// 用真实数据校准判据。
  ///
  /// 返回 null 表示无权限或读取失败（原因见 [result].errorMessage）。
  Future<ContactScanResult?> scanDuplicates() async {
    if (isDesktop()) {
      _updateStatus(
        status: ContactBackupStatus.error,
        errorMessage: '通讯录功能仅在移动端（Android/iOS）可用',
      );
      return null;
    }

    try {
      final hasPermission = await requestPermission();
      if (!hasPermission) return null;

      _updateStatus(status: ContactBackupStatus.loading);

      // 必须 allProperties：update() 只更新"当初 fetch 过的"属性，
      // 后续执行合并时要用同一批对象写回，漏 fetch 会静默丢字段。
      final contacts = await FlutterContacts.getAll(
        properties: ContactProperties.allProperties,
      );
      _logger.info('Scan: loaded ${contacts.length} contacts');

      final groups = findDuplicateGroups(contacts);
      final entries = groups
          .map(buildMergeEntry)
          .toList();

      // 存疑组与可自动合并组分开统计，供 UI 展示
      final autoMergeable = entries.where((e) => e.group.canAutoMerge).length;
      final suspicious = entries
          .where((e) => e.group.canAutoMerge && e.group.hasConflict)
          .length;
      final blocked = entries.where((e) => !e.group.canAutoMerge).length;

      _logger.info(
        'Scan: ${entries.length} duplicate groups '
        '(auto=$autoMergeable, suspicious=$suspicious, blocked=$blocked)',
      );

      _updateStatus(status: ContactBackupStatus.idle);

      return ContactScanResult(
        totalContacts: contacts.length,
        entries: entries,
        autoMergeableCount: autoMergeable,
        suspiciousCount: suspicious,
        blockedCount: blocked,
      );
    } catch (e) {
      _logger.error('Scan duplicates failed', e);
      _updateStatus(
        status: ContactBackupStatus.error,
        errorMessage: e.toString(),
      );
      return null;
    }
  }

  // ============================================================
  // 恢复（从服务器备份导入并与本地合并）
  // ============================================================

  /// 列出目标文件夹里的备份文件（.vcf），按时间倒序。
  ///
  /// 返回 null 表示未配置目标文件夹或请求失败。
  Future<List<BackupFileInfo>?> listBackupFiles() async {
    if (isDesktop()) {
      _updateStatus(
        status: ContactBackupStatus.error,
        errorMessage: '通讯录功能仅在移动端（Android/iOS）可用',
      );
      return null;
    }

    try {
      _updateStatus(status: ContactBackupStatus.loading);
      final repo = FmRepository();

      final resp = await repo.getFileList(
        // targetParentId 非空字符串（'' 表示根目录），无需判空
        parentId: _config.targetParentId,
        pageSize: 500,
      );
      if (!resp.success || resp.data == null) {
        _updateStatus(
          status: ContactBackupStatus.error,
          errorMessage: resp.message ?? '获取备份列表失败',
        );
        return null;
      }

      final files = resp.data!.files
          .where((f) => f.name.toLowerCase().endsWith('.vcf'))
          .map(
            (f) => BackupFileInfo(
              fileId: f.id,
              name: f.name,
              size: f.size.toInt(),
              updatedAt: f.updatedAt.toDateTime(),
            ),
          )
          .toList()
        ..sort((a, b) => b.updatedAt.compareTo(a.updatedAt));

      _updateStatus(status: ContactBackupStatus.idle);
      _logger.info('Found ${files.length} backup files');
      return files;
    } catch (e) {
      _logger.error('listBackupFiles failed', e);
      _updateStatus(
        status: ContactBackupStatus.error,
        errorMessage: e.toString(),
      );
      return null;
    }
  }

  /// 下载并解析备份文件，生成"导入 vs 本地"的恢复计划（**只读，不改数据**）。
  ///
  /// 与去重扫描一样，先出计划让用户确认，再执行。
  Future<RestorePlan?> planRestoreFromBackup(String fileId) async {
    if (isDesktop()) return null;

    try {
      _updateStatus(status: ContactBackupStatus.loading);

      // 1. 下载
      final path = await _downloadBackup(fileId);
      if (path == null) return null;

      // 2. 解析（复用插件自带的 vCard reader，不手写解析器）
      final content = await File(path).readAsString();
      final imported = FlutterContacts.vCard.import(content);
      _logger.info('Restore: imported ${imported.length} contacts from backup');

      if (imported.isEmpty) {
        _updateStatus(
          status: ContactBackupStatus.error,
          errorMessage: '备份文件里没有解析出任何联系人',
        );
        return null;
      }

      // 3. 读本地
      _updateStatus(status: ContactBackupStatus.loading);
      final local = await FlutterContacts.getAll(
        properties: ContactProperties.allProperties,
      );

      // 4. 规划
      final plan = planRestore(imported: imported, local: local);
      _updateStatus(status: ContactBackupStatus.idle);
      _logger.info(
        'Restore plan: create=${plan.createCount} '
        'merge=${plan.mergeCount} skip=${plan.skipCount}',
      );
      return plan;
    } catch (e) {
      _logger.error('planRestoreFromBackup failed', e);
      _updateStatus(
        status: ContactBackupStatus.error,
        errorMessage: e.toString(),
      );
      return null;
    } finally {
      // 清理临时下载文件
      unawaited(_cleanupTempBackups());
    }
  }

  /// 下载备份文件到本地临时路径
  Future<String?> _downloadBackup(String fileId) async {
    final baseUrl = _localStorage.downloadBaseUrl;
    final token = _localStorage.token;
    if (token == null) {
      _updateStatus(
        status: ContactBackupStatus.error,
        errorMessage: '未登录，无法下载备份',
      );
      return null;
    }
    final url = '$baseUrl/api/file/stream/download?file_id=$fileId';

    final dir = await getApplicationSupportDirectory();
    final target = File(p.join(dir.path, 'restore_tmp_$fileId.vcf'));

    final client = HttpClient();
    try {
      final req = await client.getUrl(Uri.parse(url));
      req.headers.set('Authorization', 'Bearer ${token.accessToken}');
      final resp = await req.close();
      if (resp.statusCode != 200) {
        _updateStatus(
          status: ContactBackupStatus.error,
          errorMessage: '下载备份失败：HTTP ${resp.statusCode}',
        );
        return null;
      }

      final sink = target.openWrite();
      await resp.pipe(sink);
      await sink.flush();
      await sink.close();
      _logger.info('Backup downloaded: ${target.path}');
      return target.path;
    } catch (e) {
      _logger.error('download backup failed', e);
      _updateStatus(
        status: ContactBackupStatus.error,
        errorMessage: '下载备份失败：$e',
      );
      return null;
    } finally {
      client.close();
    }
  }

  /// 清理恢复用的临时下载文件
  Future<void> _cleanupTempBackups() async {
    try {
      final dir = await getApplicationSupportDirectory();
      await for (final e in Directory(dir.path).list()) {
        if (e is File && p.basename(e.path).startsWith('restore_tmp_')) {
          await e.delete();
        }
      }
    } catch (_) {}
  }

  /// 执行恢复：按计划新增 / 合并；不删除任何本地联系人。
  ///
  /// 与 executeMerge 一样需要写权限，且会写撤销点（便于回滚）。
  Future<MergeOutcome> executeRestore(RestorePlan plan) async {
    if (isDesktop()) {
      return const MergeOutcome(
        success: false,
        errorMessage: '通讯录功能仅在移动端（Android/iOS）可用',
      );
    }
    if (!plan.hasChanges) {
      return const MergeOutcome(success: true, mergedGroups: 0);
    }

    if (!await requestWritePermission()) {
      return const MergeOutcome(
        success: false,
        errorMessage: '未获得通讯录写入权限，操作已取消',
      );
    }

    _updateStatus(status: ContactBackupStatus.exporting);

    // 撤销点：记录将被修改的本地联系人（恢复不删人，故 deleted 为空）
    try {
      await _writeRestoreUndoPoint(plan);
    } catch (e) {
      _logger.error('Failed to write restore undo point, aborting', e);
      _updateStatus(
        status: ContactBackupStatus.error,
        errorMessage: '创建撤销点失败，已中止恢复：$e',
      );
      return const MergeOutcome(
        success: false,
        errorMessage: '创建撤销点失败，已中止恢复（未做任何修改）',
      );
    }

    var created = 0;
    var mergedCount = 0;
    final failures = <String>[];

    // 1) 合并：把导入项的内容并进本地载体
    for (final item in plan.toMerge) {
      try {
        final target = _localById[item.targetId];
        if (target == null || item.merged == null) {
          failures.add('合并 ${item.imported.displayName}：找不到本地目标');
          continue;
        }
        await FlutterContacts.update(_buildMergedTarget(target, item.merged!));
        mergedCount++;
      } catch (e) {
        _logger.error('Restore merge failed: ${item.imported.displayName}', e);
        failures.add('合并 ${item.imported.displayName}：$e');
      }
    }

    // 2) 新增：本地没有的导入项
    for (final item in plan.toCreate) {
      try {
        await FlutterContacts.create(_buildNewContact(item.imported));
        created++;
      } catch (e) {
        _logger.error('Restore create failed: ${item.imported.displayName}', e);
        failures.add('新增 ${item.imported.displayName}：$e');
      }
    }

    _updateStatus(
      status: failures.isEmpty
          ? ContactBackupStatus.completed
          : ContactBackupStatus.error,
      errorMessage: failures.isEmpty ? null : failures.join('\n'),
    );

    return MergeOutcome(
      success: failures.isEmpty,
      mergedGroups: mergedCount,
      modifiedCount: mergedCount,
      restoredCount: created,
      failures: failures,
      errorMessage: failures.isEmpty ? null : failures.join('\n'),
    );
  }

  /// 当前计划的本地联系人索引（executeRestore 用）
  Map<String, Contact> _localById = {};

  /// 写恢复的撤销点
  Future<void> _writeRestoreUndoPoint(RestorePlan plan) async {
    final all = await FlutterContacts.getAll(
      properties: ContactProperties.allProperties,
    );
    _localById = {
      for (final c in all)
        if (c.id != null) c.id!: c,
    };

    final vCard = FlutterContacts.vCard.exportAll(all);

    final modified = <UndoModifiedEntry>[];
    for (final item in plan.toMerge) {
      final id = item.targetId;
      if (id == null) continue;
      final before = _localById[id];
      if (before != null) {
        modified.add(UndoModifiedEntry(id: id, before: before.toJson()));
      }
    }

    await ContactUndoService.save(
      log: MergeUndoLog(
        createdAt: DateTime.now(),
        schemeVersion: kContactHashSchemeVersion,
        modified: modified,
        // 恢复不删除本地联系人（新增的项不需要撤销记录 —— 它们是新加的，
        // 想清理的话用户自己删即可；若记入 deleted，撤销反而会去"重建"它们）
        deleted: const [],
      ),
      vCardContent: vCard,
    );
  }

  /// 构造合并后的本地载体（保留原 metadata）
  Contact _buildMergedTarget(Contact target, MergedContact merged) {
    final name = merged.nameChanged
        ? Name(first: merged.name, last: '')
        : target.name;
    return Contact(
      id: target.id,
      name: name,
      phones: merged.phones,
      emails: merged.emails,
      addresses: merged.addresses,
      organizations: merged.organizations,
      websites: merged.websites,
      socialMedias: merged.socialMedias,
      events: merged.events,
      relations: merged.relations,
      notes: merged.notes,
      metadata: target.metadata,
    );
  }

  /// 构造要新增的联系人（无 id、无 metadata）
  Contact _buildNewContact(Contact imported) => Contact(
    name: imported.name,
    phones: imported.phones,
    emails: imported.emails,
    addresses: imported.addresses,
    organizations: imported.organizations,
    websites: imported.websites,
    socialMedias: imported.socialMedias,
    events: imported.events,
    relations: imported.relations,
    notes: imported.notes,
    photo: imported.photo,
  );

  // ============================================================
  // 执行合并（破坏性操作，需要写权限 + 撤销点）
  // ============================================================

  /// 申请读写权限（执行合并前调用）
  Future<bool> requestWritePermission() async {
    if (isDesktop()) return false;
    final permission = await FlutterContacts.permissions.request(
      PermissionType.readWrite,
    );
    final ok =
        permission == PermissionStatus.granted ||
        permission == PermissionStatus.limited;
    if (!ok) {
      _logger.warning('write permission not granted: $permission');
    }
    return ok;
  }

  /// 执行合并：对选中的组写回合并结果并删除被合并掉的成员。
  ///
  /// 执行前会：
  ///   1. 申请读写权限
  ///   2. 导出合并前全量快照 + 写入撤销点（失败则中止，宁可不合并）
  ///
  /// 逐组独立处理：单组失败不影响其他组，失败明细在 [MergeOutcome] 中返回。
  ///
  /// **不在此处做二次确认弹窗** —— 由调用方（UI）负责确认。
  Future<MergeOutcome> executeMerge(List<MergePlanEntry> entries) async {
    if (isDesktop()) {
      return const MergeOutcome(
        success: false,
        errorMessage: '通讯录功能仅在移动端（Android/iOS）可用',
      );
    }
    if (entries.isEmpty) {
      return const MergeOutcome(success: true, mergedGroups: 0);
    }

    if (!await requestWritePermission()) {
      return const MergeOutcome(
        success: false,
        errorMessage: '未获得通讯录写入权限，操作已取消',
      );
    }

    _updateStatus(status: ContactBackupStatus.exporting);

    // ---- 1. 写撤销点（先于任何修改）----
    try {
      await _writeUndoPoint(entries);
    } catch (e) {
      _logger.error('Failed to write undo point, aborting merge', e);
      _updateStatus(
        status: ContactBackupStatus.error,
        errorMessage: '创建撤销点失败，已中止合并：$e',
      );
      return MergeOutcome(
        success: false,
        errorMessage: '创建撤销点失败，已中止合并（未做任何修改）',
      );
    }

    // ---- 2. 逐组执行 ----
    var mergedGroups = 0;
    var modified = 0;
    var deleted = 0;
    final failures = <String>[];
    final blockedIds = <String>[];

    for (final entry in entries) {
      try {
        // 写回载体：保留原 metadata（update 要求），只替换业务字段
        final carrier = _buildCarrier(entry);
        if (carrier == null) {
          failures.add('组（${entry.beforeNames.join(" / ")}）：载体 id 缺失');
          continue;
        }
        await FlutterContacts.update(carrier);
        modified++;
        mergedGroups++;

        // 删除组内其余成员（号码已合并进载体，这些是冗余记录）
        if (entry.merged.deletedIds.isNotEmpty) {
          final result = await _deleteContacts(entry.merged.deletedIds);
          deleted += result.deletedIds.length;
          blockedIds.addAll(result.blockedIds);
        }
      } catch (e) {
        _logger.error('Merge group failed: ${entry.beforeNames}', e);
        failures.add('组（${entry.beforeNames.join(" / ")}）：$e');
      }
    }

    // 删除被系统拦截：号码已合并成功，但冗余记录仍在。
    // 这不是"合并失败"，必须如实区分 —— 否则用户以为已经干净了。
    final deleteBlocked = blockedIds.isNotEmpty;

    _updateStatus(
      status: failures.isEmpty
          ? ContactBackupStatus.completed
          : ContactBackupStatus.error,
      errorMessage: failures.isEmpty ? null : failures.join('\n'),
    );

    return MergeOutcome(
      success: failures.isEmpty,
      mergedGroups: mergedGroups,
      modifiedCount: modified,
      deletedCount: deleted,
      blockedDeleteIds: blockedIds,
      deleteBlocked: deleteBlocked,
      failures: failures,
      errorMessage: failures.isEmpty ? null : failures.join('\n'),
    );
  }

  /// 删除一组联系人，并**复核是否真的删掉了**。
  ///
  /// 两个必须这么做的理由：
  ///
  /// 1. **不用 `deleteAll`**：它删的是 `RawContacts`（同步账户原始记录层），
  ///    语义上是"删除某账号的同步记录"，会触碰账户同步管控 —— MIUI 等 ROM
  ///    对这里管控严格，容易拦截。单个 `delete()` 删的是 `Contacts`
  ///    （聚合后的联系人层，用户视角的"删除联系人"），一般不拦。
  ///    （证据：插件 DeleteImpl.kt:20 = Contacts，DeleteAllImpl.kt:31 = RawContacts）
  ///
  /// 2. **不信任返回值**：`DeleteAllImpl` 丢弃了 `ContentResolver.delete()` 的
  ///    返回行数，因此系统若让删除变成静默 no-op（返回 0、不抛异常），
  ///    插件照样上报成功 → Dart 侧毫无察觉。故删除后必须重新读取复核。
  Future<_DeleteResult> _deleteContacts(List<String> ids) async {
    final deleted = <String>[];
    final blocked = <String>[];

    for (final id in ids) {
      try {
        await FlutterContacts.delete(id);
      } catch (e) {
        _logger.warning('delete($id) threw: $e');
      }
    }

    // 复核：重新读取，看这些 id 是否真的消失了
    try {
      final remaining = await FlutterContacts.getAll(
        properties: ContactProperties.none,
      );
      final remainingIds = remaining.map((c) => c.id).whereType<String>().toSet();
      for (final id in ids) {
        if (remainingIds.contains(id)) {
          blocked.add(id);
        } else {
          deleted.add(id);
        }
      }
    } catch (e) {
      // 复核失败时保守处理：不声称删掉了（宁可让用户手动确认）
      _logger.warning('delete verification failed: $e');
      blocked.addAll(ids);
    }

    if (blocked.isNotEmpty) {
      _logger.warning(
        'delete blocked by system for ${blocked.length}/${ids.length} ids',
      );
    }
    return _DeleteResult(deletedIds: deleted, blockedIds: blocked);
  }

  /// 写撤销点：合并前全量 vcf 快照 + 变更日志
  Future<void> _writeUndoPoint(List<MergePlanEntry> entries) async {
    // 全量快照：同样用 allProperties，保证撤销时能完整还原
    final all = await FlutterContacts.getAll(
      properties: ContactProperties.allProperties,
    );
    final vCard = FlutterContacts.vCard.exportAll(all);

    // 变更日志：只针对本次会被改动的联系人
    final byId = <String, Contact>{};
    for (final c in all) {
      if (c.id != null) byId[c.id!] = c;
    }

    final modified = <UndoModifiedEntry>[];
    final deleted = <UndoDeletedEntry>[];
    final seenIds = <String>{};

    for (final e in entries) {
      final carrierId = e.merged.carrierId;
      if (carrierId != null && seenIds.add(carrierId)) {
        final before = byId[carrierId];
        if (before != null) {
          modified.add(
            UndoModifiedEntry(id: carrierId, before: before.toJson()),
          );
        }
      }
      for (final id in e.merged.deletedIds) {
        if (!seenIds.add(id)) continue;
        final c = byId[id];
        if (c != null) {
          deleted.add(UndoDeletedEntry(id: id, contact: c.toJson()));
        }
      }
    }

    await ContactUndoService.save(
      log: MergeUndoLog(
        createdAt: DateTime.now(),
        schemeVersion: kContactHashSchemeVersion,
        modified: modified,
        deleted: deleted,
      ),
      vCardContent: vCard,
    );
  }

  /// 用合并结果 + 原 metadata 构造写回对象。
  ///
  /// `Contact` 字段全 `final`，不能 copyWith，只能整体重新构造。
  /// **metadata 必须保留**（`contact.dart:35` 注释：required when updating），
  /// 否则更新可能失败。名称变更写 Name.first（displayName 只读，
  /// 由平台从 Name 子字段生成 —— 见 contact.dart:39）。
  Contact? _buildCarrier(MergePlanEntry entry) {
    final original = entry.group.members.first;
    if (original.id == null) return null;

    final name = entry.merged.nameChanged
        ? Name(
            first: entry.merged.name,
            // 名称拼接后无法可靠拆回姓/名，故只写 first、last 留空。
            // 副作用：按姓氏排序时这些联系人会聚在一起（已在方案中记录）。
            last: '',
            middle: null,
            prefix: null,
            suffix: null,
            phoneticFirst: null,
            phoneticMiddle: null,
            phoneticLast: null,
          )
        : original.name;

    return Contact(
      id: original.id,
      name: name,
      phones: entry.merged.phones,
      emails: entry.merged.emails,
      addresses: entry.merged.addresses,
      organizations: entry.merged.organizations,
      websites: entry.merged.websites,
      socialMedias: entry.merged.socialMedias,
      events: entry.merged.events,
      relations: entry.merged.relations,
      notes: entry.merged.notes,
      // 必须保留原 metadata（update 要求），否则更新可能失败
      metadata: original.metadata,
    );
  }

  /// 撤销最近一次合并。
  ///
  /// 顺序很关键：**先重建被删的，再回滚被改的**。
  /// 被删联系人无法用原 id 重建（create 由系统分配新 id）。
  Future<MergeOutcome> undoLastMerge() async {
    if (isDesktop()) {
      return const MergeOutcome(
        success: false,
        errorMessage: '通讯录功能仅在移动端（Android/iOS）可用',
      );
    }

    final log = await ContactUndoService.read();
    if (log == null || log.isEmpty) {
      return const MergeOutcome(
        success: false,
        errorMessage: '没有可用的撤销点',
      );
    }

    if (!await requestWritePermission()) {
      return const MergeOutcome(
        success: false,
        errorMessage: '未获得通讯录写入权限，操作已取消',
      );
    }

    var restored = 0;
    var skippedExisting = 0;
    var rolledBack = 0;
    final failures = <String>[];

    // 0) 先读现有 id 集合：撤销点的 deleted 列表记录的是"合并前打算删的"，
    //    但可能有一部分当时被系统拦截（MIUI 等）根本没删掉。
    //    对这些仍存在的 id 再 create 一次会产生重复联系人，必须跳过。
    final existingIds = <String>{};
    try {
      final current = await FlutterContacts.getAll(
        properties: ContactProperties.none,
      );
      existingIds.addAll(current.map((c) => c.id).whereType<String>());
    } catch (e) {
      _logger.warning('Undo: failed to read current ids, '
          'will skip recreate-verification: $e');
    }

    // 1) 重建被删除的联系人（id 置 null，由系统重新分配并更新 contact_uri）
    for (final d in log.deleted) {
      // 仍在 → 当时删除被拦截，无需（也不该）重建
      if (existingIds.contains(d.id)) {
        skippedExisting++;
        continue;
      }
      try {
        final c = Contact.fromJson(d.contact);
        // 重建必须清空 id：create() 由系统分配新 id，沿用旧 id 行为未定义。
        // Contact 字段全 final，故整体重新构造。同时去掉 metadata
        // （metadata 描述的是"已存在记录的已取属性"，新建对象不该带）。
        await FlutterContacts.create(
          Contact(
            name: c.name,
            phones: c.phones,
            emails: c.emails,
            addresses: c.addresses,
            organizations: c.organizations,
            websites: c.websites,
            socialMedias: c.socialMedias,
            events: c.events,
            relations: c.relations,
            notes: c.notes,
            photo: c.photo,
          ),
        );
        restored++;
      } catch (e) {
        _logger.error('Undo: restore deleted ${d.id} failed', e);
        failures.add('重建 ${d.id}：$e');
      }
    }

    // 2) 回滚被修改的联系人（before 是 allProperties 完整快照，
    //    满足 update 的部分更新语义 —— 不会丢字段）
    for (final m in log.modified) {
      try {
        final before = Contact.fromJson(m.before);
        await FlutterContacts.update(before);
        rolledBack++;
      } catch (e) {
        _logger.error('Undo: rollback modified ${m.id} failed', e);
        failures.add('回滚 ${m.id}：$e');
      }
    }

    if (failures.isEmpty) {
      await ContactUndoService.clear();
    }

    _logger.info(
      'Undo done: restored=$restored, skippedExisting=$skippedExisting, '
      'rolledBack=$rolledBack, failures=${failures.length}',
    );

    return MergeOutcome(
      success: failures.isEmpty,
      restoredCount: restored,
      skippedExistingCount: skippedExisting,
      rolledBackCount: rolledBack,
      failures: failures,
      errorMessage: failures.isEmpty ? null : failures.join('\n'),
    );
  }

  /// 有界清理本地历史备份文件：按修改时间倒序保留 [keep] 份，其余删除。
  ///
  /// 只处理本服务自己生成的 `contacts_backup_*.vcf`，不碰目录内其他文件。
  /// 正在上传中的文件不会被删——上传通常在数秒内完成，而这里只有积累到
  /// 超过 [keep] 份时才触发，二者时间尺度不同。失败仅记日志，不影响备份。
  Future<void> _pruneOldBackups(Directory dir, {required int keep}) async {
    try {
      if (!await dir.exists()) return;
      final files = await dir
          .list()
          .where((e) =>
              e is File && p.basename(e.path).startsWith('contacts_backup_'))
          .cast<File>()
          .toList();
      if (files.length <= keep) return;

      final stats = <MapEntry<File, DateTime>>[];
      for (final f in files) {
        stats.add(MapEntry(f, (await f.stat()).modified));
      }
      // 新的在前
      stats.sort((a, b) => b.value.compareTo(a.value));

      for (final entry in stats.skip(keep)) {
        try {
          await entry.key.delete();
          _logger.info('Pruned old contact backup: ${entry.key.path}');
        } catch (e) {
          _logger.warning('Failed to prune ${entry.key.path}: $e');
        }
      }
    } catch (e) {
      _logger.warning('Prune contact backups failed: $e');
    }
  }

  /// 校验取到的联系人确实带数据（至少绝大多数要有号码）。
  ///
  /// 见 startBackup 中的说明：漏传 properties 会让 getAll 返回只有姓名的空壳对象。
  /// 这里在导出之前拦截，避免把无效备份传到服务器。
  ///
  /// 判定用「全空则失败」而非「有任一空则失败」：真实通讯录里偶尔存在
  /// 只有邮箱、无号码的联系人，那是合法数据，不该因此中止备份。
  void _assertContactDataPresent(List<Contact> contacts) {
    final withPhone = contacts.where((c) => c.phones.isNotEmpty).length;
    if (withPhone == 0) {
      throw StateError(
        '读取到的 ${contacts.length} 个联系人全部没有号码，'
        '疑似 ContactProperties 未生效（应传 allProperties），'
        '已中止以免产生无号码的无效备份',
      );
    }
    if (withPhone < contacts.length) {
      // 部分联系人无号码属正常，仅记录以便排查
      _logger.info(
        '${contacts.length - withPhone} of ${contacts.length} contacts have no phone number',
      );
    }
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

/// 重复扫描结果
class ContactScanResult {
  const ContactScanResult({
    required this.totalContacts,
    required this.entries,
    required this.autoMergeableCount,
    required this.suspiciousCount,
    required this.blockedCount,
  });

  /// 通讯录总人数
  final int totalContacts;

  /// 全部重复组（含合并预览），已按"可信度"排序（存疑组靠后）
  final List<MergePlanEntry> entries;

  /// 可自动合并的组数
  final int autoMergeableCount;

  /// 其中存疑的组数（默认不勾选）
  final int suspiciousCount;

  /// 不可自动合并的组数（需手动处理）
  final int blockedCount;

  /// 是否有重复
  bool get hasDuplicates => entries.isNotEmpty;
}

/// 合并 / 撤销的执行结果
class MergeOutcome {
  const MergeOutcome({
    required this.success,
    this.mergedGroups = 0,
    this.modifiedCount = 0,
    this.deletedCount = 0,
    this.blockedDeleteIds = const [],
    this.deleteBlocked = false,
    this.restoredCount = 0,
    this.skippedExistingCount = 0,
    this.rolledBackCount = 0,
    this.failures = const [],
    this.errorMessage,
  });

  final bool success;

  /// 成功合并的组数
  final int mergedGroups;

  /// 被修改的联系人数（撤销时为回滚数）
  final int modifiedCount;

  /// 被删除的联系人数
  final int deletedCount;

  /// 删除被系统拦截的联系人 id（号码已合并，但这些冗余记录没能删掉）
  final List<String> blockedDeleteIds;

  /// 是否发生了"删除被系统拦截"（区别于合并失败）
  final bool deleteBlocked;

  /// 撤销时：重建的被删联系人数
  final int restoredCount;

  /// 撤销时：因"当时其实没删掉"而跳过的重建数（避免产生重复联系人）
  final int skippedExistingCount;

  /// 撤销时：回滚的被改联系人数
  final int rolledBackCount;

  /// 逐项失败原因（部分成功时非空）
  final List<String> failures;

  final String? errorMessage;

  /// 是否部分成功（有成功项也有失败项）
  bool get isPartial => failures.isNotEmpty && mergedGroups > 0;
}

/// 删除一组联系人的结果（区分"真删掉了"与"被系统拦截"）
class _DeleteResult {
  const _DeleteResult({required this.deletedIds, required this.blockedIds});

  /// 复核确认已消失的 id
  final List<String> deletedIds;

  /// 复核仍在的 id（删除被系统拦截）
  final List<String> blockedIds;
}

/// 服务器上的备份文件信息
class BackupFileInfo {
  const BackupFileInfo({
    required this.fileId,
    required this.name,
    required this.size,
    required this.updatedAt,
  });

  final String fileId;
  final String name;
  final int size;
  final DateTime updatedAt;
}
