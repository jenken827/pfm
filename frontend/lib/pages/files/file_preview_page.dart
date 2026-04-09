import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/services/local_storage.dart';
import 'package:pfm/router/files_route.dart';

/// 预览页面参数
class PreviewArgs {
  const PreviewArgs({required this.file, this.images, this.initialIndex = 0});

  /// 当前文件
  final File file;

  /// 当前文件夹的所有图片文件（仅图片类型预览时使用）
  final List<File>? images;

  /// 当前图片在 images 中的初始索引
  final int initialIndex;
}

/// 文件预览页面
class FilePreviewPage extends StatefulWidget {
  const FilePreviewPage({super.key, required this.file, this.images, this.initialIndex = 0});

  final File file;
  final List<File>? images;
  final int initialIndex;

  @override
  State<FilePreviewPage> createState() => _FilePreviewPageState();
}

class _FilePreviewPageState extends State<FilePreviewPage> {
  late File _currentFile;
  late PageController _pageController;
  List<File>? _images;
  int _currentPage = 0;

  @override
  void initState() {
    super.initState();
    _currentFile = widget.file;
    _images = widget.images;
    _currentPage = widget.initialIndex;
    _pageController = PageController(initialPage: _currentPage);
  }

  @override
  void dispose() {
    _pageController.dispose();
    super.dispose();
  }

  bool get _isImage => _currentFile.type == FileType.IMAGE;
  bool get _hasMultipleImages => _images != null && _images!.length > 1;

  /// 构建预览流URL
  String _buildPreviewUrl(String fileId) {
    final ls = GetIt.I.get<LocalStorage>();
    final baseUrl = ls.fileServer ?? '';
    return '$baseUrl/api/file/stream/preview?file_id=$fileId';
  }

  /// 构建下载流URL
  String _buildDownloadUrl() {
    final ls = GetIt.I.get<LocalStorage>();
    final baseUrl = ls.fileServer ?? '';
    return '$baseUrl/api/file/stream/download?file_id=${_currentFile.id}';
  }

  /// 获取认证请求头
  Map<String, String> _getAuthHeaders() {
    final ls = GetIt.I.get<LocalStorage>();
    final token = ls.token?.accessToken;
    if (token != null) {
      return {'Authorization': 'Bearer $token'};
    }
    return {};
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text(_currentFile.name),
        centerTitle: true,
        actions: [
          // 图片计数指示
          if (_hasMultipleImages)
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 8),
              child: Center(
                child: Text(
                  '${_currentPage + 1}/${_images!.length}',
                  style: Theme.of(context).textTheme.bodyMedium,
                ),
              ),
            ),
          IconButton(
            icon: const Icon(Icons.info_outline),
            onPressed: () {
              FileDetailRoute().push(context, extra: _currentFile);
            },
          ),
          IconButton(
            icon: const Icon(Icons.download),
            onPressed: () => _downloadFile(context),
          ),
          PopupMenuButton<String>(
            icon: const Icon(Icons.more_vert),
            onSelected: (value) => _handleMenuAction(context, value),
            itemBuilder: (context) => [
              const PopupMenuItem(
                value: 'share',
                child: ListTile(leading: Icon(Icons.share), title: Text('分享')),
              ),
              const PopupMenuItem(
                value: 'rename',
                child: ListTile(leading: Icon(Icons.edit), title: Text('重命名')),
              ),
              const PopupMenuItem(
                value: 'delete',
                child: ListTile(leading: Icon(Icons.delete), title: Text('删除')),
              ),
            ],
          ),
        ],
      ),
      body: _buildPreview(context),
    );
  }

  Widget _buildPreview(BuildContext context) {
    switch (_currentFile.type) {
      case FileType.IMAGE:
        return _buildImagePreview(context);
      case FileType.VIDEO:
        return _buildVideoPreview(context);
      case FileType.AUDIO:
        return _buildAudioPreview(context);
      case FileType.DOCUMENT:
        return _buildDocumentPreview(context);
      default:
        return _buildUnsupportedPreview(context);
    }
  }

  Widget _buildImagePreview(BuildContext context) {
    final headers = _getAuthHeaders();

    // 单图或无图片列表时，直接显示
    if (!_hasMultipleImages) {
      return _buildSingleImageView(context, _currentFile, headers);
    }

    // 多图模式：PageView 左右滑动
    return PageView.builder(
      controller: _pageController,
      itemCount: _images!.length,
      onPageChanged: (index) {
        setState(() {
          _currentPage = index;
          _currentFile = _images![index];
        });
      },
      itemBuilder: (context, index) {
        return _buildSingleImageView(context, _images![index], headers);
      },
    );
  }

  Widget _buildSingleImageView(BuildContext context, File file, Map<String, String> headers) {
    final previewUrl = _buildPreviewUrl(file.id);
    return Center(
      child: InteractiveViewer(
        minScale: 0.5,
        maxScale: 4.0,
        child: Image.network(
          previewUrl,
          headers: headers,
          fit: BoxFit.contain,
          loadingBuilder: (context, child, loadingProgress) {
            if (loadingProgress == null) return child;
            return Center(
              child: CircularProgressIndicator(
                value: loadingProgress.expectedTotalBytes != null
                    ? loadingProgress.cumulativeBytesLoaded /
                        loadingProgress.expectedTotalBytes!
                    : null,
              ),
            );
          },
          errorBuilder: (context, error, stackTrace) {
            return _buildErrorWidget(context, '图片加载失败', error);
          },
        ),
      ),
    );
  }

  Widget _buildVideoPreview(BuildContext context) {
    final previewUrl = _buildPreviewUrl(_currentFile.id);
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Container(
            width: 300,
            height: 200,
            decoration: BoxDecoration(
              color: Theme.of(context).colorScheme.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(12),
            ),
            child: const Icon(Icons.play_circle_outline, size: 100, color: Colors.red),
          ),
          const SizedBox(height: 24),
          Text('视频预览', style: Theme.of(context).textTheme.titleLarge),
          const SizedBox(height: 8),
          Text(
            _currentFile.name,
            style: Theme.of(context).textTheme.bodyMedium?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 16),
          FilledButton.icon(
            onPressed: () => _openInExternalPlayer(context),
            icon: const Icon(Icons.open_in_new),
            label: const Text('使用外部播放器打开'),
          ),
          const SizedBox(height: 8),
          Text(
            '视频播放功能开发中，暂可使用外部播放器打开',
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 16),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 32),
            child: SelectableText(
              previewUrl,
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                color: Theme.of(context).colorScheme.onSurfaceVariant,
                fontFamily: 'monospace',
              ),
              textAlign: TextAlign.center,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildAudioPreview(BuildContext context) {
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Container(
            width: 200,
            height: 200,
            decoration: BoxDecoration(
              color: Theme.of(context).colorScheme.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(100),
            ),
            child: const Icon(Icons.audiotrack, size: 100, color: Colors.purple),
          ),
          const SizedBox(height: 24),
          Text('音频播放', style: Theme.of(context).textTheme.titleLarge),
          const SizedBox(height: 8),
          Text(
            _currentFile.name,
            style: Theme.of(context).textTheme.bodyMedium?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: 16),
          FilledButton.icon(
            onPressed: () => _openInExternalPlayer(context),
            icon: const Icon(Icons.open_in_new),
            label: const Text('使用外部播放器打开'),
          ),
          const SizedBox(height: 8),
          Text(
            '音频播放功能开发中，暂可使用外部播放器打开',
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildDocumentPreview(BuildContext context) {
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Container(
            width: 200,
            height: 250,
            decoration: BoxDecoration(
              color: Theme.of(context).colorScheme.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(12),
            ),
            child: const Icon(Icons.description, size: 100, color: Colors.blue),
          ),
          const SizedBox(height: 24),
          Text('文档预览', style: Theme.of(context).textTheme.titleLarge),
          const SizedBox(height: 8),
          Text(
            _currentFile.name,
            style: Theme.of(context).textTheme.bodyMedium?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 16),
          FilledButton.icon(
            onPressed: () => _downloadFile(context),
            icon: const Icon(Icons.download),
            label: const Text('下载文件'),
          ),
          const SizedBox(height: 8),
          Text(
            '文档预览功能开发中',
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildUnsupportedPreview(BuildContext context) {
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(Icons.error_outline, size: 64, color: Theme.of(context).colorScheme.error),
          const SizedBox(height: 16),
          Text('暂不支持此文件类型预览', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 16),
          FilledButton.icon(
            onPressed: () => _downloadFile(context),
            icon: const Icon(Icons.download),
            label: const Text('下载文件'),
          ),
        ],
      ),
    );
  }

  Widget _buildErrorWidget(BuildContext context, String message, dynamic error) {
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(Icons.error_outline, size: 64, color: Theme.of(context).colorScheme.error),
          const SizedBox(height: 16),
          Text(message, style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 8),
          Text(
            error.toString(),
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: 16),
          FilledButton(
            onPressed: () => _downloadFile(context),
            child: const Text('下载文件'),
          ),
        ],
      ),
    );
  }

  void _downloadFile(BuildContext context) {
    final downloadUrl = _buildDownloadUrl();
    // TODO: 实现下载逻辑
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text('下载功能开发中: $downloadUrl')),
    );
  }

  void _openInExternalPlayer(BuildContext context) {
    final previewUrl = _buildPreviewUrl(_currentFile.id);
    // TODO: 使用 url_launcher 打开外部播放器
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text('外部播放器功能开发中: $previewUrl')),
    );
  }

  void _handleMenuAction(BuildContext context, String action) {
    switch (action) {
      case 'share':
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('分享功能开发中')),
        );
      case 'rename':
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('重命名功能开发中')),
        );
      case 'delete':
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('删除功能开发中')),
        );
    }
  }
}
