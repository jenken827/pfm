import 'dart:async';

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
import 'package:pfm/router/files_route.dart';
import 'package:pfm/services/services.dart';
import 'package:pfm/blocs/transfer_bloc/transfer_bloc.dart';
import 'package:pfm/api/fm_repository.dart';
import 'package:pfm/utils/download_dir.dart';
import 'package:pfm/utils/file_utils.dart';
import 'package:pfm/widgets/zoomable_image_view.dart';
import 'package:url_launcher/url_launcher.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/services/video_transcode.dart';

/// 预取状态快照（百分比 / 聚合速率 / 是否已下完）
class _PrefetchStats {
  const _PrefetchStats({
    required this.progress,
    required this.bytesPerSecond,
    required this.complete,
  });

  final double progress;
  final double bytesPerSecond;
  final bool complete;
}

/// 把字节速率格式化成人类可读字符串
String _formatRate(double bytesPerSecond) {
  if (bytesPerSecond <= 0) return '-';
  final mb = bytesPerSecond / (1024 * 1024);
  if (mb >= 1) return '${mb.toStringAsFixed(1)}MB/s';
  return '${(bytesPerSecond / 1024).toStringAsFixed(0)}KB/s';
}

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
  bool _showOriginal = false; // 是否显示原图

  /// 当前图片是否处于放大状态；放大时禁用 PageView 翻页，
  /// 否则单指拖动会先被 PageView 抢走，变成翻页而不是移动图片
  bool _isZoomed = false;

  // 视频播放器
  Player? _videoPlayer;
  VideoController? _videoController;
  bool _isVideoInitialized = false;
  double _playbackRate = 1.0; // 播放速率

  // 视频「流畅播放」：服务端按需转 720p 低码率副本，弱网下改播它
  final FmRepository _repository = FmRepository();
  bool _smoothMode = false; // 当前播的是低码率版本
  bool _smoothBusy = false; // 正在探测 / 等转码
  int _smoothProgress = 0; // 转码进度 0-100

  // 并行预取 + 本地播放：把「网络」与「播放」解耦，卡顿不再取决于链路抖动
  MediaPrefetcher? _prefetcher;
  LocalMediaServer? _localServer;
  Timer? _prefetchTicker;
  final ValueNotifier<_PrefetchStats?> _prefetchStats = ValueNotifier(null);

  // 播放诊断：卡顿次数 / 已缓冲时长（排查"网络供不上"还是"解不动"）
  StreamSubscription<bool>? _bufferingSub;
  StreamSubscription<Duration>? _bufferSub;
  final ValueNotifier<(int, int)> _playbackStats = ValueNotifier((0, 0));
  int _stallCount = 0;
  int _bufferAheadSeconds = 0;
  bool _stallOpen = false;

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
    _bufferingSub?.cancel();
    _bufferSub?.cancel();
    _playbackStats.dispose();
    _prefetchStats.dispose();
    unawaited(_stopPrefetch());
    // VideoController 不需要显式释放，只需释放 Player
    _videoPlayer?.dispose();
    super.dispose();
  }

  /// 初始化视频播放器：优先「并行预取 + 本地播放」，预取不可用时回退直连
  Future<void> _initializeVideoPlayer() async {
    _videoPlayer = Player();
    _videoController = VideoController(_videoPlayer!);
    if (mounted) {
      setState(() => _isVideoInitialized = true);
      _watchPlaybackHealth(_videoPlayer!);
    }
    await _openVideo();
  }

  /// 打开当前视频（原文件 / 低码率版本），保留播放进度
  ///
  /// 数据面：URL 先用 [MediaPrefetcher] 多连接并行拉到本地缓存，播放器只读
  /// `127.0.0.1` 上的 [LocalMediaServer]；预取失败（服务器不支持 Range 等）时
  /// 自动回退成直连，行为与改造前一致。
  Future<void> _openVideo({Duration? position, bool? wasPlaying}) async {
    final player = _videoPlayer;
    if (player == null) return;
    final at = position ?? player.state.position;
    final playing = wasPlaying ?? player.state.playing;

    String url;
    if (_smoothMode) {
      final smoothUrl = await _buildSmoothUrl(_currentFile.id);
      if (!mounted) return;
      if (smoothUrl == null) {
        _showSnack(AppLocalizations.of(context).preview_smoothFailed);
        return;
      }
      url = smoothUrl;
    } else {
      url = _buildPreviewUrl(_currentFile.id);
    }

    final localUrl = await _startPrefetch(url: url);
    if (!mounted) return;
    final media = localUrl == null
        ? Media(url, httpHeaders: _getAuthHeaders())
        : Media(localUrl);
    await player.open(media);
    if (at > Duration.zero) {
      await player.seek(at);
    }
    if (!playing) {
      await player.pause();
    }
  }

  /// 启动预取与本地服务，返回播放器可用的本地 URL；失败返回 null（调用方回退直连）
  Future<String?> _startPrefetch({required String url}) async {
    await _stopPrefetch();
    final cacheKey = '${_currentFile.id.replaceAll(':', '_')}'
        '${_smoothMode ? '_smooth' : '_orig'}';
    try {
      final dir = await getTemporaryDirectory();
      final prefetcher = await MediaPrefetcher.open(
        url: url,
        cachePath: '${dir.path}/video_cache/$cacheKey.mp4',
      );
      final server = await LocalMediaServer.start(prefetcher);
      if (!mounted) {
        await server.close();
        await prefetcher.dispose();
        return null;
      }
      _prefetcher = prefetcher;
      _localServer = server;
      _prefetchStats.value = _PrefetchStats(
        progress: prefetcher.progress,
        bytesPerSecond: prefetcher.bytesPerSecond,
        complete: prefetcher.isComplete,
      );
      _prefetchTicker?.cancel();
      _prefetchTicker = Timer.periodic(
        const Duration(milliseconds: 700),
        (_) => _refreshPrefetch(),
      );
      return server.url;
    } catch (e) {
      getLogger().warning('视频预取不可用，回退直连: $e');
      return null;
    }
  }

  void _refreshPrefetch() {
    final prefetcher = _prefetcher;
    if (!mounted || prefetcher == null) return;
    if (prefetcher.isComplete) {
      _prefetchTicker?.cancel();
      _prefetchTicker = null;
    }
    _prefetchStats.value = _PrefetchStats(
      progress: prefetcher.progress,
      bytesPerSecond: prefetcher.bytesPerSecond,
      complete: prefetcher.isComplete,
    );
  }

  /// 停掉预取与本地服务（缓存文件保留，下次命中）
  Future<void> _stopPrefetch() async {
    _prefetchTicker?.cancel();
    _prefetchTicker = null;
    final server = _localServer;
    final prefetcher = _prefetcher;
    _localServer = null;
    _prefetcher = null;
    if (mounted) {
      _prefetchStats.value = null;
    }
    if (server != null) await server.close();
    if (prefetcher != null) await prefetcher.dispose();
  }

  void _showSnack(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(message)));
  }

  /// 订阅缓冲状态：统计卡顿次数与已缓冲时长，并写进日志。
  ///
  /// 排查卡顿时先看这两个数：缓冲长期接近 0 且 `卡顿` 不断增加 ⇒ 带宽 / 链路；
  /// 已缓冲十几秒还卡 ⇒ 客户端解码或渲染跟不上。数字同时显示在播放器下面。
  void _watchPlaybackHealth(Player player) {
    _bufferingSub = player.stream.buffering.listen((buffering) {
      if (!mounted) return;
      getLogger().debug(
        '视频缓冲状态: buffering=$buffering, 缓冲=${player.state.buffer.inSeconds}s, '
        '位置=${player.state.position.inSeconds}s, 流畅模式=$_smoothMode',
      );
      if (buffering && !_stallOpen) {
        _stallOpen = true;
      } else if (!buffering && _stallOpen) {
        _stallOpen = false;
        _stallCount += 1;
        _playbackStats.value = (_stallCount, _bufferAheadSeconds);
      }
    });
    _bufferSub = player.stream.buffer.listen((buffer) {
      if (!mounted) return;
      final seconds = buffer.inSeconds;
      if (seconds == _bufferAheadSeconds) return;
      _bufferAheadSeconds = seconds;
      _playbackStats.value = (_stallCount, seconds);
    });
  }

  /// 切换「流畅播放」/ 原文件
  Future<void> _toggleSmoothMode() async {
    if (_smoothBusy) return;
    if (_smoothMode) {
      setState(() => _smoothMode = false);
      await _openVideo();
      return;
    }

    setState(() {
      _smoothBusy = true;
      _smoothProgress = 0;
    });
    final url = await _buildSmoothUrl(_currentFile.id);
    if (!mounted) return;
    if (url == null) {
      setState(() => _smoothBusy = false);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(AppLocalizations.of(context).preview_smoothFailed)),
      );
      return;
    }

    // 第一次点会触发服务端转码（202 + 进度），转好（200/206）再换源
    final result = await pollTranscode(
      probe: () => _repository.probeTranscode(url),
      isCancelled: () => !mounted,
      onUpdate: (update) {
        if (mounted && update.state != TranscodeState.ready) {
          setState(() => _smoothProgress = update.progress);
        }
      },
    );
    if (!mounted) return;
    setState(() => _smoothBusy = false);
    if (result.state == TranscodeState.ready) {
      setState(() => _smoothMode = true);
      await _openVideo();
      return;
    }
    _showSmoothError(result);
  }

  /// 转码接口地址（带 12 小时下载票，票据不进日志）
  Future<String?> _buildSmoothUrl(String fileId) async {
    final result = await _repository.createPreviewTicket(
      fileId: fileId,
      purpose: TicketPurpose.DOWNLOAD,
    );
    final ticket = result.data?.ticket;
    if (!result.success || ticket == null || ticket.isEmpty) {
      return null;
    }
    final baseUrl = GetIt.I.get<LocalStorage>().downloadBaseUrl;
    return '$baseUrl/api/file/stream/transcode'
        '?file_id=$fileId&ticket=${Uri.encodeQueryComponent(ticket)}';
  }

  /// 切换失败时把原因告诉用户
  void _showSmoothError(TranscodeProbeResult result) {
    if (!mounted) return;
    final al = AppLocalizations.of(context);
    final message = switch (result.state) {
      TranscodeState.disabled => al.preview_smoothUnavailable,
      TranscodeState.networkError => al.preview_smoothNetwork,
      TranscodeState.timeout => al.preview_smoothTimeout,
      TranscodeState.cancelled => null,
      _ => al.preview_smoothFailed,
    };
    if (message == null) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(message)));
  }

  bool get _hasMultipleImages => _images != null && _images!.length > 1;

  /// 构建预览流URL
  String _buildPreviewUrl(String fileId, {bool original = false}) {
    final ls = GetIt.I.get<LocalStorage>();
    final baseUrl = ls.downloadBaseUrl;

    // 检查是否是历史版本（格式：history:{id}）
    if (fileId.startsWith('history:')) {
      final historyId = fileId.substring(8); // 移除 'history:' 前缀
      final url = '$baseUrl/api/file/history/preview?id=$historyId';

      return url;
    }

    if (original) {
      // 请求原图
      return '$baseUrl/api/file/stream/preview?file_id=$fileId&original=true';
    } else {
      // 预览时使用大尺寸缩略图 (400)
      return '$baseUrl/api/file/stream/preview?file_id=$fileId&size=large';
    }
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
    final al = AppLocalizations.of(context);
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
          // 图片类型显示原图/缩略图切换按钮
          if (_currentFile.type == FileType.IMAGE)
            IconButton(
              icon: Icon(_showOriginal ? Icons.image : Icons.hd),
              tooltip: _showOriginal
                  ? al.preview_viewThumbnail
                  : al.preview_viewOriginal,
              onPressed: () {
                setState(() {
                  _showOriginal = !_showOriginal;
                  _isZoomed = false; // 换图后缩放归位，翻页立刻可用
                });
              },
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
              PopupMenuItem(
                value: 'share',
                child: ListTile(
                  leading: const Icon(Icons.share),
                  title: Text(al.preview_share),
                ),
              ),
              PopupMenuItem(
                value: 'rename',
                child: ListTile(
                  leading: const Icon(Icons.edit),
                  title: Text(al.preview_rename),
                ),
              ),
              PopupMenuItem(
                value: 'delete',
                child: ListTile(
                  leading: const Icon(Icons.delete),
                  title: Text(al.preview_delete),
                ),
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
          // 放大时禁掉翻页：平移手势必须完整留给图片
          physics: _isZoomed
              ? const NeverScrollableScrollPhysics()
              : const PageScrollPhysics(),
          onPageChanged: (index) {
            setState(() {
              _currentPage = index;
              _currentFile = _images![index];
              _isZoomed = false;
            });
          },
          itemBuilder: (context, index) {
            return _buildSingleImageView(
              context,
              _images![index],
              headers,
              active: index == _currentPage,
            );
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
    Map<String, String> headers, {
    bool active = true,
  }) {
    final al = AppLocalizations.of(context);
    final previewUrl = _buildPreviewUrl(file.id, original: _showOriginal);
    return Stack(
      children: [
        SizedBox.expand(
          child: ZoomableImageView(
            // 换图后重建，缩放状态天然归位
            key: ValueKey('${file.id}_$_showOriginal'),
            isActive: active,
            resetTooltip: al.preview_resetZoom,
            onZoomChanged: (zoomed) {
              if (!mounted || zoomed == _isZoomed) return;
              setState(() => _isZoomed = zoomed);
            },
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
                return _buildErrorWidget(
                  context,
                  al.preview_imageLoadFailed,
                  error,
                );
              },
            ),
          ),
        ),
        // 左上角显示当前模式标签
        Positioned(
          top: 16,
          left: 16,
          child: Container(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
            decoration: BoxDecoration(
              color: Colors.black54,
              borderRadius: BorderRadius.circular(16),
            ),
            child: Text(
              _showOriginal ? al.preview_original : al.preview_thumbnail,
              style: const TextStyle(color: Colors.white, fontSize: 12),
            ),
          ),
        ),
      ],
    );
  }

  Widget _buildVideoPreview(BuildContext context) {
    final al = AppLocalizations.of(context);
    if (!_isVideoInitialized || _videoController == null) {
      return const Center(child: CircularProgressIndicator());
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
        // 视频控制栏（用 Wrap：窄屏放不下时换行，别把「流畅播放」挤到屏幕外）
        Padding(
          padding: const EdgeInsets.all(16),
          child: Wrap(
            alignment: WrapAlignment.center,
            crossAxisAlignment: WrapCrossAlignment.center,
            spacing: 4,
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
                  padding: const EdgeInsets.symmetric(
                    horizontal: 8,
                    vertical: 4,
                  ),
                  decoration: BoxDecoration(
                    border: Border.all(
                      color: Theme.of(context).colorScheme.outline,
                    ),
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
                  PopupMenuItem(
                    value: 1.0,
                    child: Text(al.preview_normalSpeed),
                  ),
                  const PopupMenuItem(value: 1.25, child: Text('1.25x')),
                  const PopupMenuItem(value: 1.5, child: Text('1.5x')),
                  const PopupMenuItem(value: 2.0, child: Text('2.0x')),
                ],
              ),
              const SizedBox(width: 8),
              // 流畅播放：弱网下切到服务端转好的低码率副本
              IconButton(
                tooltip: _smoothMode
                    ? al.preview_smoothPlaying
                    : al.preview_smoothPlay,
                icon: Icon(_smoothMode ? Icons.speed : Icons.speed_outlined),
                onPressed: _smoothBusy ? null : _toggleSmoothMode,
              ),
            ],
          ),
        ),
        if (_smoothBusy)
          Padding(
            padding: const EdgeInsets.only(bottom: 12),
            child: Text(
              al.preview_smoothTranscoding(_smoothProgress),
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ),
        // 预取进度：本地缓存还在拉时显示百分比与实测聚合速率
        ValueListenableBuilder<_PrefetchStats?>(
          valueListenable: _prefetchStats,
          builder: (context, stats, _) {
            if (stats == null || stats.complete) return const SizedBox.shrink();
            final percent = (stats.progress * 100).clamp(0, 100).round();
            return Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: Text(
                al.preview_buffering(percent, _formatRate(stats.bytesPerSecond)),
                textAlign: TextAlign.center,
                style: Theme.of(context).textTheme.bodySmall,
              ),
            );
          },
        ),
        // 卡顿诊断：次数 + 已缓冲秒数（用来区分"网络供不上"和"解不动"）
        ValueListenableBuilder<(int, int)>(
          valueListenable: _playbackStats,
          builder: (context, stats, _) {
            if (stats.$1 <= 0) return const SizedBox.shrink();
            return Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: Text(
                al.preview_playbackStalls(stats.$1, stats.$2),
                textAlign: TextAlign.center,
                style: Theme.of(context).textTheme.bodySmall,
              ),
            );
          },
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
    final al = AppLocalizations.of(context);
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
          Text(
            al.preview_audioPlay,
            style: Theme.of(context).textTheme.titleLarge,
          ),
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
            label: Text(al.preview_openExternalPlayer),
          ),
          const SizedBox(height: 8),
          Text(
            al.preview_audioPlayHint,
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildUnsupportedPreview(BuildContext context) {
    final al = AppLocalizations.of(context);
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
          Text(
            al.preview_unsupportedType,
            style: Theme.of(context).textTheme.titleMedium,
          ),
          const SizedBox(height: 16),
          FilledButton.icon(
            onPressed: () => _downloadFile(context),
            icon: const Icon(Icons.download),
            label: Text(al.preview_downloadFile),
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
    final al = AppLocalizations.of(context);
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
            child: Text(al.preview_downloadFile),
          ),
        ],
      ),
    );
  }

  void _downloadFile(BuildContext context) async {
    final al = AppLocalizations.of(context);
    try {
      // 下载到应用私有目录，完成后再发布到公共「下载」（分区存储限制）
      final downloadDir = await DownloadDir.resolveWriteDir();
      if (!context.mounted) return;

      final fileName = _currentFile.name.isNotEmpty
          ? _currentFile.name
          : 'unnamed';
      final initialPath = '$downloadDir/$fileName';
      // 生成唯一文件路径，避免覆盖
      final savePath = await FileUtils.getUniqueFilePath(initialPath);
      if (!context.mounted) return;
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
          SnackBar(content: Text(al.preview_downloadQueued(fileName))),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('${al.preview_downloadFailed}: $e')),
        );
      }
    }
  }

  Future<void> _openInExternalPlayer(BuildContext context) async {
    final al = AppLocalizations.of(context);
    final previewUrl = _buildPreviewUrl(_currentFile.id);
    final uri = Uri.parse(previewUrl);

    try {
      if (await canLaunchUrl(uri)) {
        await launchUrl(uri, mode: LaunchMode.externalApplication);
      } else {
        if (context.mounted) {
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(SnackBar(content: Text(al.preview_cannotOpenPlayer)));
        }
      }
    } catch (e) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('${al.preview_openPlayerFailed}: $e')),
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
    final al = AppLocalizations.of(context);
    try {
      final repo = FmRepository();
      final result = await repo.createShareLink(
        fileId: _currentFile.id,
        expireSeconds: 604800, // 7天
      );

      if (result.success && result.data != null) {
        final shareUrl = result.data!.shareUrl;

        if (context.mounted) {
          _showShareSheet(context, shareUrl, al);
        }
      } else if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(result.message ?? al.preview_createShareLinkFailed),
          ),
        );
      }
    } catch (e) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('${al.preview_shareFailed}: $e')),
        );
      }
    }
  }

  /// 显示分享底部弹窗：分享链接 + 保存到我的网盘
  void _showShareSheet(
    BuildContext context,
    String shareUrl,
    AppLocalizations al,
  ) {
    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      builder: (sheetContext) {
        return _ShareBottomSheet(shareUrl: shareUrl, al: al);
      },
    );
  }

  Future<void> _renameFile(BuildContext context) async {
    final al = AppLocalizations.of(context);
    final controller = TextEditingController(text: _currentFile.name);

    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(al.preview_rename),
        content: TextField(
          controller: controller,
          autofocus: true,
          decoration: InputDecoration(
            labelText: al.preview_fileName,
            hintText: al.preview_enterNewFilename,
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: Text(al.common_cancel),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: Text(al.common_confirm),
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

        if (result.success && context.mounted) {
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(SnackBar(content: Text(al.preview_renameSuccess)));
          // 返回上一页刷新列表
          context.pop();
        } else if (context.mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text(result.message ?? al.preview_renameFailed)),
          );
        }
      } catch (e) {
        if (context.mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('${al.preview_renameFailed}: $e')),
          );
        }
      }
    }
    controller.dispose();
  }

  Future<void> _deleteFile(BuildContext context) async {
    final al = AppLocalizations.of(context);
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(al.preview_confirmDeleteTitle),
        content: Text(al.preview_confirmDeleteMsg(_currentFile.name)),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: Text(al.common_cancel),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            style: FilledButton.styleFrom(
              backgroundColor: Theme.of(context).colorScheme.error,
            ),
            child: Text(al.preview_delete),
          ),
        ],
      ),
    );

    if (confirmed == true && context.mounted) {
      try {
        final repo = FmRepository();
        final result = await repo.deleteFile([_currentFile.id]);

        if (result.success && context.mounted) {
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(SnackBar(content: Text(al.preview_deleteSuccess)));
          // 返回上一页刷新列表
          context.pop();
        } else if (context.mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text(result.message ?? al.preview_deleteFailed)),
          );
        }
      } catch (e) {
        if (context.mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('${al.preview_deleteFailed}: $e')),
          );
        }
      }
    }
  }
}

/// 分享底部弹窗：显示分享链接 + 保存到我的网盘按钮
class _ShareBottomSheet extends StatefulWidget {
  const _ShareBottomSheet({required this.shareUrl, required this.al});

  final String shareUrl;
  final AppLocalizations al;

  @override
  State<_ShareBottomSheet> createState() => _ShareBottomSheetState();
}

class _ShareBottomSheetState extends State<_ShareBottomSheet> {
  bool _isSaving = false;
  String _statusText = '';
  int _completed = 0;
  int _total = 0;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(24, 20, 24, 32),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          // 标题
          Center(
            child: Container(
              width: 40,
              height: 4,
              decoration: BoxDecoration(
                color: Theme.of(context).colorScheme.outlineVariant,
                borderRadius: BorderRadius.circular(2),
              ),
            ),
          ),
          const SizedBox(height: 16),
          Text(
            widget.al.preview_shareTitle,
            style: Theme.of(context).textTheme.titleLarge,
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: 20),

          // 分享链接
          Text(
            widget.al.preview_shareLinkLabel,
            style: Theme.of(context).textTheme.labelMedium,
          ),
          const SizedBox(height: 8),
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
            decoration: BoxDecoration(
              color: Theme.of(context).colorScheme.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(8),
            ),
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    widget.shareUrl,
                    style: Theme.of(context).textTheme.bodySmall,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                const SizedBox(width: 8),
                IconButton(
                  icon: const Icon(Icons.copy, size: 20),
                  tooltip: widget.al.preview_shareLinkCopiedToClipboard,
                  onPressed: () async {
                    await Clipboard.setData(
                      ClipboardData(text: widget.shareUrl),
                    );
                    if (context.mounted) {
                      ScaffoldMessenger.of(context).showSnackBar(
                        SnackBar(
                          content: Text(
                            widget.al.preview_shareLinkCopiedToClipboard,
                          ),
                        ),
                      );
                    }
                  },
                ),
              ],
            ),
          ),
          const SizedBox(height: 20),

          // 保存到我的网盘按钮
          if (_isSaving) ...[
            // 进度显示
            if (_total > 0) ...[
              LinearProgressIndicator(
                value: _total > 0 ? _completed / _total : null,
              ),
              const SizedBox(height: 8),
              Text(
                widget.al.preview_saveToDiskProgress(_completed, _total),
                style: Theme.of(context).textTheme.bodySmall,
                textAlign: TextAlign.center,
              ),
            ] else ...[
              const LinearProgressIndicator(),
              const SizedBox(height: 8),
              Text(
                _statusText.isNotEmpty
                    ? _statusText
                    : widget.al.preview_savingToDisk,
                style: Theme.of(context).textTheme.bodySmall,
                textAlign: TextAlign.center,
              ),
            ],
          ] else ...[
            FilledButton.icon(
              onPressed: () => _startSave(context),
              icon: const Icon(Icons.save_alt),
              label: Text(widget.al.preview_saveToMyDisk),
            ),
          ],
        ],
      ),
    );
  }

  Future<void> _startSave(BuildContext context) async {
    setState(() {
      _isSaving = true;
      _statusText = widget.al.preview_savingToDisk;
    });

    try {
      final repo = FmRepository();

      // 从 shareUrl 中提取 shareCode（URL 最后一段路径）
      final uri = Uri.parse(widget.shareUrl);
      final shareCode = uri.pathSegments.isNotEmpty
          ? uri.pathSegments.last
          : widget.shareUrl;

      // 调用保存接口
      final saveResult = await repo.saveShareToMySpace(
        shareCode: shareCode,
        targetParentId: '', // 空字符串表示保存到根目录
      );

      if (!saveResult.success || saveResult.data == null) {
        if (context.mounted) {
          Navigator.pop(context);
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text(
                saveResult.message ?? widget.al.preview_saveToDiskFailed,
              ),
            ),
          );
        }
        return;
      }

      final taskId = saveResult.data!.taskId;
      if (taskId.isEmpty) {
        if (context.mounted) {
          Navigator.pop(context);
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text(widget.al.preview_saveToDiskSuccess)),
          );
        }
        return;
      }

      // 轮询异步任务进度
      if (!context.mounted) return;
      await _pollAsyncTask(context, taskId, repo);
    } catch (e) {
      if (context.mounted) {
        Navigator.pop(context);
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('${widget.al.preview_saveToDiskFailed}: $e')),
        );
      }
    }
  }

  Future<void> _pollAsyncTask(
    BuildContext context,
    String taskId,
    FmRepository repo,
  ) async {
    const maxAttempts = 150; // 最多轮询 5 分钟
    for (var attempt = 0; attempt < maxAttempts; attempt++) {
      await Future.delayed(const Duration(seconds: 2));

      if (!mounted) return;

      try {
        final progressResult = await repo.getAsyncTaskProgress(taskId);

        if (!progressResult.success || progressResult.data == null) {
          continue; // 网络异常时重试
        }

        final status = progressResult.data!.status;
        final total = progressResult.data!.total;
        final completed = progressResult.data!.completed;

        // 更新进度
        if (mounted) {
          setState(() {
            _total = total;
            _completed = completed;
          });
        }

        if (status == 'SUCCESS') {
          if (context.mounted) {
            Navigator.pop(context);
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(content: Text(widget.al.preview_saveToDiskSuccess)),
            );
          }
          return;
        } else if (status == 'FAILED') {
          final errorMsg = progressResult.data!.error;
          if (context.mounted) {
            Navigator.pop(context);
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(
                content: Text(
                  errorMsg.isNotEmpty
                      ? '${widget.al.preview_saveToDiskFailed}: $errorMsg'
                      : widget.al.preview_saveToDiskFailed,
                ),
              ),
            );
          }
          return;
        }
        // PENDING / RUNNING：继续轮询
      } catch (e) {
        // 网络异常时继续重试
      }
    }

    // 超时
    if (context.mounted) {
      Navigator.pop(context);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(widget.al.preview_saveToDiskFailed)),
      );
    }
  }
}
