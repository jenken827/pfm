import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:file_picker/file_picker.dart';
import 'package:get_it/get_it.dart';
import 'package:go_router/go_router.dart';
import 'package:path/path.dart' as p;
import 'package:pfm/api/api.dart';
import 'package:pfm/blocs/blocs.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/proto/native/enum.pbenum.dart' as pbenum;
import 'package:pfm/router/files_route.dart';
import 'package:pfm/pages/files/file_detail_page.dart';
import 'package:pfm/pages/files/file_preview_page.dart';
import 'package:pfm/pages/files/flyfish_viewer_page.dart';
import 'package:pfm/pages/files/folder_picker_page.dart';
import 'package:pfm/services/local_storage.dart';
import 'package:pfm/services/recent_files_storage.dart';
import 'package:pfm/utils/utils.dart';
import 'package:pfm/widgets/folder_settings_dialog.dart';
import 'package:pfm/design/design_tokens.dart';
import 'package:pfm/design/components/components.dart';

/// 文件列表页面 - Clean Minimal 设计
class FilePage extends StatelessWidget {
  const FilePage({super.key, this.dirId = ''});

  final String dirId;

  @override
  Widget build(BuildContext context) {
    return BlocProvider(
      create: (context) => FileBloc(context.read<FmRepository>())
        ..add(FileListEvent(dirId: dirId))
        ..add(const FileLoadStorageProvidersEvent()),
      child: _FileView(dirId: dirId),
    );
  }
}

class _FileView extends StatefulWidget {
  const _FileView({required this.dirId});

  final String dirId;

  @override
  State<_FileView> createState() => _FileViewState();
}

class _FileViewState extends State<_FileView> {
  final ScrollController _scrollController = ScrollController();
  double _savedScrollOffset = 0;
  bool _shouldRestoreScroll = false;

  @override
  void dispose() {
    _scrollController.dispose();
    super.dispose();
  }

  /// 保存滚动位置并刷新
  void _saveScrollAndRefresh() {
    if (_scrollController.hasClients) {
      _savedScrollOffset = _scrollController.offset;
    }
    _shouldRestoreScroll = true;
    context.read<FileBloc>().add(const FileRefreshEvent());
  }

  @override
  Widget build(BuildContext context) {
    final screenWidth = MediaQuery.of(context).size.width;
    final isMobile = screenWidth < PfmConstraints.mobileBreakpoint;

    return MultiBlocListener(
      listeners: [
        BlocListener<TransferBloc, TransferState>(
          listenWhen: (previous, current) {
            return current.hasNewCompletedUploads(previous);
          },
          listener: (context, transferState) {
            final currentDirId = context.read<FileBloc>().state.currentDirId;
            final hasCompletedInCurrentDir = transferState.uploadTasks.any(
              (t) =>
                  t.status == pbenum.UploadStatus.COMPLETED &&
                  t.parentId == currentDirId,
            );
            if (hasCompletedInCurrentDir) {
              _saveScrollAndRefresh();
            }
          },
        ),
      ],
      child: BlocConsumer<FileBloc, FileState>(
        listener: (context, state) {
          if (state.status == FileStateStatus.failure && state.msg.isNotEmpty) {
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(
                content: Text(state.msg),
                backgroundColor: Theme.of(context).colorScheme.error,
                behavior: SnackBarBehavior.floating,
              ),
            );
          }
          if (_shouldRestoreScroll && state.status == FileStateStatus.success) {
            _shouldRestoreScroll = false;
            WidgetsBinding.instance.addPostFrameCallback((_) {
              if (_scrollController.hasClients) {
                _scrollController.jumpTo(
                  _savedScrollOffset.clamp(
                    0.0,
                    _scrollController.position.maxScrollExtent,
                  ),
                );
              }
            });
          }
        },
        builder: (context, state) {
          return Scaffold(
            body: SafeArea(
              child: Column(
                children: [
                  // 顶部工具栏
                  _buildToolbar(context, state, isMobile),
                  // 面包屑导航
                  if (state.breadcrumbs.isNotEmpty)
                    _buildBreadcrumbs(context, state),
                  // 文件列表
                  Expanded(child: _buildContent(context, state, isMobile)),
                ],
              ),
            ),
            floatingActionButton: _buildFab(context, state),
          );
        },
      ),
    );
  }

  /// Clean Minimal 风格工具栏
  Widget _buildToolbar(BuildContext context, FileState state, bool isMobile) {
    final al = AppLocalizations.of(context);
    final colorScheme = Theme.of(context).colorScheme;

    if (state.isSelectMode) {
      return Container(
        padding: const EdgeInsets.symmetric(
          horizontal: PfmSpacing.lg,
          vertical: PfmSpacing.md,
        ),
        decoration: BoxDecoration(
          color: colorScheme.surface,
          border: Border(bottom: BorderSide(color: colorScheme.outlineVariant)),
        ),
        child: Row(
          children: [
            IconButton(
              icon: const Icon(Icons.close, size: 20),
              onPressed: () =>
                  context.read<FileBloc>().add(const FileSelectModeEvent()),
              visualDensity: VisualDensity.compact,
            ),
            const SizedBox(width: PfmSpacing.sm),
            Text(
              al.file_selectedCount(state.selectedFiles.length),
              style: PfmTypography.titleSmall.copyWith(
                color: colorScheme.onSurface,
              ),
            ),
            const Spacer(),
            IconButton(
              icon: const Icon(Icons.select_all, size: 20),
              onPressed: () =>
                  context.read<FileBloc>().add(const FileSelectAllEvent()),
              tooltip: al.file_selectAll,
              visualDensity: VisualDensity.compact,
            ),
          ],
        ),
      );
    }

    return Container(
      padding: EdgeInsets.symmetric(
        horizontal: isMobile ? PfmSpacing.lg : PfmSpacing.xxxxl,
        vertical: PfmSpacing.lg,
      ),
      decoration: BoxDecoration(
        color: colorScheme.surface,
        border: Border(bottom: BorderSide(color: colorScheme.outlineVariant)),
      ),
      child: Row(
        children: [
          // 返回按钮（子目录时显示）
          if (state.breadcrumbs.length > 1)
            IconButton(
              icon: const Icon(Icons.arrow_back, size: 20),
              onPressed: () {
                final parent = state.breadcrumbs[state.breadcrumbs.length - 2];
                context.read<FileBloc>().add(
                  FileNavigateEvent(parent.id, parent.name),
                );
              },
              visualDensity: VisualDensity.compact,
            ),
          if (state.breadcrumbs.length > 1)
            const SizedBox(width: PfmSpacing.sm),
          // 页面标题
          Expanded(
            child: Text(
              state.currentDirName.isEmpty
                  ? al.nav_files
                  : state.currentDirName,
              style: PfmTypography.titleLarge.copyWith(
                color: colorScheme.onSurface,
              ),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          // 操作按钮
          IconButton(
            icon: Icon(
              Icons.refresh,
              size: 20,
              color: colorScheme.onSurfaceVariant,
            ),
            onPressed:
                state.status == FileStateStatus.loading
                    ? null
                    : () => _saveScrollAndRefresh(),
            tooltip: '刷新',
            visualDensity: VisualDensity.compact,
          ),
          const SizedBox(width: PfmSpacing.xs),
          IconButton(
            icon: Icon(
              Icons.search,
              size: 20,
              color: colorScheme.onSurfaceVariant,
            ),
            onPressed: () => _showSearchDialog(context),
            tooltip: '搜索',
            visualDensity: VisualDensity.compact,
          ),
          const SizedBox(width: PfmSpacing.xs),
          IconButton(
            icon: Icon(
              state.isGridView ? Icons.view_list : Icons.grid_view,
              size: 20,
              color: colorScheme.onSurfaceVariant,
            ),
            onPressed: () => context.read<FileBloc>().add(
              FileViewModeEvent(!state.isGridView),
            ),
            tooltip: state.isGridView ? al.file_listView : al.file_gridView,
            visualDensity: VisualDensity.compact,
          ),
          const SizedBox(width: PfmSpacing.xs),
          PopupMenuButton<String>(
            icon: Icon(
              Icons.more_vert,
              size: 20,
              color: colorScheme.onSurfaceVariant,
            ),
            onSelected: (value) => _handleMenuAction(context, value, state),
            itemBuilder: (context) => [
              PopupMenuItem(
                value: 'trash',
                child: Row(
                  children: [
                    Icon(
                      Icons.delete_outline,
                      size: 18,
                      color: colorScheme.onSurfaceVariant,
                    ),
                    const SizedBox(width: PfmSpacing.md),
                    Text(al.file_recycleBin),
                  ],
                ),
              ),
              const PopupMenuDivider(),
              PopupMenuItem(
                value: 'sort_name',
                child: Text(al.file_sortByName),
              ),
              PopupMenuItem(
                value: 'sort_size',
                child: Text(al.file_sortBySize),
              ),
              PopupMenuItem(
                value: 'sort_type',
                child: Text(al.file_sortByType),
              ),
              PopupMenuItem(
                value: 'sort_modified',
                child: Text(al.file_sortByModified),
              ),
              PopupMenuItem(
                value: 'sort_created',
                child: Text(al.file_sortByCreated),
              ),
            ],
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(PfmRadius.lg),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildContent(BuildContext context, FileState state, bool isMobile) {
    final al = AppLocalizations.of(context);
    final colorScheme = Theme.of(context).colorScheme;

    // 首次加载
    if (state.status == FileStateStatus.loading) {
      return Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            const CircularProgressIndicator(),
            const SizedBox(height: PfmSpacing.lg),
            Text(
              al.file_loading,
              style: PfmTypography.bodyMedium.copyWith(
                color: colorScheme.onSurfaceVariant,
              ),
            ),
          ],
        ),
      );
    }

    // 加载失败
    if (state.status == FileStateStatus.failure) {
      return Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Container(
              width: 64,
              height: 64,
              decoration: BoxDecoration(
                color: PfmColors.errorSurface,
                borderRadius: BorderRadius.circular(PfmRadius.xl),
              ),
              child: const Icon(
                Icons.error_outline,
                size: 32,
                color: PfmColors.error,
              ),
            ),
            const SizedBox(height: PfmSpacing.lg),
            Text(
              al.file_loadFailed,
              style: PfmTypography.titleSmall.copyWith(
                color: colorScheme.onSurface,
              ),
            ),
            const SizedBox(height: PfmSpacing.lg),
            ElevatedButton.icon(
              onPressed: () => _saveScrollAndRefresh(),
              icon: const Icon(Icons.refresh, size: 18),
              label: Text(al.file_retry),
            ),
          ],
        ),
      );
    }

    // 空文件夹
    if (state.files.isEmpty) {
      return Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Container(
              width: 80,
              height: 80,
              decoration: BoxDecoration(
                color: colorScheme.surfaceContainerHighest.withValues(
                  alpha: 0.5,
                ),
                borderRadius: BorderRadius.circular(PfmRadius.xl),
              ),
              child: Icon(
                Icons.folder_open,
                size: 40,
                color: colorScheme.onSurfaceVariant,
              ),
            ),
            const SizedBox(height: PfmSpacing.xl),
            Text(
              al.file_emptyFolder,
              style: PfmTypography.titleMedium.copyWith(
                color: colorScheme.onSurface,
              ),
            ),
            const SizedBox(height: PfmSpacing.sm),
            Text(
              al.file_emptyFolderHint,
              style: PfmTypography.bodyMedium.copyWith(
                color: colorScheme.onSurfaceVariant,
              ),
              textAlign: TextAlign.center,
            ),
          ],
        ),
      );
    }

    return RefreshIndicator(
      onRefresh: () async {
        final bloc = context.read<FileBloc>();
        final completer = bloc.stream.firstWhere(
          (s) => s.status != FileStateStatus.loading,
        );
        bloc.add(const FileRefreshEvent());
        await completer;
      },
      child: state.isGridView
          ? _buildGridView(context, state, isMobile)
          : _buildListView(context, state, isMobile),
    );
  }

  Widget _buildBreadcrumbs(BuildContext context, FileState state) {
    final colorScheme = Theme.of(context).colorScheme;

    return Container(
      padding: EdgeInsets.symmetric(
        horizontal: PfmSpacing.lg,
        vertical: PfmSpacing.sm,
      ),
      decoration: BoxDecoration(
        color: colorScheme.surfaceContainerHighest.withValues(alpha: 0.3),
        border: Border(bottom: BorderSide(color: colorScheme.outlineVariant)),
      ),
      child: Row(
        children: [
          Expanded(
            child: SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                children: [
                  for (int i = 0; i < state.breadcrumbs.length; i++) ...[
                    if (i > 0)
                      Padding(
                        padding: const EdgeInsets.symmetric(
                          horizontal: PfmSpacing.xs,
                        ),
                        child: Icon(
                          Icons.chevron_right,
                          size: 14,
                          color: colorScheme.onSurfaceVariant,
                        ),
                      ),
                    _BreadcrumbItem(
                      name: state.breadcrumbs[i].name,
                      isLast: i == state.breadcrumbs.length - 1,
                      onTap: () {
                        final crumb = state.breadcrumbs[i];
                        context.read<FileBloc>().add(
                          FileNavigateEvent(crumb.id, crumb.name),
                        );
                      },
                    ),
                  ],
                ],
              ),
            ),
          ),
          // 文件数量
          Container(
            padding: const EdgeInsets.symmetric(
              horizontal: PfmSpacing.sm,
              vertical: PfmSpacing.xs,
            ),
            decoration: BoxDecoration(
              color: colorScheme.surfaceContainerHighest.withValues(alpha: 0.5),
              borderRadius: BorderRadius.circular(PfmRadius.full),
            ),
            child: Text(
              '${state.total > 0 ? state.total : state.files.length}',
              style: PfmTypography.labelSmall.copyWith(
                color: colorScheme.onSurfaceVariant,
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildListView(BuildContext context, FileState state, bool isMobile) {
    return NotificationListener<ScrollNotification>(
      onNotification: (notification) {
        if (notification is ScrollEndNotification &&
            notification.metrics.pixels >=
                notification.metrics.maxScrollExtent - 100) {
          context.read<FileBloc>().add(const FileLoadMoreEvent());
        }
        return false;
      },
      child: ListView.builder(
        controller: _scrollController,
        padding: EdgeInsets.symmetric(
          horizontal: isMobile ? PfmSpacing.sm : PfmSpacing.xxxxl,
          vertical: PfmSpacing.md,
        ),
        itemCount: state.files.length + (state.hasMore ? 1 : 0),
        itemBuilder: (context, index) {
          if (index == state.files.length) {
            return const Padding(
              padding: EdgeInsets.all(PfmSpacing.xxl),
              child: Center(child: CircularProgressIndicator()),
            );
          }

          final file = state.files[index];
          final isSelected = state.selectedFiles.contains(file);
          final iconData = _getFileIconData(file.type);
          final iconColors = _getFileIconColors(file.type);

          return Padding(
            padding: const EdgeInsets.only(bottom: PfmSpacing.xs),
            child: PfmFileListTile(
              name: file.name,
              icon: iconData,
              iconColor: iconColors.$1,
              iconBackground: iconColors.$2,
              subtitle: _formatFileInfo(file),
              isSelected: isSelected,
              isSelectMode: state.isSelectMode,
              onTap: () => _handleFileTap(context, file, state),
              onLongPress: () => _handleFileLongPress(context, file),
              onMenuPressed: () => _showFileMenu(context, file),
              thumbnailUrl: file.type == pbenum.FileType.IMAGE
                  ? _getThumbnailUrl(file.id)
                  : null,
              thumbnailHeaders: _getAuthHeaders(),
            ),
          );
        },
      ),
    );
  }

  Widget _buildGridView(BuildContext context, FileState state, bool isMobile) {
    return NotificationListener<ScrollNotification>(
      onNotification: (notification) {
        if (notification is ScrollEndNotification &&
            notification.metrics.pixels >=
                notification.metrics.maxScrollExtent - 100) {
          context.read<FileBloc>().add(const FileLoadMoreEvent());
        }
        return false;
      },
      child: LayoutBuilder(
        builder: (context, constraints) {
          final width = constraints.maxWidth;
          int crossAxisCount;
          if (width > 1200) {
            crossAxisCount = 8;
          } else if (width > 900) {
            crossAxisCount = 6;
          } else if (width > 600) {
            crossAxisCount = 5;
          } else {
            crossAxisCount = 4;
          }

          return GridView.builder(
            padding: EdgeInsets.all(
              isMobile ? PfmSpacing.sm : PfmSpacing.xxxxl,
            ),
            controller: _scrollController,
            gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
              crossAxisCount: crossAxisCount,
              childAspectRatio: 0.85,
              crossAxisSpacing: PfmSpacing.md,
              mainAxisSpacing: PfmSpacing.md,
            ),
            itemCount: state.files.length + (state.hasMore ? 1 : 0),
            itemBuilder: (context, index) {
              if (index == state.files.length) {
                return const Center(child: CircularProgressIndicator());
              }

              final file = state.files[index];
              final isSelected = state.selectedFiles.contains(file);
              final iconData = _getFileIconData(file.type);
              final iconColors = _getFileIconColors(file.type);

              return PfmFileGridTile(
                name: file.name,
                icon: iconData,
                iconColor: iconColors.$1,
                iconBackground: iconColors.$2,
                isSelected: isSelected,
                isSelectMode: state.isSelectMode,
                onTap: () => _handleFileTap(context, file, state),
                onLongPress: () => _handleFileLongPress(context, file),
                thumbnailUrl: file.type == pbenum.FileType.IMAGE
                    ? _getThumbnailUrl(file.id)
                    : null,
                thumbnailHeaders: _getAuthHeaders(),
              );
            },
          );
        },
      ),
    );
  }

  Widget? _buildFab(BuildContext context, FileState state) {
    final al = AppLocalizations.of(context);
    final colorScheme = Theme.of(context).colorScheme;

    if (state.isSelectMode) {
      return FloatingActionButton.extended(
        onPressed: () => _showBatchActionMenu(context, state),
        icon: const Icon(Icons.more_horiz, size: 20),
        label: Text(al.file_batchOperations(state.selectedFiles.length)),
        backgroundColor: colorScheme.surface,
        foregroundColor: colorScheme.onSurface,
        elevation: 2,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(PfmRadius.lg),
          side: BorderSide(color: colorScheme.outlineVariant),
        ),
      );
    }

    return FloatingActionButton(
      onPressed: () => _showFabMenu(context),
      backgroundColor: PfmColors.primary,
      foregroundColor: Colors.white,
      elevation: 2,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(PfmRadius.lg),
      ),
      child: const Icon(Icons.add, size: 24),
    );
  }

  // ============================================================
  // 辅助方法
  // ============================================================

  /// 获取文件类型图标
  static IconData _getFileIconData(pbenum.FileType type) {
    switch (type) {
      case pbenum.FileType.FOLDER:
        return Icons.folder;
      case pbenum.FileType.IMAGE:
        return Icons.image;
      case pbenum.FileType.VIDEO:
        return Icons.video_file;
      case pbenum.FileType.AUDIO:
        return Icons.audio_file;
      case pbenum.FileType.DOCUMENT:
        return Icons.description;
      case pbenum.FileType.ARCHIVE:
        return Icons.archive;
      default:
        return Icons.insert_drive_file;
    }
  }

  /// 获取文件类型颜色（图标色, 背景色）
  static (Color, Color) _getFileIconColors(pbenum.FileType type) {
    switch (type) {
      case pbenum.FileType.FOLDER:
        return (PfmColors.folder, PfmColors.folderSurface);
      case pbenum.FileType.IMAGE:
        return (PfmColors.image, PfmColors.imageSurface);
      case pbenum.FileType.VIDEO:
        return (PfmColors.video, PfmColors.videoSurface);
      case pbenum.FileType.AUDIO:
        return (PfmColors.image, PfmColors.imageSurface);
      case pbenum.FileType.DOCUMENT:
        return (PfmColors.document, PfmColors.documentSurface);
      case pbenum.FileType.ARCHIVE:
        return (PfmColors.archive, PfmColors.archiveSurface);
      default:
        return (PfmColors.lightTextSecondary, PfmColors.lightSurfaceHover);
    }
  }

  /// 格式化文件信息
  /// 文件夹不展示大小（遍历子树统计不划算，与主流网盘一致），显示 "-"
  String _formatFileInfo(File file) {
    final isFolder = file.type == pbenum.FileType.FOLDER;
    final sizeStr = isFolder ? '-' : _formatSize(file.size.toInt());
    final updatedAt = file.updatedAt;

    String dateStr = '';
    if (updatedAt.seconds > 0) {
      final date = DateTime.fromMillisecondsSinceEpoch(
        updatedAt.seconds.toInt() * 1000,
      );
      dateStr =
          '${date.year}-${date.month.toString().padLeft(2, '0')}-${date.day.toString().padLeft(2, '0')} '
          '${date.hour.toString().padLeft(2, '0')}:${date.minute.toString().padLeft(2, '0')}';
    }

    return '$sizeStr · $dateStr';
  }

  String _formatSize(int size) {
    if (size < 1024) return '$size B';
    if (size < 1024 * 1024) return '${(size / 1024).toStringAsFixed(1)} KB';
    if (size < 1024 * 1024 * 1024) {
      return '${(size / (1024 * 1024)).toStringAsFixed(1)} MB';
    }
    return '${(size / (1024 * 1024 * 1024)).toStringAsFixed(1)} GB';
  }

  /// 获取缩略图 URL
  String? _getThumbnailUrl(String fileId) {
    final ls = GetIt.I.get<LocalStorage>();
    final baseUrl = ls.downloadBaseUrl;
    if (baseUrl.isEmpty) return null;
    return '$baseUrl/api/file/stream/preview?file_id=$fileId&size=small';
  }

  /// 获取认证头
  Map<String, String>? _getAuthHeaders() {
    final ls = GetIt.I.get<LocalStorage>();
    final token = ls.token?.accessToken;
    if (token == null) return null;
    return {'Authorization': 'Bearer $token'};
  }

  // ============================================================
  // 交互处理
  // ============================================================

  void _handleFileTap(BuildContext context, File file, FileState state) {
    if (state.isSelectMode) {
      context.read<FileBloc>().add(FileSelectEvent(file));
    } else {
      if (file.type == pbenum.FileType.FOLDER) {
        context.read<FileBloc>().add(FileNavigateEvent(file.id, file.name));
      } else {
        _recordFileAccess(file);
        if (file.type == pbenum.FileType.IMAGE) {
          final images = state.files
              .where((f) => f.type == pbenum.FileType.IMAGE)
              .toList();
          final index = images.indexWhere((f) => f.id == file.id);
          context.push(
            FilePreviewRoute().location,
            extra: PreviewArgs(
              file: file,
              images: images,
              initialIndex: index >= 0 ? index : 0,
            ),
          );
        } else if (file.type == pbenum.FileType.VIDEO ||
            file.type == pbenum.FileType.AUDIO) {
          context.push(FilePreviewRoute().location, extra: file);
        } else if (file.type == pbenum.FileType.DOCUMENT ||
            file.type == pbenum.FileType.ARCHIVE) {
          Navigator.push(
            context,
            MaterialPageRoute(builder: (_) => FlyfishViewerPage(file: file)),
          );
        } else {
          context.push(FileDetailRoute().location, extra: file);
        }
      }
    }
  }

  /// 记录最近浏览（搜索结果打开文件时也复用，静态方法便于搜索代理调用）
  static void _recordFileAccess(File file) {
    try {
      final storage = GetIt.I.get<RecentFilesStorage>();
      storage.recordAccess(
        fileId: file.id,
        fileName: file.name,
        fileType: file.type.value,
        fileSize: file.size.toInt(),
        parentId: file.parentId,
        fileJson: file.toProto3Json() as Map<String, dynamic>?,
      );
    } catch (_) {}
  }

  void _handleFileLongPress(BuildContext context, File file) {
    if (!context.read<FileBloc>().state.isSelectMode) {
      context.read<FileBloc>().add(const FileSelectModeEvent());
    }
    context.read<FileBloc>().add(FileSelectEvent(file));
  }

  void _handleMenuAction(BuildContext context, String action, FileState state) {
    final bloc = context.read<FileBloc>();
    switch (action) {
      case 'view':
        bloc.add(FileViewModeEvent(!state.isGridView));
        break;
      case 'trash':
        context.push(const TrashRoute().location);
        break;
      case 'sort_name':
        bloc.add(const FileSortEvent(FileSort(type: FileSortType.name)));
        break;
      case 'sort_size':
        bloc.add(const FileSortEvent(FileSort(type: FileSortType.size)));
        break;
      case 'sort_type':
        bloc.add(const FileSortEvent(FileSort(type: FileSortType.type)));
        break;
      case 'sort_modified':
        bloc.add(
          const FileSortEvent(FileSort(type: FileSortType.modifiedTime)),
        );
        break;
      case 'sort_created':
        bloc.add(const FileSortEvent(FileSort(type: FileSortType.createdTime)));
        break;
    }
  }

  /// 离线下载对话框：输入 URL，下载到当前目录
  Future<void> _showOfflineDownloadDialog(
    BuildContext context,
    FileState state,
  ) async {
    final al = AppLocalizations.of(context);
    final urlController = TextEditingController();
    final repo = FmRepository();

    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text(al.file_offlineDownload),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '${al.file_offlineDownloadTo}: ${state.currentDirName.isEmpty ? al.file_offlineDownloadRoot : state.currentDirName}',
              style: Theme.of(
                dialogContext,
              ).textTheme.bodySmall?.copyWith(
                    color: Theme.of(dialogContext).colorScheme.onSurfaceVariant,
                  ),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: urlController,
              autofocus: true,
              keyboardType: TextInputType.url,
              decoration: InputDecoration(
                hintText: 'https://example.com/file.zip',
                labelText: al.file_offlineDownloadUrl,
                border: const OutlineInputBorder(),
              ),
            ),
            const SizedBox(height: 8),
            Text(
              al.file_offlineDownloadHint,
              style: Theme.of(dialogContext).textTheme.bodySmall?.copyWith(
                    color: Theme.of(dialogContext).colorScheme.onSurfaceVariant,
                  ),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialogContext, false),
            child: Text(al.common_cancel),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(dialogContext, true),
            child: Text(al.common_confirm),
          ),
        ],
      ),
    );

    if (confirmed != true) {
      urlController.dispose();
      return;
    }
    final url = urlController.text.trim();
    urlController.dispose();

    if (url.isEmpty || !context.mounted) return;

    // 调起创建请求
    final result = await repo.createOfflineDownload(
      url: url,
      parentId: state.currentDirId.isEmpty ? null : state.currentDirId,
    );

    if (!context.mounted) return;
    final messenger = ScaffoldMessenger.of(context);
    if (result.success) {
      messenger.showSnackBar(SnackBar(content: Text(al.file_offlineDownloadOk)));
      // 刷新列表（文件异步落库，稍后出现在当前目录）
      context.read<FileBloc>().add(FileRefreshEvent());
    } else {
      messenger.showSnackBar(
        SnackBar(content: Text(result.message ?? al.file_offlineDownloadFail)),
      );
    }
  }

  Future<void> _showSearchDialog(BuildContext context) async {
    final bloc = context.read<FileBloc>();
    final file = await showSearch<File?>(
      context: context,
      delegate: _FileSearchDelegate(bloc),
    );
    if (!context.mounted) return;
    // 用户取消/返回关闭搜索页：退出搜索模式，恢复当前目录的普通文件列表
    if (file == null) {
      bloc.add(const FileSearchEvent(''));
      return;
    }
    if (file.type == pbenum.FileType.FOLDER) {
      // 点击搜索结果中的文件夹：正常进入该目录（面包屑自动拼接，可正常返回）
      bloc.add(FileNavigateEvent(file.id, file.name));
    } else {
      // 兜底分支：文件已在搜索结果页内直接打开（返回后停留在搜索结果页），
      // 此处仅防御性地处理直接 close 返回文件对象的极端情况
      bloc.add(const FileSearchEvent(''));
      _handleFileTap(context, file, bloc.state);
    }
  }

  // ============================================================
  // 文件操作菜单
  // ============================================================

  void _showFileMenu(BuildContext context, File file) {
    final al = AppLocalizations.of(context);
    final colorScheme = Theme.of(context).colorScheme;
    final isFolder = file.type == pbenum.FileType.FOLDER;

    showModalBottomSheet(
      context: context,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(PfmRadius.xl)),
      ),
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            // 拖拽指示器
            Container(
              margin: const EdgeInsets.only(top: PfmSpacing.md),
              width: 40,
              height: 4,
              decoration: BoxDecoration(
                color: colorScheme.outlineVariant,
                borderRadius: BorderRadius.circular(PfmRadius.full),
              ),
            ),
            const SizedBox(height: PfmSpacing.lg),
            _MenuTile(
              icon: Icons.info_outline,
              title: al.file_viewDetail,
              onTap: () {
                Navigator.pop(ctx);
                context.push(FileDetailRoute().location, extra: file);
              },
            ),
            if (!isFolder)
              _MenuTile(
                icon: Icons.download_outlined,
                title: al.file_download,
                onTap: () async {
                  Navigator.pop(ctx);
                  await _downloadFile(context, file);
                },
              ),
            _MenuTile(
              icon: Icons.edit_outlined,
              title: al.file_rename,
              onTap: () {
                Navigator.pop(ctx);
                _showRenameDialog(context, file);
              },
            ),
            if (!isFolder)
              _MenuTile(
                icon: Icons.link,
                title: al.file_copyLink,
                onTap: () async {
                  Navigator.pop(ctx);
                  await _copyShareLink(context, file, al);
                },
              ),
            _MenuTile(
              icon: Icons.drive_file_move_outlined,
              title: al.file_move,
              onTap: () async {
                Navigator.pop(ctx);
                await _showMoveDialog(context, [file]);
              },
            ),
            if (!isFolder)
              _MenuTile(
                icon: Icons.label_outline,
                title: al.file_editTags,
                onTap: () {
                  Navigator.pop(ctx);
                  _showTagsDialog(context, file);
                },
              ),
            if (isFolder)
              _MenuTile(
                icon: Icons.settings_outlined,
                title: al.file_settings,
                onTap: () {
                  Navigator.pop(ctx);
                  _showFolderSettings(context, file);
                },
              ),
            const Divider(height: 1),
            _MenuTile(
              icon: Icons.delete_outline,
              title: al.file_delete,
              color: PfmColors.error,
              onTap: () {
                Navigator.pop(ctx);
                _showDeleteConfirm(context, [file.id]);
              },
            ),
            const SizedBox(height: PfmSpacing.md),
          ],
        ),
      ),
    );
  }

  Future<void> _copyShareLink(
    BuildContext context,
    File file,
    AppLocalizations al,
  ) async {
    try {
      final fmRepo = context.read<FmRepository>();
      final result = await fmRepo.createShareLink(fileId: file.id);
      if (result.success && result.data != null) {
        final url = result.data!.shareUrl;
        await Clipboard.setData(ClipboardData(text: url));
        if (context.mounted) {
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(SnackBar(content: Text(al.file_linkCopied)));
        }
      } else {
        if (context.mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text(
                '${al.file_createLinkFailed}: ${result.message ?? "未知错误"}',
              ),
            ),
          );
        }
      }
    } catch (e) {
      if (context.mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('${al.file_getLinkFailed}: $e')));
      }
    }
  }

  // ============================================================
  // 下载
  // ============================================================

  Future<void> _downloadFile(BuildContext context, File file) async {
    final al = AppLocalizations.of(context);
    try {
      // 下载到应用私有目录，完成后再发布到公共「下载」（分区存储限制）
      final downloadDir = await DownloadDir.resolveWriteDir();
      if (!context.mounted) return;
      final fileName = file.name.isNotEmpty ? file.name : 'unnamed';
      final initialPath = '$downloadDir/$fileName';
      final savePath = await FileUtils.getUniqueFilePath(initialPath);
      if (!context.mounted) return;
      GetIt.I.get<TransferBloc>().add(
        TransferAddDownloadEvent(
          fileId: file.id,
          filename: fileName,
          fileSize: file.size.toInt(),
          savePath: savePath,
          fileHash: file.hash,
        ),
      );
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(al.file_downloadStarted(fileName))),
      );
    } catch (e) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('${al.file_downloadFailed}: $e')),
        );
      }
    }
  }

  // ============================================================
  // FAB 菜单
  // ============================================================

  void _showFabMenu(BuildContext context) {
    final al = AppLocalizations.of(context);
    showModalBottomSheet(
      context: context,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(PfmRadius.xl)),
      ),
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            PfmDialogs.buildDragHandle(context),
            PfmDialogs.menuItem(
              icon: Icons.create_new_folder_outlined,
              title: al.file_newFolder,
              onTap: () {
                Navigator.pop(ctx);
                _showCreateDirDialog(context);
              },
            ),
            PfmDialogs.menuItem(
              icon: Icons.upload_file_outlined,
              title: al.file_uploadFile,
              onTap: () {
                Navigator.pop(ctx);
                _pickAndUploadFiles(context);
              },
            ),
            PfmDialogs.menuItem(
              icon: Icons.folder_outlined,
              title: al.file_uploadFolder,
              onTap: () {
                Navigator.pop(ctx);
                _pickAndUploadFolder(context);
              },
            ),
            // 服务器代下：与上面的"本地内容"区分开
            const Divider(height: 1),
            PfmDialogs.menuItem(
              icon: Icons.cloud_download_outlined,
              title: al.file_offlineDownload,
              onTap: () {
                Navigator.pop(ctx);
                _showOfflineDownloadDialog(
                  context,
                  context.read<FileBloc>().state,
                );
              },
            ),
            const SizedBox(height: PfmSpacing.md),
          ],
        ),
      ),
    );
  }

  // ============================================================
  // 文件选择与上传
  // ============================================================

  Future<void> _pickAndUploadFiles(BuildContext context) async {
    final al = AppLocalizations.of(context);
    final tagsController = TextEditingController();
    bool overwrite = false;

    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setState) => AlertDialog(
          title: Text(al.file_uploadFileTitle),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: tagsController,
                decoration: InputDecoration(
                  labelText: al.file_fileTag,
                  hintText: al.file_fileTagHint,
                  prefixIcon: const Icon(Icons.label_outline, size: 18),
                ),
              ),
              const SizedBox(height: PfmSpacing.lg),
              SwitchListTile(
                title: Text(al.file_overwriteSameName),
                subtitle: Text(al.file_overwriteHint),
                value: overwrite,
                onChanged: (value) {
                  setState(() => overwrite = value);
                },
                contentPadding: EdgeInsets.zero,
              ),
            ],
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: Text(al.common_cancel),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(ctx, true),
              child: Text(al.file_selectFile),
            ),
          ],
        ),
      ),
    );

    if (confirmed != true || !context.mounted) return;

    try {
      final result = await FilePicker.platform.pickFiles(
        allowMultiple: true,
        withData: false,
      );

      if (result != null && result.files.isNotEmpty && context.mounted) {
        final state = context.read<FileBloc>().state;
        final parentId = state.currentDirId;
        final tags = tagsController.text.trim().isEmpty
            ? null
            : tagsController.text.trim();

        final filePaths = result.files
            .where((file) => file.path != null)
            .map((file) => file.path!)
            .toList();

        if (filePaths.isNotEmpty) {
          GetIt.I.get<TransferBloc>().add(
            TransferAddBatchUploadEvent(
              filePaths,
              parentId,
              tags: tags,
              overwrite: overwrite,
            ),
          );
        }
      }
    } catch (e) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('${al.file_selectFileFailed}: $e'),
            backgroundColor: Theme.of(context).colorScheme.error,
          ),
        );
      }
    }
  }

  Future<void> _pickAndUploadFolder(BuildContext context) async {
    final al = AppLocalizations.of(context);
    try {
      final result = await FilePicker.platform.getDirectoryPath();

      if (result != null && context.mounted) {
        final state = context.read<FileBloc>().state;
        final parentId = state.currentDirId;

        showDialog(
          context: context,
          barrierDismissible: false,
          builder: (dialogContext) =>
              const Center(child: CircularProgressIndicator()),
        );

        try {
          await _uploadFolder(
            context: context,
            folderPath: result,
            parentDirId: parentId,
          );

          if (context.mounted) {
            Navigator.of(context, rootNavigator: true).pop();
            ScaffoldMessenger.of(
              context,
            ).showSnackBar(SnackBar(content: Text(al.file_folderUploadAdded)));
          }
        } catch (e) {
          if (context.mounted) {
            Navigator.of(context, rootNavigator: true).pop();
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(
                content: Text('${al.file_uploadFolderFailed}: $e'),
                backgroundColor: Theme.of(context).colorScheme.error,
              ),
            );
          }
        }
      }
    } catch (e) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('${al.file_selectFolderFailed}: $e'),
            backgroundColor: Theme.of(context).colorScheme.error,
          ),
        );
      }
    }
  }

  Future<void> _uploadFolder({
    required BuildContext context,
    required String folderPath,
    required String parentDirId,
    String? tags,
    bool overwrite = false,
  }) async {
    final folderName = p.basename(folderPath);
    final fmRepo = context.read<FmRepository>();

    final createResult = await _createDirWithRetry(
      fmRepo: fmRepo,
      name: folderName,
      parentId: parentDirId,
    );

    if (!createResult.success) {
      throw Exception('创建目录失败 "$folderName": ${createResult.message}');
    }

    final respData = createResult.data;
    if (respData == null || !respData.hasId()) {
      throw Exception('创建目录失败 "$folderName"：未返回目录 ID');
    }

    final newDirId = respData.id;
    final dir = Directory(folderPath);
    List<FileSystemEntity> entities;

    try {
      entities = dir.listSync(recursive: false);
    } catch (e) {
      throw Exception('读取目录失败: $e');
    }

    final filePaths = <String>[];
    final subDirs = <String>[];

    for (final entity in entities) {
      final type = FileSystemEntity.typeSync(entity.path);
      if (type == FileSystemEntityType.directory) {
        subDirs.add(entity.path);
      } else if (type == FileSystemEntityType.file) {
        filePaths.add(entity.path);
      }
    }

    if (filePaths.isNotEmpty) {
      GetIt.I.get<TransferBloc>().add(
        TransferAddBatchUploadEvent(
          filePaths,
          newDirId,
          tags: tags,
          overwrite: overwrite,
        ),
      );
    }

    for (final subDir in subDirs) {
      if (!context.mounted) return;
      await Future.delayed(const Duration(milliseconds: 350));
      if (!context.mounted) return;
      await _uploadFolder(
        context: context,
        folderPath: subDir,
        parentDirId: newDirId,
        tags: tags,
        overwrite: overwrite,
      );
    }
  }

  Future<ApiResult<CreateDirRespData>> _createDirWithRetry({
    required FmRepository fmRepo,
    required String name,
    required String parentId,
    bool? enableEncryption,
    int maxRetries = 3,
  }) async {
    int retryCount = 0;

    while (retryCount < maxRetries) {
      final result = await fmRepo.createDir(
        name: name,
        parentId: parentId,
        enableEncryption: enableEncryption,
      );

      if (result.success) return result;

      final isRateLimitError =
          result.message != null && result.message!.contains('429');

      if (!isRateLimitError) return result;

      retryCount++;
      if (retryCount < maxRetries) {
        await Future.delayed(Duration(seconds: retryCount));
      }
    }

    return fmRepo.createDir(
      name: name,
      parentId: parentId,
      enableEncryption: enableEncryption,
    );
  }

  // ============================================================
  // 批量操作
  // ============================================================

  void _showBatchActionMenu(BuildContext context, FileState state) {
    final al = AppLocalizations.of(context);
    final colorScheme = Theme.of(context).colorScheme;

    showModalBottomSheet(
      context: context,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(PfmRadius.xl)),
      ),
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Container(
              margin: const EdgeInsets.only(top: PfmSpacing.md),
              width: 40,
              height: 4,
              decoration: BoxDecoration(
                color: colorScheme.outlineVariant,
                borderRadius: BorderRadius.circular(PfmRadius.full),
              ),
            ),
            const SizedBox(height: PfmSpacing.lg),
            _MenuTile(
              icon: Icons.select_all,
              title: al.file_selectAll,
              onTap: () {
                Navigator.pop(ctx);
                context.read<FileBloc>().add(const FileSelectAllEvent());
              },
            ),
            _MenuTile(
              icon: Icons.drive_file_move_outlined,
              title: al.file_moveSelected(state.selectedFiles.length),
              onTap: () {
                Navigator.pop(ctx);
                _showMoveDialog(context, state.selectedFiles.toList());
              },
            ),
            _MenuTile(
              icon: Icons.delete_outline,
              title: al.file_deleteSelected(state.selectedFiles.length),
              color: PfmColors.error,
              onTap: () {
                Navigator.pop(ctx);
                final ids = state.selectedFiles.map((f) => f.id).toList();
                _showDeleteConfirm(context, ids);
              },
            ),
            const Divider(height: 1),
            _MenuTile(
              icon: Icons.close,
              title: al.file_deselect,
              onTap: () {
                Navigator.pop(ctx);
                context.read<FileBloc>().add(const FileSelectModeEvent());
              },
            ),
            const SizedBox(height: PfmSpacing.md),
          ],
        ),
      ),
    );
  }

  // ============================================================
  // 对话框
  // ============================================================

  Future<void> _showCreateDirDialog(BuildContext context) async {
    final al = AppLocalizations.of(context);
    final bloc = context.read<FileBloc>();
    final repo = bloc.fmRepository;

    // 可选存储提供者 = 管理员分配给当前用户的列表（未加载过则补一次请求）
    var providers = bloc.state.storageProviders;
    if (providers.isEmpty) {
      final resp = await repo.getMyBoundProviders();
      providers = resp.data?.providers.toList() ?? const [];
    }
    if (!context.mounted) return;
    if (providers.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(al.file_noProviderAvailable)),
      );
      return;
    }

    // 默认绑定 = 父文件夹的绑定（父目录未显式绑定时视作全部可用）
    final selectedIds = providers.map((p) => p.id.toInt()).toList();
    final parentId = bloc.state.currentDirId;
    if (parentId.isNotEmpty) {
      final info = await repo.getFileInfo(parentId);
      final parentBinding = parseProviderIdsJson(
        info.data?.boundProviderIds ?? '',
      );
      if (parentBinding.isNotEmpty) {
        selectedIds
          ..clear()
          ..addAll(
            providers
                .map((p) => p.id.toInt())
                .where(parentBinding.contains),
          );
      }
    }
    if (!context.mounted) return;

    final nameController = TextEditingController();
    final maxHistoriesController = TextEditingController(text: '10');
    bool enableEncryption = true;

    showDialog(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (builderctx, setState) => AlertDialog(
          title: Text(al.file_newFolder),
          content: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                TextField(
                  controller: nameController,
                  decoration: InputDecoration(
                    labelText: al.file_folderName,
                    hintText: al.file_folderNameHint,
                  ),
                  autofocus: true,
                ),
                const SizedBox(height: PfmSpacing.lg),
                Text(
                  al.file_bindStorageProvider,
                  style: PfmTypography.titleSmall,
                ),
                const SizedBox(height: PfmSpacing.xs),
                ...providers.map((provider) {
                  final id = provider.id.toInt();
                  return CheckboxListTile(
                    dense: true,
                    controlAffinity: ListTileControlAffinity.leading,
                    contentPadding: EdgeInsets.zero,
                    title: Text(provider.name),
                    value: selectedIds.contains(id),
                    onChanged: (checked) {
                      setState(() {
                        if (checked == true) {
                          if (!selectedIds.contains(id)) selectedIds.add(id);
                        } else {
                          selectedIds.remove(id);
                        }
                      });
                    },
                  );
                }),
                Text(
                  al.file_providerBindingHint,
                  style: PfmTypography.labelSmall.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                ),
                const SizedBox(height: PfmSpacing.lg),
                SwitchListTile(
                  title: Text(al.file_enableEncryption),
                  subtitle: Text(al.file_encryptionHint),
                  value: enableEncryption,
                  onChanged: (value) {
                    setState(() => enableEncryption = value);
                  },
                  contentPadding: EdgeInsets.zero,
                ),
                const SizedBox(height: PfmSpacing.lg),
                TextField(
                  controller: maxHistoriesController,
                  decoration: InputDecoration(
                    labelText: al.file_maxHistoryCount,
                    hintText: al.file_maxHistoryHint,
                  ),
                  keyboardType: TextInputType.number,
                ),
                const SizedBox(height: PfmSpacing.xs),
                Text(
                  al.file_maxHistoryDesc,
                  style: PfmTypography.labelSmall.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx),
              child: Text(al.common_cancel),
            ),
            FilledButton(
              onPressed: selectedIds.isEmpty
                  ? null
                  : () {
                      if (nameController.text.isEmpty) {
                        return;
                      }
                      int? maxHistories;
                      if (maxHistoriesController.text.isNotEmpty) {
                        maxHistories = int.tryParse(maxHistoriesController.text);
                        if (maxHistories == null || maxHistories < 0) {
                          ScaffoldMessenger.of(context).showSnackBar(
                            SnackBar(content: Text(al.file_maxHistoryInvalid)),
                          );
                          return;
                        }
                      }
                      context.read<FileBloc>().add(
                        FileCreateDirEvent(
                          name: nameController.text,
                          enableEncryption: enableEncryption,
                          maxHistories: maxHistories,
                          providerIds: List<int>.from(selectedIds),
                        ),
                      );
                      Navigator.pop(ctx);
                    },
              child: Text(al.common_create),
            ),
          ],
        ),
      ),
    );
  }

  void _showRenameDialog(BuildContext context, File file) {
    final al = AppLocalizations.of(context);
    final controller = TextEditingController(text: file.name);
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(al.file_rename),
        content: TextField(
          controller: controller,
          decoration: InputDecoration(hintText: al.file_enterNewName),
          autofocus: true,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: Text(al.common_cancel),
          ),
          FilledButton(
            onPressed: () {
              if (controller.text.isNotEmpty && controller.text != file.name) {
                context.read<FileBloc>().add(
                  FileRenameEvent(file.id, controller.text),
                );
                Navigator.pop(ctx);
              }
            },
            child: Text(al.common_confirm),
          ),
        ],
      ),
    );
  }

  void _showTagsDialog(BuildContext context, File file) async {
    final al = AppLocalizations.of(context);
    final fmRepo = context.read<FmRepository>();
    final tagsResult = await fmRepo.getFileTags(fileId: file.id);

    List<String> currentTags = [];
    if (tagsResult.success && tagsResult.data != null) {
      currentTags = List<String>.from(tagsResult.data!);
    } else {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('${al.file_tagsLoadFailed}: ${tagsResult.message}'),
          ),
        );
      }
      return;
    }

    final controller = TextEditingController(text: currentTags.join(','));

    if (!context.mounted) return;
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(al.file_editTags),
        content: TextField(
          controller: controller,
          decoration: InputDecoration(hintText: al.file_tagInputHint),
          autofocus: true,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: Text(al.common_cancel),
          ),
          FilledButton(
            onPressed: () async {
              final input = controller.text.trim();
              final newTags = input.isEmpty
                  ? <String>[]
                  : input
                        .split(',')
                        .map((tag) => tag.trim())
                        .where((tag) => tag.isNotEmpty)
                        .toList();

              final result = await fmRepo.updateFileTags(
                fileId: file.id,
                tags: newTags,
              );

              if (context.mounted) {
                if (result.success) {
                  _saveScrollAndRefresh();
                  ScaffoldMessenger.of(context).showSnackBar(
                    SnackBar(content: Text(al.file_tagUpdateSuccess)),
                  );
                } else {
                  ScaffoldMessenger.of(context).showSnackBar(
                    SnackBar(
                      content: Text(
                        '${al.file_tagUpdateFailed}: ${result.message}',
                      ),
                    ),
                  );
                }
              }

              if (ctx.mounted) Navigator.pop(ctx);
            },
            child: Text(al.common_save),
          ),
        ],
      ),
    );
  }

  void _showFolderSettings(BuildContext context, File folder) {
    showDialog(
      context: context,
      builder: (ctx) => FolderSettingsDialog(
        folder: folder,
        onUpdate: () {
          _saveScrollAndRefresh();
        },
      ),
    );
  }

  void _showDeleteConfirm(BuildContext context, List<String> ids) async {
    final al = AppLocalizations.of(context);
    final confirmed = await PfmDialogs.confirm(
      context,
      title: al.file_confirmDelete,
      message: al.file_confirmDeleteMsg(ids.length),
      confirmLabel: al.file_delete,
      isDestructive: true,
    );
    if (confirmed && context.mounted) {
      context.read<FileBloc>().add(FileDeleteEvent(ids));
    }
  }

  Future<void> _showMoveDialog(BuildContext context, List<File> files) async {
    final al = AppLocalizations.of(context);
    final fileIds = files.map((f) => f.id).toList();

    final pickResult = await context.push<FolderPickResult>(
      FolderPickerRoute().location,
      extra: {'initialFolderId': '', 'excludeIds': fileIds},
    );

    if (pickResult == null || !context.mounted) return;

    final fmRepo = context.read<FmRepository>();
    final moveResult = await fmRepo.moveFile(
      fileIds: fileIds,
      targetParentId: pickResult.id,
    );

    if (context.mounted) {
      if (moveResult.success) {
        final data = moveResult.data!;
        final successCount = data.successCount;
        final failedIds = data.failedIds;

        if (failedIds.isEmpty) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text(al.file_moveSuccess(successCount))),
          );
        } else {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text(
                al.file_movePartialSuccess(successCount, failedIds.length),
              ),
              backgroundColor: Theme.of(context).colorScheme.error,
            ),
          );
        }
        _saveScrollAndRefresh();
      } else {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              '${al.file_moveFailed}: ${moveResult.message ?? "未知错误"}',
            ),
            backgroundColor: Theme.of(context).colorScheme.error,
          ),
        );
      }
    }
  }
}

// ============================================================
// 独立组件
// ============================================================

/// 面包屑项
class _BreadcrumbItem extends StatelessWidget {
  const _BreadcrumbItem({
    required this.name,
    required this.isLast,
    required this.onTap,
  });

  final String name;
  final bool isLast;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;

    return InkWell(
      onTap: isLast ? null : onTap,
      borderRadius: BorderRadius.circular(PfmRadius.sm),
      child: Padding(
        padding: const EdgeInsets.symmetric(
          horizontal: PfmSpacing.sm,
          vertical: PfmSpacing.xs,
        ),
        child: Text(
          name,
          style: PfmTypography.labelMedium.copyWith(
            color: isLast ? colorScheme.primary : colorScheme.onSurfaceVariant,
            fontWeight: isLast ? FontWeight.w600 : FontWeight.w400,
          ),
        ),
      ),
    );
  }
}

/// 底部菜单项
class _MenuTile extends StatelessWidget {
  const _MenuTile({
    required this.icon,
    required this.title,
    required this.onTap,
    this.color,
  });

  final IconData icon;
  final String title;
  final VoidCallback onTap;
  final Color? color;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    final effectiveColor = color ?? colorScheme.onSurface;

    return InkWell(
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.symmetric(
          horizontal: PfmSpacing.xxl,
          vertical: PfmSpacing.md,
        ),
        child: Row(
          children: [
            Icon(icon, size: 20, color: effectiveColor),
            const SizedBox(width: PfmSpacing.lg),
            Text(
              title,
              style: PfmTypography.bodyLarge.copyWith(color: effectiveColor),
            ),
          ],
        ),
      ),
    );
  }
}

/// 文件搜索代理
class _FileSearchDelegate extends SearchDelegate<File?> {
  final FileBloc _bloc;
  bool _isTagSearch = false;

  _FileSearchDelegate(this._bloc);

  @override
  List<Widget> buildActions(BuildContext context) {
    final al = AppLocalizations.of(context);
    return [
      IconButton(
        icon: Icon(_isTagSearch ? Icons.label : Icons.search, size: 20),
        tooltip: _isTagSearch
            ? al.file_searchToggleFilename
            : al.file_searchToggleTag,
        onPressed: () {
          _isTagSearch = !_isTagSearch;
          showResults(context);
          if (query.trim().isNotEmpty) {
            _bloc.add(FileSearchEvent(query, isTagSearch: _isTagSearch));
          }
        },
      ),
      IconButton(
        icon: const Icon(Icons.clear, size: 20),
        onPressed: () => query = '',
      ),
    ];
  }

  @override
  Widget buildLeading(BuildContext context) {
    return IconButton(
      icon: const Icon(Icons.arrow_back, size: 20),
      // 只关闭搜索页并返回 null，取消/恢复列表统一由 _showSearchDialog 处理，
      // 覆盖系统返回键和返回按钮两种关闭路径
      onPressed: () => close(context, null),
    );
  }

  @override
  Widget buildResults(BuildContext context) {
    final al = AppLocalizations.of(context);
    final colorScheme = Theme.of(context).colorScheme;

    if (query.trim().isEmpty) {
      return Center(
        child: Text(
          al.file_enterKeyword,
          style: PfmTypography.bodyMedium.copyWith(
            color: colorScheme.onSurfaceVariant,
          ),
        ),
      );
    }

    _bloc.add(FileSearchEvent(query, isTagSearch: _isTagSearch));

    return BlocBuilder<FileBloc, FileState>(
      bloc: _bloc,
      builder: (context, state) {
        if (state.status == FileStateStatus.loading) {
          return const Center(child: CircularProgressIndicator());
        }
        if (state.status == FileStateStatus.failure) {
          return Center(child: Text('${al.file_searchFailed}: ${state.msg}'));
        }
        if (state.files.isEmpty) {
          return Center(
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                Icon(
                  Icons.search_off,
                  size: 48,
                  color: colorScheme.onSurfaceVariant,
                ),
                const SizedBox(height: PfmSpacing.lg),
                Text(
                  al.file_noMatchingFiles,
                  style: PfmTypography.bodyMedium.copyWith(
                    color: colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
          );
        }
        return ListView.builder(
          padding: const EdgeInsets.symmetric(vertical: PfmSpacing.sm),
          itemCount: state.files.length,
          itemBuilder: (context, index) {
            final file = state.files[index];
            final fileName = file.hasName() ? file.name : al.file_unknownFile;
            final fileSize = file.hasSize() ? file.size.toInt() : 0;
            final iconData = _FileViewState._getFileIconData(file.type);
            final iconColors = _FileViewState._getFileIconColors(file.type);

            return Padding(
              padding: const EdgeInsets.symmetric(
                horizontal: PfmSpacing.md,
                vertical: PfmSpacing.xs,
              ),
              child: PfmFileListTile(
                name: fileName,
                icon: iconData,
                iconColor: iconColors.$1,
                iconBackground: iconColors.$2,
                subtitle: _formatFileSize(fileSize),
                onTap: () => _openSearchResult(context, file),
              ),
            );
          },
        );
      },
    );
  }

  /// 打开搜索结果项：
  /// - 文件夹：关闭搜索页，由文件页进入该目录（面包屑可正常返回）；
  /// - 文件：不关闭搜索页，直接在搜索结果页之上打开预览/详情，
  ///   返回后仍停留在搜索结果页，可继续浏览其余结果。
  void _openSearchResult(BuildContext context, File file) {
    if (file.type == pbenum.FileType.FOLDER) {
      close(context, file);
      return;
    }

    _FileViewState._recordFileAccess(file);

    final Widget page;
    if (file.type == pbenum.FileType.IMAGE) {
      final images = _bloc.state.files
          .where((f) => f.type == pbenum.FileType.IMAGE)
          .toList();
      final index = images.indexWhere((f) => f.id == file.id);
      page = FilePreviewPage(
        file: file,
        images: images,
        initialIndex: index >= 0 ? index : 0,
      );
    } else if (file.type == pbenum.FileType.VIDEO ||
        file.type == pbenum.FileType.AUDIO) {
      page = FilePreviewPage(file: file);
    } else if (file.type == pbenum.FileType.DOCUMENT ||
        file.type == pbenum.FileType.ARCHIVE) {
      page = FlyfishViewerPage(file: file);
    } else {
      page = FileDetailPage(file: file);
    }
    Navigator.push(context, MaterialPageRoute(builder: (_) => page));
  }

  @override
  Widget buildSuggestions(BuildContext context) {
    final al = AppLocalizations.of(context);
    final colorScheme = Theme.of(context).colorScheme;

    if (query.trim().isEmpty) {
      return Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(
              _isTagSearch ? Icons.label_outline : Icons.search,
              size: 48,
              color: colorScheme.onSurfaceVariant,
            ),
            const SizedBox(height: PfmSpacing.lg),
            Text(
              _isTagSearch ? al.file_searchTagHint : al.file_searchFilenameHint,
              style: PfmTypography.bodyMedium.copyWith(
                color: colorScheme.onSurfaceVariant,
              ),
            ),
          ],
        ),
      );
    }
    return Center(
      child: Text(
        _isTagSearch ? al.file_searchTagEnter : al.file_searchFilenameEnter,
        style: PfmTypography.bodyMedium.copyWith(
          color: colorScheme.onSurfaceVariant,
        ),
      ),
    );
  }

  String _formatFileSize(int bytes) {
    if (bytes < 1024) return '$bytes B';
    if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
    if (bytes < 1024 * 1024 * 1024) {
      return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
    }
    return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(1)} GB';
  }
}
