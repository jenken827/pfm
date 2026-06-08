import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import 'package:go_router/go_router.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/proto/native/provider.pb.dart';
import 'package:pfm/proto/native/upload.pb.dart';
import 'package:pfm/router/files_route.dart';
import 'package:pfm/pages/files/file_preview_page.dart';
import 'package:pfm/pages/files/flyfish_viewer_page.dart';
import 'package:pfm/services/recent_files_storage.dart';
import 'package:pfm/services/local_storage.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/design/design_tokens.dart';
import 'package:pfm/design/components/components.dart';
import 'package:pfm/utils/format.dart';

/// 首页 - Clean Minimal 设计
class HomeIndexPage extends StatefulWidget {
  const HomeIndexPage({super.key});

  @override
  State<HomeIndexPage> createState() => _HomeIndexPageState();
}

class _HomeIndexPageState extends State<HomeIndexPage> {
  // 统计数据
  int _totalFiles = 0;
  String _storageUsage = '--';
  int _activeTransfers = 0;
  bool _loadingStats = true;

  // 最近文件
  List<Map<String, dynamic>> _recentFiles = [];
  bool _loadingRecent = true;

  // 存储提供者
  List<StorageProvider> _storageProviders = [];
  bool _loadingProviders = true;

  @override
  void initState() {
    super.initState();
    _loadAllData();
  }

  Future<void> _loadAllData() async {
    await Future.wait([
      _loadStats(),
      _loadRecentFiles(),
      _loadStorageProviders(),
    ]);
  }

  Future<void> _loadStats() async {
    final repository = GetIt.I.get<FmRepository>();

    // 并行获取文件列表和上传记录
    final results = await Future.wait([
      repository.getFileList(parentId: '0', offset: 0, pageSize: 1),
      repository.listUploadRecords(page: 1, pageSize: 100),
    ]);

    if (mounted) {
      setState(() {
        // 文件总数
        final fileListResult = results[0] as ApiResult<FileListRespData>;
        if (fileListResult.success && fileListResult.data != null) {
          _totalFiles = fileListResult.data!.total;
        }

        // 传输中任务数
        final uploadResult = results[1] as ApiResult<ListUploadRecordsResp>;
        if (uploadResult.success && uploadResult.data != null) {
          _activeTransfers = uploadResult.data!.records
              .where((r) => r.status == LocationStatus.PUPLOADING || r.status == LocationStatus.PPENDING)
              .length;
        }

        _loadingStats = false;
      });
    }
  }

  Future<void> _loadRecentFiles() async {
    final storage = GetIt.I.get<RecentFilesStorage>();
    final records = storage.getRecentFiles(limit: 10);
    if (mounted) {
      setState(() {
        _recentFiles = records;
        _loadingRecent = false;
      });
    }
  }

  Future<void> _loadStorageProviders() async {
    final repository = GetIt.I.get<FmRepository>();
    final result = await repository.listStorageProviders();

    if (mounted) {
      setState(() {
        if (result.success && result.data != null) {
          _storageProviders = result.data!.providers;
          // 计算总存储使用量（这里简化处理，实际可能需要更复杂的逻辑）
          _storageUsage = _formatStorageUsage(_storageProviders);
        }
        _loadingProviders = false;
      });
    }
  }

  String _formatStorageUsage(List<StorageProvider> providers) {
    if (providers.isEmpty) return '0 B';
    // 简化处理：显示提供者数量
    return '${providers.length} 个存储';
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context)!;
    final colorScheme = Theme.of(context).colorScheme;
    final screenWidth = MediaQuery.of(context).size.width;
    final isMobile = screenWidth < PfmConstraints.mobileBreakpoint;

    return Scaffold(
      body: SafeArea(
        child: SingleChildScrollView(
          padding: EdgeInsets.all(isMobile ? PfmSpacing.lg : PfmSpacing.xxxxl),
          child: Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 1200),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  // 页面标题
                  _buildPageHeader(context, al, colorScheme),
                  const SizedBox(height: PfmSpacing.xxxl),

                  // 统计卡片
                  _buildStatsSection(context, al, colorScheme, isMobile),
                  const SizedBox(height: PfmSpacing.xxxl),

                  // 快捷操作
                  _buildQuickActions(context, al, colorScheme, isMobile),
                  const SizedBox(height: PfmSpacing.xxxl),

                  // 最近文件
                  _buildRecentFiles(context, al, colorScheme),
                  const SizedBox(height: PfmSpacing.xxxl),

                  // 存储空间
                  _buildStorageSection(context, al, colorScheme),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  /// 构建页面标题
  Widget _buildPageHeader(
    BuildContext context,
    AppLocalizations al,
    ColorScheme colorScheme,
  ) {
    return Row(
      mainAxisAlignment: MainAxisAlignment.spaceBetween,
      children: [
        Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              al.home_welcome,
              style: PfmTypography.titleLarge.copyWith(
                color: colorScheme.onSurface,
              ),
            ),
            const SizedBox(height: PfmSpacing.xs),
            Text(
              '这是您的文件管理概览',
              style: PfmTypography.bodyMedium.copyWith(
                color: colorScheme.onSurfaceVariant,
              ),
            ),
          ],
        ),
        // 刷新按钮
        IconButton(
          icon: Icon(Icons.refresh, color: colorScheme.onSurfaceVariant),
          onPressed: () {
            setState(() {
              _loadingStats = true;
              _loadingProviders = true;
            });
            _loadAllData();
          },
          tooltip: '刷新',
        ),
      ],
    );
  }

  /// 构建统计卡片区域
  Widget _buildStatsSection(
    BuildContext context,
    AppLocalizations al,
    ColorScheme colorScheme,
    bool isMobile,
  ) {
    final crossAxisCount = isMobile ? 2 : 4;

    return GridView.count(
      shrinkWrap: true,
      physics: const NeverScrollableScrollPhysics(),
      crossAxisCount: crossAxisCount,
      mainAxisSpacing: PfmSpacing.lg,
      crossAxisSpacing: PfmSpacing.lg,
      childAspectRatio: isMobile ? 1.5 : 1.8,
      children: [
        PfmStatCard(
          label: al.home_totalFiles,
          value: _loadingStats ? '...' : _formatNumber(_totalFiles),
          change: _loadingStats ? '' : '总计 $_totalFiles',
          changeColor: PfmColors.primary,
        ),
        PfmStatCard(
          label: al.home_storageUsage,
          value: _loadingStats ? '...' : _storageUsage,
          change: '${_storageProviders.length} 个提供者',
        ),
        PfmStatCard(
          label: al.home_activeTransfers,
          value: _loadingStats ? '...' : '$_activeTransfers',
          change: _activeTransfers > 0
              ? '$_activeTransfers 个任务'
              : '无任务',
        ),
        PfmStatCard(
          label: al.home_backupStatus,
          value: '正常',
          changeColor: PfmColors.success,
          change: '系统正常',
        ),
      ],
    );
  }

  String _formatNumber(int number) {
    if (number >= 1000000) {
      return '${(number / 1000000).toStringAsFixed(1)}M';
    } else if (number >= 1000) {
      return '${(number / 1000).toStringAsFixed(1)}K';
    }
    return number.toString();
  }

  /// 构建快捷操作区域
  Widget _buildQuickActions(
    BuildContext context,
    AppLocalizations al,
    ColorScheme colorScheme,
    bool isMobile,
  ) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        PfmSectionHeader(title: al.home_quickActions),
        isMobile
            ? Column(
                children: [
                  PfmQuickActionCard(
                    title: al.home_uploadFile,
                    description: al.home_uploadDesc,
                    icon: Icons.upload_outlined,
                    iconColor: PfmColors.primary,
                    iconBackground: PfmColors.primarySurface,
                    onTap: () {
                      // TODO: 实现上传功能
                    },
                  ),
                  const SizedBox(height: PfmSpacing.md),
                  PfmQuickActionCard(
                    title: al.home_backupNow,
                    description: al.home_backupDesc,
                    icon: Icons.shield_outlined,
                    iconColor: PfmColors.success,
                    iconBackground: PfmColors.successSurface,
                    onTap: () {
                      // TODO: 实现备份功能
                    },
                  ),
                  const SizedBox(height: PfmSpacing.md),
                  PfmQuickActionCard(
                    title: al.home_shareFile,
                    description: al.home_shareDesc,
                    icon: Icons.share_outlined,
                    iconColor: PfmColors.warning,
                    iconBackground: PfmColors.warningSurface,
                    onTap: () {
                      // TODO: 实现分享功能
                    },
                  ),
                ],
              )
            : Row(
                children: [
                  Expanded(
                    child: PfmQuickActionCard(
                      title: al.home_uploadFile,
                      description: al.home_uploadDesc,
                      icon: Icons.upload_outlined,
                      iconColor: PfmColors.primary,
                      iconBackground: PfmColors.primarySurface,
                      onTap: () {
                        // TODO: 实现上传功能
                      },
                    ),
                  ),
                  const SizedBox(width: PfmSpacing.lg),
                  Expanded(
                    child: PfmQuickActionCard(
                      title: al.home_backupNow,
                      description: al.home_backupDesc,
                      icon: Icons.shield_outlined,
                      iconColor: PfmColors.success,
                      iconBackground: PfmColors.successSurface,
                      onTap: () {
                        // TODO: 实现备份功能
                      },
                    ),
                  ),
                  const SizedBox(width: PfmSpacing.lg),
                  Expanded(
                    child: PfmQuickActionCard(
                      title: al.home_shareFile,
                      description: al.home_shareDesc,
                      icon: Icons.share_outlined,
                      iconColor: PfmColors.warning,
                      iconBackground: PfmColors.warningSurface,
                      onTap: () {
                        // TODO: 实现分享功能
                      },
                    ),
                  ),
                ],
              ),
      ],
    );
  }

  /// 构建最近文件区域
  Widget _buildRecentFiles(
    BuildContext context,
    AppLocalizations al,
    ColorScheme colorScheme,
  ) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        PfmSectionHeader(title: al.home_recentFiles),
        const SizedBox(height: PfmSpacing.md),
        _loadingRecent
            ? const Center(child: CircularProgressIndicator())
            : _recentFiles.isEmpty
                ? _buildEmptyRecentFiles(context, al, colorScheme)
                : _buildRecentFilesList(context, al, colorScheme),
      ],
    );
  }

  /// 构建空的最近文件提示
  Widget _buildEmptyRecentFiles(
    BuildContext context,
    AppLocalizations al,
    ColorScheme colorScheme,
  ) {
    return Container(
      padding: const EdgeInsets.all(PfmSpacing.xxxxl),
      decoration: BoxDecoration(
        color: colorScheme.surfaceContainerHighest.withValues(alpha: 0.3),
        borderRadius: BorderRadius.circular(PfmRadius.lg),
      ),
      child: Center(
        child: Column(
          children: [
            Icon(
              Icons.folder_open,
              size: 48,
              color: colorScheme.onSurfaceVariant,
            ),
            const SizedBox(height: PfmSpacing.md),
            Text(
              al.home_noRecentFiles,
              style: PfmTypography.bodyMedium.copyWith(
                color: colorScheme.onSurfaceVariant,
              ),
            ),
          ],
        ),
      ),
    );
  }

  /// 构建最近文件列表
  Widget _buildRecentFilesList(
    BuildContext context,
    AppLocalizations al,
    ColorScheme colorScheme,
  ) {
    return Container(
      decoration: BoxDecoration(
        color: colorScheme.surface,
        borderRadius: BorderRadius.circular(PfmRadius.lg),
        border: Border.all(color: colorScheme.outlineVariant),
      ),
      child: ListView.separated(
        shrinkWrap: true,
        physics: const NeverScrollableScrollPhysics(),
        itemCount: _recentFiles.length,
        separatorBuilder: (context, index) => Divider(
          height: 1,
          color: colorScheme.outlineVariant,
        ),
        itemBuilder: (context, index) {
          final file = _recentFiles[index];
          return _buildRecentFileTile(context, al, colorScheme, file);
        },
      ),
    );
  }

  /// 构建单个最近文件行
  Widget _buildRecentFileTile(
    BuildContext context,
    AppLocalizations al,
    ColorScheme colorScheme,
    Map<String, dynamic> file,
  ) {
    final fileName = file['name'] as String? ?? '';
    final fileSize = file['size'] as int? ?? 0;
    final fileType = file['type'] as String? ?? '';

    return ListTile(
      leading: Container(
        width: 40,
        height: 40,
        decoration: BoxDecoration(
          color: _getFileTypeColor(fileType).withValues(alpha: 0.1),
          borderRadius: BorderRadius.circular(PfmRadius.sm),
        ),
        child: Icon(
          _getFileTypeIcon(fileType),
          color: _getFileTypeColor(fileType),
          size: 20,
        ),
      ),
      title: Text(
        fileName,
        style: PfmTypography.bodyMedium.copyWith(color: colorScheme.onSurface),
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
      subtitle: Text(
        formatCapacityString(fileSize),
        style: PfmTypography.bodyMedium.copyWith(
          color: colorScheme.onSurfaceVariant,
        ),
      ),
      trailing: Icon(
        Icons.chevron_right,
        color: colorScheme.onSurfaceVariant,
      ),
      onTap: () {
        // TODO: 打开文件详情
      },
    );
  }

  /// 构建存储空间区域
  Widget _buildStorageSection(
    BuildContext context,
    AppLocalizations al,
    ColorScheme colorScheme,
  ) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        PfmSectionHeader(title: al.home_storageSpace),
        const SizedBox(height: PfmSpacing.md),
        _loadingProviders
            ? const Center(child: CircularProgressIndicator())
            : _storageProviders.isEmpty
                ? _buildEmptyStorage(context, al, colorScheme)
                : _buildStorageProvidersList(context, al, colorScheme),
      ],
    );
  }

  /// 构建空的存储提示
  Widget _buildEmptyStorage(
    BuildContext context,
    AppLocalizations al,
    ColorScheme colorScheme,
  ) {
    return Container(
      padding: const EdgeInsets.all(PfmSpacing.xxxxl),
      decoration: BoxDecoration(
        color: colorScheme.surfaceContainerHighest.withValues(alpha: 0.3),
        borderRadius: BorderRadius.circular(PfmRadius.lg),
      ),
      child: Center(
        child: Column(
          children: [
            Icon(
              Icons.cloud_off,
              size: 48,
              color: colorScheme.onSurfaceVariant,
            ),
            const SizedBox(height: PfmSpacing.md),
            Text(
              '暂无存储提供者',
              style: PfmTypography.bodyMedium.copyWith(
                color: colorScheme.onSurfaceVariant,
              ),
            ),
            const SizedBox(height: PfmSpacing.md),
            OutlinedButton(
              onPressed: () {
                // TODO: 添加存储提供者
              },
              child: const Text('添加存储'),
            ),
          ],
        ),
      ),
    );
  }

  /// 构建存储提供者列表
  Widget _buildStorageProvidersList(
    BuildContext context,
    AppLocalizations al,
    ColorScheme colorScheme,
  ) {
    return Container(
      decoration: BoxDecoration(
        color: colorScheme.surface,
        borderRadius: BorderRadius.circular(PfmRadius.lg),
        border: Border.all(color: colorScheme.outlineVariant),
      ),
      child: ListView.separated(
        shrinkWrap: true,
        physics: const NeverScrollableScrollPhysics(),
        itemCount: _storageProviders.length,
        separatorBuilder: (context, index) => Divider(
          height: 1,
          color: colorScheme.outlineVariant,
        ),
        itemBuilder: (context, index) {
          final provider = _storageProviders[index];
          return _buildStorageProviderTile(context, al, colorScheme, provider);
        },
      ),
    );
  }

  /// 构建单个存储提供者行
  Widget _buildStorageProviderTile(
    BuildContext context,
    AppLocalizations al,
    ColorScheme colorScheme,
    StorageProvider provider,
  ) {
    return ListTile(
      leading: Container(
        width: 40,
        height: 40,
        decoration: BoxDecoration(
          color: _getProviderColor(provider.providerType).withValues(alpha: 0.1),
          borderRadius: BorderRadius.circular(PfmRadius.sm),
        ),
        child: Icon(
          _getProviderIcon(provider.providerType),
          color: _getProviderColor(provider.providerType),
          size: 20,
        ),
      ),
      title: Text(
        provider.name,
        style: PfmTypography.bodyMedium.copyWith(color: colorScheme.onSurface),
      ),
      subtitle: Text(
        _getProviderTypeName(provider.providerType),
        style: PfmTypography.bodyMedium.copyWith(
          color: colorScheme.onSurfaceVariant,
        ),
      ),
      trailing: Container(
        padding: const EdgeInsets.symmetric(
          horizontal: PfmSpacing.sm,
          vertical: PfmSpacing.xs,
        ),
        decoration: BoxDecoration(
          color: provider.enabled
              ? PfmColors.success.withValues(alpha: 0.1)
              : PfmColors.error.withValues(alpha: 0.1),
          borderRadius: BorderRadius.circular(PfmRadius.full),
        ),
        child: Text(
          provider.enabled ? '已启用' : '已禁用',
          style: PfmTypography.labelSmall.copyWith(
            color: provider.enabled ? PfmColors.success : PfmColors.error,
          ),
        ),
      ),
    );
  }

  /// 获取文件类型图标
  IconData _getFileTypeIcon(String type) {
    switch (type.toLowerCase()) {
      case 'image':
        return Icons.image;
      case 'video':
        return Icons.video_file;
      case 'audio':
        return Icons.audio_file;
      case 'document':
        return Icons.description;
      case 'archive':
        return Icons.archive;
      default:
        return Icons.insert_drive_file;
    }
  }

  /// 获取文件类型颜色
  Color _getFileTypeColor(String type) {
    switch (type.toLowerCase()) {
      case 'image':
        return PfmColors.primary;
      case 'video':
        return PfmColors.warning;
      case 'audio':
        return PfmColors.success;
      case 'document':
        return PfmColors.warning;
      case 'archive':
        return PfmColors.error;
      default:
        return PfmColors.primary;
    }
  }

  /// 获取存储提供者图标
  IconData _getProviderIcon(StorageProviderType type) {
    switch (type) {
      case StorageProviderType.TELEGRAM:
        return Icons.send;
      case StorageProviderType.BAIDU_NETDISK:
        return Icons.cloud;
      case StorageProviderType.LOCAL:
        return Icons.phone_android;
      default:
        return Icons.cloud;
    }
  }

  /// 获取存储提供者颜色
  Color _getProviderColor(StorageProviderType type) {
    switch (type) {
      case StorageProviderType.TELEGRAM:
        return PfmColors.primary;
      case StorageProviderType.BAIDU_NETDISK:
        return PfmColors.warning;
      case StorageProviderType.LOCAL:
        return PfmColors.success;
      default:
        return PfmColors.primary;
    }
  }

  /// 获取存储提供者类型名称
  String _getProviderTypeName(StorageProviderType type) {
    switch (type) {
      case StorageProviderType.TELEGRAM:
        return 'Telegram';
      case StorageProviderType.BAIDU_NETDISK:
        return '百度网盘';
      case StorageProviderType.LOCAL:
        return '本地存储';
      default:
        return '未知';
    }
  }
}
