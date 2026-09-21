import 'package:flutter_contacts/flutter_contacts.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pfm/services/contact_merge.dart';
import 'package:pfm/services/contact_restore.dart';

Contact c(
  String? id,
  String? name, {
  List<String> phones = const [],
  List<bool?> primary = const [],
}) => Contact(
  id: id,
  displayName: name,
  phones: [
    for (var i = 0; i < phones.length; i++)
      Phone(
        number: phones[i],
        label: const Label(PhoneLabel.mobile),
        isPrimary: i < primary.length ? primary[i] : null,
      ),
  ],
);

void main() {
  group('planRestore: 三种归宿', () {
    test('本地无匹配 → 新增', () {
      final plan = planRestore(
        imported: [c(null, '张三', phones: ['111'])],
        local: [c('L1', '李四', phones: ['222'])],
      );
      expect(plan.createCount, 1);
      expect(plan.mergeCount, 0);
      expect(plan.toCreate.first.imported.displayName, '张三');
    });

    test('号码相同但内容不同 → 并入本地', () {
      final plan = planRestore(
        imported: [c(null, '张三', phones: ['111', '333'])],
        local: [c('L1', '张三', phones: ['111'])],
      );
      expect(plan.mergeCount, 1);
      expect(plan.toMerge.first.targetId, 'L1');
      // 合并结果应含两个号码
      expect(plan.toMerge.first.merged!.phones.length, 2);
    });

    test('名称相同但号码不同 → 并入且号码取并集', () {
      final plan = planRestore(
        imported: [c(null, '张三', phones: ['222'])],
        local: [c('L1', '张三', phones: ['111'])],
      );
      expect(plan.mergeCount, 1);
      final nums = plan.toMerge.first.merged!.phones
          .map((p) => normalizePhone(p.number))
          .toSet();
      expect(nums, {'111', '222'});
    });

    test('完全一致 → 跳过', () {
      final plan = planRestore(
        imported: [c(null, '张三', phones: ['111'])],
        local: [c('L1', '张三', phones: ['111'])],
      );
      expect(plan.skipCount, 1);
      expect(plan.mergeCount, 0);
      expect(plan.createCount, 0);
    });

    test('号码格式不同但语义相同 → 跳过（不因格式差异重复合并）', () {
      final plan = planRestore(
        imported: [c(null, '张三', phones: ['139-4992-6587'])],
        local: [c('L1', '张三', phones: ['13949926587'])],
      );
      expect(plan.skipCount, 1, reason: '格式差异不应导致重复合并');
      expect(plan.mergeCount, 0);
    });

    test('label / isPrimary 差异不影响"完全一致"判定', () {
      // iOS 备份带 isPrimary，本地不带 —— 内容实为一致，应跳过
      final plan = planRestore(
        imported: [c(null, '张三', phones: ['111'], primary: [true])],
        local: [c('L1', '张三', phones: ['111'])],
      );
      expect(plan.skipCount, 1);
    });
  });

  group('planRestore: 幂等（恢复两次结果一致）', () {
    test('第二次恢复同一份备份 → 全部跳过，0 新增 0 合并', () {
      final backup = [
        c(null, '张三', phones: ['111', '222']),
        c(null, '李四', phones: ['333']),
        c(null, '王五', phones: ['444']),
      ];

      // 第一次：本地为空 → 全部新增
      final first = planRestore(imported: backup, local: []);
      expect(first.createCount, 3);

      // 模拟第一次执行后的本地状态（新增的项都有 id 了）
      final afterFirst = [
        c('N1', '张三', phones: ['111', '222']),
        c('N2', '李四', phones: ['333']),
        c('N3', '王五', phones: ['444']),
      ];

      // 第二次：应全部跳过
      final second = planRestore(imported: backup, local: afterFirst);
      expect(second.createCount, 0, reason: '幂等：不该再新增');
      expect(second.mergeCount, 0, reason: '幂等：不该再合并');
      expect(second.skipCount, 3);
      expect(second.hasChanges, isFalse);
    });

    test('已拼接过的名称再恢复 → 跳过而非重复拼接', () {
      // 之前合并产生 张三|张三(公司)
      final local = [c('L1', '张三|张三(公司)', phones: ['111'])];
      // 备份里含原始两条
      final backup = [
        c(null, '张三', phones: ['111']),
        c(null, '张三(公司)', phones: ['111']),
      ];

      final plan = planRestore(imported: backup, local: local);
      // 两条导入项都能匹配到本地那条（名称变体命中）
      expect(plan.createCount, 0, reason: '不该新增');
      // 合并结果不应把名称再拼一遍
      for (final item in plan.toMerge) {
        expect(
          item.merged!.name,
          '张三|张三(公司)',
          reason: '幂等：不应变成 张三|张三(公司)|张三',
        );
      }
      // 也不应产生重复号码
      for (final item in plan.toMerge) {
        expect(item.merged!.phones.length, 1);
      }
    });
  });

  group('planRestore: 防互相覆盖', () {
    test('两个导入项命中同一条本地记录 → 第二个改为新增', () {
      // 本地只有一条"张三/111"
      final local = [c('L1', '张三', phones: ['111'])];
      // 备份里两条都能匹配上它（一条靠号码、一条靠名称）
      final backup = [
        c(null, '张三', phones: ['111', '999']), // 号码+名称都命中
        c(null, '张三', phones: ['888']), // 名称命中
      ];

      final plan = planRestore(imported: backup, local: local);

      // 第一条并入 L1
      expect(plan.toMerge.length, 1);
      expect(plan.toMerge.first.targetId, 'L1');
      // 第二条不能再并入 L1（否则覆盖掉第一条的合并结果），改为新增
      expect(plan.toCreate.length, 1);
      expect(plan.toCreate.first.imported.phones.first.number, '888');
    });

    test('不同导入项命中不同本地记录 → 各自合并', () {
      final local = [
        c('L1', '张三', phones: ['111']),
        c('L2', '李四', phones: ['222']),
      ];
      final backup = [
        c(null, '张三', phones: ['111', '333']),
        c(null, '李四', phones: ['222', '444']),
      ];

      final plan = planRestore(imported: backup, local: local);
      expect(plan.mergeCount, 2);
      expect(plan.toMerge.map((i) => i.targetId).toSet(), {'L1', 'L2'});
    });
  });

  group('planRestore: 边界', () {
    test('空备份 → 无任何动作', () {
      final plan = planRestore(imported: [], local: [c('L1', '张三', phones: ['111'])]);
      expect(plan.items, isEmpty);
      expect(plan.hasChanges, isFalse);
    });

    test('本地为空且备份为空 → 无动作', () {
      final plan = planRestore(imported: [], local: []);
      expect(plan.hasChanges, isFalse);
    });

    test('无名称无号码的导入项 → 新增（不误判为重复）', () {
      final plan = planRestore(
        imported: [c(null, null)],
        local: [c('L1', null)],
      );
      // 两边都没有可比较的键 → 不算匹配，应新增
      expect(plan.createCount, 1);
    });

    test('统计字段自洽', () {
      final plan = planRestore(
        imported: [
          c(null, '张三', phones: ['111']), // 与本地一致 → skip
          c(null, '李四', phones: ['222', '999']), // 号码命中 → merge
          c(null, '赵六', phones: ['777']), // 无匹配 → create
        ],
        local: [
          c('L1', '张三', phones: ['111']),
          c('L2', '李四', phones: ['222']),
        ],
      );

      expect(plan.totalImported, 3);
      expect(plan.localCount, 2);
      expect(plan.skipCount + plan.mergeCount + plan.createCount, 3);
      expect(plan.hasChanges, isTrue);
    });
  });
}
