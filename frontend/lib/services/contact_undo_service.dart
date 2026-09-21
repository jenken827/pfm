import 'dart:convert';
import 'dart:io';

import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';
import 'package:pfm/models/contact_merge_plan.dart';
import 'package:pfm/services/services.dart';

/// 通讯录合并的撤销点读写。
///
/// **为什么不能只存一个 vcf 快照**：合并做了两类破坏性操作
///   - `update()` 覆盖了联系人字段 → 撤销需要**改回原值**
///   - `deleteAll()` 删除了联系人   → 撤销需要**重建**
/// 光有一个合并前的全量 vcf，无法知道"哪些被改过、改前是什么"，也无法
/// 精确还原（导入 vcf 会与现有数据去重合并，产生新的 id 与新的重复）。
///
/// 故撤销点由两部分组成：
///   - `contacts_pre_merge_<时间>.vcf`  ← 全量兜底快照（人工救援用）
///   - `contact_merge_undo.json`        ← 变更日志（自动撤销用）
///
/// 只保留最近 1 次（每次合并覆盖），撤销仅对最近一次合并有效。
class ContactUndoService {
  ContactUndoService._();

  static const String _undoFileName = 'contact_merge_undo.json';
  static const String _snapshotPrefix = 'contacts_pre_merge_';

  /// 撤销点所在目录（应用私有支持目录，不随缓存清理丢失）
  static Future<Directory> _dir() async {
    final base = await getApplicationSupportDirectory();
    final dir = Directory(p.join(base.path, 'contact_merge'));
    if (!await dir.exists()) {
      await dir.create(recursive: true);
    }
    return dir;
  }

  /// 写入撤销点：全量快照 vcf + 变更日志。
  ///
  /// [vCardContent] 合并前的全量 vCard（可由调用方传入，避免重复导出）。
  static Future<void> save({
    required MergeUndoLog log,
    required String vCardContent,
  }) async {
    final l = getLogger();
    final dir = await _dir();

    final ts =
        '${log.createdAt.year}'
        '${log.createdAt.month.toString().padLeft(2, '0')}'
        '${log.createdAt.day.toString().padLeft(2, '0')}_'
        '${log.createdAt.hour.toString().padLeft(2, '0')}'
        '${log.createdAt.minute.toString().padLeft(2, '0')}';

    // 1) 全量快照
    final snapshot = File(p.join(dir.path, '$_snapshotPrefix$ts.vcf'));
    await snapshot.writeAsString(vCardContent);

    // 2) 变更日志
    final undoFile = File(p.join(dir.path, _undoFileName));
    await undoFile.writeAsString(jsonEncode(log.toJson()));

    // 3) 只保留最近 1 次：清理更早的快照
    await _pruneSnapshots(dir, keepFileName: p.basename(snapshot.path));

    l.info(
      'Undo point saved: ${snapshot.path} '
      '(modified=${log.modified.length}, deleted=${log.deleted.length})',
    );
  }

  /// 是否存在可用的撤销点
  static Future<bool> hasUndo() async {
    final dir = await _dir();
    final undoFile = File(p.join(dir.path, _undoFileName));
    if (!await undoFile.exists()) return false;
    try {
      final log = await _readLog(dir);
      return log != null && !log.isEmpty;
    } catch (_) {
      return false;
    }
  }

  /// 读取撤销日志（不存在或损坏返回 null）
  static Future<MergeUndoLog?> read() async {
    final dir = await _dir();
    return _readLog(dir);
  }

  static Future<MergeUndoLog?> _readLog(Directory dir) async {
    final undoFile = File(p.join(dir.path, _undoFileName));
    if (!await undoFile.exists()) return null;
    try {
      final raw = await undoFile.readAsString();
      final json = jsonDecode(raw);
      if (json is! Map) return null;
      return MergeUndoLog.fromJson(Map<String, dynamic>.from(json));
    } catch (e) {
      getLogger().warning('Failed to read undo log: $e');
      return null;
    }
  }

  /// 撤销点信息（用于 UI 展示）
  static Future<UndoPointInfo?> info() async {
    final log = await read();
    if (log == null || log.isEmpty) return null;
    return UndoPointInfo(
      createdAt: log.createdAt,
      modifiedCount: log.modified.length,
      deletedCount: log.deleted.length,
      schemeVersion: log.schemeVersion,
    );
  }

  /// 撤销点是否存在"规则版本不匹配"（跨版本的撤销可能不安全）
  static Future<bool> isSchemeCompatible(String currentVersion) async {
    final log = await read();
    if (log == null) return false;
    return log.schemeVersion == currentVersion;
  }

  /// 清除撤销点（撤销成功后调用，避免重复撤销）
  static Future<void> clear() async {
    final dir = await _dir();
    for (final name in [_undoFileName, _snapshotPrefix]) {
      await for (final e in dir.list()) {
        if (!e.path.contains(name)) continue;
        try {
          await e.delete();
        } catch (_) {}
      }
    }
    getLogger().info('Undo point cleared');
  }

  /// 只保留指定的快照文件，其余 `contacts_pre_merge_*` 删除
  static Future<void> _pruneSnapshots(
    Directory dir, {
    required String keepFileName,
  }) async {
    try {
      await for (final e in dir.list()) {
        if (e is! File) continue;
        final name = p.basename(e.path);
        if (!name.startsWith(_snapshotPrefix)) continue;
        if (name == keepFileName) continue;
        try {
          await e.delete();
        } catch (_) {}
      }
    } catch (e) {
      getLogger().warning('Prune snapshots failed: $e');
    }
  }
}

/// 撤销点展示信息
class UndoPointInfo {
  const UndoPointInfo({
    required this.createdAt,
    required this.modifiedCount,
    required this.deletedCount,
    required this.schemeVersion,
  });

  final DateTime createdAt;
  final int modifiedCount;
  final int deletedCount;
  final String schemeVersion;
}
