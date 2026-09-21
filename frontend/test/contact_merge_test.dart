import 'package:flutter_contacts/flutter_contacts.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pfm/services/contact_merge.dart';

/// 构造测试用联系人。displayName 在真实环境中由平台生成，
/// 这里直接注入以便测试判重逻辑（判重只看 displayName + phones）。
Contact c(
  String id,
  String? name, {
  List<String> phones = const [],
  List<PhoneLabel> labels = const [],
  List<bool?> primaries = const [],
}) {
  return Contact(
    id: id,
    displayName: name,
    phones: [
      for (var i = 0; i < phones.length; i++)
        Phone(
          number: phones[i],
          label: Label(
            i < labels.length ? labels[i] : PhoneLabel.mobile,
            (i < labels.length ? labels[i] : PhoneLabel.mobile) ==
                    PhoneLabel.custom
                ? 'CELL'
                : null,
          ),
          isPrimary: i < primaries.length ? primaries[i] : null,
        ),
    ],
  );
}

void main() {
  group('normalizePhone', () {
    test('剥离 tel: 前缀与格式字符', () {
      expect(normalizePhone('tel:13949926587'), '13949926587');
      expect(normalizePhone('139-4992-6587'), '13949926587');
      expect(normalizePhone('139 4992 6587'), '13949926587');
      expect(normalizePhone('(010) 1234-5678'), '01012345678');
    });

    test('保留前导 +', () {
      expect(normalizePhone('+86 13949926587'), '+8613949926587');
      expect(normalizePhone('+1-555-0100'), '+15550100');
    });

    test('空值与纯标点返回空串', () {
      expect(normalizePhone(''), '');
      expect(normalizePhone('   '), '');
      expect(normalizePhone('---'), '');
    });
  });

  group('phoneCompareKeys: 国家码宽松比较', () {
    test('+86 与无国家码视为同一号码', () {
      final a = phoneCompareKeys('+8613949926587');
      final b = phoneCompareKeys('13949926587');
      expect(a.intersection(b), isNotEmpty,
          reason: '13949926587 是 +8613949926587 的末 11 位，应能匹配');
    });

    test('不同号码不匹配', () {
      final a = phoneCompareKeys('13949926587');
      final b = phoneCompareKeys('13949926588');
      expect(a.intersection(b), isEmpty);
    });
  });

  group('namesMatch / splitNameVariants', () {
    test('全角半角与空白归一化后相同', () {
      expect(namesMatch('张三（公司）', '张三(公司)'), isTrue);
      expect(namesMatch('张三  公司', '张三 公司'), isTrue);
    });

    test('已拼接名称的任一变体匹配即算同人（幂等关键）', () {
      expect(namesMatch('张三|张三(公司)', '张三'), isTrue);
      expect(namesMatch('张三|张三(公司)', '张三(公司)'), isTrue);
    });

    test('无关名称不匹配', () {
      expect(namesMatch('张三', '李四'), isFalse);
    });
  });

  group('buildMergedName: 拼接与幂等', () {
    test('名称不同时用 | 拼接', () {
      final members = [c('1', '张三'), c('2', '张三(公司)')];
      expect(buildMergedName(members), '张三|张三(公司)');
    });

    test('名称相同不重复拼接', () {
      final members = [c('1', '张三'), c('2', '张三')];
      expect(buildMergedName(members), '张三');
    });

    test('幂等：已拼接的不再重复拼', () {
      final members = [
        c('1', '张三|张三(公司)'),
        c('2', '张三'),
      ];
      expect(buildMergedName(members), '张三|张三(公司)');
    });

    test('全角半角差异视为同一变体', () {
      final members = [c('1', '张三（公司）'), c('2', '张三(公司)')];
      expect(buildMergedName(members), '张三（公司）');
    });
  });

  group('normalizePhoneLabel', () {
    test('CELL 自定义标签归一为 mobile（iOS 导出差异）', () {
      expect(
        normalizePhoneLabel(const Label(PhoneLabel.custom, 'CELL')),
        PhoneLabel.mobile,
      );
      expect(
        normalizePhoneLabel(const Label(PhoneLabel.custom, 'cell')),
        PhoneLabel.mobile,
      );
    });

    test('main 归一为 mobile', () {
      expect(normalizePhoneLabel(const Label(PhoneLabel.main)),
          PhoneLabel.mobile);
    });

    test('work 保持不动（不并入 mobile）', () {
      expect(normalizePhoneLabel(const Label(PhoneLabel.work)),
          PhoneLabel.work);
    });
  });

  group('findDuplicateGroups: 判重', () {
    test('名称相同 → 一组', () {
      final groups = findDuplicateGroups([c('1', '张三'), c('2', '张三')]);
      expect(groups.length, 1);
      expect(groups.first.size, 2);
    });

    test('号码相同 → 一组（含跨平台格式差异）', () {
      final groups = findDuplicateGroups([
        c('1', '张三', phones: ['13949926587']),
        c('2', '张三', phones: ['139-4992-6587']),
      ]);
      expect(groups.length, 1);
    });

    test('用户报告的场景：iOS pref 与 Android VOICE 同号码', () {
      // iOS: TEL;type=CELL;type=VOICE;type=pref:13949926587
      // Android: TEL;TYPE=VOICE:13949926587
      // 两者 label 均解析为 mobile，参数字符串不同但号码相同 → 必须判为一组
      final groups = findDuplicateGroups([
        c('ios', '张三', phones: ['13949926587'], labels: [
          PhoneLabel.mobile,
        ], primaries: [true]),
        c('android', '张三', phones: ['13949926587'], labels: [
          PhoneLabel.mobile,
        ]),
      ]);
      expect(groups.length, 1, reason: '同一号码不应因参数字符串差异被判为两人');
      final entry = buildMergeEntry(groups.first);
      expect(entry.merged.phones.length, 1, reason: '号码应去重为 1 条');
    });

    test('传递性：A~B 同号码、B~C 同名 → 三合一（并查集）', () {
      final groups = findDuplicateGroups([
        c('1', '张三', phones: ['111']),
        c('2', '李四', phones: ['111']), // 与 1 号码相同
        c('3', '李四', phones: ['222']), // 与 2 名称相同
      ]);
      expect(groups.length, 1);
      expect(groups.first.size, 3, reason: '两两配对会漏掉 A-C 的传递关系');
    });

    test('名称与号码都不同 → 不成组', () {
      final groups = findDuplicateGroups([
        c('1', '张三', phones: ['111']),
        c('2', '李四', phones: ['222']),
      ]);
      expect(groups, isEmpty);
    });

    test('单个联系人不构成重复组', () {
      expect(findDuplicateGroups([c('1', '张三')]), isEmpty);
      expect(findDuplicateGroups([]), isEmpty);
    });

    test('无名称无号码的条目不会把所有东西串起来', () {
      final groups = findDuplicateGroups([
        c('1', null),
        c('2', null),
        c('3', '张三'),
      ]);
      expect(groups, isEmpty);
    });
  });

  group('findDuplicateGroups: 存疑标记', () {
    test('号码相同但名称差异大 → 存疑', () {
      final groups = findDuplicateGroups([
        c('1', '张三', phones: ['111']),
        c('2', '老王', phones: ['111']),
      ]);
      expect(groups.first.hasConflict, isTrue);
      expect(groups.first.conflictReason, contains('共用号码'));
    });

    test('名称是包含关系 → 不存疑', () {
      final groups = findDuplicateGroups([
        c('1', '张三', phones: ['111']),
        c('2', '张三(公司)', phones: ['111']),
      ]);
      expect(groups.first.hasConflict, isFalse);
    });

    test('组内成员过多 → 存疑', () {
      final members = [
        for (var i = 0; i < 6; i++) c('$i', '同名人$i', phones: ['111']),
      ];
      final groups = findDuplicateGroups(members);
      expect(groups.first.hasConflict, isTrue);
      expect(groups.first.conflictReason, contains('判据过宽'));
    });
  });

  group('findDuplicateGroups: 不可自动合并', () {
    test('合并后名称超 200 字符 → 阻止', () {
      final long = 'A' * 150;
      final groups = findDuplicateGroups([
        c('1', long, phones: ['111']),
        c('2', '${long}BBBB', phones: ['111']),
      ]);
      expect(groups.first.canAutoMerge, isFalse);
      expect(groups.first.mergeBlockedReason, contains('200'));
    });
  });

  group('buildMergeEntry: 合并结果', () {
    test('号码取并集并去重', () {
      final groups = findDuplicateGroups([
        c('1', '张三', phones: ['111']),
        c('2', '张三', phones: ['222']),
      ]);
      final e = buildMergeEntry(groups.first);
      expect(e.merged.phones.length, 2);
      expect(
        e.merged.phones.map((p) => normalizePhone(p.number)).toSet(),
        {'111', '222'},
      );
    });

    test('重号只保留一条', () {
      final groups = findDuplicateGroups([
        c('1', '张三', phones: ['111', '111']),
        c('2', '张三', phones: ['111']),
      ]);
      final e = buildMergeEntry(groups.first);
      expect(e.merged.phones.length, 1);
    });

    test('主号优先且结果 isPrimary 取任一为 true', () {
      final groups = findDuplicateGroups([
        c('1', '张三', phones: ['111'], primaries: [true]),
        c('2', '张三', phones: ['222']),
      ]);
      final e = buildMergeEntry(groups.first);
      expect(e.merged.phones.first.isPrimary, isTrue,
          reason: '主号应排在首位且标记 isPrimary');
      expect(e.merged.phones.where((p) => p.isPrimary == true).length, 1,
          reason: '只允许一个主号');
    });

    test('名称差异 → 拼接并标记 nameChanged', () {
      final groups = findDuplicateGroups([
        c('1', '张三', phones: ['111']),
        c('2', '张三(公司)', phones: ['111']),
      ]);
      final e = buildMergeEntry(groups.first);
      expect(e.merged.name, '张三|张三(公司)');
      expect(e.merged.nameChanged, isTrue);
    });

    test('名称已一致 → 不标记 nameChanged', () {
      final groups = findDuplicateGroups([
        c('1', '张三', phones: ['111']),
        c('2', '张三', phones: ['222']),
      ]);
      final e = buildMergeEntry(groups.first);
      expect(e.merged.nameChanged, isFalse);
    });

    test('deletedIds 为除载体外的成员', () {
      final groups = findDuplicateGroups([
        c('1', '张三', phones: ['111']),
        c('2', '李四', phones: ['111']),
        c('3', '李四', phones: ['222']),
      ]);
      final e = buildMergeEntry(groups.first);
      expect(e.merged.carrierId, '1');
      expect(e.merged.deletedIds, containsAll(['2', '3']));
    });
  });

  group('端到端：跨平台备份合并', () {
    test('iOS 备份与 Android 备份的同号码联系人合为一条且名称拼接', () {
      // iOS 导出：张三（手机 139...，pref）+ 张三(公司)（同号码）
      // Android 导出：同一批人，号码无 pref 标记
      final local = [
        c('a1', '张三', phones: ['13949926587'], primaries: [true]),
        c('a2', '张三(公司)', phones: ['13949926587']),
      ];
      final groups = findDuplicateGroups(local);
      expect(groups.length, 1);
      final e = buildMergeEntry(groups.first);
      expect(e.merged.phones.length, 1);
      expect(e.merged.name, '张三|张三(公司)');

      // 幂等：对合并后的结果再扫一次，不应再产生新的合并组
      final merged = [
        Contact(
          id: 'a1',
          displayName: e.merged.name,
          phones: e.merged.phones,
        ),
      ];
      expect(findDuplicateGroups(merged), isEmpty,
          reason: '合并结果再扫描应为幂等，不产生新组');
    });
  });
}
