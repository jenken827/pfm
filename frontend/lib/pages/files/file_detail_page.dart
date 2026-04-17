import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:go_router/go_router.dart';
import 'package:intl/intl.dart';
import 'package:path_provider/path_provider.dart';
import 'package:pfm/api/fm_repository.dart';
import 'package:pfm/blocs/transfer_bloc/transfer_bloc.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/proto/native/provider.pb.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:protobuf/well_known_types/google/protobuf/timestamp.pb.dart';
import 'package:pfm/router/files_route.dart';
import 'package:pfm/services/logger.dart';
import 'package:pfm/utils/file_utils.dart';

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
                context.push(FilePreviewRoute().location, extra: widget.file);
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
            _buildTagsSection(context),
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

    // Check if there's at least one completed location
    final hasCompletedLocation = locations.any(
      (loc) => loc.status == LocationStatus.PCOMPLETED,
    );

    return _buildSection(
      context,
      title: Row(
        mainAxisAlignment: MainAxisAlignment.spaceBetween,
        children: [
          const Text('位置分布'),
          if (hasCompletedLocation)
            IconButton(
              icon: const Icon(Icons.sync),
              onPressed: () => _showSyncDialog(context),
              tooltip: '同步到其他存储',
            ),
        ],
      ),
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
    final isTelegram = loc.providerType == StorageProviderType.TELEGRAM;

    // Telegram 缓存状态
    final cacheStatusText = isTelegram
        ? _getCacheStatusText(loc.cacheStatus)
        : null;
    // 实际存储大小（可能是加密后）
    final storedSize = loc.size > 0 ? _formatFileSize(loc.size.toInt()) : null;

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
          // 实际大小
          if (storedSize != null) ...[
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
              decoration: BoxDecoration(
                color: Theme.of(context).colorScheme.surfaceContainerHighest,
                borderRadius: BorderRadius.circular(4),
              ),
              child: Text(
                storedSize,
                style: Theme.of(context).textTheme.labelSmall?.copyWith(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
            ),
            const SizedBox(width: 8),
          ],
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
          // Telegram 缓存状态
          if (cacheStatusText != null) ...[
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
              decoration: BoxDecoration(
                color: loc.cacheStatus == CacheStatus.CACHED
                    ? Colors.green.withValues(alpha: 0.2)
                    : Theme.of(context).colorScheme.secondaryContainer,
                borderRadius: BorderRadius.circular(4),
              ),
              child: Text(
                cacheStatusText,
                style: Theme.of(context).textTheme.labelSmall?.copyWith(
                  color: loc.cacheStatus == CacheStatus.CACHED
                      ? Colors.green
                      : Theme.of(context).colorScheme.onSecondaryContainer,
                ),
              ),
            ),
            const SizedBox(width: 8),
          ],
          // 状态
          Text(
            statusText,
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
              color: loc.status == LocationStatus.PCOMPLETED
                  ? Colors.green
                  : Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(width: 4),
          // 下载按钮（仅在状态不是失败时显示）
          if (loc.status == LocationStatus.PCOMPLETED)
            IconButton(
              icon: const Icon(Icons.download, size: 20),
              onPressed: () async {
                final downloadDir = await getDownloadsDirectory();
                if (!mounted) return;
                final fileName = widget.file.name.isNotEmpty
                    ? widget.file.name
                    : 'unnamed';
                final initialPath = '${downloadDir!.path}/$fileName';
                // 生成唯一文件路径，避免覆盖
                final savePath = await FileUtils.getUniqueFilePath(initialPath);
                if (!context.mounted) return;
                context.read<TransferBloc>().add(
                  TransferAddDownloadEvent(
                    fileId: widget.file.id,
                    filename: fileName,
                    fileSize: widget.file.size.toInt(),
                    savePath: savePath,
                    providerId: loc.providerId,
                  ),
                );
                final snackMsg = isTelegram && cacheStatusText != null
                    ? '已添加到下载队列（Telegram 大文件请耐心等待缓存）'
                    : '已开始下载: $fileName';
                ScaffoldMessenger.of(
                  context,
                ).showSnackBar(SnackBar(content: Text(snackMsg)));
              },
              tooltip: '下载',
            ),
        ],
      ),
    );
  }

  // ==================== 标签管理 ====================

  Widget _buildTagsSection(BuildContext context) {
    final tags = _detailData?.tags ?? [];

    return _buildSection(
      context,
      title: '标签',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (tags.isEmpty)
            Text(
              '暂无标签',
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            )
          else
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final tag in tags)
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 12,
                      vertical: 6,
                    ),
                    decoration: BoxDecoration(
                      color: Theme.of(context).colorScheme.primaryContainer,
                      borderRadius: BorderRadius.circular(16),
                    ),
                    child: Text(
                      tag,
                      style: Theme.of(context).textTheme.labelLarge?.copyWith(
                        color: Theme.of(context).colorScheme.onPrimaryContainer,
                      ),
                    ),
                  ),
              ],
            ),
          const SizedBox(height: 16),
          Align(
            alignment: Alignment.centerRight,
            child: FilledButton.tonal(
              onPressed: () => _showEditTagsDialog(context),
              child: const Text('编辑标签'),
            ),
          ),
        ],
      ),
    );
  }

  // 显示编辑标签对话框
  Future<void> _showEditTagsDialog(BuildContext context) async {
    final currentTags = List<String>.from(_detailData?.tags ?? []);
    final controller = TextEditingController(text: currentTags.join(','));

    final result = await showDialog<List<String>?>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('编辑标签'),
        content: TextField(
          controller: controller,
          decoration: const InputDecoration(
            hintText: '输入标签，用逗号分隔',
            border: OutlineInputBorder(),
          ),
          autofocus: true,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () {
              // 解析输入的标签
              final input = controller.text.trim();
              final newTags = input.isEmpty
                  ? <String>[]
                  : input
                        .split(',')
                        .map((tag) => tag.trim())
                        .where((tag) => tag.isNotEmpty)
                        .toList();

              Navigator.pop(context, newTags);
            },
            child: const Text('保存'),
          ),
        ],
      ),
    );

    if (result != null) {
      _updateFileTags(result);
    }
  }

  // 更新文件标签
  Future<void> _updateFileTags(List<String> newTags) async {
    final fileId = _detailData?.file?.id;
    if (fileId == null) return;

    setState(() {
      _isLoading = true;
    });

    try {
      final repo = FmRepository();
      final result = await repo.updateFileTags(fileId: fileId, tags: newTags);

      if (result.success) {
        // 刷新详情以获取最新的标签
        await _loadDetail();
        if (mounted) {
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(const SnackBar(content: Text('标签更新成功')));
        }
      } else {
        if (mounted) {
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(SnackBar(content: Text('标签更新失败: ${result.message}')));
        }
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('标签更新失败: $e')));
      }
    } finally {
      if (mounted) {
        setState(() {
          _isLoading = false;
        });
      }
    }
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
          const SizedBox(width: 8),
          // 预览按钮（仅对可预览的文件类型显示）
          if (_isHistoryPreviewable(h))
            IconButton(
              icon: const Icon(Icons.preview, size: 18),
              onPressed: () => _previewHistory(h),
              tooltip: '预览此版本',
              visualDensity: VisualDensity.compact,
            ),
          // 下载按钮
          IconButton(
            icon: const Icon(Icons.download, size: 18),
            onPressed: () => _downloadHistory(h),
            tooltip: '下载此版本',
            visualDensity: VisualDensity.compact,
          ),
          // 删除按钮
          IconButton(
            icon: Icon(
              Icons.delete_outline,
              size: 18,
              color: Theme.of(context).colorScheme.error,
            ),
            onPressed: () => _confirmDeleteHistory(h),
            tooltip: '删除此版本',
            visualDensity: VisualDensity.compact,
          ),
        ],
      ),
    );
  }

  // ==================== 通用组件 ====================

  Widget _buildSection(
    BuildContext context, {
    required dynamic title,
    required Widget child,
  }) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        title is String
            ? Text(title, style: Theme.of(context).textTheme.titleMedium)
            : title,
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

  /// 判断历史版本是否可预览
  bool _isHistoryPreviewable(FileHistory h) {
    // 获取文件类型
    final type = widget.file.type;
    return type == FileType.IMAGE ||
        type == FileType.VIDEO ||
        type == FileType.AUDIO ||
        type == FileType.DOCUMENT;
  }

  /// 预览文件历史版本
  Future<void> _previewHistory(FileHistory h) async {
    getLogger().debug("priview icon button pressed,preview file");
    try {
      // 跳转到预览页面，传递历史版本 ID
      final historyFile = File(
        id: 'history:${h.id}', // 使用特殊 ID 标识历史版本
        name: widget.file.name,
        size: h.size,
        hash: h.hash,
        type: widget.file.type,
        mime: widget.file.mime,
      );

      context.push(FilePreviewRoute().location, extra: historyFile);
    } catch (e) {
      getLogger().error('预览历史版本失败: $e');
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('预览失败: $e')));
    }
  }

  /// 下载文件历史版本
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

  String _getProviderStatusText(LocationStatus status) {
    switch (status) {
      case LocationStatus.PPENDING:
        return '待上传';
      case LocationStatus.PUPLOADING:
        return '上传中';
      case LocationStatus.PCOMPLETED:
        return '已完成';
      case LocationStatus.PFAILED:
        return '失败';
      default:
        return '未知';
    }
  }

  String _getCacheStatusText(CacheStatus status) {
    switch (status) {
      case CacheStatus.UNCACHED:
        return '未缓存';
      case CacheStatus.CACHING:
        return '缓存中';
      case CacheStatus.CACHED:
        return '已缓存';
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

  /// 下载文件历史版本
  Future<void> _downloadHistory(FileHistory h) async {
    try {
      final downloadDir = await getDownloadsDirectory();
      if (!mounted) return;

      // 获取文件名
      String fileName = 'history_file';
      if (h.fileId.isNotEmpty) {
        // 尝试从文件信息获取文件名
        final file = _detailData?.file;
        if (file != null && file.name.isNotEmpty) {
          fileName = file.name;
        }
      }

      // 分离文件名和扩展名，正确插入历史版本标记
      final lastDotIndex = fileName.lastIndexOf('.');
      String baseName;
      String extension = '';

      if (lastDotIndex > 0) {
        baseName = fileName.substring(0, lastDotIndex);
        extension = fileName.substring(lastDotIndex); // 包含点号
      } else {
        baseName = fileName;
      }

      final historyFileName = '${baseName}_history_${h.id}$extension';
      final initialPath = '${downloadDir!.path}/$historyFileName';

      // 生成唯一文件路径，避免覆盖
      final savePath = await FileUtils.getUniqueFilePath(initialPath);

      if (!mounted) return;

      // 使用特殊的 fileId 格式：history:{historyId}
      final historyFileId = 'history:${h.id}';

      context.read<TransferBloc>().add(
        TransferAddDownloadEvent(
          fileId: historyFileId,
          filename: historyFileName,
          fileSize: h.size.toInt(),
          savePath: savePath,
          fileHash: h.hash,
        ),
      );

      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('已添加到下载队列')));
    } catch (e) {
      getLogger().error('下载历史版本失败: $e');
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('下载失败: $e')));
    }
  }

  /// 确认删除文件历史版本
  Future<void> _confirmDeleteHistory(FileHistory h) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('确认删除'),
        content: Text(
          '确定要删除这个历史版本吗？\n'
          '版本：v${_formatTimestamp(h.versionNumber)}\n'
          '大小：${_formatFileSize(h.size.toInt())}\n\n'
          '此操作不可撤销！',
        ),
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

    if (confirmed == true) {
      await _deleteHistory(h.id.toInt());
    }
  }

  /// 删除文件历史版本
  Future<void> _deleteHistory(int historyId) async {
    try {
      setState(() => _isLoading = true);

      final repo = FmRepository();
      final result = await repo.deleteFileHistory([historyId]);

      setState(() => _isLoading = false);

      if (!mounted) return;

      if (result.success) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('删除成功')));
        // 刷新文件详情
        await _loadDetail();
      } else {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(result.message ?? '删除失败')));
      }
    } catch (e) {
      getLogger().error('删除历史版本失败: $e');
      setState(() => _isLoading = false);
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('删除失败: $e')));
    }
  }

  /// Show sync dialog to select target providers
  Future<void> _showSyncDialog(BuildContext context) async {
    final repo = FmRepository();

    // Get file hash from current file
    final fileHash = _detailData?.file.hash;
    if (fileHash == null || fileHash.isEmpty) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('文件哈希不存在，无法同步')));
      return;
    }

    // Fetch available providers and provider types in parallel
    setState(() => _isLoading = true);

    final results = await Future.wait([
      repo.getAvailableProvidersForSync(fileHash),
      repo.getStorageProviderTypes(),
    ]);

    setState(() => _isLoading = false);

    if (!mounted) return;

    final providersResult = results[0] as ApiResult<List<StorageProvider>>;
    final typesResult =
        results[1] as ApiResult<GetStorageProviderTypesRespData>;

    if (!providersResult.success || providersResult.data == null) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(providersResult.message ?? '获取可用提供者失败')),
        );
      }
      return;
    }

    final availableProviders = providersResult.data!;
    final providerTypes = typesResult.data?.types.toList() ?? [];

    if (availableProviders.isEmpty) {
      if (context.mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('没有可用的存储提供者')));
      }
      return;
    }

    // Show multi-select dialog
    if (!context.mounted) return;

    final selectedProviderIds = await showDialog<List<int>>(
      context: context,
      builder: (context) => _SyncProviderSelectionDialog(
        providers: availableProviders,
        providerTypes: providerTypes,
      ),
    );

    if (selectedProviderIds == null || selectedProviderIds.isEmpty) {
      return; // User cancelled or no selection
    }

    // Trigger sync
    setState(() => _isLoading = true);
    final syncResult = await repo.syncFileToProviders(
      widget.file.id,
      selectedProviderIds,
    );
    setState(() => _isLoading = false);

    if (!context.mounted) return;

    if (syncResult.success) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('已创建 ${selectedProviderIds.length} 个同步任务')),
      );
      // Refresh detail data
      await _loadDetail();
    } else {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(syncResult.message ?? '同步失败')));
    }
  }
}

/// Dialog for selecting target storage providers for sync
class _SyncProviderSelectionDialog extends StatefulWidget {
  const _SyncProviderSelectionDialog({
    required this.providers,
    this.providerTypes = const [],
  });

  final List<StorageProvider> providers;
  final List<StorageProviderTypeInfo> providerTypes;

  @override
  State<_SyncProviderSelectionDialog> createState() =>
      _SyncProviderSelectionDialogState();
}

class _SyncProviderSelectionDialogState
    extends State<_SyncProviderSelectionDialog> {
  final Set<int> _selectedProviderIds = {};

  /// Get icon from icon name string
  IconData _typeIcon(String iconName) {
    final iconMap = {
      'computer': Icons.computer,
      'storage': Icons.storage,
      'telegram': Icons.telegram,
      'discord': Icons.discord,
      'cloud': Icons.cloud,
      'send': Icons.send,
    };
    return iconMap[iconName] ?? Icons.storage;
  }

  /// Get provider type display name
  String _getTypeName(StorageProviderType type) {
    final typeInfo = widget.providerTypes
        .where((t) => t.type == type)
        .firstOrNull;
    return typeInfo?.displayName.isNotEmpty == true
        ? typeInfo!.displayName
        : '未知类型';
  }

  /// Get provider icon name
  String _getIconName(StorageProviderType type) {
    final typeInfo = widget.providerTypes
        .where((t) => t.type == type)
        .firstOrNull;
    return typeInfo?.iconName.isNotEmpty == true
        ? typeInfo!.iconName
        : 'storage';
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('选择同步目标'),
      content: SizedBox(
        width: double.maxFinite,
        child: ListView.builder(
          shrinkWrap: true,
          itemCount: widget.providers.length,
          itemBuilder: (context, index) {
            final provider = widget.providers[index];
            return CheckboxListTile(
              title: Text(provider.name),
              subtitle: Text(_getTypeName(provider.providerType)),
              secondary: Icon(_typeIcon(_getIconName(provider.providerType))),
              value: _selectedProviderIds.contains(provider.id),
              onChanged: (value) {
                setState(() {
                  if (value == true) {
                    _selectedProviderIds.add(provider.id);
                  } else {
                    _selectedProviderIds.remove(provider.id);
                  }
                });
              },
            );
          },
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('取消'),
        ),
        FilledButton(
          onPressed: _selectedProviderIds.isEmpty
              ? null
              : () => Navigator.pop(context, _selectedProviderIds.toList()),
          child: const Text('确认'),
        ),
      ],
    );
  }
}
