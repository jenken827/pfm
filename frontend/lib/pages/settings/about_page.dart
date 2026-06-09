import 'package:flutter/material.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/design/design_tokens.dart';

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
    final colorScheme = Theme.of(context).colorScheme;
    final screenWidth = MediaQuery.of(context).size.width;
    final isMobileScreen = screenWidth < PfmConstraints.mobileBreakpoint;

    return Scaffold(
      body: SafeArea(
        child: ListView(
          padding: EdgeInsets.symmetric(
            horizontal: isMobileScreen ? PfmSpacing.lg : PfmSpacing.xxxxl,
            vertical: PfmSpacing.xxl,
          ),
          children: [
            // 返回按钮 + 标题
            Row(
              children: [
                IconButton(
                  icon: const Icon(Icons.arrow_back, size: 20),
                  onPressed: () => Navigator.pop(context),
                ),
                const SizedBox(width: PfmSpacing.sm),
                Text(al.about_title, style: PfmTypography.titleLarge.copyWith(color: colorScheme.onSurface)),
              ],
            ),
            const SizedBox(height: PfmSpacing.xxxl),

            // 应用图标和名称
            _buildAppHeader(),
            const SizedBox(height: PfmSpacing.xxxl),

            // 版本信息
            Card(
              child: Column(
                children: [
                  _buildInfoTile(
                    icon: Icons.info_outline,
                    iconBackground: PfmColors.primarySurface,
                    iconColor: PfmColors.primary,
                    title: al.about_version,
                    subtitle: _packageInfo?.version ?? al.common_loading,
                  ),
                  const Divider(height: 1, indent: 56),
                  _buildInfoTile(
                    icon: Icons.build_outlined,
                    iconBackground: PfmColors.archiveSurface,
                    iconColor: PfmColors.archive,
                    title: al.about_buildNumber,
                    subtitle: '${_packageInfo?.buildNumber}-{{commit-hash}}',
                  ),
                  const Divider(height: 1, indent: 56),
                  _buildInfoTile(
                    icon: Icons.inventory_2_outlined,
                    iconBackground: PfmColors.documentSurface,
                    iconColor: PfmColors.document,
                    title: al.about_packageName,
                    subtitle: _packageInfo?.packageName ?? al.common_loading,
                  ),
                ],
              ),
            ),

            const SizedBox(height: PfmSpacing.xxxl),

            // 技术信息
            Text(al.about_techStack, style: PfmTypography.titleSmall.copyWith(color: colorScheme.onSurfaceVariant)),
            const SizedBox(height: PfmSpacing.md),
            Card(
              child: Column(
                children: [
                  _buildInfoTile(
                    icon: Icons.code,
                    iconBackground: PfmColors.primarySurface,
                    iconColor: PfmColors.primary,
                    title: al.about_frontend,
                    subtitle: 'Flutter',
                  ),
                  const Divider(height: 1, indent: 56),
                  _buildInfoTile(
                    icon: Icons.storage,
                    iconBackground: PfmColors.successSurface,
                    iconColor: PfmColors.success,
                    title: al.about_backend,
                    subtitle: 'Go',
                  ),
                ],
              ),
            ),

            const SizedBox(height: PfmSpacing.xxxxl),

            // 版权信息
            _buildCopyright(al),
          ],
        ),
      ),
    );
  }

  Widget _buildAppHeader() {
    final colorScheme = Theme.of(context).colorScheme;
    return Center(
      child: Column(
        children: [
          Container(
            width: 80,
            height: 80,
            decoration: BoxDecoration(
              color: PfmColors.primarySurface,
              borderRadius: BorderRadius.circular(PfmRadius.xl),
            ),
            child: const Icon(Icons.folder_outlined, size: 40, color: PfmColors.primary),
          ),
          const SizedBox(height: PfmSpacing.lg),
          Text(
            _packageInfo?.appName ?? 'PFM',
            style: PfmTypography.displayMedium.copyWith(color: colorScheme.onSurface),
          ),
          const SizedBox(height: PfmSpacing.xs),
          Text(
            'Personal File Manager',
            style: PfmTypography.bodyMedium.copyWith(color: colorScheme.onSurfaceVariant),
          ),
        ],
      ),
    );
  }

  Widget _buildSectionHeader(String title) {
    return Padding(
      padding: const EdgeInsets.only(bottom: PfmSpacing.md, left: PfmSpacing.xs),
      child: Text(
        title,
        style: PfmTypography.titleSmall.copyWith(
          color: Theme.of(context).colorScheme.onSurfaceVariant,
        ),
      ),
    );
  }

  Widget _buildInfoTile({
    required IconData icon,
    required String title,
    required String subtitle,
    Color? iconColor,
    Color? iconBackground,
  }) {
    final colorScheme = Theme.of(context).colorScheme;
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: PfmSpacing.lg, vertical: PfmSpacing.md),
      child: Row(
        children: [
          Container(
            width: 36, height: 36,
            decoration: BoxDecoration(
              color: iconBackground ?? colorScheme.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(PfmRadius.sm),
            ),
            child: Icon(icon, color: iconColor ?? colorScheme.onSurfaceVariant, size: 18),
          ),
          const SizedBox(width: PfmSpacing.md),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title, style: PfmTypography.bodyLarge.copyWith(color: colorScheme.onSurface, fontWeight: FontWeight.w500)),
                const SizedBox(height: PfmSpacing.xs),
                Text(subtitle, style: PfmTypography.labelSmall.copyWith(color: colorScheme.onSurfaceVariant)),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildCopyright(AppLocalizations al) {
    final colorScheme = Theme.of(context).colorScheme;
    return Center(
      child: Column(
        children: [
          Text('© 2026 PFM', style: PfmTypography.labelSmall.copyWith(color: colorScheme.onSurfaceVariant)),
          const SizedBox(height: PfmSpacing.xs),
          Text(al.about_appTitle, style: PfmTypography.labelSmall.copyWith(color: colorScheme.onSurfaceVariant)),
        ],
      ),
    );
  }
}
