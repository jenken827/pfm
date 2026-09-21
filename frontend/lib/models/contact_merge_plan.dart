import 'package:flutter_contacts/flutter_contacts.dart';

/// 通讯录合并计划的数据模型。
///
/// 本文件只描述"要做什么"，不含任何 IO 与插件调用，便于单测。

/// 一个待合并的重复组。
///
/// [members] 是原始读取到的联系人（不做修改，保留原始对象用于展示与撤销）。
class DuplicateGroup {
  const DuplicateGroup({
    required this.members,
    required this.reason,
    required this.hasConflict,
    this.conflictReason,
    this.mergeBlockedReason,
  });

  /// 组内成员，顺序为原始顺序（第一个作为合并载体）
  final List<Contact> members;

  /// 判重命中的原因描述（用于 UI 展示，便于用户理解为什么被归为一组）
  final List<String> reason;

  /// 是否为"存疑组"—— UI 上默认不勾选并标警告
  final bool hasConflict;

  /// 存疑原因（展示用）
  final String? conflictReason;

  /// 非空表示**不可自动合并**（如名称过长），UI 需单独列为"需手动处理"
  final String? mergeBlockedReason;

  /// 是否可自动合并
  bool get canAutoMerge => mergeBlockedReason == null;

  /// 组内人数
  int get size => members.length;
}

/// 合并结果（纯数据，尚未写回）
class MergedContact {
  const MergedContact({
    required this.carrierId,
    required this.name,
    required this.phones,
    required this.emails,
    required this.addresses,
    required this.organizations,
    required this.websites,
    required this.socialMedias,
    required this.events,
    required this.relations,
    required this.notes,
    required this.deletedIds,
    required this.nameChanged,
  });

  /// 作为写回载体的联系人 id（合并后写回它）
  final String? carrierId;

  /// 合并后的名称（用于写入 Name.first）
  final String name;

  /// 合并后的号码（已去重，按主号优先排序）
  final List<Phone> phones;

  final List<Email> emails;
  final List<Address> addresses;
  final List<Organization> organizations;
  final List<Website> websites;
  final List<SocialMedia> socialMedias;
  final List<Event> events;
  final List<Relation> relations;
  final List<Note> notes;

  /// 需要删除的联系人 id（组内除载体外的其余成员）
  final List<String> deletedIds;

  /// 名称是否发生了变化（未变则不必改 Name 字段）
  final bool nameChanged;
}

/// 一个组的合并预览，供 UI 展示「合并前 → 合并后」
class MergePlanEntry {
  const MergePlanEntry({
    required this.group,
    required this.merged,
    required this.beforeNames,
    required this.beforePhoneCount,
    required this.skippedLabels,
  });

  final DuplicateGroup group;
  final MergedContact merged;

  /// 合并前的各成员名称
  final List<String> beforeNames;

  /// 合并前的号码总数（含重复）
  final int beforePhoneCount;

  /// 因 label 冲突被丢弃的信息（写入 note 的提示文本）
  final List<String> skippedLabels;
}

/// 撤销日志。
///
/// 关键：仅靠一个 vcf 快照不足以撤销 —— 合并做了两类操作：
///   - update() 覆盖字段 → 需要改回原值（记录 before 完整快照）
///   - deleteAll() 删除联系人 → 需要重建（记录被删联系人的完整对象）
class MergeUndoLog {
  MergeUndoLog({
    required this.createdAt,
    required this.schemeVersion,
    required List<UndoModifiedEntry> modified,
    required List<UndoDeletedEntry> deleted,
  }) : modified = _dedupe(modified, (e) => e.id),
       deleted = _dedupe(deleted, (e) => e.id);

  /// 按 id 去重并保持原顺序。
  ///
  /// 在构造处强制去重，而不是依赖调用方自觉：重复的 id 会让撤销把同一个
  /// 联系人 create 两次（产生重复联系人），或在 modified 上重复回滚。
  /// 把不变量固化在数据模型里，比在调用点逐个记得去重可靠。
  static List<T> _dedupe<T>(List<T> items, String Function(T) idOf) {
    final seen = <String>{};
    final out = <T>[];
    for (final it in items) {
      if (seen.add(idOf(it))) out.add(it);
    }
    return out;
  }

  final DateTime createdAt;
  final String schemeVersion;

  /// 被修改的联系人：id → 改前的完整快照
  final List<UndoModifiedEntry> modified;

  /// 被删除的联系人完整对象
  final List<UndoDeletedEntry> deleted;

  bool get isEmpty => modified.isEmpty && deleted.isEmpty;

  Map<String, dynamic> toJson() => {
    'createdAt': createdAt.toIso8601String(),
    'schemeVersion': schemeVersion,
    'modified': modified.map((e) => e.toJson()).toList(),
    'deleted': deleted.map((e) => e.toJson()).toList(),
  };

  static MergeUndoLog fromJson(Map<String, dynamic> json) => MergeUndoLog(
    createdAt: DateTime.parse(json['createdAt'] as String),
    schemeVersion: json['schemeVersion'] as String? ?? 'unknown',
    modified: ((json['modified'] as List?) ?? [])
        .map((e) => UndoModifiedEntry.fromJson(Map<String, dynamic>.from(e)))
        .toList(),
    deleted: ((json['deleted'] as List?) ?? [])
        .map((e) => UndoDeletedEntry.fromJson(Map<String, dynamic>.from(e)))
        .toList(),
  );
}

/// 被修改联系人的撤销条目
class UndoModifiedEntry {
  const UndoModifiedEntry({required this.id, required this.before});

  final String id;

  /// 改前完整快照（必须是用 allProperties fetch 的，否则 update 的部分更新语义会丢字段）
  final Map<String, dynamic> before;

  Map<String, dynamic> toJson() => {'id': id, 'before': before};

  static UndoModifiedEntry fromJson(Map<String, dynamic> json) =>
      UndoModifiedEntry(
        id: json['id'] as String,
        before: Map<String, dynamic>.from(json['before'] as Map),
      );
}

/// 被删除联系人的撤销条目
class UndoDeletedEntry {
  const UndoDeletedEntry({required this.id, required this.contact});

  /// 原 id（重建时**不能**沿用，create 由系统分配新 id）
  final String id;

  final Map<String, dynamic> contact;

  Map<String, dynamic> toJson() => {'id': id, 'contact': contact};

  static UndoDeletedEntry fromJson(Map<String, dynamic> json) =>
      UndoDeletedEntry(
        id: json['id'] as String,
        contact: Map<String, dynamic>.from(json['contact'] as Map),
      );
}
