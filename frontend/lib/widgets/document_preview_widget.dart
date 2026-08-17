import 'package:flutter/material.dart';
import 'package:dio/dio.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/services/local_storage.dart';

/// 文档预览组件
/// PDF 和 DOCX 已迁移到 Flyfish Viewer，此处仅保留 TXT 预览
class DocumentPreviewWidget extends StatefulWidget {
  const DocumentPreviewWidget({
    super.key,
    required this.file,
    required this.previewUrl,
  });

  final File file;
  final String previewUrl;

  @override
  State<DocumentPreviewWidget> createState() => _DocumentPreviewWidgetState();
}

class _DocumentPreviewWidgetState extends State<DocumentPreviewWidget> {
  bool _isLoading = true;
  String? _error;
  String? _textContent;

  @override
  void initState() {
    super.initState();
    _loadDocument();
  }

  Future<void> _loadDocument() async {
    final al = AppLocalizations.of(context);
    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      final extension = _getFileExtension(widget.file.name);

      if (extension == 'txt' || extension == 'log' || extension == 'md') {
        await _loadTxtDocument();
      } else {
        setState(() {
          _error = al.docPreview_useFlyfishViewer;
          _isLoading = false;
        });
      }
    } catch (e) {
      setState(() {
        _error = '${al.docPreview_loadFailed}: $e';
        _isLoading = false;
      });
    }
  }

  String _getFileExtension(String fileName) {
    final parts = fileName.split('.');
    if (parts.length > 1) {
      return parts.last.toLowerCase();
    }
    return '';
  }

  Map<String, String> _getAuthHeaders() {
    final ls = GetIt.I.get<LocalStorage>();
    final token = ls.token;
    final headers = <String, String>{};
    if (token?.accessToken != null) {
      headers['Authorization'] = 'Bearer ${token!.accessToken}';
    }
    return headers;
  }

  Future<void> _loadTxtDocument() async {
    final al = AppLocalizations.of(context);
    try {
      final dio = Dio();
      final response = await dio.get(
        widget.previewUrl,
        options: Options(
          responseType: ResponseType.plain,
          headers: _getAuthHeaders(),
        ),
      );

      setState(() {
        _textContent = response.data.toString();
        _isLoading = false;
      });
    } catch (e) {
      setState(() {
        _error = '${al.docPreview_loadTextFailed}: $e';
        _isLoading = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_isLoading) {
      return const Center(child: CircularProgressIndicator());
    }

    if (_error != null) {
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
              const SizedBox(height: 24),
              Text(
                _error!,
                style: Theme.of(context).textTheme.bodyLarge,
                textAlign: TextAlign.center,
              ),
            ],
          ),
        ),
      );
    }

    return _buildTxtViewer(context);
  }

  Widget _buildTxtViewer(BuildContext context) {
    return Column(
      children: [
        Container(
          width: double.infinity,
          padding: const EdgeInsets.all(12),
          color: Theme.of(context).colorScheme.surfaceContainerHighest,
          child: Text(
            widget.file.name,
            style: Theme.of(context).textTheme.titleMedium,
            overflow: TextOverflow.ellipsis,
          ),
        ),
        Expanded(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(16),
            child: SelectableText(
              _textContent ?? '',
              style: const TextStyle(
                fontFamily: 'monospace',
                fontSize: 14,
                height: 1.6,
              ),
            ),
          ),
        ),
      ],
    );
  }
}
