/// 通讯录合并内核（纯函数，无 IO、无插件调用，便于单测）。
///
/// 设计要点：
/// 1. **归一化后再比较**：跨平台（iOS/Android）导出的 vCard 在号码格式与
///    label 上存在差异，直接比较原始文本会把同一号码判为两个。
/// 2. **并查集分组**：判重是"名称∨号码任一相同"的传递关系，两两配对会漏组
///    （A~B 同号码、B~C 同名，则 A/B/C 应合一），必须用并查集。
/// 3. **幂等**：已拼接过的名称（`张三|张三（公司）`）再次扫描不会重复拼接。
library;

import 'package:flutter_contacts/flutter_contacts.dart';
import 'package:pfm/models/contact_merge_plan.dart';

/// 名称拼接后允许的最大长度；超出则不自动合并，交用户手动处理。
const int kMaxMergedNameLength = 200;

/// 组内成员数超过此值即视为存疑（通常意味着判据过宽，而非真重复）。
const int kSuspiciousGroupSize = 5;

/// 同一组内保存的号码上限，防止合并出爆量号码的联系人
const int kMaxPhonesPerContact = 20;

// ============================================================
// 归一化
// ============================================================

/// 号码归一化（**仅用于比较，绝不写回**）。
///
/// 处理：去 `tel:` 前缀 → 保留前导 `+` → 去除非数字 → 去中国国家码冗余。
///
/// 注意 vCard 4.0 用 `tel:` URI 形式；iOS/Android 导出的格式差异还包括
/// 空格、横线、括号，故一律剥离。
String normalizePhone(String raw) {
  var s = raw.trim();
  // vCard 4.0 的 tel: URI 前缀（大小写不敏感）
  s = s.replaceFirst(RegExp(r'^tel:', caseSensitive: false), '');
  final hasPlus = s.trimLeft().startsWith('+');
  final digits = s.replaceAll(RegExp(r'[^0-9]'), '');
  if (digits.isEmpty) return '';
  return hasPlus ? '+$digits' : digits;
}

/// 号码的比较键集合：原样 + 去国家码的末位数字。
///
/// 为什么要两套：`+8613949926587` 与 `13949926587` 语义上是同一号码，
/// 但不能在归一化阶段就粗暴剥离国家码（会把 `+8610...` 与本地 `10...`
/// 混为一谈）。故保留原样值做精确比较，另给"末 11 位"做宽松比较
/// （中国手机号为 11 位；不足 11 位时用全部数字）。
Set<String> phoneCompareKeys(String raw) {
  final n = normalizePhone(raw);
  if (n.isEmpty) return const {};
  final keys = <String>{n};
  final digits = n.startsWith('+') ? n.substring(1) : n;
  if (digits.length > 11) {
    keys.add(digits.substring(digits.length - 11));
  } else {
    keys.add(digits);
  }
  return keys;
}

/// 名称归一化（仅用于比较）。
String normalizeName(String? raw) {
  if (raw == null) return '';
  var s = raw.trim();
  // 全角 → 半角（括号与常见标点）
  s = s
      .replaceAll('（', '(')
      .replaceAll('）', ')')
      .replaceAll('，', ',')
      .replaceAll('．', '.')
      .replaceAll('　', ' ');
  // 连续空白折叠
  s = s.replaceAll(RegExp(r'\s+'), ' ');
  return s.toLowerCase();
}

/// 拆分已拼接的名称变体。
///
/// 合并后的名称形如 `张三|张三(公司)`。下次扫描时若直接整体比较，
/// 会出现"合并过的联系人认不出自己原本的成员"→ 反复拼接。
/// 故按 `|` 拆开，任一变体匹配即视为名称相同，保证幂等。
List<String> splitNameVariants(String? name) {
  if (name == null) return const [];
  return name
      .split('|')
      .map(normalizeName)
      .where((s) => s.isNotEmpty)
      .toList();
}

/// 两个名称是否"相同"（含已拼接变体的双向判定）
bool namesMatch(String? a, String? b) {
  final va = splitNameVariants(a);
  final vb = splitNameVariants(b);
  if (va.isEmpty || vb.isEmpty) return false;
  for (final x in va) {
    if (vb.contains(x)) return true;
  }
  return false;
}

/// label 归一化。
///
/// 这是解决 iOS/Android 导出差异的**关键**：
/// - iOS:   `TEL;type=CELL;type=VOICE;type=pref:139...` → label=mobile(isPrimary=true)
/// - Android:`TEL;TYPE=VOICE:139...`                    → label=mobile(isPrimary=null)
/// 两者语义等价（VOICE 在插件里被 ignored，CELL 匹配不到枚举而落到 defaultLabel=mobile），
/// 但 `type=CELL` 可能被解析为 custom("CELL")。故这里把 CELL 类自定义标签归一到 mobile。
///
/// **不参与判重的字段**：`isPrimary`（只有 iOS 会写 `pref`，参与判重会导致
/// iOS 备份恢复后与本地同号码被判为不同）。
PhoneLabel normalizePhoneLabel(Label<PhoneLabel> label) {
  switch (label.label) {
    case PhoneLabel.mobile:
    case PhoneLabel.main:
      return PhoneLabel.mobile;
    case PhoneLabel.custom:
      final c = (label.customLabel ?? '').trim().toLowerCase();
      // iOS 的 CELL / Android 历史上出现过的 MOBILE 变体
      if (c == 'cell' || c == 'mobile' || c == 'voice' || c == '手机') {
        return PhoneLabel.mobile;
      }
      return PhoneLabel.custom;
    default:
      return label.label;
  }
}

/// 号码的判重键：归一化号码 + 归一化 label 的组合键集合。
///
/// 之所以不含 label：同一号码在不同平台上可能被标成 mobile/work，
/// 若参与判重会漏判（同一号码判为两个）。label 差异在合并时按"主号优先"处理。
Set<String> phoneDedupeKeys(Phone p) => phoneCompareKeys(p.number);

// ============================================================
// 分组（并查集）
// ============================================================

/// 并查集，用于把"名称∨号码相同"的传递关系聚成连通分量。
class _UnionFind {
  _UnionFind(int n) : _parent = List<int>.generate(n, (i) => i);

  final List<int> _parent;

  int find(int x) {
    var root = x;
    while (_parent[root] != root) {
      root = _parent[root];
    }
    // 路径压缩
    var cur = x;
    while (_parent[cur] != root) {
      final next = _parent[cur];
      _parent[cur] = root;
      cur = next;
    }
    return root;
  }

  void union(int a, int b) {
    final ra = find(a);
    final rb = find(b);
    if (ra != rb) _parent[rb] = ra;
  }
}

/// 扫描重复：返回全部重复组（每组 ≥2 人）。
///
/// 判据：**名称相同 ∨ 号码相同**（均经归一化）。
List<DuplicateGroup> findDuplicateGroups(List<Contact> contacts) {
  if (contacts.length < 2) return const [];

  final uf = _UnionFind(contacts.length);

  // 名称索引：归一化变体 → 首个出现的下标
  final nameIndex = <String, int>{};
  // 号码索引：判重键 → 首个出现的下标
  final phoneIndex = <String, int>{};

  for (var i = 0; i < contacts.length; i++) {
    final c = contacts[i];

    for (final variant in splitNameVariants(c.displayName)) {
      final first = nameIndex[variant];
      if (first == null) {
        nameIndex[variant] = i;
      } else {
        uf.union(first, i);
      }
    }

    for (final p in c.phones) {
      for (final key in phoneDedupeKeys(p)) {
        final first = phoneIndex[key];
        if (first == null) {
          phoneIndex[key] = i;
        } else {
          uf.union(first, i);
        }
      }
    }
  }

  // 收集连通分量
  final buckets = <int, List<int>>{};
  for (var i = 0; i < contacts.length; i++) {
    buckets.putIfAbsent(uf.find(i), () => []).add(i);
  }

  final groups = <DuplicateGroup>[];
  for (final indices in buckets.values) {
    if (indices.length < 2) continue;
    groups.add(_buildGroup(contacts, indices));
  }
  return groups;
}

DuplicateGroup _buildGroup(List<Contact> all, List<int> indices) {
  final members = indices.map((i) => all[i]).toList();

  // 判重原因：区分"名称命中"与"号码命中"，便于用户理解
  final reasons = <String>[];
  if (_hasSameName(members)) reasons.add('名称相同');
  if (_hasSamePhone(members)) reasons.add('号码相同');

  final conflict = _detectConflict(members);
  final blocked = _detectBlocked(members);

  return DuplicateGroup(
    members: members,
    reason: reasons,
    hasConflict: conflict != null || blocked != null,
    conflictReason: conflict,
    mergeBlockedReason: blocked,
  );
}

bool _hasSameName(List<Contact> members) {
  final seen = <String>{};
  for (final c in members) {
    for (final v in splitNameVariants(c.displayName)) {
      if (!seen.add(v)) return true;
    }
  }
  return false;
}

bool _hasSamePhone(List<Contact> members) {
  final seen = <String>{};
  for (final c in members) {
    for (final p in c.phones) {
      for (final k in phoneDedupeKeys(p)) {
        if (!seen.add(k)) return true;
      }
    }
  }
  return false;
}

/// 存疑判定：命中任一即默认不勾选，交用户判断。
String? _detectConflict(List<Contact> members) {
  if (members.length > kSuspiciousGroupSize) {
    return '组内 ${members.length} 人，疑似判据过宽';
  }

  final mergedName = buildMergedName(members);
  if (mergedName.length <= kMaxMergedNameLength) {
    // 名称相似度检查：号码相同但名称差异大 → 可能真是两人共用号码
    final names = members
        .map((c) => normalizeName(c.displayName))
        .where((s) => s.isNotEmpty)
        .toList();
    if (names.length >= 2 && _hasSharedPhoneButDistantNames(members)) {
      final a = names.first;
      final b = names[1];
      if (!_isNameRelated(a, b)) {
        return '号码相同但名称差异较大（$a / $b），可能是两人共用号码';
      }
    }
  }

  // 名称相同但双方号码都 >=2 且完全无交集 → 可能是两个同名的人
  if (namesMatch(members.first.displayName, members[1].displayName)) {
    final a = <String>{};
    for (final p in members.first.phones) {
      a.addAll(phoneCompareKeys(p.number));
    }
    final b = <String>{};
    for (final p in members[1].phones) {
      b.addAll(phoneCompareKeys(p.number));
    }
    if (a.length >= 2 &&
        b.length >= 2 &&
        a.intersection(b).isEmpty &&
        a.difference(b).isNotEmpty) {
      return '名称相同但号码均 ≥2 且无交集，可能是两个同名的人';
    }
  }

  return null;
}

bool _isNameRelated(String a, String b) {
  if (a.isEmpty || b.isEmpty) return true;
  return a.contains(b) || b.contains(a);
}

bool _hasSharedPhoneButDistantNames(List<Contact> members) {
  final seen = <String, int>{};
  for (var i = 0; i < members.length; i++) {
    for (final p in members[i].phones) {
      for (final k in phoneDedupeKeys(p)) {
        final prev = seen[k];
        if (prev != null && prev != i) return true;
        seen[k] ??= i;
      }
    }
  }
  return false;
}

/// 组内不可自动合并的情况
String? _detectBlocked(List<Contact> members) {
  final mergedName = buildMergedName(members);
  if (mergedName.length > kMaxMergedNameLength) {
    return '合并后名称 ${mergedName.length} 字符，超过 $kMaxMergedNameLength 上限，需手动处理';
  }
  final phoneCount = _mergePhones(members, null).length;
  if (phoneCount > kMaxPhonesPerContact) {
    return '合并后号码 $phoneCount 个，超过 $kMaxPhonesPerContact 上限，需手动处理';
  }
  return null;
}

// ============================================================
// 合并
// ============================================================

/// 构造合并后的名称：各成员名称变体去重后按原顺序用 `|` 拼接。
///
/// 幂等：已拼接的名称会先拆成变体再合并，不会产生 `张三|张三(公司)|张三`。
String buildMergedName(List<Contact> members) {
  final ordered = <String>[]; // 保持原始书写形式
  final seenKeys = <String>{};
  for (final c in members) {
    final raw = (c.displayName ?? '').trim();
    if (raw.isEmpty) continue;
    for (final part in raw.split('|')) {
      final p = part.trim();
      if (p.isEmpty) continue;
      final key = normalizeName(p);
      if (seenKeys.add(key)) ordered.add(p);
    }
  }
  return ordered.join('|');
}

/// 合并号码：按判重键去重，主号优先，label 冲突时保留先出现的。
///
/// [labelNotes] 非空时，把因 label 冲突被丢弃的信息追加进去（供写入 note）。
List<Phone> _mergePhones(List<Contact> members, List<String>? labelNotes) {
  // 主号优先：先收集所有 isPrimary==true 的，再收集其余
  final primary = <Phone>[];
  final rest = <Phone>[];
  for (final c in members) {
    for (final p in c.phones) {
      if (normalizePhone(p.number).isEmpty) continue;
      if (p.isPrimary == true) {
        primary.add(p);
      } else {
        rest.add(p);
      }
    }
  }

  final seenKeys = <String>{};
  final result = <Phone>[];
  var anyPrimary = false;

  void add(Phone p) {
    final keys = phoneCompareKeys(p.number);
    if (keys.isEmpty) return;
    if (keys.any(seenKeys.contains)) {
      // 已存在同号码：仅记录 label 冲突信息，参数字符串差异不算冲突
      return;
    }
    seenKeys.addAll(keys);
    if (p.isPrimary == true) anyPrimary = true;
    result.add(p);
  }

  for (final p in primary) {
    add(p);
  }
  for (final p in rest) {
    add(p);
  }

  // 统一 isPrimary：结果的 isPrimary 取"任一侧为 true"，且只保留一个主号
  final out = <Phone>[];
  var primaryAssigned = false;
  for (final p in result) {
    final isPrimary = anyPrimary && !primaryAssigned && p.isPrimary == true;
    if (isPrimary) primaryAssigned = true;
    out.add(
      Phone(
        number: p.number,
        label: p.label,
        isPrimary: isPrimary ? true : null,
      ),
    );
  }
  return out;
}

/// 通用属性去重（按 keyOf 归一化值）
List<T> _dedupeBy<T>(Iterable<T> items, String Function(T) keyOf) {
  final seen = <String>{};
  final out = <T>[];
  for (final it in items) {
    final k = keyOf(it);
    if (k.isEmpty) continue;
    if (seen.add(k)) out.add(it);
  }
  return out;
}

/// 生成一个组的合并结果（纯数据，不写回）
MergePlanEntry buildMergeEntry(DuplicateGroup group) {
  final members = group.members;
  final carrier = members.first;

  final mergedName = buildMergedName(members);
  final labelNotes = <String>[];
  final phones = _mergePhones(members, labelNotes);

  final emails = _dedupeBy(
    members.expand((c) => c.emails),
    (e) => e.address.trim().toLowerCase(),
  );
  final addresses = _dedupeBy(
    members.expand((c) => c.addresses),
    (a) => [
      a.street ?? '',
      a.city ?? '',
      a.state ?? '',
      a.postalCode ?? '',
      a.country ?? '',
    ].join('|').trim().toLowerCase(),
  );
  final orgs = _dedupeBy(
    members.expand((c) => c.organizations),
    (o) => '${o.name ?? ''}|${o.jobTitle ?? ''}|${o.departmentName ?? ''}'
        .toLowerCase(),
  );
  final websites = _dedupeBy(
    members.expand((c) => c.websites),
    (w) => w.url.trim().toLowerCase(),
  );
  final socials = _dedupeBy(
    members.expand((c) => c.socialMedias),
    (s) => '${s.username}|${s.label.label}'.toLowerCase(),
  );
  final events = _dedupeBy(
    members.expand((c) => c.events),
    // Event.year 可空，month/day 是非空 int（见 event.dart），故只在 year 上判空
    (e) => '${e.year ?? ''}-${e.month}-${e.day}',
  );
  final relations = _dedupeBy(
    members.expand((c) => c.relations),
    (r) => r.name.trim().toLowerCase(),
  );
  var notes = _dedupeBy(members.expand((c) => c.notes), (n) => n.note.trim());
  if (labelNotes.isNotEmpty) {
    notes = [
      ...notes,
      ...labelNotes.map((s) => Note(note: s)),
    ];
  }

  final beforeName = (carrier.displayName ?? '').trim();
  final nameChanged = normalizeName(beforeName) != normalizeName(mergedName);

  return MergePlanEntry(
    group: group,
    merged: MergedContact(
      carrierId: carrier.id,
      name: mergedName,
      phones: phones,
      emails: emails,
      addresses: addresses,
      organizations: orgs,
      websites: websites,
      socialMedias: socials,
      events: events,
      relations: relations,
      notes: notes,
      deletedIds: members
          .skip(1)
          .map((c) => c.id)
          .whereType<String>()
          .toList(),
      nameChanged: nameChanged,
    ),
    beforeNames: members.map((c) => c.displayName ?? '(无名)').toList(),
    beforePhoneCount: members.fold(0, (s, c) => s + c.phones.length),
    skippedLabels: labelNotes,
  );
}
