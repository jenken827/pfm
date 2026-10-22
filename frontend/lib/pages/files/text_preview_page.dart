import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:pfm/api/fm_repository.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/utils/text_preview.dart';
import 'package:pfm/widgets/markdown_view.dart';
import 'package:url_launcher/url_launcher.dart';

/// 纯文本预览页（配置 / 代码 / 日志 / Markdown）
///
/// 这些文件本身不大、打开频繁：用 Dart 渲染省掉 WebView 初始化与 JS chunk
/// 下载，还能跟随应用主题。Office / 压缩包等仍然交给 Flyfish Viewer。
class TextPreviewPage extends StatefulWidget {
  const TextPreviewPage({super.key, required this.file, this.loader});

  final File file;

  /// 字节加载器（仅测试注入；为 null 时走 [FmRepository]）
  final Future<ApiResult<List<int>>> Function(File file)? loader;

  @override
  State<TextPreviewPage> createState() => _TextPreviewPageState();
}

class _TextPreviewPageState extends State<TextPreviewPage> {
  /// 延迟创建：测试注入 loader 时不需要真实的 Repository（依赖 GetIt 里的登录态）
  late final FmRepository _repository = FmRepository();

  bool _isLoading = true;
  String? _error;
  DecodedText? _text;
  bool _wrap = true;
  bool _showSource = false;

  bool get _isMarkdown => isMarkdownFile(widget.file.name);

  /// Markdown 默认显示渲染结果，源码视图只在用户主动切换时出现
  bool get _renderMarkdown => _isMarkdown && !_showSource;

  @override
  void initState() {
    super.initState();
    _loadContent();
  }

  Future<void> _loadContent() async {
    setState(() {
      _isLoading = true;
      _error = null;
    });

    final result = await _fetchBytes();
    if (!mounted) return;
    // 文案在 await 之后再取：initState 里读 InheritedWidget 会触发 Flutter 断言
    final al = AppLocalizations.of(context);

    final bytes = result.data;
    if (!result.success || bytes == null) {
      setState(() {
        _error = result.message ?? al.textPreview_loadFailed;
        _isLoading = false;
      });
      return;
    }

    setState(() {
      _text = decodeTextBytes(Uint8List.fromList(bytes));
      _isLoading = false;
    });
  }

  /// 历史版本的 id 形如 `history:{id}`，走独立的历史预览接口
  Future<ApiResult<List<int>>> _fetchBytes() {
    final loader = widget.loader;
    if (loader != null) return loader(widget.file);

    final id = widget.file.id;
    if (id.startsWith('history:')) {
      final historyId = int.tryParse(id.substring('history:'.length));
      if (historyId == null) {
        return Future.value(
          ApiResult<List<int>>(success: false, message: '历史版本 ID 无效'),
        );
      }
      return _repository.fetchHistoryPreviewBytes(historyId: historyId);
    }
    return _repository.fetchPreviewBytes(
      fileId: id,
      fileName: widget.file.name,
    );
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context);
    return Scaffold(
      appBar: AppBar(
        title: Text(widget.file.name, overflow: TextOverflow.ellipsis),
        actions: [
          if (_isMarkdown && _text != null)
            IconButton(
              tooltip: _showSource
                  ? al.textPreview_showRendered
                  : al.textPreview_showSource,
              icon: Icon(_showSource ? Icons.article_outlined : Icons.code),
              onPressed: () => setState(() => _showSource = !_showSource),
            ),
          if (!_renderMarkdown && _text != null)
            IconButton(
              tooltip: al.textPreview_wrap,
              isSelected: _wrap,
              icon: const Icon(Icons.wrap_text),
              onPressed: () => setState(() => _wrap = !_wrap),
            ),
          IconButton(
            tooltip: al.textPreview_copy,
            icon: const Icon(Icons.copy_all),
            onPressed: _text == null ? null : () => _copyAll(al),
          ),
        ],
      ),
      body: _buildBody(al),
    );
  }

  Widget _buildBody(AppLocalizations al) {
    if (_isLoading) {
      return const Center(child: CircularProgressIndicator());
    }

    final text = _text;
    if (_error != null) {
      return _buildError(al, _error!);
    }
    if (text == null || text.isEmpty) {
      return Center(
        child: Text(
          al.textPreview_empty,
          style: Theme.of(context).textTheme.bodyMedium
              ?.copyWith(color: Theme.of(context).colorScheme.onSurfaceVariant),
        ),
      );
    }

    return Column(
      children: [
        if (text.truncated || text.invalidUtf8) _buildNotices(al, text),
        Expanded(
          child: _renderMarkdown
              ? _buildMarkdown(text.content, al)
              : _buildSource(text.content),
        ),
      ],
    );
  }

  Widget _buildMarkdown(String content, AppLocalizations al) {
    return SingleChildScrollView(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      child: MarkdownView(
        source: content,
        onLinkTap: (uri) => _openLink(uri, al),
      ),
    );
  }

  Widget _buildSource(String content) {
    final body = Padding(
      padding: const EdgeInsets.all(12),
      child: SelectableText(
        content,
        style: const TextStyle(
          fontFamily: 'monospace',
          fontSize: 13,
          height: 1.5,
        ),
      ),
    );

    return SingleChildScrollView(
      child: _wrap
          ? body
          : SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: body,
            ),
    );
  }

  /// 截断 / 编码提示条（不挡内容，只是说明当前看到的是"不完全/可能乱码"的文本）
  Widget _buildNotices(AppLocalizations al, DecodedText text) {
    final messages = <String>[
      if (text.truncated)
        al.textPreview_truncated(formatTextSizeHint(kTextPreviewMaxBytes)),
      if (text.invalidUtf8) al.textPreview_nonUtf8,
    ];

    return Container(
      width: double.infinity,
      color: Theme.of(context).colorScheme.secondaryContainer,
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          for (final message in messages)
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Icon(
                  Icons.info_outline,
                  size: 16,
                  color: Theme.of(context).colorScheme.onSecondaryContainer,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    message,
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: Theme.of(context).colorScheme.onSecondaryContainer,
                    ),
                  ),
                ),
              ],
            ),
        ],
      ),
    );
  }

  Widget _buildError(AppLocalizations al, String message) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
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
              message,
              style: Theme.of(context).textTheme.bodyMedium,
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 16),
            FilledButton.icon(
              onPressed: _loadContent,
              icon: const Icon(Icons.refresh),
              label: Text(al.common_retry),
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _copyAll(AppLocalizations al) async {
    final content = _text?.content;
    if (content == null) return;
    await Clipboard.setData(ClipboardData(text: content));
    if (!mounted) return;
    _showSnackBar(al.textPreview_copied);
  }

  Future<void> _openLink(Uri uri, AppLocalizations al) async {
    var opened = false;
    try {
      opened = await launchUrl(uri, mode: LaunchMode.externalApplication);
    } catch (_) {
      opened = false;
    }
    if (!opened && mounted) {
      _showSnackBar(al.textPreview_openLinkFailed);
    }
  }

  void _showSnackBar(String message) {
    ScaffoldMessenger.of(context)
        .showSnackBar(SnackBar(content: Text(message)));
  }
}
