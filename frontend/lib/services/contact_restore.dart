/// 从备份文件恢复通讯录的合并规划（纯函数，无 IO，便于单测）。
///
/// 与"去重"的区别：去重只在**本地已有**的联系人之间做归并；恢复是
/// "导入项 vs 本地"的匹配，且要对未命中的导入项**新增**。
///
/// 三种归宿（按优先级）：
///   - [RestoreAction.skip]    与本地某条完全一致 → 跳过（保证幂等）
///   - [RestoreAction.merge]   与本地某条重复（名称∨号码） → 并入本地那条
///   - [RestoreAction.create]  本地没有 → 新增
///
/// **幂等的关键**："完全一致则跳过"。若只判断"重复就合并"，同一份备份
/// 恢复两次会产生 `张三|张三` 这类重复拼接，或反复 update 同一联系人。
library;

import 'package:flutter_contacts/flutter_contacts.dart';
import 'package:pfm/models/contact_merge_plan.dart';
import 'package:pfm/services/contact_merge.dart';

/// 单个导入项的处置方式
enum RestoreAction {
  /// 新增（本地无匹配）
  create,

  /// 并入本地已有联系人
  merge,

  /// 完全一致，跳过
  skip,
}

/// 一个导入项的处置计划
class RestoreItemPlan {
  const RestoreItemPlan({
    required this.action,
    required this.imported,
    this.targetId,
    this.targetName,
    this.merged,
    this.reason,
  });

  final RestoreAction action;

  /// 来自备份文件的原始联系人
  final Contact imported;

  /// merge 时的本地目标 id
  final String? targetId;

  /// merge 时的本地目标名称（展示用）
  final String? targetName;

  /// merge 时的合并结果
  final MergedContact? merged;

  /// 判定原因（展示用）
  final String? reason;
}

/// 恢复计划汇总
class RestorePlan {
  const RestorePlan({
    required this.items,
    required this.totalImported,
    required this.localCount,
  });

  final List<RestoreItemPlan> items;

  /// 备份文件里的联系人总数
  final int totalImported;

  /// 本地原有联系人总数
  final int localCount;

  List<RestoreItemPlan> get toCreate =>
      items.where((i) => i.action == RestoreAction.create).toList();

  List<RestoreItemPlan> get toMerge =>
      items.where((i) => i.action == RestoreAction.merge).toList();

  List<RestoreItemPlan> get toSkip =>
      items.where((i) => i.action == RestoreAction.skip).toList();

  bool get hasChanges => toCreate.isNotEmpty || toMerge.isNotEmpty;

  /// 新增 + 合并的条数（合并按"影响 1 条本地记录"计）
  int get createCount => toCreate.length;
  int get mergeCount => toMerge.length;
  int get skipCount => toSkip.length;
}

/// 规划恢复：把 [imported]（来自备份文件）与 [local]（当前通讯录）比对。
///
/// 就地合并的载体始终是**本地**联系人（[local] 中的项），因为：
///   - 本地项带 metadata，可以直接 update
///   - 导入项没有本地 id，无法作为载体
RestorePlan planRestore({
  required List<Contact> imported,
  required List<Contact> local,
}) {
  // ---- 本地索引：号码判重键 / 名称变体 → 本地下标 ----
  final byPhone = <String, int>{};
  final byName = <String, int>{};
  for (var i = 0; i < local.length; i++) {
    final c = local[i];
    for (final variant in splitNameVariants(c.displayName)) {
      byName.putIfAbsent(variant, () => i);
    }
    for (final p in c.phones) {
      for (final key in phoneDedupeKeys(p)) {
        byPhone.putIfAbsent(key, () => i);
      }
    }
  }

  final items = <RestoreItemPlan>[];
  // 同一次恢复中已被"占用"的本地下标，避免多个导入项并进同一条而互相覆盖
  final claimed = <int>{};
  // 已认领本地记录所累积覆盖的名称变体 / 号码集合（用于判断后续导入项
  // 是否已被前者代表，从而跳过而非重复新增）
  final claimedNameVariants = <int, Set<String>>{};
  final claimedPhoneKeys = <int, Set<String>>{};

  for (final imp in imported) {
    // 1) 找匹配的本地联系人
    int? hit;
    String? reason;

    for (final key in _importedPhoneKeys(imp)) {
      final i = byPhone[key];
      if (i != null) {
        hit = i;
        reason = '号码相同';
        break;
      }
    }
    if (hit == null) {
      for (final variant in splitNameVariants(imp.displayName)) {
        final i = byName[variant];
        if (i != null) {
          hit = i;
          reason = '名称相同';
          break;
        }
      }
    }

    if (hit == null) {
      items.add(
        RestoreItemPlan(
          action: RestoreAction.create,
          imported: imp,
          reason: '本地无匹配',
        ),
      );
      continue;
    }

    final target = local[hit];
    final targetId = target.id;

    // 2) 完全一致 → 跳过（幂等的关键）
    if (_isIdentical(imp, target)) {
      items.add(
        RestoreItemPlan(
          action: RestoreAction.skip,
          imported: imp,
          targetId: targetId,
          targetName: target.displayName,
          reason: '与本地完全一致',
        ),
      );
      continue;
    }

    // 3) 该本地记录已被本次恢复的前一个导入项占用。
    //
    // 此时不能盲目新增：备份里可能有"已被前一条代表"的重复项。
    // 典型场景 —— 曾做过合并，本地是 `张三|张三(公司)`，
    // 备份里仍有原始的 `张三` 与 `张三(公司)` 两条：
    //   第一条命中并 merge（名称变体已含两者）
    //   第二条若判为新增，就会把刚合并掉的东西又加回来，破坏幂等。
    //
    // 故先看本条的**内容是否已被已认领的合并结果覆盖**：名称变体 ⊆ 已并入
    // 的名称变体、且号码 ⊆ 已并入的号码 → 跳过；否则才是真正的新内容 → 新增。
    if (targetId != null && claimed.contains(hit)) {
      final claimedNames = claimedNameVariants[hit] ?? <String>{};
      final claimedPhones = claimedPhoneKeys[hit] ?? <String>{};
      final subsumed = _isSubsumed(imp, claimedNames, claimedPhones);
      items.add(
        RestoreItemPlan(
          action: subsumed ? RestoreAction.skip : RestoreAction.create,
          imported: imp,
          targetId: subsumed ? targetId : null,
          targetName: subsumed ? target.displayName : null,
          reason: subsumed ? '内容已并入本地（幂等跳过）' : '本地目标已被本次恢复占用',
        ),
      );
      continue;
    }
    if (targetId != null) {
      claimed.add(hit);
      claimedNameVariants[hit] = {
        ...?claimedNameVariants[hit],
        ...splitNameVariants(target.displayName),
      };
      claimedPhoneKeys[hit] = {
        ...?claimedPhoneKeys[hit],
        ..._importedPhoneKeys(target),
      };
    }

    // 4) 合并：复用去重内核的合并逻辑（名称拼接 + 号码并集）
    final groups = findDuplicateGroups([target, imp]);
    final merged = groups.isEmpty
        // 理论上不会走到（已判定重复），兜底为不改变本地
        ? null
        : buildMergeEntry(groups.first).merged;

    if (merged == null) {
      items.add(
        RestoreItemPlan(
          action: RestoreAction.skip,
          imported: imp,
          targetId: targetId,
          targetName: target.displayName,
          reason: '无需变更',
        ),
      );
      continue;
    }

    items.add(
      RestoreItemPlan(
        action: RestoreAction.merge,
        imported: imp,
        targetId: targetId,
        targetName: target.displayName,
        merged: merged,
        reason: reason,
      ),
    );
  }

  return RestorePlan(
    items: items,
    totalImported: imported.length,
    localCount: local.length,
  );
}

Set<String> _importedPhoneKeys(Contact c) {
  final keys = <String>{};
  for (final p in c.phones) {
    keys.addAll(phoneDedupeKeys(p));
  }
  return keys;
}

/// 判断 [imp] 的内容是否已被"已认领的名称变体 / 号码集合"完全覆盖。
///
/// 用于「同一本地记录被多个导入项命中」的场景：若本条的名称变体与号码
/// 都已在已并入的集合里，说明它已被前一条代表，应跳过而非重复新增。
bool _isSubsumed(
  Contact imp,
  Set<String> claimedNames,
  Set<String> claimedPhones,
) {
  final names = splitNameVariants(imp.displayName).toSet();
  final phones = _importedPhoneKeys(imp);

  // 名称（若有）必须都已被覆盖
  if (names.isNotEmpty && !claimedNames.containsAll(names)) return false;
  // 号码（若有）必须都已被覆盖
  if (phones.isNotEmpty && !claimedPhones.containsAll(phones)) return false;
  // 两者都空 → 无可比较内容，不能算被覆盖
  if (names.isEmpty && phones.isEmpty) return false;
  return true;
}

/// 判断导入项与本地项是否"完全一致"（用于跳过，保证幂等）。
///
/// 只比较会影响用户可见结果的字段：名称变体集合 + 号码集合。
/// 刻意**不比** label / isPrimary：跨平台导出对这些的表示本就不同
/// （iOS 写 type=pref、Android 不写），把它们纳入比较会导致
/// "内容其实一样"的备份被反复合并。
bool _isIdentical(Contact a, Contact b) {
  final na = splitNameVariants(a.displayName).toSet();
  final nb = splitNameVariants(b.displayName).toSet();
  if (na.length != nb.length || !na.containsAll(nb)) return false;

  final pa = _importedPhoneKeys(a);
  final pb = _importedPhoneKeys(b);
  if (pa.length != pb.length || !pa.containsAll(pb)) return false;

  return true;
}
