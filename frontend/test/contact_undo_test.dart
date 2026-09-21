import 'dart:convert';

import 'package:flutter_contacts/flutter_contacts.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pfm/models/contact_merge_plan.dart';

/// 撤销日志的序列化回归测试。
///
/// 撤销功能完全依赖 `MergeUndoLog` 能经 jsonEncode/jsonDecode 无损往返：
///   - `deleted` 里的 Contact 用于"重建"，丢字段 = 数据永久丢失
///   - `modified` 里的 before 快照用于"回滚"，必须是完整快照，
///     否则 update() 的部分更新语义会静默丢掉未带上字段
///
/// 这里验证 Contact.toJson() 的产物确实是 JSON-safe 的普通 Map/List/String，
/// 且 fromJson 后业务字段与 metadata 都在。
void main() {
  Contact sample() => Contact(
    id: 'abc123',
    displayName: '张三|张三(公司)',
    name: Name(first: '张三|张三(公司)', last: ''),
    phones: [
      const Phone(
        number: '13949926587',
        label: Label(PhoneLabel.mobile),
        isPrimary: true,
      ),
      const Phone(number: '01012345678', label: Label(PhoneLabel.work)),
    ],
    emails: [const Email(address: 'a@b.com', label: Label(EmailLabel.work))],
    notes: [const Note(note: '备注')],
    organizations: [
      const Organization(name: '某公司', jobTitle: '工程师'),
    ],
    metadata: const ContactMetadata(
      properties: {
        ContactProperty.name,
        ContactProperty.phone,
        ContactProperty.email,
      },
      accounts: [],
    ),
  );

  group('Contact JSON 往返（撤销功能的基础）', () {
    test('toJson 产物可被 jsonEncode（JSON-safe，无 Set/枚举残留）', () {
      final json = sample().toJson();
      // 若含 Set 或枚举，jsonEncode 会抛异常
      expect(() => jsonEncode(json), returnsNormally);
      final encoded = jsonEncode(json);
      expect(encoded, contains('abc123'));
    });

    test('fromJson 还原业务字段', () {
      final json = sample().toJson();
      final decoded = Contact.fromJson(
        Map<String, dynamic>.from(jsonDecode(jsonEncode(json)) as Map),
      );

      expect(decoded.id, 'abc123');
      expect(decoded.phones.length, 2);
      expect(decoded.phones.first.number, '13949926587');
      expect(decoded.phones.first.isPrimary, isTrue);
      expect(decoded.phones.first.label.label, PhoneLabel.mobile);
      expect(decoded.phones[1].label.label, PhoneLabel.work);
      expect(decoded.emails.first.address, 'a@b.com');
      expect(decoded.notes.first.note, '备注');
      expect(decoded.organizations.first.name, '某公司');
      expect(decoded.organizations.first.jobTitle, '工程师');
    });

    test('fromJson 还原完整 metadata（update 必需，缺则回滚失败）', () {
      final json = sample().toJson();
      final decoded = Contact.fromJson(
        Map<String, dynamic>.from(jsonDecode(jsonEncode(json)) as Map),
      );

      expect(decoded.metadata, isNotNull);
      expect(
        decoded.metadata!.properties,
        containsAll([
          ContactProperty.name,
          ContactProperty.phone,
          ContactProperty.email,
        ]),
      );
    });

    test('metadata 为 null 时不崩（未 fetch 的场景）', () {
      final c = Contact(id: 'x', displayName: 'A', phones: const []);
      final json = c.toJson();
      expect(() => jsonEncode(json), returnsNormally);
      final decoded = Contact.fromJson(
        Map<String, dynamic>.from(jsonDecode(jsonEncode(json)) as Map),
      );
      expect(decoded.metadata, isNull);
    });
  });

  group('MergeUndoLog 往返', () {
    test('完整往返：modified 与 deleted 都保住', () {
      final log = MergeUndoLog(
        createdAt: DateTime(2026, 9, 20, 14, 30),
        schemeVersion: 'v2',
        modified: [
          UndoModifiedEntry(id: 'abc123', before: sample().toJson()),
        ],
        deleted: [
          UndoDeletedEntry(id: 'del1', contact: sample().toJson()),
        ],
      );

      final round = MergeUndoLog.fromJson(
        Map<String, dynamic>.from(jsonDecode(jsonEncode(log.toJson())) as Map),
      );

      expect(round.schemeVersion, 'v2');
      expect(round.createdAt, DateTime(2026, 9, 20, 14, 30));
      expect(round.modified.length, 1);
      expect(round.deleted.length, 1);
      expect(round.modified.first.id, 'abc123');
      expect(round.deleted.first.id, 'del1');

      // 回滚用的 before 快照必须能还原成可用 Contact
      final before = Contact.fromJson(round.modified.first.before);
      expect(before.phones.first.number, '13949926587');
      expect(before.metadata, isNotNull);

      // 重建用的 contact 也必须能还原
      final restored = Contact.fromJson(round.deleted.first.contact);
      expect(restored.phones.length, 2);
      expect(restored.name?.first, '张三|张三(公司)');
    });

    test('空日志 isEmpty 为 true（撤销按钮应禁用）', () {
      final log = MergeUndoLog(
        createdAt: DateTime.now(),
        schemeVersion: 'v2',
        modified: const [],
        deleted: const [],
      );
      expect(log.isEmpty, isTrue);
    });

    test('缺少可选字段时 fromJson 不崩（向前兼容）', () {
      final round = MergeUndoLog.fromJson({
        'createdAt': DateTime(2026, 1, 1).toIso8601String(),
      });
      expect(round.modified, isEmpty);
      expect(round.deleted, isEmpty);
      expect(round.schemeVersion, 'unknown');
    });
  });

  group('撤销日志覆盖合并的全部影响面', () {
    test('每个被改/被删的 id 都应能在日志中找到', () {
      final log = MergeUndoLog(
        createdAt: DateTime.now(),
        schemeVersion: 'v2',
        modified: [
          UndoModifiedEntry(id: 'c1', before: sample().toJson()),
        ],
        deleted: [
          UndoDeletedEntry(id: 'd1', contact: sample().toJson()),
          UndoDeletedEntry(id: 'd2', contact: sample().toJson()),
        ],
      );

      final modifiedIds = log.modified.map((e) => e.id).toSet();
      final deletedIds = log.deleted.map((e) => e.id).toSet();

      expect(modifiedIds, {'c1'});
      expect(deletedIds, {'d1', 'd2'});
      // 载体不应同时出现在 deleted 中（否则撤销会重复创建）
      expect(modifiedIds.intersection(deletedIds), isEmpty);
    });
  });
}
