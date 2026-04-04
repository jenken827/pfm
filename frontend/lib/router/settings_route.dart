import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../pages/pages.dart';
import '../pages/settings/theme_setting_page.dart';
import '../pages/settings/storage_provider_setting_page.dart';

part 'settings_route.g.dart';

/// 设置路由
@TypedGoRoute<SettingsRoute>(
  path: '/settings',
  routes: [
    TypedGoRoute<ThemeSettingRoute>(path: 'theme'),
    TypedGoRoute<StorageProviderSettingRoute>(path: 'storage-provider'),
  ],
)
@immutable
class SettingsRoute extends GoRouteData with $SettingsRoute {
  const SettingsRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return const SettingPage();
  }
}

/// 主题设置路由
class ThemeSettingRoute extends GoRouteData with $ThemeSettingRoute {
  const ThemeSettingRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return const ThemeSettingPage();
  }
}

/// 存储介质设置路由
class StorageProviderSettingRoute extends GoRouteData
    with $StorageProviderSettingRoute {
  const StorageProviderSettingRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return const StorageProviderSettingPage();
  }
}

