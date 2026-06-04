import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/proto/native/upload.pb.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/utils/format.dart';
import 'package:pfm/widgets/upload_record_detail_dialog.dart';

/// 上传记录列表
class UploadRecordList extends StatefulWidget {
  const UploadRecordList({super.key, this.onRefreshRegistered});

  /// 注册刷新回调，供父页面调用
  final ValueChanged<VoidCallback>? onRefreshRegistered;

  @override
  State<UploadRecordList> createState() => _UploadRecordListState();
}

class _UploadRecordListState extends State<UploadRecordList> {
  List<UploadRecordInfo> _records = [];
  int _total = 0;
  int _failedCount = 0;
  int _page = 1;
  final int _pageSize = 20;
  bool _isLoading = false;
  bool _hasMore = true;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    _loadRecords();
    // 注册刷新回调
    WidgetsBinding.instance.addPostFrameCallback((_) {
      widget.onRefreshRegistered?.call(refresh);
    });
  }

  /// 公开的刷新方法，供外部调用
  void refresh() {
    _page = 1;
    _hasMore = true;
    _loadRecords(reset: true);
  }

  Future<void> _loadRecords({bool reset = false}) async {
    if (_isLoading) return;
    if (!reset && !_hasMore) return;

    if (mounted) {
      setState(() {
        _isLoading = true;
        _errorMessage = null;
        if (reset) {
          _page = 1;
          _hasMore = true;
        }
      });
    }

    try {
      final repository = context.read<FmRepository>();
      final result = await repository.listUploadRecords(
        page: _page,
        pageSize: _pageSize,
      );

      if (!mounted) return;

      if (result.success) {
        final newRecords = result.data!.records
            .where((r) => r.status != LocationStatus.PCOMPLETED)
            .toList();
        setState(() {
          if (reset || _page == 1) {
            _records = newRecords;
          } else {
            _records.addAll(newRecords);
          }
          // 服务端返回的全局统计
          if (result.data!.hasTotal()) {
            _total = result.data!.total;
          }
          if (result.data!.hasFailedCount()) {
            _failedCount = result.data!.failedCount;
          }
          _hasMore = newRecords.length >= _pageSize;
          _isLoading = false;
        });
      } else {
        setState(() {
          _errorMessage = result.message ?? '未知错误';
          _isLoading = false;
        });
      }
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _errorMessage = e.toString();
        _isLoading = false;
      });
    }
  }

  /// 加载下一页
  void _loadMore() {
    if (_isLoading || !_hasMore) return;
    _page++;
    _loadRecords();
  }

  /// 解析 JSON 数组并返回提供者数量
  String _getProviderCount(AppLocalizations al, String providerIds) {
    if (providerIds.isEmpty) return al.common_count(0);

    try {
      final ids = jsonDecode(providerIds) as List;
      return al.common_count(ids.length);
    } catch (e) {
      return al.common_count(0);
    }
  }

  String _getStatusText(AppLocalizations al, LocationStatus status) {
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
        return Colors.grey;
      case LocationStatus.PUPLOADING:
        return Colors.blue;
      case LocationStatus.PCOMPLETED:
        return Colors.green;
      case LocationStatus.PFAILED:
        return Colors.red;
      default:
        return Colors.grey;
    }
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context)!;

    if (_isLoading) {
      return const Center(child: CircularProgressIndicator());
    }

    if (_errorMessage != null) {
      return Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Text('${al.uploadRecord_loadFailed}: $_errorMessage'),
            const SizedBox(height: 16),
            ElevatedButton(onPressed: _loadRecords, child: Text(al.common_retry)),
          ],
        ),
      );
    }

    if (_records.isEmpty) {
      return Center(child: Text(al.uploadRecord_noRecords));
    }

    return Column(
      children: [
        // 批量操作菜单
        _buildActionBar(al),
        // 提示信息
        Container(
          width: double.infinity,
          padding: const EdgeInsets.all(12),
          color: Theme.of(context).colorScheme.surfaceContainerHighest,
          child: Row(
            children: [
              Icon(
                Icons.info_outline,
                size: 16,
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  al.uploadRecord_recordsHint,
                  style: TextStyle(
                    fontSize: 12,
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                ),
              ),
            ],
          ),
        ),
        // 列表
        Expanded(
          child: RefreshIndicator(
            onRefresh: () async => refresh(),
            child: ListView.builder(
              itemCount: _records.length + (_hasMore ? 1 : 0),
              itemBuilder: (context, index) {
                // 底部加载更多指示器
                if (index == _records.length) {
                  _loadMore();
                  return const Padding(
                    padding: EdgeInsets.all(16),
                    child: Center(child: CircularProgressIndicator()),
                  );
                }
                final record = _records[index];
                return _buildRecordTile(al, record);
              },
            ),
          ),
        ),
      ],
    );
  }

  Widget _buildActionBar(AppLocalizations al) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surfaceContainerLow,
        border: Border(
          bottom: BorderSide(
            color: Theme.of(context).colorScheme.outlineVariant,
          ),
        ),
      ),
      child: Row(
        children: [
          // 统计信息
          Text(
            al.uploadRecord_total(_total),
            style: TextStyle(
              fontSize: 12,
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
          if (_failedCount > 0) ...[
            const SizedBox(width: 8),
            Text(
              al.uploadRecord_failedCount(_failedCount),
              style: TextStyle(
                fontSize: 12,
                color: Theme.of(context).colorScheme.error,
              ),
            ),
          ],
          const Spacer(),
          // 重试失败按钮
          if (_failedCount > 0)
            TextButton.icon(
              onPressed: _retryAllFailed,
              icon: const Icon(Icons.refresh, size: 16),
              label: Text(al.uploadRecord_retryFailed),
              style: TextButton.styleFrom(
                padding: const EdgeInsets.symmetric(horizontal: 8),
                minimumSize: Size.zero,
                tapTargetSize: MaterialTapTargetSize.shrinkWrap,
              ),
            ),
        ],
      ),
    );
  }

  Future<void> _retryAllFailed() async {
    final al = AppLocalizations.of(context)!;
    if (_failedCount == 0) return;

    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(al.uploadRecord_retryFailedTitle),
        content: Text(al.uploadRecord_retryFailedConfirm(_failedCount)),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: Text(al.common_cancel),
          ),
          ElevatedButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: Text(al.uploadRecord_retry),
          ),
        ],
      ),
    );

    if (confirmed != true || !mounted) return;

    final repository = context.read<FmRepository>();
    final result = await repository.retryAllFailedUploadRecords();

    if (mounted) {
      if (result.success) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(al.uploadRecord_retrySubmitted)),
        );
      } else {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('${al.uploadRecord_retryFailedMsg}: ${result.message}')),
        );
      }
      refresh();
    }
  }

  Widget _buildRecordTile(AppLocalizations al, UploadRecordInfo record) {
    return ListTile(
      leading: Icon(
        Icons.insert_drive_file,
        color: Theme.of(context).primaryColor,
      ),
      title: Text(record.fileName),
      subtitle: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(formatCapacityString(record.fileSize.toInt())),
          const SizedBox(height: 4),
          Row(
            children: [
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                decoration: BoxDecoration(
                  color: _getStatusColor(record.status).withValues(alpha: 0.1),
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Text(
                  _getStatusText(al, record.status),
                  style: TextStyle(
                    color: _getStatusColor(record.status),
                    fontSize: 12,
                  ),
                ),
              ),
              const SizedBox(width: 8),
              Text(
                al.uploadRecord_target(_getProviderCount(al, record.targetProviderIds)),
                style: const TextStyle(fontSize: 12),
              ),
            ],
          ),
        ],
      ),
      trailing: PopupMenuButton<String>(
        icon: const Icon(Icons.more_vert),
        onSelected: (value) => _handleMenuAction(value, record),
        itemBuilder: (context) => [
          PopupMenuItem(
            value: 'detail',
            child: ListTile(
              leading: const Icon(Icons.info_outline),
              title: Text(al.uploadRecord_viewDetail),
            ),
          ),
          if (record.status == LocationStatus.PFAILED)
            PopupMenuItem(
              value: 'retry',
              child: ListTile(leading: const Icon(Icons.refresh), title: Text(al.uploadRecord_retry)),
            ),
          PopupMenuItem(
            value: 'delete',
            child: ListTile(leading: const Icon(Icons.delete), title: Text(al.uploadRecord_delete)),
          ),
        ],
      ),
    );
  }

  void _handleMenuAction(String action, UploadRecordInfo record) async {
    switch (action) {
      case 'detail':
        _showDetailDialog(record);
        break;
      case 'retry':
        await _retryRecord(record);
        break;
      case 'delete':
        await _deleteRecord(record);
        break;
    }
  }

  Future<void> _retryRecord(UploadRecordInfo record) async {
    final al = AppLocalizations.of(context)!;
    try {
      final repository = context.read<FmRepository>();
      final result = await repository.retryUploadRecord(record.id.toInt());

      if (result.success && mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(al.uploadRecord_restarted)));
        // 刷新列表
        refresh();
      } else if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('${al.uploadRecord_retryFailedMsg}: ${result.message}')));
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('${al.uploadRecord_retryFailedMsg}: $e')));
      }
    }
  }

  Future<void> _deleteRecord(UploadRecordInfo record) async {
    final al = AppLocalizations.of(context)!;
    // 确认删除
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(al.uploadRecord_confirmDelete),
        content: Text(al.uploadRecord_confirmDeleteMsg(record.fileName)),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: Text(al.common_cancel),
          ),
          ElevatedButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: Text(al.uploadRecord_delete),
          ),
        ],
      ),
    );

    if (confirmed != true || !mounted) return;

    try {
      final repository = context.read<FmRepository>();
      final result = await repository.deleteUploadRecord(record.id.toInt());

      if (result.success && mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(al.uploadRecord_deleted)));
        // 刷新列表
        refresh();
      } else if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('${al.uploadRecord_deleteFailed}: ${result.message}')));
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('${al.uploadRecord_deleteFailed}: $e')));
      }
    }
  }

  void _showDetailDialog(UploadRecordInfo record) {
    showDialog(
      context: context,
      builder: (context) =>
          UploadRecordDetailDialog(uploadId: record.id.toInt()),
    );
  }
}
