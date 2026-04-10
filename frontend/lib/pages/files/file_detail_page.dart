import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:intl/intl.dart';
import 'package:path_provider/path_provider.dart';
import 'package:pfm/api/fm_repository.dart';
import 'package:pfm/blocs/transfer_bloc/transfer_bloc.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:protobuf/well_known_types/google/protobuf/timestamp.pb.dart';
import 'package:pfm/router/files_route.dart';
import 'package:pfm/services/logger.dart';

/// 文件详情页面（从API获取完整数据）
class FileDetailPage extends StatefulWidget {
  const FileDetailPage({super.key, required this.file});

  final File file;

  @override
  State<FileDetailPage> createState() => _FileDetailPageState();
}

class _FileDetailPageState extends State<FileDetailPage> {
  FileDetailRespData? _detailData;
  bool _isLoading = true;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    _loadDetail();
  }

  Future<void> _loadDetail() async {
    setState(() {
      _isLoading = true;
      _errorMessage = null;
    });

    try {
      final repo = FmRepository();
      final result = await repo.getFileDetail(widget.file.id);
      if (result.success && result.data != null) {
        setState(() {
          _detailData = result.data;
          _isLoading = false;
        });
      } else {
        setState(() {
          _errorMessage = result.message ?? '获取详情失败';
          _isLoading = false;
        });
      }
    } catch (e) {
      getLogger().error('加载文件详情失败: $e');
      setState(() {
        _errorMessage = e.toString();
        _isLoading = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('文件详情'),
        centerTitle: true,
        actions: [
          IconButton(
            icon: const Icon(Icons.preview),
            onPressed: () {
              if (_isPreviewable()) {
                FilePreviewRoute().push(context, extra: widget.file);
              }
            },
          ),
          IconButton(icon: const Icon(Icons.refresh), onPressed: _loadDetail),
        ],
      ),
      body: _isLoading
          ? const Center(child: CircularProgressIndicator())
          : _errorMessage != null
          ? _buildErrorWidget(context)
          : _buildContent(context),
    );
  }

  Widget _buildErrorWidget(BuildContext context) {
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(
            Icons.error_outline,
            size: 48,
            color: Theme.of(context).colorScheme.error,
          ),
          const SizedBox(height: 16),
          Text('加载失败', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 8),
          Text(
            _errorMessage ?? '',
            style: Theme.of(context).textTheme.bodySmall,
          ),
          const SizedBox(height: 16),
          FilledButton(onPressed: _loadDetail, child: const Text('重试')),
        ],
      ),
    );
  }

  Widget _buildContent(BuildContext context) {
    final file = _detailData?.file ?? widget.file;
    return RefreshIndicator(
      onRefresh: _loadDetail,
      child: SingleChildScrollView(
        physics: const AlwaysScrollableScrollPhysics(),
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _buildFileHeader(context, file),
            const SizedBox(height: 24),
            _buildBasicInfo(context, file),
            const SizedBox(height: 24),
            _buildLocationDistribution(context),
            const SizedBox(height: 24),
            _buildFileHistory(context),
          ],
        ),
      ),
    );
  }

  // ==================== 文件头部 ====================

  Widget _buildFileHeader(BuildContext context, File file) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(12),
      ),
      child: Row(
        children: [
          Icon(
            _getFileIcon(file.type),
            color: _getFileColor(file.type),
            size: 48,
          ),
          const SizedBox(width: 16),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  file.name,
                  style: Theme.of(context).textTheme.titleLarge,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                ),
                const SizedBox(height: 4),
                Text(
                  _getFileTypeText(file.type),
                  style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  // ==================== 基本信息 ====================

  Widget _buildBasicInfo(BuildContext context, File file) {
    return _buildSection(
      context,
      title: '基本信息',
      child: Column(
        children: [
          _buildInfoRow(
            context,
            Icons.sd_storage,
            '文件大小',
            _formatFileSize(file.size.toInt()),
          ),
          _buildInfoRow(
            context,
            Icons.tag,
            '文件哈希',
            file.hash,
            isMonospace: true,
          ),
          _buildInfoRow(context, Icons.insert_drive_file, 'MIME类型', file.mime),
          _buildInfoRow(
            context,
            Icons.schedule,
            '创建时间',
            _formatTimestamp(file.createdAt),
          ),
          _buildInfoRow(
            context,
            Icons.update,
            '修改时间',
            _formatTimestamp(file.updatedAt),
          ),
        ],
      ),
    );
  }

  // ==================== 位置分布 ====================

  Widget _buildLocationDistribution(BuildContext context) {
    final locations = _detailData?.locations ?? [];

    return _buildSection(
      context,
      title: '位置分布',
      child: locations.isEmpty
          ? Center(
              child: Text(
                '暂无位置信息',
                style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
            )
          : Column(
              children: [
                for (final loc in locations) _buildLocationRow(context, loc),
              ],
            ),
    );
  }

  Widget _buildLocationRow(BuildContext context, ProviderLocationInfo loc) {
    final providerName = loc.providerName;
    final encrypted = loc.encrypted;
    final isChunked = loc.isChunked;
    final chunkTotal = loc.chunkTotal;
    final statusText = _getProviderStatusText(loc.status);

    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Row(
        children: [
          Icon(_getProviderIcon(loc.providerType), size: 20),
          const SizedBox(width: 8),
          Expanded(
            flex: 2,
            child: Text(
              providerName,
              style: Theme.of(context).textTheme.bodyMedium,
            ),
          ),
          // 加密标记
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
            decoration: BoxDecoration(
              color: encrypted
                  ? Theme.of(context).colorScheme.errorContainer
                  : Theme.of(context).colorScheme.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(4),
            ),
            child: Text(
              encrypted ? '已加密' : '未加密',
              style: Theme.of(context).textTheme.labelSmall?.copyWith(
                color: encrypted
                    ? Theme.of(context).colorScheme.onErrorContainer
                    : Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ),
          ),
          const SizedBox(width: 8),
          // 分片标记
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
            decoration: BoxDecoration(
              color: isChunked
                  ? Theme.of(context).colorScheme.tertiaryContainer
                  : Theme.of(context).colorScheme.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(4),
            ),
            child: Text(
              isChunked ? '$chunkTotal 分片' : '单文件',
              style: Theme.of(context).textTheme.labelSmall?.copyWith(
                color: isChunked
                    ? Theme.of(context).colorScheme.onTertiaryContainer
                    : Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ),
          ),
          const SizedBox(width: 8),
          // 状态
          Text(
            statusText,
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
              color: loc.status == UploadProviderStatus.PCOMPLETED
                  ? Colors.green
                  : Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(width: 4),
          // 下载按钮
          IconButton(
            icon: const Icon(Icons.download, size: 20),
            onPressed: () async {
              final downloadDir = await getDownloadsDirectory();
              if (!mounted) return;
              final fileName = widget.file.name.isNotEmpty
                  ? widget.file.name
                  : 'unnamed';
              final savePath = '${downloadDir!.path}/$fileName';
              if (!mounted) return;
              context.read<TransferBloc>().add(
                TransferAddDownloadEvent(
                  fileId: widget.file.id,
                  filename: fileName,
                  fileSize: widget.file.size.toInt(),
                  savePath: savePath,
                  providerId: loc.providerId,
                ),
              );
              ScaffoldMessenger.of(
                context,
              ).showSnackBar(SnackBar(content: Text('已开始下载: $fileName')));
            },
            tooltip: '下载',
          ),
        ],
      ),
    );
  }

  // ==================== 文件历史 ====================

  Widget _buildFileHistory(BuildContext context) {
    final histories = _detailData?.histories ?? [];

    return _buildSection(
      context,
      title: '文件历史',
      child: histories.isEmpty
          ? Center(
              child: Text(
                '暂无历史版本',
                style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
            )
          : Column(
              children: [
                for (final h in histories) _buildHistoryRow(context, h),
              ],
            ),
    );
  }

  Widget _buildHistoryRow(BuildContext context, FileHistory h) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        children: [
          Icon(
            Icons.history,
            size: 16,
            color: Theme.of(context).colorScheme.onSurfaceVariant,
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'v${_formatTimestamp(h.versionNumber)}',
                  style: Theme.of(context).textTheme.bodyMedium,
                ),
                Text(
                  _formatFileSize(h.size.toInt()),
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
          ),
          Text(
            h.hash.length > 12 ? h.hash.substring(0, 12) : h.hash,
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
              fontFamily: 'monospace',
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }

  // ==================== 通用组件 ====================

  Widget _buildSection(
    BuildContext context, {
    required String title,
    required Widget child,
  }) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(title, style: Theme.of(context).textTheme.titleMedium),
        const SizedBox(height: 8),
        Container(
          width: double.infinity,
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            border: Border.all(
              color: Theme.of(context).colorScheme.outlineVariant,
            ),
            borderRadius: BorderRadius.circular(8),
          ),
          child: child,
        ),
      ],
    );
  }

  Widget _buildInfoRow(
    BuildContext context,
    IconData icon,
    String label,
    String value, {
    bool isMonospace = false,
  }) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        children: [
          Icon(
            icon,
            size: 18,
            color: Theme.of(context).colorScheme.onSurfaceVariant,
          ),
          const SizedBox(width: 8),
          SizedBox(
            width: 80,
            child: Text(
              label,
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ),
          ),
          Expanded(
            child: SelectableText(
              value.isEmpty ? '-' : value,
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                fontFamily: isMonospace ? 'monospace' : null,
              ),
            ),
          ),
        ],
      ),
    );
  }

  // ==================== 工具方法 ====================

  bool _isPreviewable() {
    final type = widget.file.type;
    return type == FileType.IMAGE ||
        type == FileType.VIDEO ||
        type == FileType.AUDIO ||
        type == FileType.DOCUMENT;
  }

  IconData _getFileIcon(FileType type) {
    switch (type) {
      case FileType.FOLDER:
        return Icons.folder;
      case FileType.IMAGE:
        return Icons.image;
      case FileType.VIDEO:
        return Icons.video_file;
      case FileType.AUDIO:
        return Icons.audio_file;
      case FileType.DOCUMENT:
        return Icons.description;
      case FileType.ARCHIVE:
        return Icons.archive;
      default:
        return Icons.insert_drive_file;
    }
  }

  Color _getFileColor(FileType type) {
    switch (type) {
      case FileType.FOLDER:
        return Colors.amber;
      case FileType.IMAGE:
        return Colors.green;
      case FileType.VIDEO:
        return Colors.red;
      case FileType.AUDIO:
        return Colors.purple;
      case FileType.DOCUMENT:
        return Colors.blue;
      case FileType.ARCHIVE:
        return Colors.orange;
      default:
        return Colors.grey;
    }
  }

  String _getFileTypeText(FileType type) {
    switch (type) {
      case FileType.FOLDER:
        return '文件夹';
      case FileType.IMAGE:
        return '图片';
      case FileType.VIDEO:
        return '视频';
      case FileType.AUDIO:
        return '音频';
      case FileType.DOCUMENT:
        return '文档';
      case FileType.ARCHIVE:
        return '压缩包';
      default:
        return '文件';
    }
  }

  IconData _getProviderIcon(StorageProviderType type) {
    switch (type) {
      case StorageProviderType.LOCAL:
        return Icons.computer;
      case StorageProviderType.TELEGRAM:
        return Icons.send;
      case StorageProviderType.DISCORD:
        return Icons.discord;
      case StorageProviderType.HUGGINGFACE:
        return Icons.cloud;
      case StorageProviderType.PAN123:
        return Icons.cloud;
      default:
        return Icons.cloud;
    }
  }

  String _getProviderStatusText(UploadProviderStatus status) {
    switch (status) {
      case UploadProviderStatus.PPENDING:
        return '待上传';
      case UploadProviderStatus.PUPLOADING:
        return '上传中';
      case UploadProviderStatus.PCOMPLETED:
        return '已完成';
      case UploadProviderStatus.PFAILED:
        return '失败';
      default:
        return '未知';
    }
  }

  String _formatFileSize(int bytes) {
    if (bytes <= 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    int unitIndex = 0;
    double size = bytes.toDouble();
    while (size >= 1024 && unitIndex < units.length - 1) {
      size /= 1024;
      unitIndex++;
    }
    return '${size.toStringAsFixed(size.truncate() == size ? 0 : 1)} ${units[unitIndex]}';
  }

  String _formatTimestamp(Timestamp? ts) {
    if (ts == null) return '-';
    final dt = DateTime.fromMillisecondsSinceEpoch(ts.seconds.toInt() * 1000);
    return DateFormat('yyyy-MM-dd HH:mm:ss').format(dt);
  }
}
