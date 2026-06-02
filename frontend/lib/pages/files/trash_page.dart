import 'package:flutter/material.dart';
import 'package:pfm/api/fm_repository.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';

/// 回收站页面
class TrashPage extends StatefulWidget {
  const TrashPage({super.key});

  @override
  State<TrashPage> createState() => _TrashPageState();
}

class _TrashPageState extends State<TrashPage> {
  List<File> _files = [];
  final Set<String> _selectedIds = {};
  bool _isLoading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _loadTrash();
  }

  Future<void> _loadTrash() async {
    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      final repo = FmRepository();
      final result = await repo.getTrashList();

      if (result.success && result.data != null) {
        setState(() {
          _files = result.data!.files;
          _isLoading = false;
        });
      } else {
        setState(() {
          _error = result.message ?? '获取回收站列表失败';
          _isLoading = false;
        });
      }
    } catch (e) {
      setState(() {
        _error = '加载失败: $e';
        _isLoading = false;
      });
    }
  }

  Future<void> _restoreSelected() async {
    if (_selectedIds.isEmpty) return;

    final al = AppLocalizations.of(context)!;

    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(al.trash_restore),
        content: Text(al.trash_restoreMsg(_selectedIds.length)),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: Text(al.common_cancel),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: Text(al.trash_restoreBtn),
          ),
        ],
      ),
    );

    if (confirmed != true) return;

    try {
      final repo = FmRepository();
      final result = await repo.restoreFromTrash(_selectedIds.toList());

      if (result.success && mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(al.trash_restored)));
        _selectedIds.clear();
        _loadTrash();
      } else if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(result.message ?? al.trash_restoreFailed)));
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('恢复失败: $e')));
      }
    }
  }

  Future<void> _deleteSelectedPermanently() async {
    if (_selectedIds.isEmpty) return;

    final al = AppLocalizations.of(context)!;

    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(al.trash_permanentDelete),
        content: Text(al.trash_permanentDeleteMsg(_selectedIds.length)),
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
            child: Text(al.trash_permanentDelete),
          ),
        ],
      ),
    );

    if (confirmed != true) return;

    try {
      final repo = FmRepository();
      final result = await repo.deletePermanently(_selectedIds.toList());

      if (result.success && mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(al.trash_permanentDeleted)));
        _selectedIds.clear();
        _loadTrash();
      } else if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(result.message ?? al.trash_permanentDeleteFailed)));
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('删除失败: $e')));
      }
    }
  }

  Future<void> _emptyTrash() async {
    if (_files.isEmpty) return;

    final al = AppLocalizations.of(context)!;

    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(al.trash_emptyBin),
        content: Text(al.trash_emptyBinMsg),
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
            child: Text(al.trash_emptyBtn),
          ),
        ],
      ),
    );

    if (confirmed != true) return;

    try {
      final repo = FmRepository();
      final result = await repo.emptyTrash();

      if (result.success && mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(al.trash_emptied)));
        _selectedIds.clear();
        _loadTrash();
      } else if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(result.message ?? al.trash_emptyFailed)));
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('清空失败: $e')));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context)!;

    return Scaffold(
      appBar: AppBar(
        title: Text(al.trash_title),
        actions: [
          if (_selectedIds.isNotEmpty) ...[
            IconButton(
              icon: const Icon(Icons.restore),
              tooltip: al.trash_restoreSelected,
              onPressed: _restoreSelected,
            ),
            IconButton(
              icon: const Icon(Icons.delete_forever),
              tooltip: al.trash_deleteSelected,
              onPressed: _deleteSelectedPermanently,
            ),
          ],
          if (_files.isNotEmpty)
            IconButton(
              icon: const Icon(Icons.delete_sweep),
              tooltip: al.trash_emptyBin,
              onPressed: _emptyTrash,
            ),
        ],
      ),
      body: _buildBody(),
    );
  }

  Widget _buildBody() {
    final al = AppLocalizations.of(context)!;

    if (_isLoading) {
      return const Center(child: CircularProgressIndicator());
    }

    if (_error != null) {
      return Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(
              Icons.error_outline,
              size: 64,
              color: Theme.of(context).colorScheme.error,
            ),
            const SizedBox(height: 16),
            Text(_error!, style: Theme.of(context).textTheme.bodyLarge),
            const SizedBox(height: 16),
            FilledButton(onPressed: _loadTrash, child: Text(al.common_retry)),
          ],
        ),
      );
    }

    if (_files.isEmpty) {
      return Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(
              Icons.delete_outline,
              size: 64,
              color: Theme.of(context).colorScheme.outline,
            ),
            const SizedBox(height: 16),
            Text(al.trash_emptyHint, style: Theme.of(context).textTheme.bodyLarge),
          ],
        ),
      );
    }

    return Column(
      children: [
        // 选中提示
        if (_selectedIds.isNotEmpty)
          Container(
            width: double.infinity,
            padding: const EdgeInsets.all(12),
            color: Theme.of(context).colorScheme.primaryContainer,
            child: Text(
              al.trash_selectedCount(_selectedIds.length),
              style: TextStyle(
                color: Theme.of(context).colorScheme.onPrimaryContainer,
              ),
            ),
          ),
        // 文件列表
        Expanded(
          child: ListView.builder(
            itemCount: _files.length,
            itemBuilder: (context, index) {
              final file = _files[index];
              final isSelected = _selectedIds.contains(file.id);

              return ListTile(
                leading: Icon(_getFileIcon(file.type)),
                title: Text(file.name),
                subtitle: Text(_formatFileSize(file.size.toInt())),
                trailing: Checkbox(
                  value: isSelected,
                  onChanged: (checked) {
                    setState(() {
                      if (checked == true) {
                        _selectedIds.add(file.id);
                      } else {
                        _selectedIds.remove(file.id);
                      }
                    });
                  },
                ),
                onTap: () {
                  setState(() {
                    if (isSelected) {
                      _selectedIds.remove(file.id);
                    } else {
                      _selectedIds.add(file.id);
                    }
                  });
                },
              );
            },
          ),
        ),
      ],
    );
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
      case FileType.FONT:
        return Icons.font_download;
      default:
        return Icons.insert_drive_file;
    }
  }

  String _formatFileSize(int size) {
    if (size < 1024) {
      return '$size B';
    } else if (size < 1024 * 1024) {
      return '${(size / 1024).toStringAsFixed(1)} KB';
    } else if (size < 1024 * 1024 * 1024) {
      return '${(size / (1024 * 1024)).toStringAsFixed(1)} MB';
    } else {
      return '${(size / (1024 * 1024 * 1024)).toStringAsFixed(1)} GB';
    }
  }
}
