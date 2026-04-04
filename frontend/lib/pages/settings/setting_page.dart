import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/router/router.dart';
import 'package:pfm/services/services.dart';

/// 设置页面
class SettingPage extends StatelessWidget {
  const SettingPage({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('设置'), centerTitle: true),
      body: ListView(
        children: [
          // 主题设置
          _SettingsTile(
            icon: Icons.palette_outlined,
            title: '主题设置',
            subtitle: '自定义应用外观',
            onTap: () => const ThemeSettingRoute().push(context),
          ),
          const Divider(height: 1),

          // 语言设置
          _SettingsTile(
            icon: Icons.language,
            title: '语言设置',
            subtitle: '选择应用语言',
            onTap: () => _showLanguageDialog(context),
          ),
          const Divider(height: 1),

          // 存储介质设置
          _SettingsTile(
            icon: Icons.storage_outlined,
            title: '存储提供者设置',
            subtitle: '管理存储提供者',
            onTap: () => const StorageProviderSettingRoute().push(context),
          ),
          const Divider(height: 1),

          // 退出登录
          _SettingsTile(
            icon: Icons.logout,
            title: '退出登录',
            subtitle: '清除登录信息',
            iconColor: Theme.of(context).colorScheme.error,
            onTap: () => _showLogoutDialog(context),
          ),
        ],
      ),
    );
  }

  void _showLanguageDialog(BuildContext context) {
    final ls = GetIt.I.get<LocalStorage>();
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('选择语言'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            RadioListTile<String>(
              title: const Text('简体中文'),
              value: 'zh',
              groupValue: ls.themeLanguage.locale,
              onChanged: (value) async {
                if (value != null) {
                  final tl = ls.themeLanguage.copyWith(locale: value);
                  await ls.saveThemeLanguage(tl);
                  if (ctx.mounted) Navigator.pop(ctx);
                }
              },
            ),
            RadioListTile<String>(
              title: const Text('English'),
              value: 'en',
              groupValue: ls.themeLanguage.locale,
              onChanged: (value) async {
                if (value != null) {
                  final tl = ls.themeLanguage.copyWith(locale: value);
                  await ls.saveThemeLanguage(tl);
                  if (ctx.mounted) Navigator.pop(ctx);
                }
              },
            ),
          ],
        ),
      ),
    );
  }

  void _showLogoutDialog(BuildContext context) {
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('退出登录'),
        content: const Text('确定要退出登录吗？'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('取消'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
              backgroundColor: Theme.of(context).colorScheme.error,
            ),
            onPressed: () async {
              await GetIt.I.get<LocalStorage>().clearLoginInfo();
              if (ctx.mounted) Navigator.pop(ctx);
              if (context.mounted) {
                const LoginRoute().go(context);
              }
            },
            child: const Text('退出'),
          ),
        ],
      ),
    );
  }
}

/// 设置项组件
class _SettingsTile extends StatelessWidget {
  const _SettingsTile({
    required this.icon,
    required this.title,
    required this.subtitle,
    required this.onTap,
    this.iconColor,
  });

  final IconData icon;
  final String title;
  final String subtitle;
  final VoidCallback onTap;
  final Color? iconColor;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      leading: Icon(icon, color: iconColor),
      title: Text(title),
      subtitle: Text(subtitle, maxLines: 1, overflow: TextOverflow.ellipsis),
      trailing: const Icon(Icons.chevron_right),
      onTap: onTap,
    );
  }
}
