import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/proto/native/upload.pb.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/utils/format.dart';
import 'package:pfm/widgets/upload_record_detail_dialog.dart';

/// 上传记录列表
class UploadRecordList extends StatefulWidget {
  const UploadRecordList({super.key});

  @override
  State<UploadRecordList> createState() => _UploadRecordListState();
}

class _UploadRecordListState extends State<UploadRecordList> {
  List<UploadRecordInfo> _records = [];
  int _total = 0;
  int _page = 1;
  final int _pageSize = 20;
  bool _isLoading = false;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    _loadRecords();
  }

  Future<void> _loadRecords() async {
    if (_isLoading) return;

    setState(() {
      _isLoading = true;
      _errorMessage = null;
    });

    try {
      final repository = context.read<FmRepository>();
      final result = await repository.listUploadRecords(
        page: _page,
        pageSize: _pageSize,
      );

      if (result.success) {
        setState(() {
          // 只显示进行中和失败的记录，过滤已成功的
          _records = result.data!.records.where((r) =>
            r.status != UploadProviderStatus.PCOMPLETED
          ).toList();
          _total = result.data!.total.toInt();
          _isLoading = false;
        });
      } else {
        setState(() {
          _errorMessage = result.message ?? '未知错误';
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

  /// 解析 JSON 数组并返回提供者数量
  String _getProviderCount(String providerIds) {
    if (providerIds.isEmpty) return '0 个';

    try {
      final ids = jsonDecode(providerIds) as List;
      return '${ids.length} 个';
    } catch (e) {
      return '0 个';
    }
  }

  String _getStatusText(UploadProviderStatus status) {
    switch (status) {
      case UploadProviderStatus.PPENDING:
        return '等待中';
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

  Color _getStatusColor(UploadProviderStatus status) {
    switch (status) {
      case UploadProviderStatus.PPENDING:
        return Colors.grey;
      case UploadProviderStatus.PUPLOADING:
        return Colors.blue;
      case UploadProviderStatus.PCOMPLETED:
        return Colors.green;
      case UploadProviderStatus.PFAILED:
        return Colors.red;
      default:
        return Colors.grey;
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_isLoading) {
      return const Center(child: CircularProgressIndicator());
    }

    if (_errorMessage != null) {
      return Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Text('加载失败: $_errorMessage'),
            const SizedBox(height: 16),
            ElevatedButton(
              onPressed: _loadRecords,
              child: const Text('重试'),
            ),
          ],
        ),
      );
    }

    if (_records.isEmpty) {
      return const Center(child: Text('暂无上传记录'));
    }

    return Column(
      children: [
        // 提示信息
        Container(
          width: double.infinity,
          padding: const EdgeInsets.all(12),
          color: Theme.of(context).colorScheme.surfaceContainerHighest,
          child: Row(
            children: [
              Icon(Icons.info_outline, size: 16, color: Theme.of(context).colorScheme.onSurfaceVariant),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  '仅显示进行中和失败的云上传记录，已成功的记录不显示',
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
            onRefresh: _loadRecords,
            child: ListView.builder(
              itemCount: _records.length,
              itemBuilder: (context, index) {
                final record = _records[index];
                return _buildRecordTile(record);
              },
            ),
          ),
        ),
      ],
    );
  }

  Widget _buildRecordTile(UploadRecordInfo record) {
    return ListTile(
      leading: Icon(
        Icons.insert_drive_file,
        color: Theme.of(context).primaryColor,
      ),
      title: Text(record.fileName),
      subtitle: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('${formatCapacityString(record.fileSize.toInt())}'),
          const SizedBox(height: 4),
          Row(
            children: [
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                decoration: BoxDecoration(
                  color: _getStatusColor(record.status).withOpacity(0.1),
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Text(
                  _getStatusText(record.status),
                  style: TextStyle(
                    color: _getStatusColor(record.status),
                    fontSize: 12,
                  ),
                ),
              ),
              const SizedBox(width: 8),
              Text(
                '目标: ${_getProviderCount(record.targetProviderIds)}',
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
          const PopupMenuItem(
            value: 'detail',
            child: ListTile(
              leading: Icon(Icons.info_outline),
              title: Text('查看详情'),
            ),
          ),
          if (record.status == UploadProviderStatus.PFAILED)
            const PopupMenuItem(
              value: 'retry',
              child: ListTile(
                leading: Icon(Icons.refresh),
                title: Text('重试'),
              ),
            ),
          const PopupMenuItem(
            value: 'delete',
            child: ListTile(
              leading: Icon(Icons.delete),
              title: Text('删除'),
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
    try {
      final repository = context.read<FmRepository>();
      final result = await repository.retryUploadRecord(record.id.toInt());

      if (result.success && mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('已重新开始上传')),
        );
        // 刷新列表
        _loadRecords();
      } else if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('重试失败: ${result.message}')),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('重试失败: $e')),
        );
      }
    }
  }

  Future<void> _deleteRecord(UploadRecordInfo record) async {
    // 确认删除
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('确认删除'),
        content: Text('确定要删除上传记录"${record.fileName}"吗？\n这将同时删除相关的失败文件位置记录。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          ElevatedButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('删除'),
          ),
        ],
      ),
    );

    if (confirmed != true) return;

    try {
      final repository = context.read<FmRepository>();
      final result = await repository.deleteUploadRecord(record.id.toInt());

      if (result.success && mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('已删除上传记录')),
        );
        // 刷新列表
        _loadRecords();
      } else if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('删除失败: ${result.message}')),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('删除失败: $e')),
        );
      }
    }
  }

  void _showDetailDialog(UploadRecordInfo record) {
    showDialog(
      context: context,
      builder: (context) => UploadRecordDetailDialog(
        uploadId: record.id.toInt(),
      ),
    );
  }
}