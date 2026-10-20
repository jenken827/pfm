import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:get_it/get_it.dart';
import 'package:go_router/go_router.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/proto/native/permission.pb.dart';
import 'package:pfm/services/recent_files_storage.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/design/design_tokens.dart';
import 'package:pfm/design/components/components.dart';
import 'package:pfm/utils/format.dart';
import 'package:pfm/router/router.dart';
import 'package:pfm/router/files_route.dart';

/// 首页 - 分类浏览 + 最近文件
class HomeIndexPage extends StatefulWidget {
  const HomeIndexPage({super.key});

  @override
  State<HomeIndexPage> createState() => _HomeIndexPageState();
}

class _HomeIndexPageState extends State<HomeIndexPage> {
  // 最近文件
  List<Map<String, dynamic>> _recentFiles = [];
  bool _loadingRecent = true;

  // 存储空间
  UserSpace? _userSpace;

  @override
  void initState() {
    super.initState();
    _loadRecentFiles();
    _loadUserSpace();
  }

  Future<void> _loadUserSpace() async {
    final repo = FmRepository();
    final result = await repo.getUserSpace();
    if (mounted && result.success && result.data != null) {
      setState(() {
        _userSpace = result.data!.space;
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

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context);
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

                  // 存储空间
                  if (_userSpace != null) ...[
                    _buildStorageCard(context, al, colorScheme),
                    const SizedBox(height: PfmSpacing.xxxl),
                  ],

                  // 分类浏览
                  _buildCategoryGrid(context, al, colorScheme, isMobile),
                  const SizedBox(height: PfmSpacing.xxxl),

                  // 最近浏览
                  _buildRecentFiles(context, al, colorScheme),
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
              al.home_categoryBrowse,
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
              _loadingRecent = true;
            });
            _loadRecentFiles();
          },
          tooltip: '刷新',
        ),
      ],
    );
  }

  /// 构建存储空间卡片
  Widget _buildStorageCard(
    BuildContext context,
    AppLocalizations al,
    ColorScheme colorScheme,
  ) {
    final space = _userSpace!;
    final usedBytes = space.usedBytes.toInt();
    final quotaBytes = space.quotaBytes.toInt();
    final isUnlimited = quotaBytes == 0;
    final ratio = isUnlimited ? 0.0 : (usedBytes / quotaBytes).clamp(0.0, 1.0);
    final usedStr = formatCapacityString(usedBytes);
    final quotaStr = isUnlimited ? '不限' : formatCapacityString(quotaBytes);
    final barColor = ratio > 0.9
        ? PfmColors.error
        : ratio > 0.7
        ? PfmColors.warning
        : colorScheme.primary;

    return Container(
      padding: const EdgeInsets.all(PfmSpacing.lg),
      decoration: BoxDecoration(
        color: colorScheme.surfaceContainerHighest.withValues(alpha: 0.3),
        borderRadius: BorderRadius.circular(PfmRadius.lg),
        border: Border.all(color: colorScheme.outlineVariant),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.cloud_outlined, size: 18, color: colorScheme.primary),
              const SizedBox(width: PfmSpacing.sm),
              Text(
                al.home_storageSpace,
                style: PfmTypography.titleSmall.copyWith(
                  color: colorScheme.onSurface,
                ),
              ),
              const Spacer(),
              Text(
                '$usedStr / $quotaStr',
                style: PfmTypography.labelSmall.copyWith(
                  color: colorScheme.onSurfaceVariant,
                ),
              ),
            ],
          ),
          const SizedBox(height: PfmSpacing.md),
          ClipRRect(
            borderRadius: BorderRadius.circular(4),
            child: LinearProgressIndicator(
              value: isUnlimited ? 1.0 : ratio,
              minHeight: 8,
              backgroundColor: colorScheme.surfaceContainerHighest,
              valueColor: AlwaysStoppedAnimation<Color>(
                isUnlimited ? colorScheme.primary : barColor,
              ),
            ),
          ),
        ],
      ),
    );
  }

  /// 构建分类浏览网格
  Widget _buildCategoryGrid(
    BuildContext context,
    AppLocalizations al,
    ColorScheme colorScheme,
    bool isMobile,
  ) {
    final categories = [
      _CategoryItem(
        icon: Icons.image_outlined,
        label: al.fileType_image,
        fileType: FileType.IMAGE,
        color: PfmColors.primary,
      ),
      _CategoryItem(
        icon: Icons.video_file_outlined,
        label: al.fileType_video,
        fileType: FileType.VIDEO,
        color: PfmColors.warning,
      ),
      _CategoryItem(
        icon: Icons.audio_file_outlined,
        label: al.fileType_audio,
        fileType: FileType.AUDIO,
        color: PfmColors.success,
      ),
      _CategoryItem(
        icon: Icons.description_outlined,
        label: al.fileType_document,
        fileType: FileType.DOCUMENT,
        color: PfmColors.info,
      ),
      _CategoryItem(
        icon: Icons.archive_outlined,
        label: al.fileType_archive,
        fileType: FileType.ARCHIVE,
        color: PfmColors.error,
      ),
      _CategoryItem(
        icon: Icons.font_download_outlined,
        label: al.fileType_font,
        fileType: FileType.FONT,
        color: const Color(0xFF9C27B0), // Purple
      ),
      _CategoryItem(
        icon: Icons.android_outlined,
        label: al.fileType_application,
        fileType: FileType.APPLICATION,
        color: const Color(0xFF607D8B), // Blue Grey
      ),
      _CategoryItem(
        icon: Icons.folder_outlined,
        label: al.home_allFiles,
        fileType: FileType.FILE_TYPE_UNKNOWN,
        color: colorScheme.onSurfaceVariant,
      ),
    ];

    final crossAxisCount = isMobile ? 2 : 4;

    return GridView.builder(
      shrinkWrap: true,
      physics: const NeverScrollableScrollPhysics(),
      gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
        crossAxisCount: crossAxisCount,
        mainAxisSpacing: PfmSpacing.lg,
        crossAxisSpacing: PfmSpacing.lg,
        childAspectRatio: 1.2,
      ),
      itemCount: categories.length,
      itemBuilder: (context, index) {
        return _buildCategoryCard(context, categories[index], colorScheme);
      },
    );
  }

  /// 构建单个分类卡片
  Widget _buildCategoryCard(
    BuildContext context,
    _CategoryItem category,
    ColorScheme colorScheme,
  ) {
    return Card(
      elevation: 0,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(PfmRadius.lg),
        side: BorderSide(color: colorScheme.outlineVariant),
      ),
      child: InkWell(
        borderRadius: BorderRadius.circular(PfmRadius.lg),
        onTap: () => _navigateToCategory(context, category),
        child: Padding(
          padding: const EdgeInsets.all(PfmSpacing.lg),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Container(
                width: 56,
                height: 56,
                decoration: BoxDecoration(
                  color: category.color.withValues(alpha: 0.1),
                  borderRadius: BorderRadius.circular(PfmRadius.md),
                ),
                child: Icon(category.icon, size: 28, color: category.color),
              ),
              const SizedBox(height: PfmSpacing.md),
              Text(
                category.label,
                style: PfmTypography.titleSmall.copyWith(
                  color: colorScheme.onSurface,
                ),
                textAlign: TextAlign.center,
              ),
            ],
          ),
        ),
      ),
    );
  }

  /// 导航到分类页面
  void _navigateToCategory(BuildContext context, _CategoryItem category) {
    if (category.fileType == FileType.FILE_TYPE_UNKNOWN) {
      // 全部文件 -> 切换到文件 tab
      const FileListRoute().go(context);
    } else {
      // 按类型浏览 -> push 到文件类型页面（支持返回手势）
      FileTypeRoute(fileType: category.fileType.value).push(context);
    }
  }

  /// 构建最近浏览区域
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

  /// 构建空的最近浏览提示
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
            Icon(Icons.history, size: 48, color: colorScheme.onSurfaceVariant),
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

  /// 构建最近浏览列表
  Widget _buildRecentFilesList(
    BuildContext context,
    AppLocalizations al,
    ColorScheme colorScheme,
  ) {
    return Material(
      color: colorScheme.surface,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(PfmRadius.lg),
        side: BorderSide(color: colorScheme.outlineVariant),
      ),
      clipBehavior: Clip.antiAlias,
      child: ListView.separated(
        shrinkWrap: true,
        physics: const NeverScrollableScrollPhysics(),
        itemCount: _recentFiles.length,
        separatorBuilder: (context, index) =>
            Divider(height: 1, color: colorScheme.outlineVariant),
        itemBuilder: (context, index) {
          final file = _recentFiles[index];
          return _buildRecentFileTile(context, al, colorScheme, file);
        },
      ),
    );
  }

  /// 构建单个最近浏览行
  Widget _buildRecentFileTile(
    BuildContext context,
    AppLocalizations al,
    ColorScheme colorScheme,
    Map<String, dynamic> file,
  ) {
    final fileName = file['fileName'] as String? ?? '';
    final fileSize = file['fileSize'] as int? ?? 0;
    final fileType = file['fileType'] as int? ?? 0;

    return ListTile(
      leading: Container(
        width: 40,
        height: 40,
        decoration: BoxDecoration(
          color: _getFileTypeColorByInt(fileType).withValues(alpha: 0.1),
          borderRadius: BorderRadius.circular(PfmRadius.sm),
        ),
        child: Icon(
          _getFileTypeIconByInt(fileType),
          color: _getFileTypeColorByInt(fileType),
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
      trailing: Icon(Icons.chevron_right, color: colorScheme.onSurfaceVariant),
      onTap: () => _onRecentFileTap(context, file, fileType),
    );
  }

  /// 点击最近浏览文件
  Future<void> _onRecentFileTap(
    BuildContext context,
    Map<String, dynamic> file,
    int fileType,
  ) async {
    // Hive 解码后嵌套 map 是 Map<dynamic, dynamic>，硬 cast 会抛 TypeError
    // （表现为「点了没反应」），统一走 asStringMap 归一化
    final fileJson = asStringMap(file['fileJson']);
    if (fileJson != null) {
      try {
        final protoFile = File()..mergeFromProto3Json(fileJson);
        _navigateToFile(context, protoFile);
        return;
      } catch (_) {}
    }
    // 无 fileJson 时通过 API 获取文件信息
    final fileId = file['fileId'] as String? ?? '';
    if (fileId.isEmpty) {
      FileTypeRoute(fileType: fileType).push(context);
      return;
    }
    final repo = context.read<FmRepository>();
    final result = await repo.getFileInfo(fileId);
    if (result.success && result.data != null) {
      final info = result.data!;
      final protoFile = File()
        ..id = info.id
        ..name = info.name
        ..parentId = info.parentId
        ..type = info.type
        ..size = info.size;
      if (context.mounted) _navigateToFile(context, protoFile);
    } else if (context.mounted) {
      FileTypeRoute(fileType: fileType).push(context);
    }
  }

  /// 导航到文件详情或预览
  void _navigateToFile(BuildContext context, File protoFile) {
    final isPreviewable =
        protoFile.type == FileType.IMAGE ||
        protoFile.type == FileType.VIDEO ||
        protoFile.type == FileType.AUDIO ||
        protoFile.type == FileType.DOCUMENT;
    if (isPreviewable) {
      context.push('/files/preview', extra: protoFile);
    } else {
      context.push('/files/detail', extra: protoFile);
    }
  }

  /// 获取文件类型图标（int 版本）
  IconData _getFileTypeIconByInt(int type) {
    switch (type) {
      case 2:
        return Icons.image;
      case 3:
        return Icons.video_file;
      case 4:
        return Icons.audio_file;
      case 1:
        return Icons.description;
      case 5:
        return Icons.archive;
      case 6:
        return Icons.font_download;
      case 7:
        return Icons.android;
      default:
        return Icons.insert_drive_file;
    }
  }

  /// 获取文件类型颜色（int 版本）
  Color _getFileTypeColorByInt(int type) {
    switch (type) {
      case 2:
        return PfmColors.primary;
      case 3:
        return PfmColors.warning;
      case 4:
        return PfmColors.success;
      case 1:
        return PfmColors.info;
      case 5:
        return PfmColors.error;
      case 6:
        return const Color(0xFF9C27B0);
      case 7:
        return const Color(0xFF607D8B);
      default:
        return PfmColors.primary;
    }
  }
}

/// 分类项数据类
class _CategoryItem {
  final IconData icon;
  final String label;
  final FileType fileType;
  final Color color;

  const _CategoryItem({
    required this.icon,
    required this.label,
    required this.fileType,
    required this.color,
  });
}
