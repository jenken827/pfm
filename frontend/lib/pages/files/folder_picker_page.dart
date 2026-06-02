import 'package:flutter/material.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:flutter_bloc/flutter_bloc.dart';

/// 选择文件夹页面
///
/// 用于选择目标文件夹（如移动文件时选择目标位置）
/// 默认从根目录开始，支持面包屑导航和文件夹浏览
/// 文件夹选择结果
class FolderPickResult {
  final String id;
  final String name;

  const FolderPickResult({required this.id, required this.name});
}

class FolderPickerPage extends StatefulWidget {
  const FolderPickerPage({
    super.key,
    this.initialFolderId = '',
    this.excludeIds = const [],
  });

  /// 初始文件夹ID（默认根目录 ''）
  final String initialFolderId;

  /// 要排除的文件夹ID列表（不能移动到自己或子文件夹）
  final List<String> excludeIds;

  @override
  State<FolderPickerPage> createState() => _FolderPickerPageState();
}

class _FolderPickerPageState extends State<FolderPickerPage> {
  /// 当前文件夹ID
  String _currentFolderId = '';

  /// 面包屑路径
  List<_BreadcrumbItem> _breadcrumbs = [];

  /// 文件夹列表
  List<File> _folders = [];

  /// 是否正在加载
  bool _isLoading = false;

  /// 错误信息
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    _currentFolderId = widget.initialFolderId;
    _loadFolders();
  }

  /// 加载文件夹列表
  Future<void> _loadFolders() async {
    final fmRepo = context.read<FmRepository>();

    setState(() {
      _isLoading = true;
      _errorMessage = null;
    });

    try {
      final result = await fmRepo.getFolderList(parentId: _currentFolderId);

      if (!mounted) return;

      if (result.success && result.data != null) {
        setState(() {
          _folders = result.data!.folders;
          _isLoading = false;
        });
      } else {
        setState(() {
          _errorMessage = result.message ?? '加载失败';
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

  /// 进入子文件夹
  void _enterFolder(String folderId, String folderName) {
    setState(() {
      _breadcrumbs.add(_BreadcrumbItem(id: folderId, name: folderName));
      _currentFolderId = folderId;
    });
    _loadFolders();
  }

  /// 返回上级目录
  void _goBack() {
    if (_breadcrumbs.isNotEmpty) {
      setState(() {
        _breadcrumbs.removeLast();
        _currentFolderId = _breadcrumbs.isEmpty ? '' : _breadcrumbs.last.id;
      });
      _loadFolders();
    }
  }

  /// 跳转到指定面包屑
  void _navigateToBreadcrumb(int index) {
    if (index < 0) {
      // 返回根目录
      setState(() {
        _breadcrumbs.clear();
        _currentFolderId = '';
      });
    } else if (index < _breadcrumbs.length) {
      setState(() {
        _breadcrumbs = _breadcrumbs.sublist(0, index + 1);
        _currentFolderId = _breadcrumbs.last.id;
      });
    }
    _loadFolders();
  }

  /// 确认选择当前文件夹
  void _confirmSelection() {
    final al = AppLocalizations.of(context)!;
    final name = _breadcrumbs.isEmpty ? al.folderPicker_root : _breadcrumbs.last.name;
    Navigator.of(context).pop(FolderPickResult(id: _currentFolderId, name: name));
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context)!;

    return Scaffold(
      appBar: AppBar(
        title: Text(al.folderPicker_selectTarget),
        leading: IconButton(
          icon: const Icon(Icons.close),
          onPressed: () => Navigator.of(context).pop(),
        ),
        actions: [
          // 返回上级按钮
          if (_breadcrumbs.isNotEmpty)
            IconButton(
              icon: const Icon(Icons.arrow_upward),
              onPressed: _goBack,
              tooltip: al.folderPicker_goBack,
            ),
        ],
      ),
      body: Column(
        children: [
          // 面包屑导航
          if (_breadcrumbs.isNotEmpty || _currentFolderId.isNotEmpty)
            _buildBreadcrumbs(),
          const Divider(height: 1),
          // 文件夹列表
          Expanded(child: _buildFolderList()),
          // 底部操作栏
          _buildBottomBar(),
        ],
      ),
    );
  }

  /// 构建面包屑导航
  Widget _buildBreadcrumbs() {
    final al = AppLocalizations.of(context)!;

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: Row(
          children: [
            // 根目录
            InkWell(
              onTap: () => _navigateToBreadcrumb(-1),
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 4),
                child: Text(
                  al.folderPicker_root,
                  style: const TextStyle(fontWeight: FontWeight.w500),
                ),
              ),
            ),
            // 面包屑路径
            ..._breadcrumbs.asMap().entries.map((entry) {
              final index = entry.key;
              final item = entry.value;
              final isLast = index == _breadcrumbs.length - 1;

              return Row(
                children: [
                  Icon(
                    Icons.chevron_right,
                    size: 16,
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                  InkWell(
                    onTap: isLast ? null : () => _navigateToBreadcrumb(index),
                    child: Padding(
                      padding: const EdgeInsets.symmetric(horizontal: 4),
                      child: Text(
                        item.name,
                        style: TextStyle(
                          fontWeight: isLast
                              ? FontWeight.bold
                              : FontWeight.normal,
                          color: isLast
                              ? Theme.of(context).colorScheme.primary
                              : Theme.of(context).colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ),
                  ),
                ],
              );
            }),
          ],
        ),
      ),
    );
  }

  /// 构建文件夹列表
  Widget _buildFolderList() {
    final al = AppLocalizations.of(context)!;

    if (_isLoading) {
      return const Center(child: CircularProgressIndicator());
    }

    if (_errorMessage != null) {
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
            Text(_errorMessage!),
            const SizedBox(height: 16),
            ElevatedButton.icon(
              onPressed: _loadFolders,
              icon: const Icon(Icons.refresh),
              label: Text(al.common_retry),
            ),
          ],
        ),
      );
    }

    if (_folders.isEmpty) {
      return Center(child: Text(al.folderPicker_noSubfolders));
    }

    return ListView.builder(
      itemCount: _folders.length,
      itemBuilder: (context, index) {
        final folder = _folders[index];
        final isExcluded = widget.excludeIds.contains(folder.id);

        return ListTile(
          leading: Icon(
            Icons.folder,
            color: isExcluded ? Colors.grey : Colors.amber,
          ),
          title: Text(
            folder.name,
            style: TextStyle(color: isExcluded ? Colors.grey : null),
          ),
          trailing: isExcluded
              ? const Icon(Icons.block, color: Colors.grey)
              : const Icon(Icons.arrow_forward),
          enabled: !isExcluded,
          onTap: isExcluded ? null : () => _enterFolder(folder.id, folder.name),
        );
      },
    );
  }

  /// 构建底部操作栏
  Widget _buildBottomBar() {
    final al = AppLocalizations.of(context)!;
    final path = _currentFolderId.isEmpty ? al.folderPicker_root : (_breadcrumbs.isNotEmpty ? _breadcrumbs.last.name : al.folderPicker_root);

    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surface,
        boxShadow: [
          BoxShadow(
            color: Colors.black.withValues(alpha: 0.1),
            blurRadius: 4,
            offset: const Offset(0, -2),
          ),
        ],
      ),
      child: SafeArea(
        child: Row(
          children: [
            Expanded(
              child: Text(
                al.folderPicker_currentLocation(path),
                style: Theme.of(context).textTheme.bodyMedium,
              ),
            ),
            FilledButton.icon(
              onPressed: _confirmSelection,
              icon: const Icon(Icons.check),
              label: Text(al.folderPicker_selectHere),
            ),
          ],
        ),
      ),
    );
  }
}

/// 面包屑项
class _BreadcrumbItem {
  final String id;
  final String name;

  _BreadcrumbItem({required this.id, required this.name});
}
