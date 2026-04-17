import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:dio/dio.dart';
import 'package:docx_file_viewer/docx_file_viewer.dart';
import 'package:get_it/get_it.dart';
import 'package:pdfrx/pdfrx.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/services/local_storage.dart';

/// 文档预览组件
class DocumentPreviewWidget extends StatefulWidget {
  const DocumentPreviewWidget({
    super.key,
    required this.file,
    required this.previewUrl,
  });

  final File file;
  final String previewUrl; // 直接传递预览URL，已处理历史版本

  @override
  State<DocumentPreviewWidget> createState() => _DocumentPreviewWidgetState();
}

class _DocumentPreviewWidgetState extends State<DocumentPreviewWidget> {
  bool _isLoading = true;
  String? _error;
  
  // TXT 内容
  String? _textContent;
  
  // DOCX 数据
  Uint8List? _docxData;

  @override
  void initState() {
    super.initState();
    _loadDocument();
  }

  /// 加载文档
  Future<void> _loadDocument() async {
    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      final extension = _getFileExtension(widget.file.name);
      
      if (extension == 'txt') {
        await _loadTxtDocument();
      } else if (extension == 'pdf') {
        // PDF 使用 URI 流式加载，无需预先下载
        setState(() {
          _isLoading = false;
        });
      } else if (extension == 'docx') {
        await _loadDocxDocument();
      } else if (extension == 'doc') {
        // .doc 是旧版 Word 格式，暂不支持
        setState(() {
          _error = '暂不支持 .doc 格式预览\n\n建议：\n• 将文件转换为 .docx 格式\n• 或下载后使用 Word 打开';
          _isLoading = false;
        });
      } else {
        setState(() {
          _error = '暂不支持此文件格式预览';
          _isLoading = false;
        });
      }
    } catch (e) {
      setState(() {
        _error = '加载文档失败: $e';
        _isLoading = false;
      });
    }
  }

  /// 获取文件扩展名
  String _getFileExtension(String fileName) {
    final parts = fileName.split('.');
    if (parts.length > 1) {
      return parts.last.toLowerCase();
    }
    return '';
  }

  /// 获取认证请求头
  Map<String, String> _getAuthHeaders() {
    final ls = GetIt.I.get<LocalStorage>();
    final token = ls.token;
    final headers = <String, String>{};
    if (token?.accessToken != null) {
      headers['Authorization'] = 'Bearer ${token!.accessToken}';
    }
    return headers;
  }

  /// 加载 TXT 文档
  Future<void> _loadTxtDocument() async {
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
        _error = '加载文本文件失败: $e';
        _isLoading = false;
      });
    }
  }

  /// 加载 DOCX 文档
  Future<void> _loadDocxDocument() async {
    try {
      final dio = Dio();
      final response = await dio.get(
        widget.previewUrl,
        options: Options(
          responseType: ResponseType.bytes,
          headers: _getAuthHeaders(),
        ),
      );

      setState(() {
        _docxData = Uint8List.fromList(response.data as List<int>);
        _isLoading = false;
      });
    } catch (e) {
      setState(() {
        _error = '加载 Word 文档失败: $e';
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
              Icon(Icons.error_outline, size: 64, color: Theme.of(context).colorScheme.error),
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

    final extension = _getFileExtension(widget.file.name);
    
    if (extension == 'txt') {
      return _buildTxtViewer(context);
    } else if (extension == 'pdf') {
      return _buildPdfViewer(context);
    } else if (extension == 'docx') {
      return _buildDocxViewer(context);
    }

    return const Center(child: Text('不支持的文件格式'));
  }

  /// TXT 查看器
  Widget _buildTxtViewer(BuildContext context) {
    return Column(
      children: [
        // 标题栏
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
        // 内容区域
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

  /// PDF 查看器（使用 URI 流式加载）
  Widget _buildPdfViewer(BuildContext context) {
    return PdfViewer.uri(
      Uri.parse(widget.previewUrl),
      useProgressiveLoading: true,   // 渐进式加载
      preferRangeAccess: true,       // 使用 Range 请求，按需加载
      headers: _getAuthHeaders(),    // 认证头
      params: PdfViewerParams(
        textSelectionParams: const PdfTextSelectionParams(enabled: true),
        // 自适应屏幕：使用 fitZoom（适应宽度）
        calculateInitialZoom: (document, controller, fitZoom, coverZoom) {
          return fitZoom; // 适应查看器宽度
        },
        // 页面锚点：顶部居中
        pageAnchor: PdfPageAnchor.top,
        // 允许缩放范围
        minScale: 0.5,
        maxScale: 4.0,
        // 启用缩放和平移
        scaleEnabled: true,
        panEnabled: true,
      ),
    );
  }

  /// DOCX 查看器
  Widget _buildDocxViewer(BuildContext context) {
    if (_docxData == null) {
      return const Center(child: Text('Word 文档加载失败'));
    }

    return DocxView.bytes(
      _docxData!,
      config: const DocxViewConfig(
        enableSearch: true,
        enableZoom: true,
        pageMode: DocxPageMode.continuous,
      ),
    );
  }
}
