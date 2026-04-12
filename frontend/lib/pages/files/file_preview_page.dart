import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:get_it/get_it.dart';
import 'package:go_router/go_router.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';
import 'package:path_provider/path_provider.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/services/local_storage.dart';
import 'package:pfm/router/files_route.dart';
import 'package:pfm/widgets/document_preview_widget.dart';
import 'package:pfm/blocs/transfer_bloc/transfer_bloc.dart';
import 'package:pfm/api/fm_repository.dart';
import 'package:url_launcher/url_launcher.dart';

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
  const FilePreviewPage({
    super.key,
    required this.file,
    this.images,
    this.initialIndex = 0,
  });

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

  // 视频播放器
  Player? _videoPlayer;
  VideoController? _videoController;
  bool _isVideoInitialized = false;
  double _playbackRate = 1.0; // 播放速率

  @override
  void initState() {
    super.initState();
    _currentFile = widget.file;
    _images = widget.images;
    _currentPage = widget.initialIndex;
    _pageController = PageController(initialPage: _currentPage);

    // 如果是视频，初始化播放器
    if (_currentFile.type == FileType.VIDEO) {
      _initializeVideoPlayer();
    }
  }

  @override
  void dispose() {
    _pageController.dispose();
    // VideoController 不需要显式释放，只需释放 Player
    _videoPlayer?.dispose();
    super.dispose();
  }

  /// 初始化视频播放器
  Future<void> _initializeVideoPlayer() async {
    final previewUrl = _buildPreviewUrl(_currentFile.id);
    final headers = _getAuthHeaders();

    // 创建播放器
    _videoPlayer = Player();
    _videoController = VideoController(_videoPlayer!);

    // 设置播放器
    await _videoPlayer!.open(
      Media(
        previewUrl,
        httpHeaders: headers,
      ),
    );

    if (mounted) {
      setState(() {
        _isVideoInitialized = true;
      });
    }
  }

  bool get _hasMultipleImages => _images != null && _images!.length > 1;

  /// 构建预览流URL
  String _buildPreviewUrl(String fileId) {
    final ls = GetIt.I.get<LocalStorage>();
    final baseUrl = ls.fileServer ?? '';
    return '$baseUrl/api/file/stream/preview?file_id=$fileId';
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
              context.push(FileDetailRoute().location, extra: _currentFile);
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

    // 多图模式：PageView 左右滑动 + 箭头导航
    return Stack(
      children: [
        PageView.builder(
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
        ),
        // 左箭头（非第一张时显示）
        if (_currentPage > 0)
          Positioned(
            left: 16,
            top: 0,
            bottom: 0,
            child: Center(
              child: _buildNavigationArrow(
                icon: Icons.chevron_left,
                onPressed: () => _navigateToPage(_currentPage - 1),
              ),
            ),
          ),
        // 右箭头（非最后一张时显示）
        if (_currentPage < _images!.length - 1)
          Positioned(
            right: 16,
            top: 0,
            bottom: 0,
            child: Center(
              child: _buildNavigationArrow(
                icon: Icons.chevron_right,
                onPressed: () => _navigateToPage(_currentPage + 1),
              ),
            ),
          ),
      ],
    );
  }

  /// 构建导航箭头按钮
  Widget _buildNavigationArrow({
    required IconData icon,
    required VoidCallback onPressed,
  }) {
    return Container(
      decoration: BoxDecoration(
        color: Colors.black54,
        borderRadius: BorderRadius.circular(24),
      ),
      child: IconButton(
        icon: Icon(icon, color: Colors.white, size: 32),
        onPressed: onPressed,
      ),
    );
  }

  /// 导航到指定页面
  void _navigateToPage(int pageIndex) {
    _pageController.animateToPage(
      pageIndex,
      duration: const Duration(milliseconds: 300),
      curve: Curves.easeInOut,
    );
  }

  Widget _buildSingleImageView(
    BuildContext context,
    File file,
    Map<String, String> headers,
  ) {
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
    if (!_isVideoInitialized || _videoController == null) {
      return const Center(
        child: CircularProgressIndicator(),
      );
    }

    return Column(
      children: [
        // 视频播放器
        Expanded(
          child: Center(
            child: AspectRatio(
              aspectRatio: 16 / 9,
              child: Video(
                controller: _videoController!,
                controls: MaterialVideoControls,
              ),
            ),
          ),
        ),
        // 视频控制栏
        Padding(
          padding: const EdgeInsets.all(16),
          child: Row(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              IconButton(
                icon: const Icon(Icons.replay_10),
                onPressed: () => _seekVideo(-10),
              ),
              IconButton(
                icon: Icon(
                  _videoPlayer?.state.playing == true
                      ? Icons.pause
                      : Icons.play_arrow,
                ),
                onPressed: _togglePlayPause,
              ),
              IconButton(
                icon: const Icon(Icons.forward_10),
                onPressed: () => _seekVideo(10),
              ),
              const SizedBox(width: 16),
              // 倍速选择
              PopupMenuButton<double>(
                icon: Container(
                  padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                  decoration: BoxDecoration(
                    border: Border.all(color: Theme.of(context).colorScheme.outline),
                    borderRadius: BorderRadius.circular(4),
                  ),
                  child: Text(
                    '${_playbackRate}x',
                    style: Theme.of(context).textTheme.bodyMedium,
                  ),
                ),
                initialValue: _playbackRate,
                onSelected: (rate) {
                  _setPlaybackRate(rate);
                },
                itemBuilder: (context) => [
                  const PopupMenuItem(value: 0.5, child: Text('0.5x')),
                  const PopupMenuItem(value: 0.75, child: Text('0.75x')),
                  const PopupMenuItem(value: 1.0, child: Text('1.0x (正常)')),
                  const PopupMenuItem(value: 1.25, child: Text('1.25x')),
                  const PopupMenuItem(value: 1.5, child: Text('1.5x')),
                  const PopupMenuItem(value: 2.0, child: Text('2.0x')),
                ],
              ),
            ],
          ),
        ),
      ],
    );
  }

  /// 切换播放/暂停
  void _togglePlayPause() {
    if (_videoPlayer == null) return;

    if (_videoPlayer!.state.playing) {
      _videoPlayer!.pause();
    } else {
      _videoPlayer!.play();
    }

    setState(() {});
  }

  /// 快进/快退
  void _seekVideo(int seconds) {
    if (_videoPlayer == null) return;

    final currentPos = _videoPlayer!.state.position;
    final newPos = currentPos + Duration(seconds: seconds);
    _videoPlayer!.seek(newPos);
  }

  /// 设置播放速率
  void _setPlaybackRate(double rate) {
    if (_videoPlayer == null) return;

    _videoPlayer!.setRate(rate);
    setState(() {
      _playbackRate = rate;
    });
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
            child: const Icon(
              Icons.audiotrack,
              size: 100,
              color: Colors.purple,
            ),
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
    final ls = GetIt.I.get<LocalStorage>();
    final baseUrl = ls.fileServer ?? '';
    
    return DocumentPreviewWidget(
      file: _currentFile,
      baseUrl: baseUrl,
    );
  }

  Widget _buildUnsupportedPreview(BuildContext context) {
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(
            Icons.error_outline,
            size: 64,
            color: Theme.of(context).colorScheme.error,
          ),
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

  Widget _buildErrorWidget(
    BuildContext context,
    String message,
    dynamic error,
  ) {
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(
            Icons.error_outline,
            size: 64,
            color: Theme.of(context).colorScheme.error,
          ),
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

  void _downloadFile(BuildContext context) async {
    try {
      final downloadDir = await getDownloadsDirectory();
      if (!mounted) return;
      
      final fileName = _currentFile.name.isNotEmpty ? _currentFile.name : 'unnamed';
      final savePath = '${downloadDir!.path}/$fileName';
      
      // 添加到下载队列
      context.read<TransferBloc>().add(
        TransferAddDownloadEvent(
          fileId: _currentFile.id,
          filename: fileName,
          fileSize: _currentFile.size.toInt(),
          savePath: savePath,
          fileHash: _currentFile.hash,
        ),
      );
      
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('已添加到下载队列: $fileName')),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('下载失败: $e')),
        );
      }
    }
  }

  Future<void> _openInExternalPlayer(BuildContext context) async {
    final previewUrl = _buildPreviewUrl(_currentFile.id);
    final uri = Uri.parse(previewUrl);
    
    try {
      if (await canLaunchUrl(uri)) {
        await launchUrl(uri, mode: LaunchMode.externalApplication);
      } else {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            const SnackBar(content: Text('无法打开外部播放器')),
          );
        }
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('打开外部播放器失败: $e')),
        );
      }
    }
  }

  void _handleMenuAction(BuildContext context, String action) async {
    switch (action) {
      case 'share':
        await _shareFile(context);
        break;
      case 'rename':
        await _renameFile(context);
        break;
      case 'delete':
        await _deleteFile(context);
        break;
    }
  }
  
  Future<void> _shareFile(BuildContext context) async {
    try {
      final repo = FmRepository();
      final result = await repo.createShareLink(
        fileId: _currentFile.id,
        expireSeconds: 604800, // 7天
      );
      
      if (result.success && result.data != null) {
        final shareUrl = result.data!.shareUrl;
        if (mounted) {
          // 复制到剪贴板
          await Clipboard.setData(ClipboardData(text: shareUrl));
          ScaffoldMessenger.of(context).showSnackBar(
            const SnackBar(content: Text('分享链接已复制到剪贴板')),
          );
        }
      } else if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(result.message ?? '创建分享链接失败')),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('分享失败: $e')),
        );
      }
    }
  }
  
  Future<void> _renameFile(BuildContext context) async {
    final controller = TextEditingController(text: _currentFile.name);
    
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('重命名'),
        content: TextField(
          controller: controller,
          autofocus: true,
          decoration: const InputDecoration(
            labelText: '文件名',
            hintText: '请输入新文件名',
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('确定'),
          ),
        ],
      ),
    );
    
    if (confirmed == true && controller.text.isNotEmpty && mounted) {
      try {
        final repo = FmRepository();
        final result = await repo.renameFile([
          RenameObj(id: _currentFile.id, newName: controller.text),
        ]);
        
        if (result.success && mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            const SnackBar(content: Text('重命名成功')),
          );
          // 返回上一页刷新列表
          context.pop();
        } else if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text(result.message ?? '重命名失败')),
          );
        }
      } catch (e) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('重命名失败: $e')),
          );
        }
      }
    }
    controller.dispose();
  }
  
  Future<void> _deleteFile(BuildContext context) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('确认删除'),
        content: Text('确定要删除 "${_currentFile.name}" 吗？'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            style: FilledButton.styleFrom(
              backgroundColor: Theme.of(context).colorScheme.error,
            ),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    
    if (confirmed == true && mounted) {
      try {
        final repo = FmRepository();
        final result = await repo.deleteFile([_currentFile.id]);
        
        if (result.success && mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            const SnackBar(content: Text('删除成功')),
          );
          // 返回上一页刷新列表
          context.pop();
        } else if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text(result.message ?? '删除失败')),
          );
        }
      } catch (e) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('删除失败: $e')),
          );
        }
      }
    }
  }
}
