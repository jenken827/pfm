import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:get_it/get_it.dart';
import 'package:go_router/go_router.dart';
import 'package:pfm/blocs/blocs.dart';
import 'package:pfm/services/services.dart';
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
      bottomNavigationBar: BlocProvider.value(
        value: GetIt.I.get<TransferBloc>(),
        child: Builder(
          builder: (context) {
            final activeCount = context.select(
              (TransferBloc bloc) => bloc.state.activeTransferCount,
            );
            return BottomNavigationBar(
              selectedFontSize: 16,
              unselectedFontSize: 14,
              showUnselectedLabels: false,
              type: BottomNavigationBarType.fixed,
              items: <BottomNavigationBarItem>[
                const BottomNavigationBarItem(
                  icon: Icon(Icons.home_outlined),
                  activeIcon: Icon(Icons.home),
                  label: '主页',
                ),
                const BottomNavigationBarItem(
                  icon: Icon(Icons.folder_outlined),
                  activeIcon: Icon(Icons.folder),
                  label: '文件',
                ),
                BottomNavigationBarItem(
                  icon: _buildTransferIcon(activeCount, false),
                  activeIcon: _buildTransferIcon(activeCount, true),
                  label: al.transfer,
                ),
                const BottomNavigationBarItem(
                  icon: Icon(Icons.settings_outlined),
                  activeIcon: Icon(Icons.settings),
                  label: '设置',
                ),
              ],
              currentIndex: navigationShell.currentIndex,
              onTap: (index) => _onTap(context, index),
            );
          },
        ),
      ),
    );
  }

  /// 构建传输图标（带徽章）
  Widget _buildTransferIcon(int count, bool isActive) {
    if (count <= 0) {
      return Icon(isActive ? Icons.sync : Icons.sync_outlined);
    }
    return Stack(
      clipBehavior: Clip.none,
      children: [
        Icon(isActive ? Icons.sync : Icons.sync_outlined),
        Positioned(
          right: -6,
          top: -4,
          child: Container(
            padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 1),
            decoration: BoxDecoration(
              color: Colors.red,
              borderRadius: BorderRadius.circular(10),
            ),
            constraints: const BoxConstraints(minWidth: 16),
            child: Text(
              count > 99 ? '99+' : '$count',
              style: const TextStyle(
                color: Colors.white,
                fontSize: 10,
                fontWeight: FontWeight.bold,
              ),
              textAlign: TextAlign.center,
            ),
          ),
        ),
      ],
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
            child: BlocProvider(
              create: (context) =>
                  TransferBloc(GetIt.I.get<BackgroundService>())
                    ..add(TransferUploadSubscribeEvent())
                    ..add(TransferDownloadSubscribeEvent()),
              child: Builder(
                builder: (context) {
                  final activeCount = context.select(
                    (TransferBloc bloc) => bloc.state.activeTransferCount,
                  );
                  return NavigationRail(
                    selectedIndex: navigationShell.currentIndex,
                    onDestinationSelected: (index) => _onTap(context, index),
                    labelType: NavigationRailLabelType.all,
                    leading: const SizedBox(height: 8),
                    destinations: <NavigationRailDestination>[
                      const NavigationRailDestination(
                        icon: Icon(Icons.home_outlined),
                        selectedIcon: Icon(Icons.home),
                        label: Text('主页'),
                      ),
                      const NavigationRailDestination(
                        icon: Icon(Icons.folder_outlined),
                        selectedIcon: Icon(Icons.folder),
                        label: Text('文件'),
                      ),
                      NavigationRailDestination(
                        icon: _buildTransferIcon(activeCount, false),
                        selectedIcon: _buildTransferIcon(activeCount, true),
                        label: Text(al.transfer),
                      ),
                      const NavigationRailDestination(
                        icon: Icon(Icons.settings_outlined),
                        selectedIcon: Icon(Icons.settings),
                        label: Text('设置'),
                      ),
                    ],
                  );
                },
              ),
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
