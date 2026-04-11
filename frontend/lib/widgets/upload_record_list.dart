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
          _records = result.data!.records;
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

    return RefreshIndicator(
      onRefresh: _loadRecords,
      child: ListView.builder(
        itemCount: _records.length,
        itemBuilder: (context, index) {
          final record = _records[index];
          return _buildRecordTile(record);
        },
      ),
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
                '目标: ${record.targetProviderIds}',
                style: const TextStyle(fontSize: 12),
              ),
            ],
          ),
        ],
      ),
      trailing: IconButton(
        icon: const Icon(Icons.info_outline),
        onPressed: () => _showDetailDialog(record),
      ),
    );
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