import 'dart:io';

import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/api/fm_repository.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/proto/native/enum.pb.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/services/local_storage.dart';
import 'package:url_launcher/url_launcher.dart';
import 'package:webview_flutter/webview_flutter.dart';

/// Flyfish Viewer 预览页面
/// 移动端：通过 WebView 加载 file-viewer（URL 模式）
/// 桌面端：在系统浏览器中打开 file-viewer
///
/// 凭证用**单文件短期票据**而不是 access token：viewer 只能把凭证写在 URL 查询串里
/// （浏览器请求带不了 Authorization 头），而 access token 是账户级、长期有效的，
/// 落进 WebView / 浏览器历史与 Referer 就等于交出全部文件的读权限。因此每次加载
/// （含刷新、每次打开浏览器）都先申请一张 10 分钟有效、只对这一文件有效的票据，
/// 见 `FmRepository.createPreviewTicket` 与后端 `internal/auth/preview_ticket.go`。
class FlyfishViewerPage extends StatefulWidget {
  final File file;

  const FlyfishViewerPage({super.key, required this.file});

  @override
  State<FlyfishViewerPage> createState() => _FlyfishViewerPageState();
}

class _FlyfishViewerPageState extends State<FlyfishViewerPage> {
  WebViewController? _controller;
  bool _isLoading = true;
  bool _preparing = false; // 正在等云端缓存就绪（Telegram 大文件 202）
  String? _error;
  late final String _baseUrl;
  late final FmRepository _repository;
  final bool _isDesktop =
      Platform.isWindows || Platform.isMacOS || Platform.isLinux;

  @override
  void initState() {
    super.initState();
    _baseUrl = GetIt.I.get<LocalStorage>().fileServer ?? '';
    _repository = FmRepository();

    if (_isDesktop) {
      _isLoading = false;
    } else {
      _loadViewer();
    }
  }

  /// 申请新票据并加载 viewer（刷新按钮也走这里，每次都换新票据）
  Future<void> _loadViewer() async {
    setState(() {
      _isLoading = true;
      _preparing = false;
      _error = null;
    });

    final viewerUrl = await _buildViewerUrl();
    if (!mounted || viewerUrl == null) return;

    _controller ??= _createController();
    await _controller!.loadRequest(Uri.parse(viewerUrl));
  }

  /// 申请票据并拼出 viewer 地址；失败时把错误写进 state 并返回 null
  Future<String?> _buildViewerUrl() async {
    final al = AppLocalizations.of(context);
    // 阅读器会边翻边发 Range 请求，10 分钟的预览票不够用，这里要下载票（12 小时）
    final result = await _repository.createPreviewTicket(
      fileId: widget.file.id,
      purpose: TicketPurpose.DOWNLOAD,
    );
    if (!mounted) return null;

    final ticket = result.data?.ticket;
    if (!result.success || ticket == null || ticket.isEmpty) {
      setState(() {
        _error = result.message ?? al.flyfish_ticketFailed;
        _isLoading = false;
      });
      return null;
    }

    final fileName = Uri.encodeComponent(widget.file.name);
    final fileUrl =
        '$_baseUrl/api/file/preview/raw/$fileName'
        '?file_id=${widget.file.id}'
        '&ticket=${Uri.encodeQueryComponent(ticket)}';

    // Telegram 大文件（> 阈值）首次访问时服务端回 202「云端正在缓存」，阅读器
    // 拿到 202 的 JSON 会当成坏文件。先探测到 200/206 再交给阅读器；
    // 预下载在服务端异步进行，30MB 量级通常几秒到几十秒。
    final ready = await _waitPreviewReady(fileUrl);
    if (!mounted) return null;
    if (!ready) {
      setState(() {
        _error = al.flyfish_prepareTimeout;
        _isLoading = false;
        _preparing = false;
      });
      return null;
    }
    setState(() => _preparing = false);

    final options = Uri.encodeComponent('{"toolbar":true}');

    return '$_baseUrl/file-viewer/index.html'
        '?url=${Uri.encodeComponent(fileUrl)}'
        '&options=$options';
  }

  /// 等文件在服务端就绪；返回 true 表示可以交给阅读器。
  ///
  /// 200 / 206 = 就绪；202 = 云端缓存中（继续等）；401/403/404 = 票据或文件有问题，
  /// 不再等待；其它（网络抖动 / 0）继续重试。最多等约 60 秒。
  Future<bool> _waitPreviewReady(String fileUrl) async {
    for (var attempt = 0; attempt < 60; attempt++) {
      if (!mounted) return false;
      final status = await _repository.probePreviewReady(fileUrl);
      if (!mounted) return false;
      if (status == 200 || status == 206) return true;
      if (status == 401 || status == 403 || status == 404) return false;
      if (!_preparing) {
        setState(() => _preparing = true);
      }
      await Future<void>.delayed(const Duration(seconds: 1));
    }
    return false;
  }

  WebViewController _createController() {
    return WebViewController()
      ..setJavaScriptMode(JavaScriptMode.unrestricted)
      ..setNavigationDelegate(
        NavigationDelegate(
          onPageFinished: (url) {
            if (mounted) setState(() => _isLoading = false);
          },
          onWebResourceError: (error) {
            if (mounted) {
              setState(() {
                _error = '加载失败: ${error.description}';
                _isLoading = false;
              });
            }
          },
        ),
      );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text(
          widget.file.name,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
        actions: [
          if (!_isDesktop)
            IconButton(
              icon: const Icon(Icons.refresh),
              tooltip: AppLocalizations.of(context).common_refresh,
              onPressed: _isLoading ? null : _loadViewer,
            ),
        ],
      ),
      body: _isDesktop ? _buildDesktopView() : _buildMobileView(),
    );
  }

  /// 桌面端：在系统浏览器中打开文件预览
  Widget _buildDesktopView() {
    final al = AppLocalizations.of(context);

    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(Icons.open_in_browser, size: 64, color: Colors.blue[400]),
            const SizedBox(height: 16),
            Text(
              al.flyfish_willOpenInBrowser,
              style: TextStyle(
                fontSize: 18,
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
              textAlign: TextAlign.center,
            ),
            if (_error != null) ...[
              const SizedBox(height: 12),
              Text(
                _error!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
                textAlign: TextAlign.center,
              ),
            ],
            const SizedBox(height: 24),
            ElevatedButton.icon(
              onPressed: _isLoading ? null : _openInBrowser,
              icon: const Icon(Icons.open_in_new),
              label: Text(al.flyfish_openPreview),
            ),
          ],
        ),
      ),
    );
  }

  /// 移动端：WebView 预览
  Widget _buildMobileView() {
    final al = AppLocalizations.of(context);

    return Stack(
      children: [
        if (_error != null)
          Center(
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                const Icon(Icons.error_outline, size: 48, color: Colors.red),
                const SizedBox(height: 16),
                Text(_error!, textAlign: TextAlign.center),
                const SizedBox(height: 16),
                ElevatedButton(
                  onPressed: _loadViewer,
                  child: Text(al.common_retry),
                ),
              ],
            ),
          )
        else if (_controller != null)
          WebViewWidget(controller: _controller!),
        if (_isLoading)
          Center(
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                const CircularProgressIndicator(),
                const SizedBox(height: 16),
                Text(
                  _preparing
                      ? al.flyfish_preparingCloud
                      : al.flyfish_loadingDocument,
                ),
              ],
            ),
          ),
      ],
    );
  }

  /// 在系统浏览器中打开文件预览（现签票据，浏览器历史里只留 10 分钟的单文件凭证）
  Future<void> _openInBrowser() async {
    setState(() {
      _isLoading = true;
      _error = null;
    });

    final viewerUrl = await _buildViewerUrl();
    if (!mounted) return;

    setState(() => _isLoading = false);
    if (viewerUrl == null) return;

    final uri = Uri.parse(viewerUrl);
    if (await canLaunchUrl(uri)) {
      await launchUrl(uri, mode: LaunchMode.externalApplication);
    } else if (mounted) {
      setState(
        () => _error = AppLocalizations.of(context).flyfish_openBrowserFailed,
      );
    }
  }
}
