import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/services/local_storage.dart';
import 'package:url_launcher/url_launcher.dart';
import 'package:webview_flutter/webview_flutter.dart';

/// Flyfish Viewer 预览页面
/// 移动端：通过 WebView 加载 file-viewer（URL 模式）
/// 桌面端：在系统浏览器中打开 file-viewer
class FlyfishViewerPage extends StatefulWidget {
  final File file;

  const FlyfishViewerPage({super.key, required this.file});

  @override
  State<FlyfishViewerPage> createState() => _FlyfishViewerPageState();
}

class _FlyfishViewerPageState extends State<FlyfishViewerPage> {
  late final WebViewController? _controller;
  bool _isLoading = true;
  String? _error;
  late final String _baseUrl;
  late final String _token;
  final bool _isDesktop =
      Platform.isWindows || Platform.isMacOS || Platform.isLinux;

  @override
  void initState() {
    super.initState();
    final ls = GetIt.I.get<LocalStorage>();
    _baseUrl = ls.fileServer ?? '';
    _token = ls.token?.accessToken ?? '';

    if (_isDesktop) {
      _controller = null;
      _isLoading = false;
    } else {
      _initWebView();
    }
  }

  void _initWebView() {
    final fileName = Uri.encodeComponent(widget.file.name);
    final fileUrl =
        '$_baseUrl/api/file/preview/raw/$fileName?file_id=${widget.file.id}&token=$_token';
    final options = Uri.encodeComponent('{"toolbar":true}');

    // 使用 URL 模式（不是二进制推送模式）
    final viewerUrl =
        '$_baseUrl/file-viewer/index.html'
        '?url=${Uri.encodeComponent(fileUrl)}'
        '&options=$options';

    _controller = WebViewController()
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
      )
      ..loadRequest(Uri.parse(viewerUrl));
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context)!;

    return Scaffold(
      appBar: AppBar(
        title: Text(widget.file.name,
            maxLines: 1, overflow: TextOverflow.ellipsis),
        actions: [
          if (!_isDesktop)
            IconButton(
              icon: const Icon(Icons.refresh),
              onPressed: () {
                setState(() {
                  _isLoading = true;
                  _error = null;
                });
                _initWebView();
              },
            ),
        ],
      ),
      body: _isDesktop ? _buildDesktopView() : _buildMobileView(),
    );
  }

  /// 桌面端：在系统浏览器中打开文件预览
  Widget _buildDesktopView() {
    final al = AppLocalizations.of(context)!;

    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(Icons.open_in_browser, size: 64, color: Colors.blue[400]),
          const SizedBox(height: 16),
          Text(
            al.flyfish_willOpenInBrowser,
            style: TextStyle(fontSize: 18, color: Colors.grey[600]),
          ),
          const SizedBox(height: 24),
          ElevatedButton.icon(
            onPressed: () => _openInBrowser(),
            icon: const Icon(Icons.open_in_new),
            label: Text(al.flyfish_openPreview),
          ),
        ],
      ),
    );
  }

  /// 移动端：WebView 预览
  Widget _buildMobileView() {
    final al = AppLocalizations.of(context)!;

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
                  onPressed: () {
                    setState(() {
                      _isLoading = true;
                      _error = null;
                    });
                    _initWebView();
                  },
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
                Text(al.flyfish_loadingDocument),
              ],
            ),
          ),
      ],
    );
  }

  /// 在系统浏览器中打开文件预览
  Future<void> _openInBrowser() async {
    final fileName = Uri.encodeComponent(widget.file.name);
    final fileUrl =
        '$_baseUrl/api/file/preview/raw/$fileName?file_id=${widget.file.id}&token=$_token';
    final options = Uri.encodeComponent('{"toolbar":true}');

    final viewerUrl =
        '$_baseUrl/file-viewer/index.html'
        '?url=${Uri.encodeComponent(fileUrl)}'
        '&options=$options';

    final uri = Uri.parse(viewerUrl);
    if (await canLaunchUrl(uri)) {
      await launchUrl(uri, mode: LaunchMode.externalApplication);
    }
  }
}
