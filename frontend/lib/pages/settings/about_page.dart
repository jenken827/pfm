import 'package:flutter/material.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:pfm/l10n/app_localizations.dart';

/// 关于页面
class AboutPage extends StatefulWidget {
  const AboutPage({super.key});

  @override
  State<AboutPage> createState() => _AboutPageState();
}

class _AboutPageState extends State<AboutPage> {
  PackageInfo? _packageInfo;

  @override
  void initState() {
    super.initState();
    _loadPackageInfo();
  }

  Future<void> _loadPackageInfo() async {
    final info = await PackageInfo.fromPlatform();
    if (mounted) {
      setState(() {
        _packageInfo = info;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context)!;
    return Scaffold(
      appBar: AppBar(title: Text(al.about_title), centerTitle: true),
      body: ListView(
        children: [
          // 应用图标和名称
          _buildAppHeader(),
          const SizedBox(height: 24),

          // 版本信息
          _buildInfoTile(
            icon: Icons.info_outline,
            title: al.about_version,
            subtitle: _packageInfo?.version ?? al.common_loading,
          ),
          const Divider(height: 1),

          _buildInfoTile(
            icon: Icons.build_outlined,
            title: al.about_buildNumber,
            subtitle: '${_packageInfo?.buildNumber}-{{commit-hash}}',
          ),
          const Divider(height: 1),

          _buildInfoTile(
            icon: Icons.inventory_2_outlined,
            title: al.about_packageName,
            subtitle: _packageInfo?.packageName ?? al.common_loading,
          ),
          const SizedBox(height: 24),

          // 应用信息
          _buildSectionHeader(al.about_appInfo),
          _buildInfoTile(
            icon: Icons.description_outlined,
            title: al.about_appName,
            subtitle: _packageInfo?.appName ?? 'PFM',
          ),
          const Divider(height: 1),

          // 技术信息
          _buildSectionHeader(al.about_techStack),
          _buildInfoTile(icon: Icons.code, title: al.about_frontend, subtitle: 'Flutter'),
          const Divider(height: 1),
          _buildInfoTile(icon: Icons.storage, title: al.about_backend, subtitle: 'Go'),
          const SizedBox(height: 24),

          // 版权信息
          _buildCopyright(al),
        ],
      ),
    );
  }

  Widget _buildAppHeader() {
    return Container(
      padding: const EdgeInsets.symmetric(vertical: 32),
      child: Column(
        children: [
          // 应用图标
          Container(
            width: 80,
            height: 80,
            decoration: BoxDecoration(
              color: Theme.of(context).colorScheme.primaryContainer,
              borderRadius: BorderRadius.circular(16),
            ),
            child: Icon(
              Icons.folder_outlined,
              size: 48,
              color: Theme.of(context).colorScheme.onPrimaryContainer,
            ),
          ),
          const SizedBox(height: 16),
          // 应用名称
          Text(
            _packageInfo?.appName ?? 'PFM',
            style: Theme.of(
              context,
            ).textTheme.headlineSmall?.copyWith(fontWeight: FontWeight.bold),
          ),
          const SizedBox(height: 4),
          Text(
            'Personal File Manager',
            style: Theme.of(context).textTheme.bodyMedium?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildSectionHeader(String title) {
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

  Widget _buildInfoTile({
    required IconData icon,
    required String title,
    required String subtitle,
  }) {
    return ListTile(
      leading: Icon(icon),
      title: Text(title),
      subtitle: Text(
        subtitle,
        style: Theme.of(context).textTheme.bodyMedium?.copyWith(
          color: Theme.of(context).colorScheme.onSurfaceVariant,
        ),
      ),
    );
  }

  Widget _buildCopyright(AppLocalizations al) {
    return Padding(
      padding: const EdgeInsets.all(16),
      child: Column(
        children: [
          Text(
            '© 2026 PFM',
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 4),
          Text(
            al.about_appTitle,
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }
}
