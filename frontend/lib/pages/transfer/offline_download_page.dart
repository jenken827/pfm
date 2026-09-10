import 'package:flutter/material.dart';
import 'package:pfm/api/fm_repository.dart';
import 'package:pfm/design/design_tokens.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/proto/native/download.pb.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';

/// 离线下载任务管理页（任务14）
///
/// 展示服务器 aria2 下载任务：URL/文件名、状态、大小、时间，
/// 支持删除（停止下载）与重试（失败任务）。
class OfflineDownloadPage extends StatefulWidget {
  const OfflineDownloadPage({super.key});

  @override
  State<OfflineDownloadPage> createState() => _OfflineDownloadPageState();
}

class _OfflineDownloadPageState extends State<OfflineDownloadPage> {
  final FmRepository _repo = FmRepository();
  List<DownloadRecord> _records = [];
  bool _isLoading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _isLoading = true;
      _error = null;
    });
    final result = await _repo.listOfflineDownloads(page: 1, pageSize: 100);
    if (!mounted) return;
    setState(() {
      _isLoading = false;
      if (result.success) {
        _records = result.data?.records ?? [];
      } else {
        _error = result.message ?? '加载失败';
      }
    });
  }

  Future<void> _delete(DownloadRecord r) async {
    final al = AppLocalizations.of(context);
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text(al.offlineDl_deleteTitle),
        content: Text(al.offlineDl_deleteMsg(_displayName(r))),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialogContext, false),
            child: Text(al.common_cancel),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(dialogContext, true),
            child: Text(al.common_delete),
          ),
        ],
      ),
    );
    if (confirmed != true) return;

    final result = await _repo.deleteOfflineDownload(r.id);
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          result.success ? al.offlineDl_deleted : (result.message ?? ''),
        ),
      ),
    );
    if (result.success) _load();
  }

  Future<void> _retry(DownloadRecord r) async {
    final al = AppLocalizations.of(context);
    final result = await _repo.retryOfflineDownload(r.id);
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          result.success ? al.offlineDl_retried : (result.message ?? ''),
        ),
      ),
    );
    if (result.success) _load();
  }

  String _displayName(DownloadRecord r) {
    if (r.sourceName.isNotEmpty) return r.sourceName;
    if (r.url.isNotEmpty) return r.url;
    return '#${r.id}';
  }

  String _formatBytes(int v) {
    if (v < 1024) return '$v B';
    if (v < 1024 * 1024) return '${(v / 1024).toStringAsFixed(1)} KB';
    if (v < 1024 * 1024 * 1024) {
      return '${(v / (1024 * 1024)).toStringAsFixed(1)} MB';
    }
    return '${(v / (1024 * 1024 * 1024)).toStringAsFixed(1)} GB';
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context);
    final colorScheme = Theme.of(context).colorScheme;

    return Scaffold(
      appBar: AppBar(title: Text(al.offlineDl_title), centerTitle: true),
      body: _isLoading
          ? const Center(child: CircularProgressIndicator())
          : _error != null
              ? Center(
                  child: Column(
                    mainAxisAlignment: MainAxisAlignment.center,
                    children: [
                      Text(
                        _error!,
                        style: TextStyle(color: colorScheme.onSurfaceVariant),
                      ),
                      const SizedBox(height: 16),
                      ElevatedButton.icon(
                        onPressed: _load,
                        icon: const Icon(Icons.refresh),
                        label: Text(al.offlineDl_retry),
                      ),
                    ],
                  ),
                )
              : _records.isEmpty
                  ? Center(
                      child: Text(
                        al.offlineDl_empty,
                        style: TextStyle(color: colorScheme.onSurfaceVariant),
                      ),
                    )
                  : RefreshIndicator(
                      onRefresh: _load,
                      child: ListView.separated(
                        itemCount: _records.length,
                        separatorBuilder: (_, _) =>
                            Divider(height: 1, color: colorScheme.outlineVariant),
                        itemBuilder: (context, index) {
                          final r = _records[index];
                          return _buildRow(context, r);
                        },
                      ),
                    ),
    );
  }

  Widget _buildRow(BuildContext context, DownloadRecord r) {
    final al = AppLocalizations.of(context);
    final colorScheme = Theme.of(context).colorScheme;

    // 状态展示
    final (icon, color, label) = _stateUi(r.state, colorScheme);

    return ListTile(
      leading: Icon(icon, color: color),
      title: Text(
        _displayName(r),
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
      subtitle: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (r.url.isNotEmpty)
            Text(
              r.url,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                fontSize: 12,
                color: colorScheme.onSurfaceVariant,
              ),
            ),
          Row(
            children: [
              Text(
                label,
                style: TextStyle(fontSize: 12, color: color),
              ),
              if (r.fileSize.toInt() > 0) ...[
                const SizedBox(width: 8),
                Text(
                  _formatBytes(r.fileSize.toInt()),
                  style: TextStyle(
                    fontSize: 12,
                    color: colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
              if (r.updatedAt.seconds.toInt() > 0) ...[
                const SizedBox(width: 8),
                Text(
                  al.offlineDl_updated(_formatTime(r.updatedAt)),
                  style: TextStyle(
                    fontSize: 12,
                    color: colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ],
          ),
          if (r.errorMessage.isNotEmpty) ...[
            const SizedBox(height: 2),
            Text(
              r.errorMessage,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                fontSize: 12,
                color: colorScheme.error,
              ),
            ),
          ],
        ],
      ),
      isThreeLine: false,
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (r.state == DownloadRecordState.DOWNLOAD_FAILED)
            IconButton(
              icon: Icon(
                Icons.refresh,
                size: 18,
                color: colorScheme.onSurfaceVariant,
              ),
              tooltip: al.offlineDl_retry,
              onPressed: () => _retry(r),
            ),
          IconButton(
            icon: Icon(
              Icons.delete_outline,
              size: 18,
              color: colorScheme.onSurfaceVariant,
            ),
            tooltip: al.common_delete,
            onPressed: () => _delete(r),
          ),
        ],
      ),
    );
  }

  (IconData, Color, String) _stateUi(
    DownloadRecordState state,
    ColorScheme colorScheme,
  ) {
    final al = AppLocalizations.of(context);
    switch (state) {
      case DownloadRecordState.DOWNLOAD_PENDING:
        return (Icons.hourglass_empty, PfmColors.warning, al.offlineDl_pending);
      case DownloadRecordState.DOWNLOADING:
        return (Icons.downloading, PfmColors.primary, al.offlineDl_downloading);
      case DownloadRecordState.DOWNLOAD_COMPLETED:
        return (Icons.check_circle, PfmColors.success, al.offlineDl_completed);
      case DownloadRecordState.DOWNLOAD_FAILED:
        return (Icons.error_outline, PfmColors.error, al.offlineDl_failed);
      default:
        return (Icons.help_outline, colorScheme.onSurfaceVariant, '未知');
    }
  }

  String _formatTime(dynamic ts) {
    // protobuf Timestamp: seconds 字段
    try {
      final sec = ts.seconds.toInt();
      final t = DateTime.fromMillisecondsSinceEpoch(sec * 1000).toLocal();
      return '${t.year}-${t.month.toString().padLeft(2, '0')}-${t.day.toString().padLeft(2, '0')} '
          '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}';
    } catch (_) {
      return '';
    }
  }
}
