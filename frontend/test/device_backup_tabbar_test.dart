import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

/// 回归测试：TabBar 必须能拿到 TabController。
///
/// 真机报错（短信与通话记录备份页）：
///   FlutterError: No TabController for TabBar.
///   When creating a TabBar, you must either provide an explicit TabController
///   using the "controller" property, or you must ensure that there is a
///   DefaultTabController above the TabBar.
///   → _TabBarState._updateTabController (tabs.dart:1655)
///
/// 成因：设备备份页的 TabBar 既没有显式 controller，外层也没有
/// DefaultTabController（该页内容不是 TabBarView，而是由 _tab 驱动的单个列表）。
///
/// **flutter analyze 抓不到**：这是运行时对 InheritedWidget 的查找失败，
/// 不是类型错误。故用测试复现 Flutter 的这个契约本身。
///
/// 本文件不直接构造 DeviceBackupPage（它依赖 DeviceBackupService 的
/// MethodChannel），而是验证"有 controller → 不抛 / 无 controller → 抛"
/// 这条契约，正是该页修复所依赖的性质。
void main() {
  /// 构造一个与设备备份页同构的结构：Scaffold > Column > TabBar + Expanded
  /// （注意：内容区不是 TabBarView —— 这正是原 bug 的场景）
  Widget buildPage({TabController? controller}) {
    return MaterialApp(
      home: Scaffold(
        body: Column(
          children: [
            TabBar(
              controller: controller,
              tabs: const [
                Tab(text: 'SMS'),
                Tab(text: 'Call log'),
              ],
            ),
            const Expanded(child: Center(child: Text('content'))),
          ],
        ),
      ),
    );
  }

  testWidgets('无 controller 且无 DefaultTabController → 复现真机报错', (tester) async {
    // 必须包 MaterialApp：否则会先因缺 Directionality 报错，
    // 掩盖掉我们要验证的 TabController 错误
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Column(
            children: [
              TabBar(
                tabs: const [
                  Tab(text: 'SMS'),
                  Tab(text: 'Call log'),
                ],
              ),
              const Expanded(child: Center(child: Text('content'))),
            ],
          ),
        ),
      ),
    );

    // Flutter 在 build 期间抛错，测试框架会捕获为异常
    final err = tester.takeException();
    expect(err, isNotNull, reason: '缺少 controller 时 TabBar 应抛错');
    expect(
      err.toString(),
      contains('No TabController for TabBar'),
      reason: '应复现真机上完全相同的错误信息',
    );
  });

  testWidgets('显式 controller（本次修复的方案）→ 不抛错', (tester) async {
    late TabController controller;
    await tester.pumpWidget(
      _Host(
        onReady: (c) => controller = c,
        builder: (c) => buildPage(controller: c),
      ),
    );

    expect(tester.takeException(), isNull, reason: '提供 controller 后不应抛错');
    expect(find.text('SMS'), findsOneWidget);
    expect(find.text('Call log'), findsOneWidget);

    // 切换 index 应生效（页面靠 listener 同步 _tab）
    controller.index = 1;
    await tester.pumpAndSettle();
    expect(controller.index, 1);
  });

  testWidgets('DefaultTabController 包裹（另一种可行修法）→ 不抛错', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: DefaultTabController(
          length: 2,
          child: Builder(
            builder: (_) => Scaffold(
              body: Column(
                children: [
                  TabBar(
                    tabs: const [
                      Tab(text: 'SMS'),
                      Tab(text: 'Call log'),
                    ],
                  ),
                  const Expanded(child: Center(child: Text('content'))),
                ],
              ),
            ),
          ),
        ),
      ),
    );

    expect(tester.takeException(), isNull);
    expect(find.text('SMS'), findsOneWidget);
  });
}

/// 承载 SingleTickerProviderStateMixin，用于创建 TabController
class _Host extends StatefulWidget {
  const _Host({required this.onReady, required this.builder});

  final void Function(TabController) onReady;
  final Widget Function(TabController) builder;

  @override
  State<_Host> createState() => _HostState();
}

class _HostState extends State<_Host> with SingleTickerProviderStateMixin {
  late final TabController _c;

  @override
  void initState() {
    super.initState();
    _c = TabController(length: 2, vsync: this);
    widget.onReady(_c);
  }

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.builder(_c);
}
