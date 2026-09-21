import 'package:flutter_test/flutter_test.dart';
import 'package:pfm/models/contact_merge_plan.dart';

/// 撤销日志在"删除被系统拦截"场景下的正确性测试。
///
/// 背景：MIUI 等系统会拦截删除联系人。这带来一个隐蔽的撤销陷阱：
/// 撤销点的 `deleted` 列表记录的是"合并前**打算**删的"id，快照在删除之前
/// 生成，因此它无法知道哪些删除最终被系统拦下了。
///
/// 若撤销时无条件对 `deleted` 里的每一项执行 create()，对那些**其实没删掉**
/// 的联系人就会重建出重复记录 —— 撤销反而把通讯录搞得更乱。
/// 故 undoLastMerge 必须先读现有 id 集合，跳过仍存在的。
///
/// 这里守住两个不变量：
///   1. deleted 列表存的是完整快照（能还原成 Contact），这是重建的前提
///   2. 载体 id 不应出现在 deleted 中（否则撤销会重复创建载体自身）
void main() {
  MergeUndoLog logWith({
    required List<UndoModifiedEntry> modified,
    required List<UndoDeletedEntry> deleted,
  }) => MergeUndoLog(
    createdAt: DateTime(2026, 9, 20),
    schemeVersion: 'v2',
    modified: modified,
    deleted: deleted,
  );

  Map<String, dynamic> fakeContactJson(String id, String name) => {
    'id': id,
    'displayName': name,
    'phones': [
      {'number': '13900000000', 'label': {'label': 'mobile'}},
    ],
  };

  group('撤销点：删除被拦截场景', () {
    test('deleted 列表保留完整快照（重建的前提）', () {
      final log = logWith(
        modified: [],
        deleted: [
          UndoDeletedEntry(id: 'd1', contact: fakeContactJson('d1', '张三')),
        ],
      );

      final entry = log.deleted.single;
      // 必须能读出电话，否则重建出来的是个空壳联系人
      final phones = entry.contact['phones'] as List;
      expect(phones, isNotEmpty);
      expect((phones.first as Map)['number'], '13900000000');
      expect(entry.contact['displayName'], '张三');
    });

    test('载体不应出现在 deleted（否则撤销重复创建自身）', () {
      // 模拟一次真实合并：载体 c1 被改，d1 被（尝试）删
      final log = logWith(
        modified: [
          UndoModifiedEntry(id: 'c1', before: fakeContactJson('c1', '张三')),
        ],
        deleted: [
          UndoDeletedEntry(id: 'd1', contact: fakeContactJson('d1', '张三')),
        ],
      );

      final modifiedIds = log.modified.map((e) => e.id).toSet();
      final deletedIds = log.deleted.map((e) => e.id).toSet();
      expect(modifiedIds.intersection(deletedIds), isEmpty);
    });

    test('同一个 id 不应在 deleted 中重复出现', () {
      final log = logWith(
        modified: [],
        deleted: [
          UndoDeletedEntry(id: 'd1', contact: fakeContactJson('d1', 'A')),
          UndoDeletedEntry(id: 'd1', contact: fakeContactJson('d1', 'A')),
        ],
      );

      final ids = log.deleted.map((e) => e.id).toList();
      expect(ids.length, ids.toSet().length, reason: '重复 id 会导致重建两次');
    });
  });

  group('跳过仍存在的 id（撤销不产生重复）', () {
    /// 复刻 undoLastMerge 的判定：仍存在 → 跳过重建
    ({int toCreate, int skipped}) planRecreate(
      MergeUndoLog log,
      Set<String> existingIds,
    ) {
      var toCreate = 0;
      var skipped = 0;
      for (final d in log.deleted) {
        if (existingIds.contains(d.id)) {
          skipped++;
        } else {
          toCreate++;
        }
      }
      return (toCreate: toCreate, skipped: skipped);
    }

    test('删除被拦截（id 仍在）→ 跳过重建，不产生重复', () {
      final log = logWith(
        modified: [],
        deleted: [
          UndoDeletedEntry(id: 'd1', contact: fakeContactJson('d1', 'A')),
          UndoDeletedEntry(id: 'd2', contact: fakeContactJson('d2', 'B')),
        ],
      );

      // MIUI 拦截：两条都还在
      final plan = planRecreate(log, {'d1', 'd2'});
      expect(plan.toCreate, 0, reason: '都还在 → 一条都不该重建');
      expect(plan.skipped, 2);
    });

    test('正常删除（id 已消失）→ 全部重建', () {
      final log = logWith(
        modified: [],
        deleted: [
          UndoDeletedEntry(id: 'd1', contact: fakeContactJson('d1', 'A')),
          UndoDeletedEntry(id: 'd2', contact: fakeContactJson('d2', 'B')),
        ],
      );

      final plan = planRecreate(log, {});
      expect(plan.toCreate, 2);
      expect(plan.skipped, 0);
    });

    test('部分被拦截 → 只重建真正删掉的那些', () {
      final log = logWith(
        modified: [],
        deleted: [
          UndoDeletedEntry(id: 'd1', contact: fakeContactJson('d1', 'A')),
          UndoDeletedEntry(id: 'd2', contact: fakeContactJson('d2', 'B')),
        ],
      );

      // d1 删掉了、d2 被拦截
      final plan = planRecreate(log, {'d2'});
      expect(plan.toCreate, 1);
      expect(plan.skipped, 1);
    });
  });
}
