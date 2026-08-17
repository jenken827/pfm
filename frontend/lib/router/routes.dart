import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:get_it/get_it.dart';
import 'package:go_router/go_router.dart';
import 'package:pfm/blocs/blocs.dart';
import 'package:pfm/design/design_tokens.dart';
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

/// Clean Minimal 页面切换动画 - 淡入 + 轻微上移
CustomTransitionPage<T> buildFadeSlideTransition<T>({
  required BuildContext context,
  required GoRouterState state,
  required Widget child,
}) {
  return CustomTransitionPage<T>(
    key: state.pageKey,
    child: child,
    transitionsBuilder: (context, animation, secondaryAnimation, child) {
      return FadeTransition(
        opacity: CurveTween(curve: Curves.easeInOut).animate(animation),
        child: SlideTransition(
          position: Tween<Offset>(
            begin: const Offset(0, 0.02),
            end: Offset.zero,
          ).animate(CurveTween(curve: Curves.easeOut).animate(animation)),
          child: child,
        ),
      );
    },
    transitionDuration: PfmDuration.normal,
    reverseTransitionDuration: PfmDuration.fast,
  );
}

/// 主页面脚手架 - 包含侧边导航栏（桌面）或底部导航栏（移动端）
class MainScaffold extends StatelessWidget {
  final StatefulNavigationShell navigationShell;

  const MainScaffold({super.key, required this.navigationShell});

  @override
  Widget build(BuildContext context) {
    final screenWidth = MediaQuery.of(context).size.width;
    final isMobile = screenWidth < PfmConstraints.mobileBreakpoint;

    return BlocProvider.value(
      value: GetIt.I.get<TransferBloc>(),
      child: isMobile
          ? _buildMobileScaffold(context)
          : _buildDesktopScaffold(context),
    );
  }

  /// 移动端布局 - 底部导航栏
  Widget _buildMobileScaffold(BuildContext context) {
    final al = AppLocalizations.of(context);
    final colorScheme = Theme.of(context).colorScheme;

    return Scaffold(
      body: navigationShell,
      bottomNavigationBar: Builder(
        builder: (context) {
          final activeCount = context.select(
            (TransferBloc bloc) => bloc.state.activeTransferCount,
          );
          return NavigationBar(
            selectedIndex: navigationShell.currentIndex,
            onDestinationSelected: (index) => _onTap(context, index),
            destinations: <NavigationDestination>[
              NavigationDestination(
                icon: const Icon(Icons.home_outlined),
                selectedIcon: const Icon(Icons.home),
                label: al.nav_home,
              ),
              NavigationDestination(
                icon: const Icon(Icons.folder_outlined),
                selectedIcon: const Icon(Icons.folder),
                label: al.nav_files,
              ),
              NavigationDestination(
                icon: _buildTransferIcon(activeCount, false, colorScheme),
                selectedIcon: _buildTransferIcon(
                  activeCount,
                  true,
                  colorScheme,
                ),
                label: al.nav_transfer,
              ),
              NavigationDestination(
                icon: const Icon(Icons.settings_outlined),
                selectedIcon: const Icon(Icons.settings),
                label: al.nav_settings,
              ),
            ],
          );
        },
      ),
    );
  }

  /// 桌面端布局 - 侧边导航栏
  Widget _buildDesktopScaffold(BuildContext context) {
    final al = AppLocalizations.of(context);
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;

    return Scaffold(
      body: Row(
        children: <Widget>[
          // 侧边导航栏
          Container(
            width: PfmConstraints.navRailWidth,
            decoration: BoxDecoration(
              color: colorScheme.surface,
              border: Border(
                right: BorderSide(color: colorScheme.outline, width: 1),
              ),
            ),
            child: Column(
              children: [
                // Logo 区域
                _buildNavHeader(context),
                // 导航项
                Expanded(
                  child: SingleChildScrollView(
                    padding: const EdgeInsets.symmetric(
                      horizontal: PfmSpacing.md,
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        _buildNavSectionLabel(al.nav_home, colorScheme),
                        _buildNavItem(
                          context,
                          icon: Icons.home_outlined,
                          activeIcon: Icons.home,
                          label: al.nav_home,
                          index: 0,
                          colorScheme: colorScheme,
                        ),
                        _buildNavItem(
                          context,
                          icon: Icons.folder_outlined,
                          activeIcon: Icons.folder,
                          label: al.nav_files,
                          index: 1,
                          colorScheme: colorScheme,
                        ),
                        Builder(
                          builder: (context) {
                            final activeCount = context.select(
                              (TransferBloc bloc) =>
                                  bloc.state.activeTransferCount,
                            );
                            return _buildNavItem(
                              context,
                              icon: Icons.swap_vert,
                              activeIcon: Icons.swap_vert,
                              label: al.nav_transfer,
                              index: 2,
                              colorScheme: colorScheme,
                              badge: activeCount > 0 ? activeCount : null,
                            );
                          },
                        ),
                        const SizedBox(height: PfmSpacing.xxl),
                        _buildNavSectionLabel(al.nav_settings, colorScheme),
                        _buildNavItem(
                          context,
                          icon: Icons.settings_outlined,
                          activeIcon: Icons.settings,
                          label: al.nav_settings,
                          index: 3,
                          colorScheme: colorScheme,
                        ),
                      ],
                    ),
                  ),
                ),
                // 用户信息
                _buildNavUser(context, colorScheme),
              ],
            ),
          ),
          // 主内容区
          Expanded(child: navigationShell),
        ],
      ),
    );
  }

  /// 构建导航栏头部（Logo）
  Widget _buildNavHeader(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;

    return Container(
      padding: const EdgeInsets.all(PfmSpacing.xl),
      child: Row(
        children: [
          Container(
            width: 36,
            height: 36,
            decoration: BoxDecoration(
              color: colorScheme.primary,
              borderRadius: BorderRadius.circular(PfmRadius.md),
            ),
            child: const Center(
              child: Text(
                'P',
                style: TextStyle(
                  color: Colors.white,
                  fontWeight: FontWeight.w700,
                  fontSize: 16,
                ),
              ),
            ),
          ),
          const SizedBox(width: PfmSpacing.md),
          Text(
            'PFM',
            style: PfmTypography.titleLarge.copyWith(
              color: colorScheme.onSurface,
              letterSpacing: -0.3,
            ),
          ),
        ],
      ),
    );
  }

  /// 构建导航分组标签
  Widget _buildNavSectionLabel(String label, ColorScheme colorScheme) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(
        PfmSpacing.md,
        PfmSpacing.xxl,
        PfmSpacing.md,
        PfmSpacing.sm,
      ),
      child: Text(
        label.toUpperCase(),
        style: PfmTypography.labelSmall.copyWith(
          color: colorScheme.onSurfaceVariant,
          letterSpacing: 0.5,
        ),
      ),
    );
  }

  /// 构建导航项
  Widget _buildNavItem(
    BuildContext context, {
    required IconData icon,
    required IconData activeIcon,
    required String label,
    required int index,
    required ColorScheme colorScheme,
    int? badge,
  }) {
    final isActive = navigationShell.currentIndex == index;

    return Padding(
      padding: const EdgeInsets.symmetric(vertical: PfmSpacing.xs),
      child: Material(
        color: Colors.transparent,
        borderRadius: BorderRadius.circular(PfmRadius.md),
        child: InkWell(
          onTap: () => _onTap(context, index),
          borderRadius: BorderRadius.circular(PfmRadius.md),
          child: Container(
            padding: const EdgeInsets.symmetric(
              horizontal: PfmSpacing.md,
              vertical: PfmSpacing.md,
            ),
            decoration: BoxDecoration(
              color: isActive
                  ? colorScheme.primaryContainer
                  : Colors.transparent,
              borderRadius: BorderRadius.circular(PfmRadius.md),
            ),
            child: Row(
              children: [
                Icon(
                  isActive ? activeIcon : icon,
                  size: 20,
                  color: isActive
                      ? colorScheme.primary
                      : colorScheme.onSurfaceVariant,
                ),
                const SizedBox(width: PfmSpacing.md),
                Expanded(
                  child: Text(
                    label,
                    style: PfmTypography.labelLarge.copyWith(
                      color: isActive
                          ? colorScheme.primary
                          : colorScheme.onSurfaceVariant,
                    ),
                  ),
                ),
                if (badge != null)
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: PfmSpacing.sm,
                      vertical: PfmSpacing.xs,
                    ),
                    decoration: BoxDecoration(
                      color: colorScheme.primary,
                      borderRadius: BorderRadius.circular(PfmRadius.full),
                    ),
                    child: Text(
                      badge > 99 ? '99+' : '$badge',
                      style: PfmTypography.labelSmall.copyWith(
                        color: Colors.white,
                        fontSize: 10,
                      ),
                    ),
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  /// 构建用户信息区域
  Widget _buildNavUser(BuildContext context, ColorScheme colorScheme) {
    return Container(
      padding: const EdgeInsets.all(PfmSpacing.lg),
      decoration: BoxDecoration(
        border: Border(top: BorderSide(color: colorScheme.outline, width: 1)),
      ),
      child: Row(
        children: [
          Container(
            width: 36,
            height: 36,
            decoration: BoxDecoration(
              gradient: const LinearGradient(
                colors: [Color(0xFF667eea), Color(0xFF764ba2)],
                begin: Alignment.topLeft,
                end: Alignment.bottomRight,
              ),
              borderRadius: BorderRadius.circular(PfmRadius.full),
            ),
            child: const Center(
              child: Text(
                'K',
                style: TextStyle(
                  color: Colors.white,
                  fontWeight: FontWeight.w600,
                  fontSize: 14,
                ),
              ),
            ),
          ),
          const SizedBox(width: PfmSpacing.md),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(
                  'ken json',
                  style: PfmTypography.labelLarge.copyWith(
                    color: colorScheme.onSurface,
                  ),
                ),
                Text(
                  'ken@example.com',
                  style: PfmTypography.labelSmall.copyWith(
                    color: colorScheme.onSurfaceVariant,
                  ),
                  overflow: TextOverflow.ellipsis,
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  /// 构建传输图标（带徽章）
  Widget _buildTransferIcon(int count, bool isActive, ColorScheme colorScheme) {
    if (count <= 0) {
      return const Icon(Icons.swap_vert);
    }
    return Stack(
      clipBehavior: Clip.none,
      children: [
        const Icon(Icons.swap_vert),
        Positioned(
          right: -6,
          top: -4,
          child: Container(
            padding: const EdgeInsets.symmetric(
              horizontal: PfmSpacing.xs,
              vertical: 1,
            ),
            decoration: BoxDecoration(
              color: PfmColors.error,
              borderRadius: BorderRadius.circular(PfmRadius.full),
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

  void _onTap(BuildContext context, int index) {
    navigationShell.goBranch(
      index,
      initialLocation: index == navigationShell.currentIndex,
    );
  }
}
