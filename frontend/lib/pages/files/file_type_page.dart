import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:go_router/go_router.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/design/design_tokens.dart';
import 'package:pfm/utils/format.dart';
import 'package:pfm/pages/files/file_preview_page.dart';

/// 按文件类型浏览页面
class FileTypePage extends StatefulWidget {
  final int fileType;

  const FileTypePage({super.key, required this.fileType});

  @override
  State<FileTypePage> createState() => _FileTypePageState();
}

class _FileTypePageState extends State<FileTypePage> {
  final List<File> _files = [];
  bool _loading = true;
  bool _hasMore = true;
  int _offset = 0;
  final int _pageSize = 20;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    _loadFiles();
  }

  Future<void> _loadFiles() async {
    if (!_hasMore) return;

    final repository = context.read<FmRepository>();
    final result = await repository.getFilesByType(
      fileType: FileType.valueOf(widget.fileType) ?? FileType.FILE_TYPE_UNKNOWN,
      offset: _offset,
      pageSize: _pageSize,
    );

    if (mounted) {
      setState(() {
        if (result.success && result.data != null) {
          final newFiles = result.data!.files;
          if (newFiles.length < _pageSize) {
            _hasMore = false;
          }
          _files.addAll(newFiles);
          _offset += newFiles.length;
        } else {
          _errorMessage = result.message ?? '加载失败';
        }
        _loading = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context);
    final colorScheme = Theme.of(context).colorScheme;
    final typeName = _getFileTypeName(widget.fileType, al);

    return Scaffold(
      appBar: AppBar(
        title: Text(typeName, style: TextStyle(color: colorScheme.onSurface)),
        foregroundColor: colorScheme.onSurface,
        leading: IconButton(
          icon: const Icon(Icons.arrow_back),
          onPressed: () => Navigator.of(context).pop(),
        ),
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : _errorMessage != null
          ? _buildError(context, colorScheme)
          : _files.isEmpty
          ? _buildEmpty(context, al, colorScheme)
          : _buildFileList(context, colorScheme),
    );
  }

  Widget _buildError(BuildContext context, ColorScheme colorScheme) {
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(Icons.error_outline, size: 48, color: colorScheme.error),
          const SizedBox(height: PfmSpacing.md),
          Text(
            _errorMessage!,
            style: PfmTypography.bodyMedium.copyWith(color: colorScheme.error),
          ),
          const SizedBox(height: PfmSpacing.lg),
          ElevatedButton(
            onPressed: () {
              setState(() {
                _loading = true;
                _errorMessage = null;
                _offset = 0;
                _files.clear();
                _hasMore = true;
              });
              _loadFiles();
            },
            child: const Text('重试'),
          ),
        ],
      ),
    );
  }

  Widget _buildEmpty(
    BuildContext context,
    AppLocalizations al,
    ColorScheme colorScheme,
  ) {
    final typeName = _getFileTypeName(widget.fileType, al);
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(
            _getFileIcon(widget.fileType),
            size: 64,
            color: colorScheme.onSurfaceVariant,
          ),
          const SizedBox(height: PfmSpacing.lg),
          Text(
            '暂无$typeName文件',
            style: PfmTypography.titleMedium.copyWith(
              color: colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildFileList(BuildContext context, ColorScheme colorScheme) {
    return NotificationListener<ScrollNotification>(
      onNotification: (scrollInfo) {
        if (scrollInfo.metrics.pixels == scrollInfo.metrics.maxScrollExtent &&
            _hasMore &&
            !_loading) {
          _loadFiles();
        }
        return false;
      },
      child: ListView.separated(
        padding: const EdgeInsets.all(PfmSpacing.lg),
        itemCount: _files.length + (_hasMore ? 1 : 0),
        separatorBuilder: (context, index) =>
            const SizedBox(height: PfmSpacing.sm),
        itemBuilder: (context, index) {
          if (index == _files.length) {
            return const Center(
              child: Padding(
                padding: EdgeInsets.all(PfmSpacing.lg),
                child: CircularProgressIndicator(),
              ),
            );
          }
          return _buildFileTile(context, _files[index], colorScheme);
        },
      ),
    );
  }

  Widget _buildFileTile(
    BuildContext context,
    File file,
    ColorScheme colorScheme,
  ) {
    final fileTypeName = _getFileTypeName(
      file.type.value,
      AppLocalizations.of(context),
    );

    return Card(
      elevation: 0,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(PfmRadius.md),
        side: BorderSide(color: colorScheme.outlineVariant),
      ),
      child: ListTile(
        leading: Container(
          width: 40,
          height: 40,
          decoration: BoxDecoration(
            color: _getFileColor(file.type.value).withValues(alpha: 0.1),
            borderRadius: BorderRadius.circular(PfmRadius.sm),
          ),
          child: Icon(
            _getFileIcon(file.type.value),
            color: _getFileColor(file.type.value),
            size: 20,
          ),
        ),
        title: Text(
          file.name,
          style: PfmTypography.bodyMedium.copyWith(
            color: colorScheme.onSurface,
          ),
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
        subtitle: Text(
          '${formatCapacityString(file.size.toInt())} · $fileTypeName',
          style: PfmTypography.bodySmall.copyWith(
            color: colorScheme.onSurfaceVariant,
          ),
        ),
        trailing: Icon(
          Icons.chevron_right,
          color: colorScheme.onSurfaceVariant,
        ),
        onTap: () {
          if (file.type == FileType.IMAGE) {
            // 图片类型：画廊模式
            final images = _files
                .where((f) => f.type == FileType.IMAGE)
                .toList();
            context.push(
              '/files/preview',
              extra: PreviewArgs(
                file: file,
                images: images,
                initialIndex: images.indexOf(file),
              ),
            );
          } else if (file.type == FileType.VIDEO ||
              file.type == FileType.AUDIO ||
              file.type == FileType.DOCUMENT) {
            context.push('/files/preview', extra: file);
          } else {
            context.push('/files/detail', extra: file);
          }
        },
      ),
    );
  }

  String _getFileTypeName(int fileType, AppLocalizations al) {
    switch (fileType) {
      case 2:
        return al.fileType_image;
      case 3:
        return al.fileType_video;
      case 4:
        return al.fileType_audio;
      case 5:
        return al.fileType_document;
      case 6:
        return al.fileType_archive;
      case 7:
        return al.fileType_font;
      case 8:
        return al.fileType_application;
      default:
        return '文件';
    }
  }

  IconData _getFileIcon(int fileType) {
    switch (fileType) {
      case 2:
        return Icons.image;
      case 3:
        return Icons.video_file;
      case 4:
        return Icons.audio_file;
      case 5:
        return Icons.description;
      case 6:
        return Icons.archive;
      case 7:
        return Icons.font_download;
      case 8:
        return Icons.android;
      default:
        return Icons.insert_drive_file;
    }
  }

  Color _getFileColor(int fileType) {
    switch (fileType) {
      case 2:
        return PfmColors.primary;
      case 3:
        return PfmColors.warning;
      case 4:
        return PfmColors.success;
      case 5:
        return PfmColors.info;
      case 6:
        return PfmColors.error;
      case 7:
        return const Color(0xFF9C27B0);
      case 8:
        return const Color(0xFF607D8B);
      default:
        return PfmColors.primary;
    }
  }
}
