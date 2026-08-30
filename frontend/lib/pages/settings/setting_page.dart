import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/api/fm_repository.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/models/casdoor.dart';
import 'package:pfm/pages/settings/about_page.dart';
import 'package:pfm/router/router.dart';
import 'package:pfm/services/services.dart';
import 'package:pfm/design/design_tokens.dart';
import 'package:pfm/design/components/components.dart';
import 'package:talker_flutter/talker_flutter.dart';

/// 设置页面
class SettingPage extends StatefulWidget {
  const SettingPage({super.key});

  @override
  State<SettingPage> createState() => _SettingPageState();
}

class _SettingPageState extends State<SettingPage> {
  bool _isAdmin = false;

  @override
  void initState() {
    super.initState();
    _checkAdminRole();
  }

  Future<void> _checkAdminRole() async {
    final ls = GetIt.I.get<LocalStorage>();
    // 先从本地存储检查
    if (ls.user?.roles.contains('pfm_admin') ?? false) {
      if (mounted) setState(() => _isAdmin = true);
      return;
    }

    // 本地没有 roles，从后端获取
    try {
      final repo = FmRepository();
      final result = await repo.getMyRoles();
      if (result.success && result.data != null) {
        final isAdmin = result.data!.isAdmin;
        // 更新本地存储
        if (ls.user != null) {
          final user = ls.user!;
          final updatedUser = User(
            id: user.id,
            name: user.name,
            createdTime: user.createdTime,
            updatedTime: user.updatedTime,
            avatar: user.avatar,
            email: user.email,
            phone: user.phone,
            roles: result.data!.roles.map((e) => e.toString()).toList(),
          );
          await ls.saveUser(updatedUser);
        }
        if (mounted) setState(() => _isAdmin = isAdmin);
      }
    } catch (e) {
      // 忽略错误
    }
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context);
    final ls = GetIt.I.get<LocalStorage>();
    final casdoorConfig = ls.casdoorConfig;
    final isLoggedIn = ls.isLoggedIn;

    final screenWidth = MediaQuery.of(context).size.width;
    final isMobile = screenWidth < PfmConstraints.mobileBreakpoint;

    return Scaffold(
      body: SafeArea(
        child: ListView(
          padding: EdgeInsets.symmetric(
            horizontal: isMobile ? PfmSpacing.lg : PfmSpacing.xxxxl,
            vertical: PfmSpacing.xxl,
          ),
          children: [
            // 页面标题
            Padding(
              padding: const EdgeInsets.only(bottom: PfmSpacing.xxxl),
              child: Text(
                al.settings_title,
                style: PfmTypography.displayLarge.copyWith(
                  color: Theme.of(context).colorScheme.onSurface,
                ),
              ),
            ),

            // 服务器配置信息
            _SectionHeader(title: al.settings_serverConfig),
            Card(
              child: Column(
                children: [
                  _SettingsTile(
                    icon: Icons.dns_outlined,
                    iconBackground: PfmColors.primarySurface,
                    iconColor: PfmColors.primary,
                    title: 'API Server',
                    subtitle: ls.fileServer ?? al.common_notConfigured,
                    onTap: isLoggedIn
                        ? null
                        : () => _showServerConfigDialog(context),
                  ),
                  const Divider(height: 1, indent: 56),
                  _SettingsTile(
                    icon: Icons.key,
                    iconBackground: PfmColors.archiveSurface,
                    iconColor: PfmColors.archive,
                    title: 'Casdoor',
                    subtitle:
                        casdoorConfig?.serverUrl ?? al.common_notConfigured,
                    onTap: isLoggedIn
                        ? null
                        : () => const LoginCasdoorConfigRoute().push(context),
                  ),
                  const Divider(height: 1, indent: 56),
                  _SettingsTile(
                    icon: Icons.person_outline,
                    iconBackground: PfmColors.documentSurface,
                    iconColor: PfmColors.document,
                    title: al.settings_currentUser,
                    subtitle: ls.user?.name ?? al.common_notLoggedIn,
                    onTap: null,
                  ),
                ],
              ),
            ),
            if (isLoggedIn)
              Padding(
                padding: const EdgeInsets.only(
                  top: PfmSpacing.sm,
                  left: PfmSpacing.sm,
                ),
                child: Text(
                  al.settings_changeServerHint,
                  style: PfmTypography.labelSmall.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                ),
              ),

            const SizedBox(height: PfmSpacing.xxxl),

            // 应用设置
            _SectionHeader(title: al.settings_appSettings),
            Card(
              child: Column(
                children: [
                  _SettingsTile(
                    icon: Icons.palette_outlined,
                    iconBackground: PfmColors.imageSurface,
                    iconColor: PfmColors.image,
                    title: al.settings_themeSetting,
                    subtitle: al.settings_themeSubtitle,
                    onTap: () => const ThemeSettingRoute().push(context),
                  ),
                  const Divider(height: 1, indent: 56),
                  _SettingsTile(
                    icon: Icons.language,
                    iconBackground: PfmColors.primarySurface,
                    iconColor: PfmColors.primary,
                    title: al.settings_languageSetting,
                    subtitle: al.settings_languageSubtitle,
                    onTap: () => _showLanguageDialog(context),
                  ),
                  const Divider(height: 1, indent: 56),
                  _SettingsTile(
                    icon: Icons.tune_outlined,
                    iconBackground: PfmColors.videoSurface,
                    iconColor: PfmColors.video,
                    title: al.settings_uploadSetting,
                    subtitle: al.settings_uploadSubtitle,
                    onTap: () => _showUploadSettingsDialog(context),
                  ),
                ],
              ),
            ),

            const SizedBox(height: PfmSpacing.xxxl),

            // 数据管理
            _SectionHeader(title: '数据管理'),
            Card(
              child: Column(
                children: [
                  if (_isAdmin) ...[
                    _SettingsTile(
                      icon: Icons.storage_outlined,
                      iconBackground: PfmColors.primarySurface,
                      iconColor: PfmColors.primary,
                      title: al.settings_storageProvider,
                      subtitle: al.settings_storageProviderSubtitle,
                      onTap: () =>
                          const StorageProviderSettingRoute().push(context),
                    ),
                    const Divider(height: 1, indent: 56),
                    _SettingsTile(
                      icon: Icons.dns_outlined,
                      iconBackground: PfmColors.successSurface,
                      iconColor: PfmColors.success,
                      title: al.settings_serverBackup,
                      subtitle: al.settings_serverBackupSubtitle,
                      onTap: () => const ServerBackupRoute().push(context),
                    ),
                    const Divider(height: 1, indent: 56),
                  ],
                  // 客户端自动备份仅桌面端：移动端受分区存储限制，目录整树授权
                  // 被系统禁止(Download/Android/存储根)，无法可靠扫描本地目录，
                  // 故隐藏入口，移动端用户改为手动选择文件上传。
                  if (!Platform.isAndroid && !Platform.isIOS) ...[
                    _SettingsTile(
                      icon: Icons.phone_android_outlined,
                      iconBackground: PfmColors.archiveSurface,
                      iconColor: PfmColors.archive,
                      title: al.settings_autoBackup,
                      subtitle: al.settings_autoBackupSubtitle,
                      onTap: () => const ClientBackupRoute().push(context),
                    ),
                  ],
                  // 相册备份仅移动端：用设备类型判断(Platform.isAndroid/isIOS)。
                  // 不能复用上面的 isMobile(屏幕宽度<600)——大屏Android Pad
                  // (如华为MatePad)宽度>600会被误判为桌面而隐藏入口
                  if (Platform.isAndroid || Platform.isIOS) ...[
                    const Divider(height: 1, indent: 56),
                    _SettingsTile(
                      icon: Icons.photo_library_outlined,
                      iconBackground: PfmColors.imageSurface,
                      iconColor: PfmColors.image,
                      title: al.settings_albumBackup,
                      subtitle: al.settings_albumBackupSubtitle,
                      onTap: () => const AlbumBackupRoute().push(context),
                    ),
                    const Divider(height: 1, indent: 56),
                    _SettingsTile(
                      icon: Icons.contacts_outlined,
                      iconBackground: PfmColors.documentSurface,
                      iconColor: PfmColors.document,
                      title: al.settings_contactBackup,
                      subtitle: al.settings_contactBackupSubtitle,
                      onTap: () => const ContactBackupRoute().push(context),
                    ),
                  ],
                  if (_isAdmin) ...[
                    const Divider(height: 1, indent: 56),
                    _SettingsTile(
                      icon: Icons.security,
                      iconBackground: PfmColors.errorSurface,
                      iconColor: PfmColors.error,
                      title: al.permission_title,
                      subtitle: al.permission_subtitle,
                      onTap: () =>
                          const PermissionManagementRoute().push(context),
                    ),
                  ],
                ],
              ),
            ),

            const SizedBox(height: PfmSpacing.xxxl),

            // 其他
            _SectionHeader(title: '其他'),
            Card(
              child: Column(
                children: [
                  _SettingsTile(
                    icon: Icons.speed_outlined,
                    iconBackground: PfmColors.warningSurface,
                    iconColor: PfmColors.warning,
                    title: al.settings_logLevel,
                    subtitle: al.settings_logLevelSubtitle,
                    onTap: () => _showLogLevelDialog(context),
                  ),
                  const Divider(height: 1, indent: 56),
                  _SettingsTile(
                    icon: Icons.description_outlined,
                    iconBackground: PfmColors.documentSurface,
                    iconColor: PfmColors.document,
                    title: al.settings_viewLog,
                    subtitle: al.settings_viewLogSubtitle,
                    onTap: () => const LogRoute().push(context),
                  ),
                  const Divider(height: 1, indent: 56),
                  _SettingsTile(
                    icon: Icons.bug_report_outlined,
                    iconBackground: PfmColors.warningSurface,
                    iconColor: PfmColors.warning,
                    title: al.settings_exportLog,
                    subtitle: al.settings_exportLogSubtitle,
                    onTap: () => _exportLogs(context),
                  ),
                  const Divider(height: 1, indent: 56),
                  _SettingsTile(
                    icon: Icons.info_outline,
                    iconBackground: PfmColors.primarySurface,
                    iconColor: PfmColors.primary,
                    title: al.settings_about,
                    subtitle: al.settings_aboutSubtitle,
                    onTap: () => Navigator.push(
                      context,
                      MaterialPageRoute(
                        builder: (context) => const AboutPage(),
                      ),
                    ),
                  ),
                ],
              ),
            ),

            const SizedBox(height: PfmSpacing.xxxl),

            // 退出登录
            Card(
              child: _SettingsTile(
                icon: Icons.logout,
                iconBackground: PfmColors.errorSurface,
                iconColor: PfmColors.error,
                title: al.settings_logout,
                subtitle: al.settings_logoutSubtitle,
                onTap: () => _showLogoutDialog(context),
              ),
            ),

            const SizedBox(height: PfmSpacing.xxxxl),
          ],
        ),
      ),
    );
  }

  /// 显示设置文件服务器对话框
  void _showServerConfigDialog(BuildContext context) {
    final al = AppLocalizations.of(context);
    final ls = GetIt.I.get<LocalStorage>();
    final controller = TextEditingController(text: ls.fileServer ?? '');
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(al.login_setServer),
        content: TextField(
          controller: controller,
          decoration: InputDecoration(
            hintText: al.login_serverHint,
            prefixIcon: Icon(Icons.link),
          ),
          autofocus: true,
          keyboardType: TextInputType.url,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: Text(al.common_cancel),
          ),
          FilledButton(
            onPressed: () async {
              final url = controller.text.trim();
              if (url.isNotEmpty) {
                await ls.saveFileServer(url);
                if (ctx.mounted) {
                  Navigator.pop(ctx);
                  ScaffoldMessenger.of(
                    ctx,
                  ).showSnackBar(SnackBar(content: Text(al.login_serverSaved)));
                }
              }
            },
            child: Text(al.common_save),
          ),
        ],
      ),
    );
  }

  void _showLanguageDialog(BuildContext context) {
    final al = AppLocalizations.of(context);
    final ls = GetIt.I.get<LocalStorage>();
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(al.settings_chooseLanguage),
        content: RadioGroup<String>(
          groupValue: ls.themeLanguage.locale,
          onChanged: (String? value) async {
            if (value != null) {
              final tl = ls.themeLanguage.copyWith(locale: value);
              await ls.saveThemeLanguage(tl);
              if (ctx.mounted) Navigator.pop(ctx);
            }
          },
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              RadioListTile<String>(
                title: Text(al.settings_chinese),
                value: 'zh',
              ),
              RadioListTile<String>(
                title: Text(al.settings_english),
                value: 'en',
              ),
            ],
          ),
        ),
      ),
    );
  }

  /// 显示日志等级选择对话框
  Future<void> _showLogLevelDialog(BuildContext context) async {
    final al = AppLocalizations.of(context);
    final ls = GetIt.I.get<LocalStorage>();

    final current = LogLevel.values.firstWhere(
      (l) => l.name == ls.logLevel,
      orElse: () => kDebugMode ? LogLevel.debug : LogLevel.info,
    );
    final options = <(String, LogLevel)>[
      (al.log_levelDebug, LogLevel.debug),
      (al.log_levelInfo, LogLevel.info),
      (al.log_levelWarning, LogLevel.warning),
      (al.log_levelError, LogLevel.error),
    ];

    final selected = await showDialog<LogLevel>(
      context: context,
      builder: (ctx) => SimpleDialog(
        title: Text(al.settings_logLevel),
        children: [
          for (final (label, level) in options)
            ListTile(
              title: Text(label),
              // 避免 RadioListTile 的 groupValue/onChanged 废弃告警，用勾选标记当前值
              trailing: level == current
                  ? Icon(
                      Icons.check,
                      size: 20,
                      color: Theme.of(ctx).colorScheme.primary,
                    )
                  : null,
              onTap: () => Navigator.pop(ctx, level),
            ),
        ],
      ),
    );
    if (selected == null || selected == current) return;

    await ls.saveLogLevel(selected.name);
    setLogLevel(selected);
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(al.settings_logLevelApplied(selected.name))),
    );
  }

  void _showLogoutDialog(BuildContext context) async {
    final al = AppLocalizations.of(context);
    final confirmed = await PfmDialogs.confirm(
      context,
      title: al.settings_logout,
      message: al.settings_logoutConfirm,
      confirmLabel: '退出',
      isDestructive: true,
    );
    if (confirmed && context.mounted) {
      await GetIt.I.get<LocalStorage>().clearLoginInfo();
      GetIt.I.get<BackgroundService>().cancelAllTransfers();
      if (context.mounted) {
        const LoginRoute().go(context);
      }
    }
  }

  void _showUploadSettingsDialog(BuildContext context) {
    final ls = GetIt.I.get<LocalStorage>();
    final chunkSizeController = TextEditingController(
      text: (ls.uploadChunkSize ~/ 1024).toString(),
    );
    final thresholdController = TextEditingController(
      text: (ls.smallFileThreshold ~/ (1024 * 1024)).toString(),
    );
    final uploadConcurrencyController = TextEditingController(
      text: ls.uploadConcurrency.toString(),
    );
    final downloadConcurrencyController = TextEditingController(
      text: ls.downloadConcurrency.toString(),
    );

    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('传输设置'),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: chunkSizeController,
                decoration: const InputDecoration(
                  labelText: '分片大小 (KB)',
                  hintText: '建议 1024-4096（1-4MB）',
                  helperText: '网络不好时建议调小',
                ),
                keyboardType: TextInputType.number,
              ),
              const SizedBox(height: 16),
              TextField(
                controller: thresholdController,
                decoration: const InputDecoration(
                  labelText: '小文件阈值 (MB)',
                  hintText: '低于此大小一次性上传',
                  helperText: '超过此大小使用分片上传',
                ),
                keyboardType: TextInputType.number,
              ),
              const SizedBox(height: 16),
              TextField(
                controller: uploadConcurrencyController,
                decoration: const InputDecoration(
                  labelText: '上传并发数',
                  hintText: '建议 1-5',
                  helperText: '同时上传的任务数量',
                ),
                keyboardType: TextInputType.number,
              ),
              const SizedBox(height: 16),
              TextField(
                controller: downloadConcurrencyController,
                decoration: const InputDecoration(
                  labelText: '下载并发数',
                  hintText: '建议 1-5',
                  helperText: '同时下载的任务数量',
                ),
                keyboardType: TextInputType.number,
              ),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () async {
              final chunkKB = int.tryParse(chunkSizeController.text) ?? 2048;
              final thresholdMB = int.tryParse(thresholdController.text) ?? 5;
              final uploadCon =
                  int.tryParse(uploadConcurrencyController.text) ?? 3;
              final downloadCon =
                  int.tryParse(downloadConcurrencyController.text) ?? 3;
              await ls.saveUploadChunkSize(chunkKB * 1024);
              await ls.saveSmallFileThreshold(thresholdMB * 1024 * 1024);
              await ls.saveUploadConcurrency(uploadCon.clamp(1, 10));
              await ls.saveDownloadConcurrency(downloadCon.clamp(1, 10));
              if (ctx.mounted) {
                Navigator.pop(ctx);
                ScaffoldMessenger.of(
                  ctx,
                ).showSnackBar(const SnackBar(content: Text('传输设置已保存')));
              }
            },
            child: const Text('保存'),
          ),
        ],
      ),
    );
  }

  Future<void> _exportLogs(BuildContext context) async {
    try {
      final files = await getLogFiles();
      if (files.isEmpty) {
        if (context.mounted) {
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(const SnackBar(content: Text('暂无日志文件')));
        }
        return;
      }

      // 显示日志文件信息
      final totalSize = files.fold<int>(0, (sum, f) => sum + f.lengthSync());
      final sizeStr = totalSize > 1024 * 1024
          ? '${(totalSize / (1024 * 1024)).toStringAsFixed(1)} MB'
          : '${(totalSize / 1024).toStringAsFixed(1)} KB';

      if (context.mounted) {
        final confirm = await showDialog<bool>(
          context: context,
          builder: (ctx) => AlertDialog(
            title: const Text('导出日志'),
            content: Text('找到 ${files.length} 个日志文件，共 $sizeStr\n\n是否导出？'),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(ctx, false),
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed: () => Navigator.pop(ctx, true),
                child: const Text('导出'),
              ),
            ],
          ),
        );

        if (confirm == true) {
          await shareLogFiles();
        }
      }
    } catch (e) {
      if (context.mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('导出失败: $e')));
      }
    }
  }
}

/// 设置项组件 - Clean Minimal 风格
class _SettingsTile extends StatelessWidget {
  const _SettingsTile({
    required this.icon,
    required this.title,
    required this.subtitle,
    required this.onTap,
    this.iconColor,
    this.iconBackground,
  });

  final IconData icon;
  final String title;
  final String subtitle;
  final VoidCallback? onTap;
  final Color? iconColor;
  final Color? iconBackground;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;

    return InkWell(
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.symmetric(
          horizontal: PfmSpacing.lg,
          vertical: PfmSpacing.md,
        ),
        child: Row(
          children: [
            Container(
              width: 36,
              height: 36,
              decoration: BoxDecoration(
                color: iconBackground ?? colorScheme.surfaceContainerHighest,
                borderRadius: BorderRadius.circular(PfmRadius.sm),
              ),
              child: Icon(
                icon,
                color: iconColor ?? colorScheme.onSurfaceVariant,
                size: 18,
              ),
            ),
            const SizedBox(width: PfmSpacing.md),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    title,
                    style: PfmTypography.bodyLarge.copyWith(
                      color: colorScheme.onSurface,
                      fontWeight: FontWeight.w500,
                    ),
                  ),
                  const SizedBox(height: PfmSpacing.xs),
                  Text(
                    subtitle,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: PfmTypography.labelSmall.copyWith(
                      color: colorScheme.onSurfaceVariant,
                    ),
                  ),
                ],
              ),
            ),
            if (onTap != null)
              Icon(
                Icons.chevron_right,
                size: 18,
                color: colorScheme.onSurfaceVariant,
              ),
          ],
        ),
      ),
    );
  }
}

/// 分组标题组件 - Clean Minimal 风格
class _SectionHeader extends StatelessWidget {
  const _SectionHeader({required this.title});

  final String title;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(
        bottom: PfmSpacing.md,
        left: PfmSpacing.xs,
      ),
      child: Text(
        title,
        style: PfmTypography.titleSmall.copyWith(
          color: Theme.of(context).colorScheme.onSurfaceVariant,
        ),
      ),
    );
  }
}
