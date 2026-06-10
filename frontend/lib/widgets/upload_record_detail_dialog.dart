import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/proto/native/upload.pb.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/utils/format.dart';

/// 上传记录详情对话框
class UploadRecordDetailDialog extends StatefulWidget {
  final int uploadId;

  const UploadRecordDetailDialog({super.key, required this.uploadId});

  @override
  State<UploadRecordDetailDialog> createState() =>
      _UploadRecordDetailDialogState();
}

class _UploadRecordDetailDialogState extends State<UploadRecordDetailDialog> {
  UploadRecordInfo? _record;
  List<FileLocationInfo> _locations = [];
  bool _isLoading = true;
  String? _errorMessage;
  bool _loaded = false;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (!_loaded) {
      _loaded = true;
      _loadDetail();
    }
  }

  Future<void> _loadDetail() async {
    final al = AppLocalizations.of(context)!;
    try {
      final repository = context.read<FmRepository>();
      final result = await repository.getUploadRecordDetail(widget.uploadId);

      if (result.success) {
        setState(() {
          _record = result.data!.record;
          _locations = result.data!.locations;
          _isLoading = false;
        });
      } else {
        setState(() {
          _errorMessage = result.message ?? al.uploadRecord_unknownError;
          _isLoading = false;
        });
      }
    } catch (e) {
      setState(() {
        _errorMessage = e.toString();
        _isLoading = false;
      });
    }
  }

  String _getStatusText(LocationStatus status) {
    final al = AppLocalizations.of(context)!;
    switch (status) {
      case LocationStatus.PPENDING:
        return al.uploadRecord_statusWaiting;
      case LocationStatus.PUPLOADING:
        return al.uploadRecord_statusUploading;
      case LocationStatus.PCOMPLETED:
        return al.uploadRecord_statusCompleted;
      case LocationStatus.PFAILED:
        return al.uploadRecord_statusFailed;
      default:
        return al.uploadRecord_statusUnknown;
    }
  }

  Color _getStatusColor(LocationStatus status) {
    switch (status) {
      case LocationStatus.PPENDING:
        return Theme.of(context).colorScheme.onSurfaceVariant;
      case LocationStatus.PUPLOADING:
        return Colors.blue;
      case LocationStatus.PCOMPLETED:
        return Colors.green;
      case LocationStatus.PFAILED:
        return Colors.red;
      default:
        return Theme.of(context).colorScheme.onSurfaceVariant;
    }
  }

  String _getCacheStatusText(CacheStatus status) {
    final al = AppLocalizations.of(context)!;
    switch (status) {
      case CacheStatus.CACHE_STATUS_UNSPECIFIED:
      case CacheStatus.UNCACHED:
        return al.uploadRecord_cacheNotCached;
      case CacheStatus.CACHING:
        return al.uploadRecord_cacheCaching;
      case CacheStatus.CACHED:
        return al.uploadRecord_cacheCached;
      default:
        return al.uploadRecord_statusUnknown;
    }
  }

  String _getTaskTypeText(UploadTaskType taskType) {
    switch (taskType) {
      case UploadTaskType.UPLOAD:
        return '普通上传';
      case UploadTaskType.SYNC:
        return '同步任务';
      default:
        return '未知';
    }
  }

  Color _getCacheStatusColor(CacheStatus status) {
    switch (status) {
      case CacheStatus.UNCACHED:
        return Colors.orange;
      case CacheStatus.CACHING:
        return Colors.blue;
      case CacheStatus.CACHED:
        return Colors.green;
      default:
        return Theme.of(context).colorScheme.onSurfaceVariant;
    }
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('上传详情'),
      content: _buildContent(),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('关闭'),
        ),
      ],
    );
  }

  Widget _buildContent() {
    if (_isLoading) {
      return const SizedBox(
        width: 300,
        height: 200,
        child: Center(child: CircularProgressIndicator()),
      );
    }

    if (_errorMessage != null) {
      return SizedBox(width: 300, child: Text('加载失败: $_errorMessage'));
    }

    if (_record == null) {
      return const SizedBox(width: 300, child: Text('未找到记录'));
    }

    return SizedBox(
      width: 500,
      height: 400,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _buildRecordInfo(),
          const Divider(),
          const Text('存储提供者状态：', style: TextStyle(fontWeight: FontWeight.bold)),
          const SizedBox(height: 8),
          Expanded(
            child: ListView.builder(
              itemCount: _locations.length,
              itemBuilder: (context, index) {
                return _buildLocationTile(_locations[index]);
              },
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildRecordInfo() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('文件名：${_record!.fileName}'),
        Text('文件大小：${formatCapacityString(_record!.fileSize.toInt())}'),
        Text('文件哈希：${_record!.fileHash}'),
        Text('上传类型：${_getTaskTypeText(_record!.taskType)}'),
        Text('上传时间：${_record!.createdAt.toDateTime().toLocal()}'),
        // 显示错误信息
        if (_record!.message.isNotEmpty) ...[
          const SizedBox(height: 8),
          Container(
            width: double.infinity,
            padding: const EdgeInsets.all(8),
            decoration: BoxDecoration(
              color: Theme.of(context).colorScheme.errorContainer,
              borderRadius: BorderRadius.circular(8),
            ),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Icon(
                  Icons.error_outline,
                  size: 16,
                  color: Theme.of(context).colorScheme.onErrorContainer,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    '错误信息：${_record!.message}',
                    style: TextStyle(
                      fontSize: 12,
                      color: Theme.of(context).colorScheme.onErrorContainer,
                    ),
                  ),
                ),
              ],
            ),
          ),
        ],
      ],
    );
  }

  Widget _buildLocationTile(FileLocationInfo location) {
    final colorScheme = Theme.of(context).colorScheme;
    return Card(
      child: ListTile(
        title: Text(location.providerName, style: TextStyle(color: colorScheme.onSurface)),
        subtitle: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 8,
                    vertical: 2,
                  ),
                  decoration: BoxDecoration(
                    color: _getStatusColor(
                      location.status,
                    ).withValues(alpha: 0.1),
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: Text(
                    _getStatusText(location.status),
                    style: TextStyle(
                      color: _getStatusColor(location.status),
                      fontSize: 12,
                    ),
                  ),
                ),
                if (location.progress.toInt() > 0) ...[
                  const SizedBox(width: 8),
                  Text('进度: ${location.progress.toInt()}%', style: TextStyle(color: colorScheme.onSurfaceVariant)),
                ],
              ],
            ),
            if (location.cacheStatus !=
                CacheStatus.CACHE_STATUS_UNSPECIFIED) ...[
              const SizedBox(height: 4),
              Row(
                children: [
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 8,
                      vertical: 2,
                    ),
                    decoration: BoxDecoration(
                      color: _getCacheStatusColor(
                        location.cacheStatus,
                      ).withValues(alpha: 0.1),
                      borderRadius: BorderRadius.circular(12),
                    ),
                    child: Text(
                      '缓存: ${_getCacheStatusText(location.cacheStatus)}',
                      style: TextStyle(
                        color: _getCacheStatusColor(location.cacheStatus),
                        fontSize: 12,
                      ),
                    ),
                  ),
                ],
              ),
            ],
            if (location.message.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text(
                  '消息: ${location.message}',
                  style: TextStyle(fontSize: 12, color: Theme.of(context).colorScheme.onSurfaceVariant),
                ),
              ),
          ],
        ),
        trailing: location.encrypted == true
            ? Icon(Icons.lock, size: 16, color: Theme.of(context).colorScheme.onSurfaceVariant)
            : null,
      ),
    );
  }
}
