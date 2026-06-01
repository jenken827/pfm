import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/pages/settings/about_page.dart';
import 'package:pfm/router/router.dart';
import 'package:pfm/services/services.dart';
import 'package:pfm/utils/platform.dart';

/// 设置页面
class SettingPage extends StatelessWidget {
  const SettingPage({super.key});

  @override
  Widget build(BuildContext context) {
    final ls = GetIt.I.get<LocalStorage>();
    final casdoorConfig = ls.casdoorConfig;
    final isLoggedIn = ls.isLoggedIn;

    return Scaffold(
      appBar: AppBar(title: const Text('设置'), centerTitle: true),
      body: ListView(
        children: [
          // 服务器配置信息
          _SectionHeader(title: '服务器配置'),
          _SettingsTile(
            icon: Icons.dns_outlined,
            title: 'API Server',
            subtitle: ls.fileServer ?? '未配置',
            onTap: isLoggedIn ? null : () => _showServerConfigDialog(context),
          ),
          const Divider(height: 1),
          _SettingsTile(
            icon: Icons.key,
            title: 'Casdoor',
            subtitle: casdoorConfig?.serverUrl ?? '未配置',
            onTap: isLoggedIn ? null : () => const LoginCasdoorConfigRoute().push(context),
          ),
          const Divider(height: 1),
          _SettingsTile(
            icon: Icons.person_outline,
            title: '当前用户',
            subtitle: ls.user?.name ?? '未登录',
            onTap: null,
          ),
          if (isLoggedIn) ...[
            const Divider(height: 1),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
              child: Text(
                '如需修改服务器配置，请先退出登录',
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
            ),
          ],
          const Divider(height: 1),

          const SizedBox(height: 16),

          // 应用设置
          _SectionHeader(title: '应用设置'),
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

          // 服务端备份
          _SettingsTile(
            icon: Icons.dns_outlined,
            title: '服务端备份',
            subtitle: '备份服务器本地文件到应用目录',
            onTap: () => const ServerBackupRoute().push(context),
          ),
          const Divider(height: 1),

          // 自动备份
          _SettingsTile(
            icon: Icons.phone_android_outlined,
            title: '自动备份',
            subtitle: '备份本地文件到服务器',
            onTap: () => const ClientBackupRoute().push(context),
          ),
          const Divider(height: 1),

          // 相册备份（仅移动端）
          if (isMobile()) ...[
            _SettingsTile(
              icon: Icons.photo_library_outlined,
              title: '相册备份',
              subtitle: '自动备份手机相册到服务器',
              onTap: () => const AlbumBackupRoute().push(context),
            ),
            const Divider(height: 1),
          ],

          // 通讯录备份（仅移动端）
          if (isMobile()) ...[
            _SettingsTile(
              icon: Icons.contacts_outlined,
              title: '通讯录备份',
              subtitle: '备份联系人为 vCard 格式',
              onTap: () => const ContactBackupRoute().push(context),
            ),
            const Divider(height: 1),
          ],

          // 上传设置
          _SettingsTile(
            icon: Icons.tune_outlined,
            title: '上传设置',
            subtitle: '分片大小和并发数',
            onTap: () => _showUploadSettingsDialog(context),
          ),
          const Divider(height: 1),

          // 导出日志
          _SettingsTile(
            icon: Icons.bug_report_outlined,
            title: '导出日志',
            subtitle: '导出日志文件用于问题分析',
            onTap: () => _exportLogs(context),
          ),
          const Divider(height: 1),

          // 关于
          _SettingsTile(
            icon: Icons.info_outline,
            title: '关于',
            subtitle: '查看应用信息',
            onTap: () => Navigator.push(
              context,
              MaterialPageRoute(builder: (context) => const AboutPage()),
            ),
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

  /// 显示设置文件服务器对话框
  void _showServerConfigDialog(BuildContext context) {
    final ls = GetIt.I.get<LocalStorage>();
    final controller = TextEditingController(text: ls.fileServer ?? '');
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('设置文件服务器'),
        content: TextField(
          controller: controller,
          decoration: const InputDecoration(
            hintText: '请输入服务器地址',
            prefixIcon: Icon(Icons.link),
          ),
          autofocus: true,
          keyboardType: TextInputType.url,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('取消'),
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
                  ).showSnackBar(const SnackBar(content: Text('服务器地址已保存')));
                }
              }
            },
            child: const Text('保存'),
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
                title: const Text('简体中文'),
                value: 'zh',
              ),
              RadioListTile<String>(
                title: const Text('English'),
                value: 'en',
              ),
            ],
          ),
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
              final uploadCon = int.tryParse(uploadConcurrencyController.text) ?? 3;
              final downloadCon = int.tryParse(downloadConcurrencyController.text) ?? 3;
              await ls.saveUploadChunkSize(chunkKB * 1024);
              await ls.saveSmallFileThreshold(thresholdMB * 1024 * 1024);
              await ls.saveUploadConcurrency(uploadCon.clamp(1, 10));
              await ls.saveDownloadConcurrency(downloadCon.clamp(1, 10));
              if (ctx.mounted) {
                Navigator.pop(ctx);
                ScaffoldMessenger.of(ctx).showSnackBar(
                  const SnackBar(content: Text('传输设置已保存')),
                );
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
          ScaffoldMessenger.of(context).showSnackBar(
            const SnackBar(content: Text('暂无日志文件')),
          );
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
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('导出失败: $e')),
        );
      }
    }
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
  final VoidCallback? onTap;
  final Color? iconColor;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      leading: Icon(icon, color: iconColor),
      title: Text(title),
      subtitle: Text(subtitle, maxLines: 1, overflow: TextOverflow.ellipsis),
      trailing: onTap != null ? const Icon(Icons.chevron_right) : null,
      onTap: onTap,
    );
  }
}

/// 分组标题组件
class _SectionHeader extends StatelessWidget {
  const _SectionHeader({required this.title});

  final String title;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
      child: Text(
        title,
        style: Theme.of(context).textTheme.titleSmall?.copyWith(
          color: Theme.of(context).colorScheme.primary,
          fontWeight: FontWeight.bold,
        ),
      ),
    );
  }
}
