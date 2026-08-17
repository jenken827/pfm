import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
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
import 'package:pfm/l10n/app_localizations.dart';

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
    final al = AppLocalizations.of(context);
    return Scaffold(
      appBar: AppBar(
        title: Text(al.detail_fileDetail),
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
    final al = AppLocalizations.of(context);
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
          Text(
            al.common_loadingFailed,
            style: Theme.of(context).textTheme.titleMedium,
          ),
          const SizedBox(height: 8),
          Text(
            _errorMessage ?? '',
            style: Theme.of(context).textTheme.bodySmall,
          ),
          const SizedBox(height: 16),
          FilledButton(onPressed: _loadDetail, child: Text(al.common_retry)),
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
    final al = AppLocalizations.of(context);
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
                  _getFileTypeText(file.type, al),
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
    final al = AppLocalizations.of(context);
    return _buildSection(
      context,
      title: al.detail_basicInfo,
      child: Column(
        children: [
          _buildInfoRow(
            context,
            Icons.sd_storage,
            al.detail_fileSize,
            _formatFileSize(file.size.toInt()),
          ),
          _buildInfoRow(
            context,
            Icons.tag,
            al.detail_fileHash,
            file.hash,
            isMonospace: true,
          ),
          _buildInfoRow(
            context,
            Icons.insert_drive_file,
            al.detail_mimeType,
            file.mime,
          ),
          _buildInfoRow(
            context,
            Icons.schedule,
            al.detail_createdTime,
            _formatTimestamp(file.createdAt),
          ),
          _buildInfoRow(
            context,
            Icons.update,
            al.detail_modifiedTime,
            _formatTimestamp(file.updatedAt),
          ),
        ],
      ),
    );
  }

  // ==================== 位置分布 ====================

  Widget _buildLocationDistribution(BuildContext context) {
    final al = AppLocalizations.of(context);
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
          Text(al.detail_locationDistribution),
          if (hasCompletedLocation)
            IconButton(
              icon: const Icon(Icons.sync),
              onPressed: () => _showSyncDialog(context),
              tooltip: al.detail_syncToOther,
            ),
        ],
      ),
      child: locations.isEmpty
          ? Center(
              child: Text(
                al.detail_noLocationInfo,
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
    final al = AppLocalizations.of(context);
    final providerName = loc.providerName;
    final encrypted = loc.encrypted;
    final isChunked = loc.isChunked;
    final chunkTotal = loc.chunkTotal;
    final statusText = _getProviderStatusText(loc.status, al);
    final isTelegram = loc.providerType == StorageProviderType.TELEGRAM;

    // Telegram 缓存状态
    final cacheStatusText = isTelegram
        ? _getCacheStatusText(loc.cacheStatus, al)
        : null;
    // 实际存储大小（可能是加密后）
    final storedSize = loc.size > 0 ? _formatFileSize(loc.size.toInt()) : null;

    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // 第一行：图标 + 名称 + 状态 + 下载按钮
          Row(
            children: [
              Icon(_getProviderIcon(loc.providerType), size: 20),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  providerName,
                  style: Theme.of(context).textTheme.bodyMedium,
                ),
              ),
              Text(
                statusText,
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                  color: loc.status == LocationStatus.PCOMPLETED
                      ? Colors.green
                      : Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
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
                    final savePath = await FileUtils.getUniqueFilePath(
                      initialPath,
                    );
                    if (!context.mounted) return;
                    context.read<TransferBloc>().add(
                      TransferAddDownloadEvent(
                        fileId: widget.file.id,
                        filename: fileName,
                        fileSize: widget.file.size.toInt(),
                        savePath: savePath,
                        fileHash: widget.file.hash,
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
                  tooltip: al.detail_download,
                ),
            ],
          ),
          const SizedBox(height: 4),
          // 第二行：标签自动换行
          Wrap(
            spacing: 8,
            runSpacing: 4,
            children: [
              if (storedSize != null) _buildBadge(context, storedSize),
              _buildBadge(
                context,
                encrypted ? al.detail_encrypted : al.detail_notEncrypted,
                isError: encrypted,
              ),
              _buildBadge(
                context,
                isChunked ? al.detail_chunks(chunkTotal) : al.detail_singleFile,
                isTertiary: isChunked,
              ),
              if (cacheStatusText != null)
                _buildBadge(
                  context,
                  cacheStatusText,
                  isGreen: loc.cacheStatus == CacheStatus.CACHED,
                ),
            ],
          ),
          // 第三行：存储路径（可复制）
          if (loc.storagePath.isNotEmpty) ...[
            const SizedBox(height: 4),
            Row(
              children: [
                Icon(
                  Icons.folder_open,
                  size: 14,
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
                const SizedBox(width: 4),
                Expanded(
                  child: Text(
                    loc.storagePath,
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: Theme.of(context).colorScheme.onSurfaceVariant,
                      fontFamily: 'monospace',
                    ),
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                InkWell(
                  onTap: () {
                    Clipboard.setData(ClipboardData(text: loc.storagePath));
                    ScaffoldMessenger.of(context).showSnackBar(
                      const SnackBar(
                        content: Text('已复制存储路径'),
                        duration: Duration(seconds: 1),
                      ),
                    );
                  },
                  child: Icon(
                    Icons.copy,
                    size: 14,
                    color: Theme.of(context).colorScheme.primary,
                  ),
                ),
              ],
            ),
          ],
        ],
      ),
    );
  }

  // ==================== 标签管理 ====================

  /// 构建标签 badge
  Widget _buildBadge(
    BuildContext context,
    String text, {
    bool isError = false,
    bool isTertiary = false,
    bool isGreen = false,
  }) {
    final colorScheme = Theme.of(context).colorScheme;
    Color bgColor;
    Color fgColor;
    if (isError) {
      bgColor = colorScheme.errorContainer;
      fgColor = colorScheme.onErrorContainer;
    } else if (isTertiary) {
      bgColor = colorScheme.tertiaryContainer;
      fgColor = colorScheme.onTertiaryContainer;
    } else if (isGreen) {
      bgColor = Colors.green.withValues(alpha: 0.2);
      fgColor = Colors.green;
    } else {
      bgColor = colorScheme.surfaceContainerHighest;
      fgColor = colorScheme.onSurfaceVariant;
    }
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: BoxDecoration(
        color: bgColor,
        borderRadius: BorderRadius.circular(4),
      ),
      child: Text(
        text,
        style: Theme.of(context).textTheme.labelSmall?.copyWith(color: fgColor),
      ),
    );
  }

  Widget _buildTagsSection(BuildContext context) {
    final al = AppLocalizations.of(context);
    final tags = _detailData?.tags ?? [];

    return _buildSection(
      context,
      title: al.detail_tags,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (tags.isEmpty)
            Text(
              al.detail_noTags,
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
              child: Text(al.detail_editTags),
            ),
          ),
        ],
      ),
    );
  }

  // 显示编辑标签对话框
  Future<void> _showEditTagsDialog(BuildContext context) async {
    final al = AppLocalizations.of(context);
    final currentTags = List<String>.from(_detailData?.tags ?? []);
    final controller = TextEditingController(text: currentTags.join(','));

    final result = await showDialog<List<String>?>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(al.detail_editTags),
        content: TextField(
          controller: controller,
          decoration: InputDecoration(
            hintText: al.detail_tagInputHint,
            border: const OutlineInputBorder(),
          ),
          autofocus: true,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: Text(al.common_cancel),
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
            child: Text(al.common_save),
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
    final al = AppLocalizations.of(context);
    final fileId = _detailData?.file.id;
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
          ).showSnackBar(SnackBar(content: Text(al.detail_tagUpdateSuccess)));
        }
      } else {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text('${al.detail_tagUpdateFailed}: ${result.message}'),
            ),
          );
        }
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('${al.detail_tagUpdateFailed}: $e')),
        );
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
    final al = AppLocalizations.of(context);
    final histories = _detailData?.histories ?? [];

    return _buildSection(
      context,
      title: al.detail_fileHistory,
      child: histories.isEmpty
          ? Center(
              child: Text(
                al.detail_noHistory,
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
    final al = AppLocalizations.of(context);
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
                  al.detail_version('${h.versionNumber}'),
                  style: Theme.of(context).textTheme.bodyMedium,
                ),
                Text(
                  al.detail_versionSize(_formatFileSize(h.size.toInt())),
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
              tooltip: al.detail_previewVersion,
              visualDensity: VisualDensity.compact,
            ),
          // 下载按钮
          IconButton(
            icon: const Icon(Icons.download, size: 18),
            onPressed: () => _downloadHistory(h),
            tooltip: al.detail_downloadVersion,
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
            tooltip: al.detail_deleteVersion,
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
    final al = AppLocalizations.of(context);
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
      ).showSnackBar(SnackBar(content: Text('${al.detail_previewFailed}: $e')));
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

  String _getFileTypeText(FileType type, AppLocalizations al) {
    switch (type) {
      case FileType.FOLDER:
        return al.detail_folder;
      case FileType.IMAGE:
        return al.detail_image;
      case FileType.VIDEO:
        return al.detail_video;
      case FileType.AUDIO:
        return al.detail_audio;
      case FileType.DOCUMENT:
        return al.detail_document;
      case FileType.ARCHIVE:
        return al.detail_archive;
      default:
        return al.detail_file;
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
      case StorageProviderType.BAIDU_NETDISK:
        return Icons.cloud_circle;
      default:
        return Icons.cloud;
    }
  }

  String _getProviderStatusText(LocationStatus status, AppLocalizations al) {
    switch (status) {
      case LocationStatus.PPENDING:
        return al.detail_statusPending;
      case LocationStatus.PUPLOADING:
        return al.detail_statusUploading;
      case LocationStatus.PCOMPLETED:
        return al.detail_statusCompleted;
      case LocationStatus.PFAILED:
        return al.detail_statusFailed;
      default:
        return al.detail_statusUnknown;
    }
  }

  String _getCacheStatusText(CacheStatus status, AppLocalizations al) {
    switch (status) {
      case CacheStatus.CACHE_STATUS_UNSPECIFIED:
      case CacheStatus.UNCACHED:
        return al.detail_cacheNotCached;
      case CacheStatus.CACHING:
        return al.detail_cacheCaching;
      case CacheStatus.CACHED:
        return al.detail_cacheCached;
      default:
        return al.detail_statusUnknown;
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
    final al = AppLocalizations.of(context);
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
      ).showSnackBar(SnackBar(content: Text(al.detail_downloadQueued)));
    } catch (e) {
      getLogger().error('下载历史版本失败: $e');
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('${al.detail_downloadFailed}: $e')),
      );
    }
  }

  /// 确认删除文件历史版本
  Future<void> _confirmDeleteHistory(FileHistory h) async {
    final al = AppLocalizations.of(context);
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(al.detail_confirmDeleteVersion),
        content: Text(
          '${al.detail_confirmDeleteVersionMsg}\n'
          '${al.detail_version('${h.versionNumber}')}\n'
          '${al.detail_versionSize(_formatFileSize(h.size.toInt()))}\n\n'
          '${al.detail_cannotUndo}',
        ),
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
            child: Text(al.common_delete),
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
    final al = AppLocalizations.of(context);
    try {
      setState(() => _isLoading = true);

      final repo = FmRepository();
      final result = await repo.deleteFileHistory([historyId]);

      setState(() => _isLoading = false);

      if (!mounted) return;

      if (result.success) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(al.detail_deleteSuccess)));
        // 刷新文件详情
        await _loadDetail();
      } else {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(result.message ?? al.detail_deleteFailed)),
        );
      }
    } catch (e) {
      getLogger().error('删除历史版本失败: $e');
      setState(() => _isLoading = false);
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('${al.detail_deleteFailed}: $e')));
    }
  }

  /// Show sync dialog to select target providers
  Future<void> _showSyncDialog(BuildContext context) async {
    final al = AppLocalizations.of(context);
    final repo = FmRepository();

    // Get file hash from current file
    final fileHash = _detailData?.file.hash;
    if (fileHash == null || fileHash.isEmpty) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(al.detail_hashNotExists)));
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
          SnackBar(
            content: Text(
              providersResult.message ?? al.detail_getProvidersFailed,
            ),
          ),
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
        ).showSnackBar(SnackBar(content: Text(al.detail_noAvailableProviders)));
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
        SnackBar(
          content: Text(al.detail_syncTasksCreated(selectedProviderIds.length)),
        ),
      );
      // Refresh detail data
      await _loadDetail();
    } else {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(syncResult.message ?? al.detail_syncFailed)),
      );
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
    final al = AppLocalizations.of(context);
    return AlertDialog(
      title: Text(al.detail_selectSyncTarget),
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
          child: Text(al.common_cancel),
        ),
        FilledButton(
          onPressed: _selectedProviderIds.isEmpty
              ? null
              : () => Navigator.pop(context, _selectedProviderIds.toList()),
          child: Text(al.common_confirm),
        ),
      ],
    );
  }
}
