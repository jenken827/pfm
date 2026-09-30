import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../pages/pages.dart';
import '../pages/settings/theme_setting_page.dart';
import '../pages/settings/storage_provider_setting_page.dart';
import '../pages/settings/server_backup_page.dart';
import '../pages/settings/client_backup_page.dart';
import '../pages/settings/album_backup_page.dart';
import '../pages/settings/contact_backup_page.dart';
import '../pages/settings/permission_management_page.dart';
import '../pages/settings/profile_edit_page.dart';
import '../pages/settings/webdav_account_page.dart';

part 'settings_route.g.dart';

/// 设置路由
@TypedGoRoute<SettingsRoute>(
  path: '/settings',
  routes: [
    TypedGoRoute<ThemeSettingRoute>(path: 'theme'),
    TypedGoRoute<StorageProviderSettingRoute>(path: 'storage-provider'),
    TypedGoRoute<ServerBackupRoute>(path: 'server-backup'),
    TypedGoRoute<ClientBackupRoute>(path: 'client-backup'),
    TypedGoRoute<AlbumBackupRoute>(path: 'album-backup'),
    TypedGoRoute<ContactBackupRoute>(path: 'contact-backup'),
        TypedGoRoute<PermissionManagementRoute>(path: 'permission'),
        TypedGoRoute<ShareManageRoute>(path: 'share-manage'),
        TypedGoRoute<LogRoute>(path: 'logs'),
        TypedGoRoute<ProfileEditRoute>(path: 'profile'),
        TypedGoRoute<WebdavAccountRoute>(path: 'webdav-accounts'),
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

/// 服务端备份路由
class ServerBackupRoute extends GoRouteData with $ServerBackupRoute {
  const ServerBackupRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return const ServerBackupPage();
  }
}

/// 客户端备份路由
class ClientBackupRoute extends GoRouteData with $ClientBackupRoute {
  const ClientBackupRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return const ClientBackupPage();
  }
}

/// 相册备份路由
class AlbumBackupRoute extends GoRouteData with $AlbumBackupRoute {
  const AlbumBackupRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return const AlbumBackupPage();
  }
}

/// 通讯录备份路由
class ContactBackupRoute extends GoRouteData with $ContactBackupRoute {
  const ContactBackupRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return const ContactBackupPage();
  }
}

/// 权限管理路由
class PermissionManagementRoute extends GoRouteData
    with $PermissionManagementRoute {
  const PermissionManagementRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return const PermissionManagementPage();
  }
}

/// 分享管理路由
class ShareManageRoute extends GoRouteData with $ShareManageRoute {
  const ShareManageRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return const ShareManagePage();
  }
}

/// 日志查看路由
class LogRoute extends GoRouteData with $LogRoute {
  const LogRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return const LogPage();
  }
}

/// 个人资料编辑路由
class ProfileEditRoute extends GoRouteData with $ProfileEditRoute {
  const ProfileEditRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return const ProfileEditPage();
  }
}

/// WebDAV 账号管理路由
class WebdavAccountRoute extends GoRouteData with $WebdavAccountRoute {
  const WebdavAccountRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return const WebdavAccountPage();
  }
}
