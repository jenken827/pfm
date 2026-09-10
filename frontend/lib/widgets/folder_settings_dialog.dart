import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/proto/native/provider.pb.dart';

/// 文件夹设置对话框
class FolderSettingsDialog extends StatefulWidget {
  final File folder;
  final VoidCallback? onUpdate;

  const FolderSettingsDialog({super.key, required this.folder, this.onUpdate});

  @override
  State<FolderSettingsDialog> createState() => _FolderSettingsDialogState();
}

class _FolderSettingsDialogState extends State<FolderSettingsDialog> {
  late TextEditingController _nameController;
  late TextEditingController _maxHistoriesController;
  bool _enableEncryption = false;
  bool _isLoading = false;

  /// 管理员分配给当前用户的存储提供者（可选范围）
  List<StorageProvider> _providers = const [];
  /// 当前选中的绑定（至少一个）
  List<int> _selectedProviderIds = [];
  bool _providersLoaded = false;

  @override
  void initState() {
    super.initState();
    _nameController = TextEditingController(text: widget.folder.name);

    // 初始化 maxHistories 配置
    final maxHistories = widget.folder.maxHistories;
    _maxHistoriesController = TextEditingController(
      text: maxHistories > 0 ? maxHistories.toString() : '',
    );

    // 初始化加密配置
    _enableEncryption = widget.folder.enableEncryption;

    _loadProviders();
  }

  @override
  void dispose() {
    _nameController.dispose();
    _maxHistoriesController.dispose();
    super.dispose();
  }

  /// 加载可选存储提供者，并预选当前生效的绑定：
  /// 目录自身绑定 → 父目录绑定 → 全部可用（与后端解析规则一致）
  Future<void> _loadProviders() async {
    final repository = context.read<FmRepository>();
    final resp = await repository.getMyBoundProviders();
    final providers = resp.data?.providers.toList() ?? const <StorageProvider>[];
    if (!mounted) return;

    var selected = parseProviderIdsJson(widget.folder.boundProviderIds);
    if (selected.isEmpty && widget.folder.parentId.isNotEmpty) {
      final info = await repository.getFileInfo(widget.folder.parentId);
      if (!mounted) return;
      selected = parseProviderIdsJson(info.data?.boundProviderIds ?? '');
    }

    final availableIds = providers.map((p) => p.id.toInt()).toSet();
    selected = selected.where(availableIds.contains).toList();
    if (selected.isEmpty) {
      // 未显式绑定（或原绑定已不在可用范围内）→ 默认全部可用
      selected = providers.map((p) => p.id.toInt()).toList();
    }

    setState(() {
      _providers = providers;
      _selectedProviderIds = selected;
      _providersLoaded = true;
    });
  }

  Future<void> _updateFolder() async {
    final al = AppLocalizations.of(context);
    if (_nameController.text.isEmpty) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(al.folderSettings_nameEmpty)));
      return;
    }

    // 至少绑定一个存储提供者
    if (_providers.isNotEmpty && _selectedProviderIds.isEmpty) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(al.folderSettings_providerRequired)));
      return;
    }

    // 解析 maxHistories
    int? maxHistories;
    if (_maxHistoriesController.text.isNotEmpty) {
      maxHistories = int.tryParse(_maxHistoriesController.text);
      if (maxHistories == null || maxHistories < 0) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(al.folderSettings_maxHistoryInvalid)),
        );
        return;
      }
    }

    setState(() => _isLoading = true);

    try {
      final repository = context.read<FmRepository>();
      final result = await repository.updateDir(
        id: widget.folder.id,
        name: _nameController.text,
        enableEncryption: _enableEncryption,
        maxHistories: maxHistories,
        providerIds: _selectedProviderIds.isEmpty
            ? null
            : List<int>.from(_selectedProviderIds),
      );

      if (result.success) {
        if (mounted) {
          Navigator.of(context).pop();
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(SnackBar(content: Text(al.folderSettings_updated)));
          widget.onUpdate?.call();
        }
      } else {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text(
                '${al.folderSettings_updateFailed}: ${result.message}',
              ),
            ),
          );
        }
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('${al.folderSettings_updateFailed}: $e')),
        );
      }
    } finally {
      if (mounted) {
        setState(() => _isLoading = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context);
    return AlertDialog(
      title: Text(al.folderSettings_title),
      content: SizedBox(
        width: 400,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              // 文件夹名称
              TextField(
                controller: _nameController,
                decoration: InputDecoration(
                  labelText: al.folderSettings_folderName,
                  border: OutlineInputBorder(),
                ),
              ),
              const SizedBox(height: 16),

              // 存储提供者绑定
              Text(
                al.folderSettings_boundProviders,
                style: const TextStyle(fontWeight: FontWeight.w600),
              ),
              const SizedBox(height: 8),
              if (!_providersLoaded)
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 8),
                  child: Center(
                    child: SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    ),
                  ),
                )
              else if (_providers.isEmpty)
                Text(
                  al.folderSettings_noProviders,
                  style: TextStyle(
                    fontSize: 12,
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                )
              else ...[
                ..._providers.map((provider) {
                  final id = provider.id.toInt();
                  return CheckboxListTile(
                    dense: true,
                    controlAffinity: ListTileControlAffinity.leading,
                    contentPadding: EdgeInsets.zero,
                    title: Text(provider.name),
                    value: _selectedProviderIds.contains(id),
                    onChanged: _isLoading
                        ? null
                        : (checked) {
                            setState(() {
                              if (checked == true) {
                                if (!_selectedProviderIds.contains(id)) {
                                  _selectedProviderIds.add(id);
                                }
                              } else {
                                _selectedProviderIds.remove(id);
                              }
                            });
                          },
                  );
                }),
                Text(
                  al.folderSettings_providerHint,
                  style: TextStyle(
                    fontSize: 12,
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
              const SizedBox(height: 16),

              // 加密配置
              SwitchListTile(
                title: Text(al.folderSettings_enableEncryption),
                subtitle: Text(al.folderSettings_encryptionHint),
                value: _enableEncryption,
                onChanged: (value) {
                  setState(() => _enableEncryption = value);
                },
              ),
              const SizedBox(height: 16),

              // 最大历史记录数
              TextField(
                controller: _maxHistoriesController,
                decoration: InputDecoration(
                  labelText: al.folderSettings_maxHistory,
                  hintText: al.folderSettings_maxHistoryHint,
                  border: OutlineInputBorder(),
                ),
                keyboardType: TextInputType.number,
              ),
              const SizedBox(height: 8),
              Text(
                al.folderSettings_maxHistoryDesc,
                style: TextStyle(
                  fontSize: 12,
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: _isLoading ? null : () => Navigator.of(context).pop(),
          child: Text(al.common_cancel),
        ),
        ElevatedButton(
          onPressed: _isLoading ? null : _updateFolder,
          child: _isLoading
              ? const SizedBox(
                  width: 16,
                  height: 16,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : Text(al.common_save),
        ),
      ],
    );
  }
}
