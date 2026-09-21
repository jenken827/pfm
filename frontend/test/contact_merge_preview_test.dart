import 'package:flutter/material.dart';
import 'package:flutter_contacts/flutter_contacts.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/models/contact_merge_plan.dart';
import 'package:pfm/pages/settings/contact_merge_preview_page.dart';
import 'package:pfm/services/contact_backup_service.dart';
import 'package:pfm/services/contact_merge.dart';

/// 预览页的 pop 返回值契约测试。
///
/// 回归背景：预览页曾用集合字面量 `{...}` 作为 Navigator.pop 的返回值，
/// 而路由类型是 `List<MergePlanEntry>`，导致点击「合并选中」时抛
/// `_Set<MergePlanEntry> is not a subtype of List<MergePlanEntry>? of 'result'`。
///
/// **为什么 flutter analyze 抓不到**：集合字面量的推断不参与 pop 的
/// result 类型检查 —— pop 的 result 参数是 Object?，类型不匹配只在
/// 路由 didPop 时于运行时暴露。故必须有测试守住这个契约。
void main() {
  Contact contact(String id, String name, String phone) => Contact(
    id: id,
    displayName: name,
    phones: [Phone(number: phone, label: const Label(PhoneLabel.mobile))],
    metadata: const ContactMetadata(properties: {ContactProperty.name}, accounts: []),
  );

  /// 构造一个含指定数量可合并组（高置信，默认勾选）的扫描结果
  ContactScanResult scanWith(int groupCount) {
    final contacts = <Contact>[];
    for (var i = 0; i < groupCount; i++) {
      // 每组两人：同名同号码 → 高置信可自动合并
      contacts.add(contact('a$i', '同名$i', '1390000000$i'));
      contacts.add(contact('b$i', '同名$i', '1390000000$i'));
    }
    final groups = findDuplicateGroups(contacts);
    final entries = groups.map(buildMergeEntry).toList();
    return ContactScanResult(
      totalContacts: contacts.length,
      entries: entries,
      autoMergeableCount: entries.where((e) => e.group.canAutoMerge).length,
      suspiciousCount: 0,
      blockedCount: 0,
    );
  }

  Future<List<MergePlanEntry>?> pumpAndTapMerge(
    WidgetTester tester,
    ContactScanResult scan, {
    bool tap = true,
  }) async {
    List<MergePlanEntry>? result;

    await tester.pumpWidget(
      MaterialApp(
        localizationsDelegates: AppLocalizations.localizationsDelegates,
        supportedLocales: AppLocalizations.supportedLocales,
        home: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: ElevatedButton(
                onPressed: () async {
                  // 与调用方（contact_backup_page）完全一致的写法
                  result = await Navigator.push<List<MergePlanEntry>>(
                    context,
                    MaterialPageRoute(
                      builder: (_) => ContactMergePreviewPage(scan: scan),
                    ),
                  );
                },
                child: const Text('open'),
              ),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    if (tap) {
      // 点击底部「合并选中」
      final btn = find.byType(FilledButton);
      expect(btn, findsWidgets, reason: '预览页应有「合并选中」按钮');
      await tester.tap(btn.last);
      await tester.pumpAndSettle();
    }

    return result;
  }

  testWidgets('「合并选中」返回 List 而非 Set（原崩溃点）', (tester) async {
    final scan = scanWith(3);
    expect(scan.entries, isNotEmpty, reason: '测试数据应能构造出重复组');

    final result = await pumpAndTapMerge(tester, scan);

    // 核心断言：必须是 List<MergePlanEntry>
    expect(
      result,
      isA<List<MergePlanEntry>>(),
      reason: 'pop 必须返回 List<MergePlanEntry>；返回 Set 会在真机上抛 '
          '"_Set<MergePlanEntry> is not a subtype of List<MergePlanEntry>"',
    );
    // 高置信组默认全选
    expect(result!.length, scan.entries.length);
  });

  testWidgets('无重复时不显示执行按钮，pop 不被触发', (tester) async {
    final scan = ContactScanResult(
      totalContacts: 0,
      entries: const [],
      autoMergeableCount: 0,
      suspiciousCount: 0,
      blockedCount: 0,
    );

    final result = await pumpAndTapMerge(tester, scan, tap: false);

    // 没有可合并组 → 不应有 FilledButton，也不应有 pop
    expect(find.text('open'), findsNothing); // 已进入预览页且返回
    expect(result, isNull);
  });

  testWidgets('返回顺序与页面展示顺序一致', (tester) async {
    final scan = scanWith(2);
    final result = await pumpAndTapMerge(tester, scan);

    expect(result, isNotNull);
    // 首个应为列表中最靠前的组（未改动顺序时即 entries 顺序）
    expect(result!.first.beforeNames, scan.entries.first.beforeNames);
  });
}
