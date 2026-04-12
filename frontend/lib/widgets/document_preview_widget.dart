import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:dio/dio.dart';
import 'package:get_it/get_it.dart';
import 'package:pdfx/pdfx.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/services/local_storage.dart';

/// 文档预览组件
class DocumentPreviewWidget extends StatefulWidget {
  const DocumentPreviewWidget({
    super.key,
    required this.file,
    required this.baseUrl,
  });

  final File file;
  final String baseUrl;

  @override
  State<DocumentPreviewWidget> createState() => _DocumentPreviewWidgetState();
}

class _DocumentPreviewWidgetState extends State<DocumentPreviewWidget> {
  bool _isLoading = true;
  String? _error;
  
  // TXT 内容
  String? _textContent;
  
  // PDF 文档和控制器
  PdfDocument? _pdfDocument;
  PdfController? _pdfController;
  int _totalPages = 0;
  int _currentPage = 1;

  @override
  void initState() {
    super.initState();
    _loadDocument();
  }

  @override
  void dispose() {
    _pdfController?.dispose();
    super.dispose();
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
        await _loadPdfDocument();
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

  /// 加载 TXT 文档
  Future<void> _loadTxtDocument() async {
    try {
      final ls = GetIt.I.get<LocalStorage>();
      final token = ls.token;
      
      final dio = Dio();
      final response = await dio.get(
        '${widget.baseUrl}/api/file/stream/preview?file_id=${widget.file.id}',
        options: Options(
          responseType: ResponseType.plain,
          headers: {
            if (token?.accessToken != null)
              'Authorization': 'Bearer ${token!.accessToken}',
          },
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

  /// 加载 PDF 文档
  Future<void> _loadPdfDocument() async {
    try {
      final ls = GetIt.I.get<LocalStorage>();
      final token = ls.token;
      
      final headers = <String, String>{};
      if (token?.accessToken != null) {
        headers['Authorization'] = 'Bearer ${token!.accessToken}';
      }

      // 获取 PDF 数据
      final pdfData = await _fetchPdfData(
        '${widget.baseUrl}/api/file/stream/preview?file_id=${widget.file.id}',
        headers,
      );

      // 使用数据打开 PDF
      _pdfDocument = await PdfDocument.openData(pdfData);
      
      if (_pdfDocument != null) {
        _pdfController = PdfController(
          document: Future.value(_pdfDocument),
        );
        setState(() {
          _totalPages = _pdfDocument!.pagesCount;
          _isLoading = false;
        });
      } else {
        setState(() {
          _error = '无法加载 PDF 文档';
          _isLoading = false;
        });
      }
    } catch (e) {
      setState(() {
        _error = '加载 PDF 失败: $e';
        _isLoading = false;
      });
    }
  }

  /// 获取 PDF 数据
  Future<Uint8List> _fetchPdfData(String url, Map<String, String> headers) async {
    final dio = Dio();
    final response = await dio.get(
      url,
      options: Options(
        responseType: ResponseType.bytes,
        headers: headers,
      ),
    );
    return Uint8List.fromList(response.data as List<int>);
  }

  @override
  Widget build(BuildContext context) {
    if (_isLoading) {
      return const Center(child: CircularProgressIndicator());
    }

    if (_error != null) {
      return Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(Icons.error_outline, size: 64, color: Theme.of(context).colorScheme.error),
            const SizedBox(height: 16),
            Text(_error!, style: Theme.of(context).textTheme.bodyLarge),
          ],
        ),
      );
    }

    final extension = _getFileExtension(widget.file.name);
    
    if (extension == 'txt') {
      return _buildTxtViewer(context);
    } else if (extension == 'pdf') {
      return _buildPdfViewer(context);
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

  /// PDF 查看器
  Widget _buildPdfViewer(BuildContext context) {
    if (_pdfController == null) {
      return const Center(child: Text('PDF 加载失败'));
    }

    return Column(
      children: [
        // 标题栏和页码控制
        Container(
          width: double.infinity,
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
          color: Theme.of(context).colorScheme.surfaceContainerHighest,
          child: Row(
            children: [
              Expanded(
                child: Text(
                  widget.file.name,
                  style: Theme.of(context).textTheme.titleSmall,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              // 页码控制
              Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  IconButton(
                    icon: const Icon(Icons.chevron_left),
                    onPressed: _currentPage > 1
                        ? () {
                            _pdfController!.animateToPage(
                              _currentPage - 1,
                              duration: const Duration(milliseconds: 200),
                              curve: Curves.easeInOut,
                            );
                            setState(() => _currentPage--);
                          }
                        : null,
                  ),
                  Text('$_currentPage / $_totalPages'),
                  IconButton(
                    icon: const Icon(Icons.chevron_right),
                    onPressed: _currentPage < _totalPages
                        ? () {
                            _pdfController!.animateToPage(
                              _currentPage + 1,
                              duration: const Duration(milliseconds: 200),
                              curve: Curves.easeInOut,
                            );
                            setState(() => _currentPage++);
                          }
                        : null,
                  ),
                ],
              ),
            ],
          ),
        ),
        // PDF 内容
        Expanded(
          child: PdfView(
            controller: _pdfController!,
            onPageChanged: (page) {
              setState(() {
                _currentPage = page + 1;
              });
            },
          ),
        ),
      ],
    );
  }
}
