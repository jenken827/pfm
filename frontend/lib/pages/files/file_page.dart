import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:file_picker/file_picker.dart';
import 'package:get_it/get_it.dart';
import 'package:go_router/go_router.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/blocs/blocs.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/proto/native/enum.pbenum.dart' as pbenum;
import 'package:pfm/router/files_route.dart';
import 'package:pfm/pages/files/file_preview_page.dart';
import 'package:pfm/pages/files/flyfish_viewer_page.dart';
import 'package:pfm/pages/files/folder_picker_page.dart';
import 'package:pfm/services/local_storage.dart';
import 'package:pfm/utils/utils.dart';
import 'package:pfm/widgets/folder_settings_dialog.dart';

/// 文件列表页面
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
  TransferState? _previousTransferState;
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
    return MultiBlocListener(
      listeners: [
        // 监听 TransferBloc 状态变化，检测上传完成
        BlocListener<TransferBloc, TransferState>(
          listenWhen: (previous, current) {
            // 检测是否有新的上传任务完成
            return current.hasNewCompletedUploads(previous);
          },
          listener: (context, transferState) {
            // 只在上传完成的文件属于当前目录时才刷新
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
          // 处理失败状态，显示错误提示
          if (state.status == FileStateStatus.failure && state.msg.isNotEmpty) {
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(
                content: Text(state.msg),
                backgroundColor: Theme.of(context).colorScheme.error,
                behavior: SnackBarBehavior.floating,
              ),
            );
          }
          // 加载完成后恢复滚动位置
          if (_shouldRestoreScroll &&
              state.status == FileStateStatus.success) {
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
            appBar: _buildAppBar(context, state),
            body: Column(
              children: [
                // 面包屑导航
                if (state.breadcrumbs.isNotEmpty)
                  _buildBreadcrumbs(context, state),
                // 文件列表
                Expanded(child: _buildContent(context, state)),
              ],
            ),
            floatingActionButton: _buildFab(context, state),
          );
        },
      ),
    );
  }

  PreferredSizeWidget _buildAppBar(BuildContext context, FileState state) {
    // 如果在多选模式，显示不同的 AppBar
    if (state.isSelectMode) {
      return AppBar(
        leading: IconButton(
          icon: const Icon(Icons.close),
          onPressed: () =>
              context.read<FileBloc>().add(const FileSelectModeEvent()),
        ),
        title: Text('已选择 ${state.selectedFiles.length} 项'),
        actions: [
          IconButton(
            icon: const Icon(Icons.select_all),
            onPressed: () =>
                context.read<FileBloc>().add(const FileSelectAllEvent()),
          ),
        ],
      );
    }

    return AppBar(
      title: Text(state.currentDirName.isEmpty ? '文件' : state.currentDirName),
      centerTitle: true,
      actions: [
        // 刷新按钮
        IconButton(
          icon: const Icon(Icons.refresh),
          onPressed: () =>
              _saveScrollAndRefresh(),
        ),
        // 搜索按钮
        IconButton(
          icon: const Icon(Icons.search),
          onPressed: () => _showSearchDialog(context),
        ),
        // 更多选项菜单
        PopupMenuButton<String>(
          icon: const Icon(Icons.more_vert),
          onSelected: (value) => _handleMenuAction(context, value, state),
          itemBuilder: (context) => [
            // 视图切换
            PopupMenuItem(
              value: 'view',
              child: Row(
                children: [
                  Icon(state.isGridView ? Icons.view_list : Icons.grid_view),
                  const SizedBox(width: 8),
                  Text(state.isGridView ? '列表视图' : '网格视图'),
                ],
              ),
            ),
            // 回收站
            const PopupMenuItem(
              value: 'trash',
              child: Row(
                children: [
                  Icon(Icons.delete_outline),
                  SizedBox(width: 8),
                  Text('回收站'),
                ],
              ),
            ),
            const PopupMenuDivider(),
            // 排序选项
            const PopupMenuItem(value: 'sort_name', child: Text('按名称排序')),
            const PopupMenuItem(value: 'sort_size', child: Text('按大小排序')),
            const PopupMenuItem(value: 'sort_type', child: Text('按类型排序')),
            const PopupMenuItem(value: 'sort_modified', child: Text('按修改时间排序')),
            const PopupMenuItem(value: 'sort_created', child: Text('按创建时间排序')),
          ],
        ),
      ],
    );
  }

  Widget _buildContent(BuildContext context, FileState state) {
    // 处理加载状态（首次加载）
    if (state.status == FileStateStatus.loading) {
      return const Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            CircularProgressIndicator(),
            SizedBox(height: 16),
            Text('加载中...'),
          ],
        ),
      );
    }

    // 处理失败状态 - 已通过 listener 显示 snackbar，这里显示重试界面
    if (state.status == FileStateStatus.failure) {
      return Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(
              Icons.error_outline,
              size: 48,
              color: Theme.of(context).colorScheme.error,
            ),
            const SizedBox(height: 16),
            const Text('加载失败'),
            const SizedBox(height: 16),
            ElevatedButton.icon(
              onPressed: () =>
                  _saveScrollAndRefresh(),
              icon: const Icon(Icons.refresh),
              label: const Text('重试'),
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
            Icon(
              Icons.folder_open,
              size: 64,
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
            const SizedBox(height: 16),
            Text('空文件夹', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 8),
            Text(
              '点击下方按钮上传文件或创建文件夹',
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ),
          ],
        ),
      );
    }

    return state.isGridView
        ? _buildGridView(context, state)
        : _buildListView(context, state);
  }

  Widget _buildBreadcrumbs(BuildContext context, FileState state) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      child: Row(
        children: [
          Expanded(
            child: SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                children: state.breadcrumbs.map((crumb) {
                  final isLast = crumb == state.breadcrumbs.last;
                  return Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      InkWell(
                        onTap: isLast
                            ? null
                            : () => context.read<FileBloc>().add(
                                FileNavigateEvent(crumb.id, crumb.name),
                              ),
                        child: Padding(
                          padding: const EdgeInsets.symmetric(horizontal: 4),
                          child: Text(
                            crumb.name,
                            style: TextStyle(
                              color: isLast
                                  ? Theme.of(context).colorScheme.primary
                                  : Theme.of(
                                      context,
                                    ).colorScheme.onSurfaceVariant,
                              fontWeight: isLast
                                  ? FontWeight.bold
                                  : FontWeight.normal,
                            ),
                          ),
                        ),
                      ),
                      if (!isLast)
                        Icon(
                          Icons.chevron_right,
                          size: 16,
                          color: Theme.of(context).colorScheme.onSurfaceVariant,
                        ),
                    ],
                  );
                }).toList(),
              ),
            ),
          ),
          // 文件数量
          Padding(
            padding: const EdgeInsets.only(left: 8),
            child: Text(
              state.total > 0 ? '${state.total}' : '${state.files.length}',
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildListView(BuildContext context, FileState state) {
    return NotificationListener<ScrollNotification>(
      onNotification: (notification) {
        if (notification is ScrollEndNotification &&
            notification.metrics.pixels >=
                notification.metrics.maxScrollExtent - 100) {
          // 距离底部100像素时触发加载更多
          context.read<FileBloc>().add(const FileLoadMoreEvent());
        }
        return false;
      },
      child: ListView.builder(
        controller: _scrollController,
        itemCount: state.files.length + (state.hasMore ? 1 : 0),
        itemBuilder: (context, index) {
          // 显示加载更多指示器
          if (index == state.files.length) {
            return const Padding(
              padding: EdgeInsets.all(16),
              child: Center(child: CircularProgressIndicator()),
            );
          }

          final file = state.files[index];
          final isSelected = state.selectedFiles.contains(file);

          return _FileListTile(
            file: file,
            isSelected: isSelected,
            isSelectMode: state.isSelectMode,
            onTap: () => _handleFileTap(context, file, state),
            onLongPress: () => _handleFileLongPress(context, file),
            onMenuPressed: () => _showFileMenu(context, file),
          );
        },
      ),
    );
  }

  Widget _buildGridView(BuildContext context, FileState state) {
    return NotificationListener<ScrollNotification>(
      onNotification: (notification) {
        if (notification is ScrollEndNotification &&
            notification.metrics.pixels >=
                notification.metrics.maxScrollExtent - 100) {
          // 距离底部100像素时触发加载更多
          context.read<FileBloc>().add(const FileLoadMoreEvent());
        }
        return false;
      },
      child: LayoutBuilder(
        builder: (context, constraints) {
          // 根据屏幕宽度动态计算列数
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
            padding: const EdgeInsets.all(8),
            gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
              crossAxisCount: crossAxisCount,
              childAspectRatio: 0.85,
              crossAxisSpacing: 8,
              mainAxisSpacing: 8,
            ),
            itemCount: state.files.length + (state.hasMore ? 1 : 0),
            itemBuilder: (context, index) {
              // 显示加载更多指示器
              if (index == state.files.length) {
                return const Center(child: CircularProgressIndicator());
              }

              final file = state.files[index];
              final isSelected = state.selectedFiles.contains(file);

              return _FileGridTile(
                file: file,
                isSelected: isSelected,
                isSelectMode: state.isSelectMode,
                onTap: () => _handleFileTap(context, file, state),
                onLongPress: () => _handleFileLongPress(context, file),
              );
            },
          );
        },
      ),
    );
  }

  Widget? _buildFab(BuildContext context, FileState state) {
    if (state.isSelectMode) {
      return FloatingActionButton.extended(
        onPressed: () => _showBatchActionMenu(context, state),
        icon: const Icon(Icons.more_horiz),
        label: Text('批量操作 (${state.selectedFiles.length})'),
      );
    }

    return FloatingActionButton(
      onPressed: () => _showFabMenu(context),
      child: const Icon(Icons.add),
    );
  }

  void _handleFileTap(BuildContext context, File file, FileState state) {
    if (state.isSelectMode) {
      context.read<FileBloc>().add(FileSelectEvent(file));
    } else {
      // 目录则进入
      if (file.type == pbenum.FileType.FOLDER) {
        context.read<FileBloc>().add(FileNavigateEvent(file.id, file.name));
      } else {
        // 文件则根据类型跳转
        if (file.type == pbenum.FileType.IMAGE) {
          // 图片：传递图片列表支持左右滑动
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
          // 视频/音频：使用原生播放器预览
          context.push(FilePreviewRoute().location, extra: file);
        } else if (file.type == pbenum.FileType.DOCUMENT ||
            file.type == pbenum.FileType.ARCHIVE) {
          // 文档/压缩包：使用 Flyfish Viewer
          Navigator.push(
            context,
            MaterialPageRoute(
              builder: (_) => FlyfishViewerPage(file: file),
            ),
          );
        } else {
          // 其他类型：跳转到详情页
          context.push(FileDetailRoute().location, extra: file);
        }
      }
    }
  }

  /// 判断文件是否支持预览
  bool _isPreviewable(File file) {
    // 支持预览的文件类型：图片、视频、音频、文档
    return file.type == pbenum.FileType.IMAGE ||
        file.type == pbenum.FileType.VIDEO ||
        file.type == pbenum.FileType.AUDIO ||
        file.type == pbenum.FileType.DOCUMENT;
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

  void _showSearchDialog(BuildContext context) {
    showSearch(
      context: context,
      delegate: _FileSearchDelegate(context.read<FileBloc>()),
    );
  }

  void _showFileMenu(BuildContext context, File file) {
    final isFolder = file.type == pbenum.FileType.FOLDER;

    showModalBottomSheet(
      context: context,
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            // 查看详情
            ListTile(
              leading: const Icon(Icons.info_outline),
              title: const Text('查看详情'),
              onTap: () {
                Navigator.pop(ctx);
                context.push(FileDetailRoute().location, extra: file);
              },
            ),
            // 下载（仅文件，文件夹不显示）
            if (!isFolder)
              ListTile(
                leading: const Icon(Icons.download),
                title: const Text('下载'),
                onTap: () async {
                  Navigator.pop(ctx);
                  await _downloadFile(context, file);
                },
              ),
            // 重命名
            ListTile(
              leading: const Icon(Icons.edit),
              title: const Text('重命名'),
              onTap: () {
                Navigator.pop(ctx);
                _showRenameDialog(context, file);
              },
            ),
            // 复制链接（仅文件）
            if (!isFolder)
              ListTile(
                leading: const Icon(Icons.link),
                title: const Text('复制链接'),
                onTap: () async {
                  Navigator.pop(ctx);
                  try {
                    final fmRepo = context.read<FmRepository>();
                    final result = await fmRepo.createShareLink(
                      fileId: file.id,
                    );
                    if (result.success && result.data != null) {
                      final url = result.data!.shareUrl;
                      await Clipboard.setData(ClipboardData(text: url));
                      if (context.mounted) {
                        ScaffoldMessenger.of(context).showSnackBar(
                          const SnackBar(content: Text('链接已复制到剪贴板')),
                        );
                      }
                    } else {
                      if (context.mounted) {
                        ScaffoldMessenger.of(context).showSnackBar(
                          SnackBar(
                            content: Text(
                              '创建链接失败: ${result.message ?? "未知错误"}',
                            ),
                          ),
                        );
                      }
                    }
                  } catch (e) {
                    if (context.mounted) {
                      ScaffoldMessenger.of(
                        context,
                      ).showSnackBar(SnackBar(content: Text('获取链接失败: $e')));
                    }
                  }
                },
              ),
            // 移动
            ListTile(
              leading: const Icon(Icons.drive_file_move),
              title: const Text('移动'),
              onTap: () async {
                Navigator.pop(ctx);
                await _showMoveDialog(context, [file]);
              },
            ),
            // 修改标签（仅文件）
            if (!isFolder)
              ListTile(
                leading: const Icon(Icons.label),
                title: const Text('修改标签'),
                onTap: () {
                  Navigator.pop(ctx);
                  _showTagsDialog(context, file);
                },
              ),
            // 设置（仅文件夹）
            if (isFolder)
              ListTile(
                leading: const Icon(Icons.settings),
                title: const Text('设置'),
                onTap: () {
                  Navigator.pop(ctx);
                  _showFolderSettings(context, file);
                },
              ),
            // 删除
            ListTile(
              leading: Icon(
                Icons.delete,
                color: Theme.of(context).colorScheme.error,
              ),
              title: Text(
                '删除',
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
              onTap: () {
                Navigator.pop(ctx);
                _showDeleteConfirm(context, [file.id]);
              },
            ),
          ],
        ),
      ),
    );
  }

  /// 下载文件
  Future<void> _downloadFile(BuildContext context, File file) async {
    try {
      final downloadDir = await getDownloadsDirectory();
      if (downloadDir == null) return;
      final fileName = file.name.isNotEmpty ? file.name : 'unnamed';
      final initialPath = '${downloadDir.path}/$fileName';
      // 生成唯一文件路径，避免覆盖
      final savePath = await FileUtils.getUniqueFilePath(initialPath);
      if (!context.mounted) return;
      GetIt.I.get<TransferBloc>().add(
        TransferAddDownloadEvent(
          fileId: file.id,
          filename: fileName,
          fileSize: file.size.toInt(),
          savePath: savePath,
        ),
      );
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('已开始下载: $fileName')));
    } catch (e) {
      if (context.mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('下载失败: $e')));
      }
    }
  }

  void _showFabMenu(BuildContext context) {
    showModalBottomSheet(
      context: context,
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              leading: const Icon(Icons.create_new_folder),
              title: const Text('新建文件夹'),
              onTap: () {
                Navigator.pop(ctx);
                _showCreateDirDialog(context);
              },
            ),
            ListTile(
              leading: const Icon(Icons.upload_file),
              title: const Text('上传文件'),
              onTap: () {
                Navigator.pop(ctx);
                _pickAndUploadFiles(context);
              },
            ),
            ListTile(
              leading: const Icon(Icons.folder),
              title: const Text('上传文件夹'),
              onTap: () {
                Navigator.pop(ctx);
                _pickAndUploadFolder(context);
              },
            ),
          ],
        ),
      ),
    );
  }

  /// 选择并上传文件
  Future<void> _pickAndUploadFiles(BuildContext context) async {
    final tagsController = TextEditingController();
    bool overwrite = false;

    // 先弹出对话框让用户输入标签和选择是否覆盖
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setState) => AlertDialog(
          title: const Text('上传文件'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: tagsController,
                decoration: const InputDecoration(
                  labelText: '文件标签',
                  hintText: '可选，多个标签用逗号分隔',
                  prefixIcon: Icon(Icons.label_outline),
                ),
              ),
              const SizedBox(height: 16),
              SwitchListTile(
                title: const Text('覆盖同名文件'),
                subtitle: const Text('如果存在同名文件则覆盖'),
                value: overwrite,
                onChanged: (value) {
                  setState(() => overwrite = value);
                },
              ),
            ],
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(ctx, true),
              child: const Text('选择文件'),
            ),
          ],
        ),
      ),
    );

    if (confirmed != true || !context.mounted) return;

    // 用户确认后再选择文件
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

        // 收集所有文件路径，批量提交
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
            content: Text('选择文件失败: $e'),
            backgroundColor: Theme.of(context).colorScheme.error,
          ),
        );
      }
    }
  }

  /// 选择并上传文件夹
  Future<void> _pickAndUploadFolder(BuildContext context) async {
    try {
      final result = await FilePicker.platform.getDirectoryPath();

      if (result != null && context.mounted) {
        final state = context.read<FileBloc>().state;
        final parentId = state.currentDirId;

        // 显示进度对话框
        showDialog(
          context: context,
          barrierDismissible: false,
          builder: (dialogContext) =>
              const Center(child: CircularProgressIndicator()),
        );

        try {
          // 递归遍历文件夹并上传
          await _uploadFolder(
            context: context,
            folderPath: result,
            parentDirId: parentId,
          );

          // 关闭进度对话框
          if (context.mounted) {
            Navigator.of(context, rootNavigator: true).pop();

            ScaffoldMessenger.of(context).showSnackBar(
              const SnackBar(
                content: Text('文件夹上传任务已添加'),
                behavior: SnackBarBehavior.floating,
              ),
            );
          }
        } catch (e) {
          // 关闭进度对话框
          if (context.mounted) {
            Navigator.of(context, rootNavigator: true).pop();

            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(
                content: Text('上传文件夹失败: $e'),
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
            content: Text('选择文件夹失败: $e'),
            backgroundColor: Theme.of(context).colorScheme.error,
          ),
        );
      }
    }
  }

  /// 递归上传文件夹
  ///
  /// [folderPath] 本地文件夹路径
  /// [parentDirId] 服务器端父目录 ID
  /// [tags] 标签（可选）
  /// [overwrite] 是否覆盖同名文件
  Future<void> _uploadFolder({
    required BuildContext context,
    required String folderPath,
    required String parentDirId,
    String? tags,
    bool overwrite = false,
  }) async {
    final folderName = p.basename(folderPath);
    final fmRepo = context.read<FmRepository>();

    // 1. 在服务器端创建对应的文件夹（带重试）
    // 不传递 enableEncryption，让后端从父文件夹继承
    final createResult = await _createDirWithRetry(
      fmRepo: fmRepo,
      name: folderName,
      parentId: parentDirId,
    );

    if (!createResult.success) {
      throw Exception('创建目录失败 "$folderName": ${createResult.message}');
    }

    // 2. 从响应数据中获取目录 ID
    final respData = createResult.data;
    if (respData == null || !respData.hasId()) {
      throw Exception('创建目录失败 "$folderName"：未返回目录 ID');
    }

    final newDirId = respData.id;

    // 3. 遍历本地文件夹，收集所有文件
    final dir = Directory(folderPath);
    List<FileSystemEntity> entities;

    try {
      entities = dir.listSync(recursive: false);
    } catch (e) {
      throw Exception('读取目录失败: $e');
    }

    // 收集本目录下的文件路径
    final filePaths = <String>[];
    // 子目录列表
    final subDirs = <String>[];

    for (final entity in entities) {
      final type = FileSystemEntity.typeSync(entity.path);
      if (type == FileSystemEntityType.directory) {
        subDirs.add(entity.path);
      } else if (type == FileSystemEntityType.file) {
        filePaths.add(entity.path);
      }
    }

    // 批量提交本目录的文件
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

    // 递归处理子目录
    for (final subDir in subDirs) {
      if (!context.mounted) return;

      // 添加延迟以避免触发频率限制（每个端点限制 3 次/秒）
      await Future.delayed(const Duration(milliseconds: 350));

      await _uploadFolder(
        context: context,
        folderPath: subDir,
        parentDirId: newDirId,
        tags: tags,
        overwrite: overwrite,
      );
    }
  }

  /// 创建目录（带重试，处理频率限制）
  /// [enableEncryption] 加密设置，null 表示从父文件夹继承
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

      // 如果成功，直接返回
      if (result.success) {
        return result;
      }

      // 检查是否是频率限制错误
      final isRateLimitError =
          result.message != null && result.message!.contains('429');

      if (!isRateLimitError) {
        // 不是频率限制错误，直接返回失败结果
        return result;
      }

      // 频率限制错误，等待后重试
      retryCount++;
      if (retryCount < maxRetries) {
        final waitTime = Duration(seconds: retryCount);

        await Future.delayed(waitTime);
      }
    }

    // 重试失败，返回最后一次结果
    return fmRepo.createDir(
      name: name,
      parentId: parentId,
      enableEncryption: enableEncryption,
    );
  }

  void _showBatchActionMenu(BuildContext context, FileState state) {
    showModalBottomSheet(
      context: context,
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              leading: const Icon(Icons.select_all),
              title: const Text('全选'),
              onTap: () {
                Navigator.pop(ctx);
                context.read<FileBloc>().add(const FileSelectAllEvent());
              },
            ),
            ListTile(
              leading: const Icon(Icons.drive_file_move),
              title: Text('移动已选 (${state.selectedFiles.length})'),
              onTap: () {
                Navigator.pop(ctx);
                _showMoveDialog(context, state.selectedFiles.toList());
              },
            ),
            ListTile(
              leading: Icon(
                Icons.delete,
                color: Theme.of(context).colorScheme.error,
              ),
              title: Text(
                '删除已选 (${state.selectedFiles.length})',
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
              onTap: () {
                Navigator.pop(ctx);
                final ids = state.selectedFiles.map((f) => f.id).toList();
                _showDeleteConfirm(context, ids);
              },
            ),
            ListTile(
              leading: const Icon(Icons.close),
              title: const Text('取消选择'),
              onTap: () {
                Navigator.pop(ctx);
                context.read<FileBloc>().add(const FileSelectModeEvent());
              },
            ),
          ],
        ),
      ),
    );
  }

  void _showCreateDirDialog(BuildContext context) {
    final nameController = TextEditingController();
    final maxHistoriesController = TextEditingController();
    bool enableEncryption = false;
    List<int> selectedProviderIds = [];

    // 从 Bloc 获取存储提供者列表
    final fileState = context.read<FileBloc>().state;
    final providers = fileState.storageProviders;
    // 默认选中 defaultBound 为 true 的存储提供者
    selectedProviderIds = providers
        .where((p) => p.defaultBound)
        .map((p) => p.id)
        .toList();

    showDialog(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (builderctx, setState) => AlertDialog(
          title: const Text('新建文件夹'),
          content: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                TextField(
                  controller: nameController,
                  decoration: const InputDecoration(
                    labelText: '文件夹名称',
                    hintText: '请输入文件夹名称',
                  ),
                  autofocus: true,
                ),
                const SizedBox(height: 16),
                // 存储提供者选择
                if (providers.isNotEmpty) ...[
                  Text(
                    '绑定存储提供者',
                    style: Theme.of(context).textTheme.titleSmall,
                  ),
                  const SizedBox(height: 8),
                  ...providers.map(
                    (provider) => CheckboxListTile(
                      title: Text(provider.name),
                      subtitle: Text(provider.enabled ? '已启用' : '已禁用'),
                      value: selectedProviderIds.contains(provider.id),
                      enabled: provider.enabled,
                      onChanged: (checked) {
                        setState(() {
                          if (checked == true) {
                            selectedProviderIds.add(provider.id);
                          } else {
                            selectedProviderIds.remove(provider.id);
                          }
                        });
                      },
                      dense: true,
                      controlAffinity: ListTileControlAffinity.leading,
                      contentPadding: EdgeInsets.zero,
                    ),
                  ),
                  const SizedBox(height: 16),
                ],
                // 加密选项
                SwitchListTile(
                  title: const Text('启用加密'),
                  subtitle: const Text('对文件夹内容进行加密存储'),
                  value: enableEncryption,
                  onChanged: (value) {
                    setState(() => enableEncryption = value);
                  },
                ),
                const SizedBox(height: 16),
                // 最大历史记录数
                TextField(
                  controller: maxHistoriesController,
                  decoration: const InputDecoration(
                    labelText: '最大历史记录数',
                    hintText: '默认 0',
                  ),
                  keyboardType: TextInputType.number,
                ),
                const SizedBox(height: 4),
                const Text(
                  '覆盖文件时保留的历史版本数量，默认 0 表示不保留历史',
                  style: TextStyle(fontSize: 12, color: Colors.grey),
                ),
                if (enableEncryption) ...[const Divider()],
              ],
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () {
                if (nameController.text.isNotEmpty) {
                  // 解析 maxHistories
                  int? maxHistories;
                  if (maxHistoriesController.text.isNotEmpty) {
                    maxHistories = int.tryParse(maxHistoriesController.text);
                    if (maxHistories == null || maxHistories < 0) {
                      ScaffoldMessenger.of(context).showSnackBar(
                        const SnackBar(content: Text('最大历史记录数必须是有效的非负整数')),
                      );
                      return;
                    }
                  }
                  context.read<FileBloc>().add(
                    FileCreateDirEvent(
                      name: nameController.text,
                      enableEncryption: enableEncryption,
                      providerIds: selectedProviderIds.isEmpty
                          ? null
                          : selectedProviderIds,
                      maxHistories: maxHistories,
                    ),
                  );
                  Navigator.pop(ctx);
                }
              },
              child: const Text('创建'),
            ),
          ],
        ),
      ),
    );
  }

  void _showRenameDialog(BuildContext context, File file) {
    final controller = TextEditingController(text: file.name);
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('重命名'),
        content: TextField(
          controller: controller,
          decoration: const InputDecoration(hintText: '请输入新名称'),
          autofocus: true,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('取消'),
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
            child: const Text('确定'),
          ),
        ],
      ),
    );
  }

  /// 显示修改标签对话框
  /// 注意：现在使用专门的 GetFileTags API 获取标签，提高了性能
  void _showTagsDialog(BuildContext context, File file) async {
    // 获取文件标签
    final fmRepo = context.read<FmRepository>();
    final tagsResult = await fmRepo.getFileTags(fileId: file.id);

    List<String> currentTags = [];
    if (tagsResult.success && tagsResult.data != null) {
      currentTags = List<String>.from(tagsResult.data!);
    } else {
      // 如果获取标签失败，显示错误信息
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('获取文件标签失败: ${tagsResult.message}')),
        );
      }
      return;
    }

    final controller = TextEditingController(text: currentTags.join(','));

    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('修改标签'),
        content: TextField(
          controller: controller,
          decoration: const InputDecoration(
            hintText: '输入标签，用逗号分隔',
            border: OutlineInputBorder(),
          ),
          autofocus: true,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () async {
              // 解析输入的标签
              final input = controller.text.trim();
              final newTags = input.isEmpty
                  ? <String>[]
                  : input
                        .split(',')
                        .map((tag) => tag.trim())
                        .where((tag) => tag.isNotEmpty)
                        .toList();

              // 更新文件标签
              final result = await fmRepo.updateFileTags(
                fileId: file.id,
                tags: newTags,
              );

              if (context.mounted) {
                if (result.success) {
                  // 成功更新后刷新文件列表
                  _saveScrollAndRefresh();
                  ScaffoldMessenger.of(
                    context,
                  ).showSnackBar(const SnackBar(content: Text('标签更新成功')));
                } else {
                  ScaffoldMessenger.of(context).showSnackBar(
                    SnackBar(content: Text('标签更新失败: ${result.message}')),
                  );
                }
              }

              Navigator.pop(ctx);
            },
            child: const Text('保存'),
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
          // 刷新文件列表
          _saveScrollAndRefresh();
        },
      ),
    );
  }

  void _showDeleteConfirm(BuildContext context, List<String> ids) {
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('确认删除'),
        content: Text('确定要删除 ${ids.length} 个文件吗？文件将移至回收站。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('取消'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
              backgroundColor: Theme.of(context).colorScheme.error,
            ),
            onPressed: () {
              context.read<FileBloc>().add(FileDeleteEvent(ids));
              Navigator.pop(ctx);
            },
            child: const Text('删除'),
          ),
        ],
      ),
    );
  }

  /// 显示移动文件对话框
  Future<void> _showMoveDialog(BuildContext context, List<File> files) async {
    // 要排除的文件夹ID（不能移动到自己或子文件夹）
    final fileIds = files.map((f) => f.id).toList();

    // 导航到选择文件夹页面
    final pickResult = await context.push<FolderPickResult>(
      FolderPickerRoute().location,
      extra: {'initialFolderId': '', 'excludeIds': fileIds},
    );

    // 用户取消选择
    if (pickResult == null || !context.mounted) return;

    // 用户选择了目标文件夹，执行移动
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
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(SnackBar(content: Text('成功移动 $successCount 个文件')));
        } else {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text('成功移动 $successCount 个文件，失败 ${failedIds.length} 个'),
              backgroundColor: Theme.of(context).colorScheme.error,
            ),
          );
        }
        // 刷新文件列表
        _saveScrollAndRefresh();
      } else {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('移动失败: ${moveResult.message ?? "未知错误"}'),
            backgroundColor: Theme.of(context).colorScheme.error,
          ),
        );
      }
    }
  }
}

/// 文件列表项组件
class _FileListTile extends StatelessWidget {
  const _FileListTile({
    required this.file,
    required this.isSelected,
    required this.isSelectMode,
    required this.onTap,
    required this.onLongPress,
    required this.onMenuPressed,
  });

  final File file;
  final bool isSelected;
  final bool isSelectMode;
  final VoidCallback onTap;
  final VoidCallback onLongPress;
  final VoidCallback onMenuPressed;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      leading: _buildIcon(),
      title: Text(file.name, maxLines: 1, overflow: TextOverflow.ellipsis),
      subtitle: Text(_formatFileInfo()),
      trailing: isSelectMode
          ? Checkbox(value: isSelected, onChanged: (_) => onTap())
          : IconButton(
              icon: const Icon(Icons.more_vert),
              onPressed: onMenuPressed,
            ),
      onTap: onTap,
      onLongPress: onLongPress,
    );
  }

  Widget _buildIcon() {
    IconData icon;
    Color color;

    switch (file.type) {
      case pbenum.FileType.FOLDER:
        icon = Icons.folder;
        color = Colors.amber;
        break;
      case pbenum.FileType.IMAGE:
        icon = Icons.image;
        color = Colors.green;
        break;
      case pbenum.FileType.VIDEO:
        icon = Icons.video_file;
        color = Colors.red;
        break;
      case pbenum.FileType.AUDIO:
        icon = Icons.audio_file;
        color = Colors.purple;
        break;
      case pbenum.FileType.DOCUMENT:
        icon = Icons.description;
        color = Colors.blue;
        break;
      case pbenum.FileType.ARCHIVE:
        icon = Icons.archive;
        color = Colors.orange;
        break;
      default:
        icon = Icons.insert_drive_file;
        color = Colors.grey;
    }

    return Icon(icon, color: color, size: 32);
  }

  String _formatFileInfo() {
    final size = file.size.toInt();
    final updatedAt = file.updatedAt;

    final sizeStr = _formatSize(size);
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
}

/// 文件网格项组件
class _FileGridTile extends StatelessWidget {
  const _FileGridTile({
    required this.file,
    required this.isSelected,
    required this.isSelectMode,
    required this.onTap,
    required this.onLongPress,
  });

  final File file;
  final bool isSelected;
  final bool isSelectMode;
  final VoidCallback onTap;
  final VoidCallback onLongPress;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;

    return GestureDetector(
      onTap: onTap,
      onLongPress: onLongPress,
      child: Card(
        elevation: isSelected ? 2 : 0,
        color: isSelected ? colorScheme.primaryContainer : null,
        clipBehavior: Clip.antiAlias,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            // 图标/缩略图区域（占满上方空间）
            Expanded(
              child: Stack(
                alignment: Alignment.center,
                children: [
                  // 图标或缩略图
                  _buildIcon(),
                  // 选择模式下的复选框
                  if (isSelectMode)
                    Positioned(
                      top: 4,
                      right: 4,
                      child: Container(
                        decoration: BoxDecoration(
                          color: Colors.white.withOpacity(0.8),
                          shape: BoxShape.circle,
                        ),
                        child: Checkbox(
                          value: isSelected,
                          onChanged: (_) => onTap(),
                          materialTapTargetSize:
                              MaterialTapTargetSize.shrinkWrap,
                          visualDensity: VisualDensity.compact,
                        ),
                      ),
                    ),
                ],
              ),
            ),
            // 文件名区域
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
              color: isSelected
                  ? colorScheme.primaryContainer.withOpacity(0.5)
                  : colorScheme.surfaceContainerHighest.withOpacity(0.3),
              child: Text(
                file.name,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                textAlign: TextAlign.center,
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildIcon() {
    // 图片类型使用缩略图
    if (file.type == pbenum.FileType.IMAGE) {
      return _buildThumbnail();
    }

    IconData icon;
    Color color;
    double size = 48;

    switch (file.type) {
      case pbenum.FileType.FOLDER:
        icon = Icons.folder;
        color = Colors.amber;
        size = 56;
        break;
      case pbenum.FileType.VIDEO:
        icon = Icons.video_file;
        color = Colors.red;
        break;
      case pbenum.FileType.AUDIO:
        icon = Icons.audio_file;
        color = Colors.purple;
        break;
      case pbenum.FileType.DOCUMENT:
        icon = Icons.description;
        color = Colors.blue;
        break;
      case pbenum.FileType.ARCHIVE:
        icon = Icons.archive;
        color = Colors.orange;
        break;
      default:
        icon = Icons.insert_drive_file;
        color = Colors.grey;
    }

    return Icon(icon, color: color, size: size);
  }

  Widget _buildThumbnail() {
    final ls = GetIt.I.get<LocalStorage>();
    final baseUrl = ls.fileServer ?? '';
    // 网格视图使用小尺寸缩略图 (200)
    final previewUrl =
        '$baseUrl/api/file/stream/preview?file_id=${file.id}&size=small';
    final token = ls.token?.accessToken;
    final headers = <String, String>{};
    if (token != null) {
      headers['Authorization'] = 'Bearer $token';
    }

    return Container(
      width: double.infinity,
      height: double.infinity,
      color: Colors.grey[100],
      child: Image.network(
        previewUrl,
        headers: headers,
        fit: BoxFit.cover,
        errorBuilder: (_, _, _) => const Center(
          child: Icon(Icons.broken_image, color: Colors.grey, size: 40),
        ),
        loadingBuilder: (_, child, loadingProgress) {
          if (loadingProgress == null) return child;
          return const Center(
            child: SizedBox(
              width: 24,
              height: 24,
              child: CircularProgressIndicator(strokeWidth: 2),
            ),
          );
        },
      ),
    );
  }
}

/// 文件搜索代理
class _FileSearchDelegate extends SearchDelegate<String> {
  final FileBloc _bloc;
  bool _isTagSearch = false;

  _FileSearchDelegate(this._bloc);

  @override
  List<Widget> buildActions(BuildContext context) {
    return [
      IconButton(
        icon: Icon(_isTagSearch ? Icons.label : Icons.search),
        tooltip: _isTagSearch ? '切换为按文件名搜索' : '切换为按标签搜索',
        onPressed: () {
          _isTagSearch = !_isTagSearch;
          // 触发 UI 重建
          showResults(context);
          // 如果已有搜索词，立即重新搜索
          if (query.trim().isNotEmpty) {
            _bloc.add(FileSearchEvent(query, isTagSearch: _isTagSearch));
          }
        },
      ),
      IconButton(icon: const Icon(Icons.clear), onPressed: () => query = ''),
    ];
  }

  @override
  Widget buildLeading(BuildContext context) {
    return IconButton(
      icon: const Icon(Icons.arrow_back),
      onPressed: () {
        // 关闭搜索时清空搜索状态，恢复文件列表
        _bloc.add(const FileSearchEvent(''));
        close(context, '');
      },
    );
  }

  @override
  Widget buildResults(BuildContext context) {
    if (query.trim().isEmpty) {
      return const Center(child: Text('请输入关键词搜索'));
    }

    _bloc.add(FileSearchEvent(query, isTagSearch: _isTagSearch));

    return BlocBuilder<FileBloc, FileState>(
      bloc: _bloc,
      builder: (context, state) {
        if (state.status == FileStateStatus.loading) {
          return const Center(child: CircularProgressIndicator());
        }
        if (state.status == FileStateStatus.failure) {
          return Center(child: Text('搜索失败: ${state.msg}'));
        }
        if (state.files.isEmpty) {
          return const Center(child: Text('没有找到匹配的文件'));
        }
        return ListView.builder(
          itemCount: state.files.length,
          itemBuilder: (context, index) {
            final file = state.files[index];
            final fileName = file.hasName() ? file.name : '未知文件';
            final fileSize = file.hasSize() ? file.size.toInt() : 0;
            final fileId = file.hasId() ? file.id : '';

            return ListTile(
              leading: const Icon(Icons.insert_drive_file),
              title: Text(fileName, maxLines: 2, overflow: TextOverflow.ellipsis),
              subtitle: Text(_formatFileSize(fileSize)),
              onTap: () {
                close(context, fileId);
              },
            );
          },
        );
      },
    );
  }

  @override
  Widget buildSuggestions(BuildContext context) {
    if (query.trim().isEmpty) {
      return Center(
        child: Text(_isTagSearch ? '输入标签关键词搜索' : '输入关键词搜索文件'),
      );
    }
    return Center(
      child: Text(_isTagSearch ? '按回车搜索标签' : '按回车搜索文件名'),
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
