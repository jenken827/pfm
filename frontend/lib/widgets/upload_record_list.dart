import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/proto/native/upload.pb.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/utils/format.dart';
import 'package:pfm/design/design_tokens.dart';
import 'package:pfm/design/components/components.dart';
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
            .where((r) => r.status != UploadStatus.COMPLETED)
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

  String _getStatusText(AppLocalizations al, UploadStatus status) {
    switch (status) {
      case UploadStatus.PENDING:
        return al.uploadRecord_statusWaiting;
      case UploadStatus.UPLOADING:
        return al.uploadRecord_statusUploading;
      case UploadStatus.COMPLETED:
        return al.uploadRecord_statusCompleted;
      case UploadStatus.FAILED:
        return al.uploadRecord_statusFailed;
      default:
        return al.uploadRecord_statusUnknown;
    }
  }

  Color _getStatusColor(UploadStatus status) {
    switch (status) {
      case UploadStatus.PENDING:
        return Theme.of(context).colorScheme.onSurfaceVariant;
      case UploadStatus.UPLOADING:
        return Colors.blue;
      case UploadStatus.COMPLETED:
        return Colors.green;
      case UploadStatus.FAILED:
        return Colors.red;
      default:
        return Theme.of(context).colorScheme.onSurfaceVariant;
    }
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context);

    if (_isLoading) {
      return const Center(child: CircularProgressIndicator());
    }

    if (_errorMessage != null) {
      final colorScheme = Theme.of(context).colorScheme;
      return Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Container(
              width: 64,
              height: 64,
              decoration: BoxDecoration(
                color: PfmColors.errorSurface,
                borderRadius: BorderRadius.circular(PfmRadius.xl),
              ),
              child: const Icon(
                Icons.error_outline,
                size: 32,
                color: PfmColors.error,
              ),
            ),
            const SizedBox(height: PfmSpacing.lg),
            Text(
              al.uploadRecord_loadFailed,
              style: PfmTypography.titleSmall.copyWith(
                color: colorScheme.onSurface,
              ),
            ),
            const SizedBox(height: PfmSpacing.lg),
            ElevatedButton.icon(
              onPressed: _loadRecords,
              icon: const Icon(Icons.refresh, size: 18),
              label: Text(al.common_retry),
            ),
          ],
        ),
      );
    }

    if (_records.isEmpty) {
      return Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Container(
              width: 80,
              height: 80,
              decoration: BoxDecoration(
                color: PfmColors.archiveSurface,
                borderRadius: BorderRadius.circular(PfmRadius.xl),
              ),
              child: const Icon(
                Icons.history,
                size: 40,
                color: PfmColors.archive,
              ),
            ),
            const SizedBox(height: PfmSpacing.xl),
            Text(
              al.uploadRecord_noRecords,
              style: PfmTypography.titleMedium.copyWith(
                color: Theme.of(context).colorScheme.onSurface,
              ),
            ),
          ],
        ),
      );
    }

    return Column(
      children: [
        // 批量操作菜单
        _buildActionBar(al),
        Container(
          width: double.infinity,
          padding: const EdgeInsets.symmetric(
            horizontal: PfmSpacing.lg,
            vertical: PfmSpacing.md,
          ),
          color: Theme.of(
            context,
          ).colorScheme.surfaceContainerHighest.withValues(alpha: 0.3),
          child: Row(
            children: [
              Icon(
                Icons.info_outline,
                size: 14,
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
              const SizedBox(width: PfmSpacing.sm),
              Expanded(
                child: Text(
                  al.uploadRecord_recordsHint,
                  style: PfmTypography.labelSmall.copyWith(
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
    final colorScheme = Theme.of(context).colorScheme;
    return Container(
      padding: const EdgeInsets.symmetric(
        horizontal: PfmSpacing.lg,
        vertical: PfmSpacing.sm,
      ),
      decoration: BoxDecoration(
        color: colorScheme.surfaceContainerHighest.withValues(alpha: 0.3),
        border: Border(bottom: BorderSide(color: colorScheme.outlineVariant)),
      ),
      child: Row(
        children: [
          Text(
            al.uploadRecord_total(_total),
            style: PfmTypography.labelMedium.copyWith(
              color: colorScheme.onSurfaceVariant,
            ),
          ),
          if (_failedCount > 0) ...[
            const SizedBox(width: PfmSpacing.sm),
            Container(
              padding: const EdgeInsets.symmetric(
                horizontal: PfmSpacing.sm,
                vertical: PfmSpacing.xs,
              ),
              decoration: BoxDecoration(
                color: PfmColors.error.withValues(alpha: 0.1),
                borderRadius: BorderRadius.circular(PfmRadius.full),
              ),
              child: Text(
                al.uploadRecord_failedCount(_failedCount),
                style: PfmTypography.labelSmall.copyWith(
                  color: PfmColors.error,
                ),
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
    final al = AppLocalizations.of(context);
    if (_failedCount == 0) return;

    final confirmed = await PfmDialogs.confirm(
      context,
      title: al.uploadRecord_retryFailedTitle,
      message: al.uploadRecord_retryFailedConfirm(_failedCount),
      confirmLabel: al.uploadRecord_retry,
    );
    if (!confirmed || !mounted) return;

    final repository = context.read<FmRepository>();
    final result = await repository.retryAllFailedUploadRecords();

    if (mounted) {
      if (result.success) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(al.uploadRecord_retrySubmitted)));
      } else {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              '${al.uploadRecord_retryFailedMsg}: ${result.message}',
            ),
          ),
        );
      }
      refresh();
    }
  }

  Widget _buildRecordTile(AppLocalizations al, UploadRecordInfo record) {
    final colorScheme = Theme.of(context).colorScheme;
    return ListTile(
      leading: Container(
        width: 40,
        height: 40,
        decoration: BoxDecoration(
          color: PfmColors.primarySurface,
          borderRadius: BorderRadius.circular(PfmRadius.sm),
        ),
        child: const Icon(
          Icons.insert_drive_file,
          color: PfmColors.primary,
          size: 20,
        ),
      ),
      title: Text(
        record.fileName,
        style: TextStyle(color: colorScheme.onSurface),
      ),
      subtitle: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            formatCapacityString(record.fileSize.toInt()),
            style: TextStyle(color: colorScheme.onSurfaceVariant),
          ),
          const SizedBox(height: 4),
          Row(
            children: [
              Container(
                padding: const EdgeInsets.symmetric(
                  horizontal: PfmSpacing.sm,
                  vertical: PfmSpacing.xs,
                ),
                decoration: BoxDecoration(
                  color: _getStatusColor(record.status).withValues(alpha: 0.1),
                  borderRadius: BorderRadius.circular(PfmRadius.full),
                ),
                child: Text(
                  _getStatusText(al, record.status),
                  style: PfmTypography.labelSmall.copyWith(
                    color: _getStatusColor(record.status),
                  ),
                ),
              ),
              const SizedBox(width: 8),
              Text(
                al.uploadRecord_target(
                  _getProviderCount(al, record.targetProviderIds),
                ),
                style: TextStyle(
                  fontSize: 12,
                  color: colorScheme.onSurfaceVariant,
                ),
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
          if (record.status == UploadStatus.FAILED)
            PopupMenuItem(
              value: 'retry',
              child: ListTile(
                leading: const Icon(Icons.refresh),
                title: Text(al.uploadRecord_retry),
              ),
            ),
          PopupMenuItem(
            value: 'delete',
            child: ListTile(
              leading: const Icon(Icons.delete),
              title: Text(al.uploadRecord_delete),
            ),
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
    final al = AppLocalizations.of(context);
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
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              '${al.uploadRecord_retryFailedMsg}: ${result.message}',
            ),
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('${al.uploadRecord_retryFailedMsg}: $e')),
        );
      }
    }
  }

  Future<void> _deleteRecord(UploadRecordInfo record) async {
    final al = AppLocalizations.of(context);
    final confirmed = await PfmDialogs.confirm(
      context,
      title: al.uploadRecord_confirmDelete,
      message: al.uploadRecord_confirmDeleteMsg(record.fileName),
      confirmLabel: al.uploadRecord_delete,
      isDestructive: true,
    );
    if (!confirmed || !mounted) return;

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
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('${al.uploadRecord_deleteFailed}: ${result.message}'),
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('${al.uploadRecord_deleteFailed}: $e')),
        );
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
