import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import 'package:go_router/go_router.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/pages/files/file_preview_page.dart';
import 'package:pfm/pages/files/flyfish_viewer_page.dart';
import 'package:pfm/router/router.dart';
import 'package:pfm/services/local_storage.dart';

/// 首页 - 最近使用 + 分类浏览
class HomeIndexPage extends StatefulWidget {
  const HomeIndexPage({super.key});

  @override
  State<HomeIndexPage> createState() => _HomeIndexPageState();
}

class _HomeIndexPageState extends State<HomeIndexPage> {
  List<File> _recentFiles = [];
  bool _loadingRecent = true;

  @override
  void initState() {
    super.initState();
    _loadRecentFiles();
  }

  Future<void> _loadRecentFiles() async {
    final repo = FmRepository();
    final result = await repo.getRecentFiles(pageSize: 20);
    if (mounted && result.success && result.data != null) {
      setState(() {
        _recentFiles = result.data!.files;
        _loadingRecent = false;
      });
    } else if (mounted) {
      setState(() => _loadingRecent = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;

    return Scaffold(
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.symmetric(vertical: 16),
          children: [
            // 最近使用
            _buildSectionTitle(context, '最近使用', Icons.access_time),
            const SizedBox(height: 12),
            _buildRecentFiles(context),

            const SizedBox(height: 32),

            // 分类浏览
            _buildSectionTitle(context, '分类浏览', Icons.grid_view),
            const SizedBox(height: 12),
            _buildCategoryGrid(context),
          ],
        ),
      ),
    );
  }

  Widget _buildSectionTitle(
    BuildContext context,
    String title,
    IconData icon,
  ) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16),
      child: Row(
        children: [
          Icon(icon, size: 20, color: theme.colorScheme.primary),
          const SizedBox(width: 8),
          Text(
            title,
            style: theme.textTheme.titleMedium?.copyWith(
              fontWeight: FontWeight.bold,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildRecentFiles(BuildContext context) {
    if (_loadingRecent) {
      return const SizedBox(
        height: 120,
        child: Center(child: CircularProgressIndicator()),
      );
    }

    if (_recentFiles.isEmpty) {
      return SizedBox(
        height: 120,
        child: Center(
          child: Text(
            '暂无最近使用的文件',
            style: TextStyle(color: Colors.grey[500]),
          ),
        ),
      );
    }

    return SizedBox(
      height: 120,
      child: ListView.separated(
        scrollDirection: Axis.horizontal,
        padding: const EdgeInsets.symmetric(horizontal: 16),
        itemCount: _recentFiles.length,
        separatorBuilder: (_, __) => const SizedBox(width: 12),
        itemBuilder: (context, index) {
          final file = _recentFiles[index];
          return _RecentFileCard(file: file);
        },
      ),
    );
  }

  Widget _buildCategoryGrid(BuildContext context) {
    final categories = [
      _CategoryItem(
        icon: Icons.photo_outlined,
        label: '照片',
        color: Colors.blue,
        fileType: FileType.IMAGE,
      ),
      _CategoryItem(
        icon: Icons.videocam_outlined,
        label: '视频',
        color: Colors.red,
        fileType: FileType.VIDEO,
      ),
      _CategoryItem(
        icon: Icons.audiotrack_outlined,
        label: '音频',
        color: Colors.orange,
        fileType: FileType.AUDIO,
      ),
      _CategoryItem(
        icon: Icons.description_outlined,
        label: '文档',
        color: Colors.green,
        fileType: FileType.DOCUMENT,
      ),
      _CategoryItem(
        icon: Icons.archive_outlined,
        label: '压缩包',
        color: Colors.purple,
        fileType: FileType.ARCHIVE,
      ),
      _CategoryItem(
        icon: Icons.insert_drive_file_outlined,
        label: '其他',
        color: Colors.grey,
        fileType: FileType.FILE_TYPE_UNKNOWN,
      ),
    ];

    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16),
      child: GridView.builder(
        shrinkWrap: true,
        physics: const NeverScrollableScrollPhysics(),
        gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
          crossAxisCount: 3,
          mainAxisSpacing: 12,
          crossAxisSpacing: 12,
          childAspectRatio: 1.2,
        ),
        itemCount: categories.length,
        itemBuilder: (context, index) {
          final cat = categories[index];
          return _CategoryCard(
            item: cat,
            onTap: () {
              Navigator.push(
                context,
                MaterialPageRoute(
                  builder: (_) => CategoryFilesPage(
                    fileType: cat.fileType,
                    title: cat.label,
                  ),
                ),
              );
            },
          );
        },
      ),
    );
  }
}

/// 最近使用文件卡片
class _RecentFileCard extends StatelessWidget {
  final File file;

  const _RecentFileCard({required this.file});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;

    return SizedBox(
      width: 100,
      child: Column(
        children: [
          Container(
            width: 64,
            height: 64,
            decoration: BoxDecoration(
              color: colorScheme.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(12),
            ),
            child: Icon(
              _getFileIcon(file.type.value),
              size: 32,
              color: colorScheme.primary,
            ),
          ),
          const SizedBox(height: 8),
          Text(
            file.name,
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            textAlign: TextAlign.center,
            style: theme.textTheme.bodySmall,
          ),
        ],
      ),
    );
  }

  IconData _getFileIcon(int type) {
    switch (type) {
      case 2: // IMAGE
        return Icons.image;
      case 3: // VIDEO
        return Icons.videocam;
      case 4: // AUDIO
        return Icons.audiotrack;
      case 5: // DOCUMENT
        return Icons.description;
      case 6: // ARCHIVE
        return Icons.archive;
      default:
        return Icons.insert_drive_file;
    }
  }
}

/// 分类项目数据
class _CategoryItem {
  final IconData icon;
  final String label;
  final Color color;
  final FileType fileType;

  const _CategoryItem({
    required this.icon,
    required this.label,
    required this.color,
    required this.fileType,
  });
}

/// 分类卡片
class _CategoryCard extends StatelessWidget {
  final _CategoryItem item;
  final VoidCallback onTap;

  const _CategoryCard({required this.item, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Card(
      elevation: 0,
      color: item.color.withValues(alpha: 0.1),
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(12),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(item.icon, size: 32, color: item.color),
            const SizedBox(height: 8),
            Text(
              item.label,
              style: theme.textTheme.bodyMedium?.copyWith(
                fontWeight: FontWeight.w500,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// 分类文件列表页
class CategoryFilesPage extends StatefulWidget {
  final FileType fileType;
  final String title;

  const CategoryFilesPage({
    super.key,
    required this.fileType,
    required this.title,
  });

  @override
  State<CategoryFilesPage> createState() => _CategoryFilesPageState();
}

class _CategoryFilesPageState extends State<CategoryFilesPage> {
  final List<File> _files = [];
  bool _loading = true;
  bool _hasMore = true;
  int _offset = 0;
  static const int _pageSize = 50;

  @override
  void initState() {
    super.initState();
    _loadFiles();
  }

  Future<void> _loadFiles() async {
    final repo = FmRepository();
    final result = await repo.getFilesByType(
      fileType: widget.fileType,
      offset: _offset,
      pageSize: _pageSize,
    );
    if (mounted && result.success && result.data != null) {
      setState(() {
        _files.addAll(result.data!.files);
        _hasMore = result.data!.hasMore;
        _loading = false;
        _offset += result.data!.files.length;
      });
    } else if (mounted) {
      setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text(widget.title)),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : _files.isEmpty
              ? Center(
                  child: Text(
                    '暂无${widget.title}文件',
                    style: TextStyle(color: Colors.grey[500]),
                  ),
                )
              : ListView.builder(
                  itemCount: _files.length + (_hasMore ? 1 : 0),
                  itemBuilder: (context, index) {
                    if (index == _files.length) {
                      _loadFiles();
                      return const Padding(
                        padding: EdgeInsets.all(16),
                        child: Center(child: CircularProgressIndicator()),
                      );
                    }
                    final file = _files[index];
                    return _buildFileTile(context, file);
                  },
                ),
    );
  }

  Widget _buildFileTile(BuildContext context, File file) {
    final isImage = file.type.value == 2;
    final isVideo = file.type.value == 3;

    return ListTile(
      leading: isImage
          ? _buildImageThumbnail(file)
          : Icon(_getFileIcon(file.type.value)),
      title: Text(file.name, maxLines: 1, overflow: TextOverflow.ellipsis),
      subtitle: Text(_formatSize(file.size.toInt())),
      onTap: () => _handleFileTap(context, file),
      onLongPress: () => _handleFileLongPress(context, file),
    );
  }

  Widget _buildImageThumbnail(File file) {
    final ls = GetIt.I.get<LocalStorage>();
    final baseUrl = ls.fileServer ?? '';
    final previewUrl = '$baseUrl/api/file/stream/preview?file_id=${file.id}&size=small';
    final token = ls.token?.accessToken;
    final headers = <String, String>{};
    if (token != null) {
      headers['Authorization'] = 'Bearer $token';
    }
    return ClipRRect(
      borderRadius: BorderRadius.circular(4),
      child: Image.network(
        previewUrl,
        width: 40,
        height: 40,
        fit: BoxFit.cover,
        headers: headers,
        errorBuilder: (_, __, ___) => const Icon(Icons.image, size: 40),
      ),
    );
  }

  void _handleFileTap(BuildContext context, File file) {
    if (file.type.value == 2) {
      // 图片：传递所有图片列表支持左右滑动
      final images = _files.where((f) => f.type.value == 2).toList();
      final index = images.indexWhere((f) => f.id == file.id);
      context.push(
        FilePreviewRoute().location,
        extra: PreviewArgs(
          file: file,
          images: images,
          initialIndex: index >= 0 ? index : 0,
        ),
      );
    } else if (file.type.value == 3 || file.type.value == 4) {
      // 视频/音频：使用原生播放器预览
      context.push(FilePreviewRoute().location, extra: file);
    } else if (file.type.value == 5 || file.type.value == 6) {
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

  void _handleFileLongPress(BuildContext context, File file) {
    context.push(FileDetailRoute().location, extra: file);
  }

  IconData _getFileIcon(int type) {
    switch (type) {
      case 2:
        return Icons.image;
      case 3:
        return Icons.videocam;
      case 4:
        return Icons.audiotrack;
      case 5:
        return Icons.description;
      case 6:
        return Icons.archive;
      default:
        return Icons.insert_drive_file;
    }
  }

  String _formatSize(int bytes) {
    if (bytes < 1024) return '$bytes B';
    if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
    if (bytes < 1024 * 1024 * 1024) {
      return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
    }
    return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(1)} GB';
  }
}
