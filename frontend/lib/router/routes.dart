import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'home_route.dart' as home;
import 'files_route.dart' as files;
import 'transfer_route.dart' as transfer;
import 'settings_route.dart' as settings;
import 'login_route.dart' as login;
import '../l10n/app_localizations.dart';

/// 全局路由配置
final GoRouter approuter = GoRouter(
  initialLocation: login.LoginRoute().location,
  routes: <RouteBase>[
    ...login.$appRoutes,
    // 主页底部导航 - 直接挂载到 /
    StatefulShellRoute.indexedStack(
      builder:
          (
            BuildContext context,
            GoRouterState state,
            StatefulNavigationShell navigationShell,
          ) {
            return MainScaffold(navigationShell: navigationShell);
          },
      branches: <StatefulShellBranch>[
        // 首页
        StatefulShellBranch(routes: <RouteBase>[...home.$appRoutes]),
        // 文件
        StatefulShellBranch(routes: <RouteBase>[...files.$appRoutes]),
        // 传输
        StatefulShellBranch(routes: <RouteBase>[...transfer.$appRoutes]),
        // 设置
        StatefulShellBranch(routes: <RouteBase>[...settings.$appRoutes]),
      ],
    ),
  ],
);

/// 主页面脚手架 - 包含底部导航栏
class MainScaffold extends StatelessWidget {
  final StatefulNavigationShell navigationShell;

  const MainScaffold({super.key, required this.navigationShell});

  @override
  Widget build(BuildContext context) {
    final isWideScreen = MediaQuery.of(context).size.width >= 600;
    return isWideScreen
        ? _buildRailScaffold(context)
        : _buildBottomBarScaffold(context);
  }

  /// 构建底部导航栏布局（移动端）
  Widget _buildBottomBarScaffold(BuildContext context) {
    final al = AppLocalizations.of(context)!;
    return Scaffold(
      body: navigationShell,
      bottomNavigationBar: BottomNavigationBar(
        selectedFontSize: 16,
        unselectedFontSize: 14,
        showUnselectedLabels: false,
        type: BottomNavigationBarType.fixed,
        items: <BottomNavigationBarItem>[
          BottomNavigationBarItem(
            icon: const Icon(Icons.home_outlined),
            activeIcon: const Icon(Icons.home),
            label: al.home,
          ),
          BottomNavigationBarItem(
            icon: const Icon(Icons.folder_outlined),
            activeIcon: const Icon(Icons.folder),
            label: al.files,
          ),
          BottomNavigationBarItem(
            icon: const Icon(Icons.sync_outlined),
            activeIcon: const Icon(Icons.sync),
            label: al.transfer,
          ),
          BottomNavigationBarItem(
            icon: const Icon(Icons.settings_outlined),
            activeIcon: const Icon(Icons.settings),
            label: al.settings,
          ),
        ],
        currentIndex: navigationShell.currentIndex,
        onTap: (index) => _onTap(context, index),
      ),
    );
  }

  /// 构建导航轨道布局（平板/桌面端）
  Widget _buildRailScaffold(BuildContext context) {
    final al = AppLocalizations.of(context)!;
    return Scaffold(
      body: Row(
        children: <Widget>[
          MediaQuery.removePadding(
            context: context,
            removeBottom: true,
            removeTop: true,
            removeRight: true,
            child: NavigationRail(
              selectedIndex: navigationShell.currentIndex,
              onDestinationSelected: (index) => _onTap(context, index),
              labelType: NavigationRailLabelType.all,
              leading: const SizedBox(height: 8),
              destinations: <NavigationRailDestination>[
                NavigationRailDestination(
                  icon: const Icon(Icons.home_outlined),
                  selectedIcon: const Icon(Icons.home),
                  label: Text(al.home),
                ),
                NavigationRailDestination(
                  icon: const Icon(Icons.folder_outlined),
                  selectedIcon: const Icon(Icons.folder),
                  label: Text(al.files),
                ),
                NavigationRailDestination(
                  icon: const Icon(Icons.sync_outlined),
                  selectedIcon: const Icon(Icons.sync),
                  label: Text(al.transfer),
                ),
                NavigationRailDestination(
                  icon: const Icon(Icons.settings_outlined),
                  selectedIcon: const Icon(Icons.settings),
                  label: Text(al.settings),
                ),
              ],
            ),
          ),
          const VerticalDivider(thickness: 1, width: 1),
          Expanded(child: navigationShell),
        ],
      ),
    );
  }

  void _onTap(BuildContext context, int index) {
    navigationShell.goBranch(
      index,
      initialLocation: index == navigationShell.currentIndex,
    );
  }
}
