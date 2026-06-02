import 'dart:convert';

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

  const FolderSettingsDialog({
    super.key,
    required this.folder,
    this.onUpdate,
  });

  @override
  State<FolderSettingsDialog> createState() => _FolderSettingsDialogState();
}

class _FolderSettingsDialogState extends State<FolderSettingsDialog> {
  late TextEditingController _nameController;
  late TextEditingController _maxHistoriesController;
  bool _enableEncryption = false;
  List<int> _selectedProviderIds = [];
  List<StorageProvider> _storageProviders = [];
  bool _isLoading = false;
  bool _isLoadingProviders = true;

  @override
  void initState() {
    super.initState();
    _nameController = TextEditingController(text: widget.folder.name);

    // 初始化 maxHistories 配置
    final maxHistories = widget.folder.maxHistories;
    _maxHistoriesController = TextEditingController(
      text: maxHistories != null && maxHistories > 0 ? maxHistories.toString() : '',
    );

    // 初始化加密配置
    _enableEncryption = widget.folder.enableEncryption;

    // 初始化绑定的存储提供者（JSON 数组格式）
    if (widget.folder.boundProviderIds.isNotEmpty) {
      try {
        final providerIds = jsonDecode(widget.folder.boundProviderIds) as List;
        _selectedProviderIds = providerIds.map((id) => id as int).toList();
      } catch (e) {
        // 解析失败，使用空列表
        _selectedProviderIds = [];
      }
    }

    // 加载存储提供者列表
    _loadStorageProviders();
  }

  Future<void> _loadStorageProviders() async {
    try {
      final repository = context.read<FmRepository>();
      final result = await repository.listStorageProviders();

      if (result.success && mounted) {
        setState(() {
          _storageProviders = result.data!.providers;
          _isLoadingProviders = false;

          // 过滤掉已删除的存储提供者
          final availableIds = _storageProviders
              .map((p) => p.id.toInt())
              .toSet();
          _selectedProviderIds = _selectedProviderIds
              .where((id) => availableIds.contains(id))
              .toList();
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _isLoadingProviders = false;
        });
      }
    }
  }

  @override
  void dispose() {
    _nameController.dispose();
    _maxHistoriesController.dispose();
    super.dispose();
  }

  Future<void> _updateFolder() async {
    final al = AppLocalizations.of(context)!;
    if (_nameController.text.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(al.folderSettings_nameEmpty)),
      );
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
        providerIds: _selectedProviderIds,
        maxHistories: maxHistories,
      );

      if (result.success) {
        if (mounted) {
          Navigator.of(context).pop();
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text(al.folderSettings_updated)),
          );
          widget.onUpdate?.call();
        }
      } else {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('${al.folderSettings_updateFailed}: ${result.message}')),
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
    final al = AppLocalizations.of(context)!;
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
                  color: Colors.grey,
                ),
              ),
              const SizedBox(height: 16),

              // 绑定的存储提供者
              Text(
                al.folderSettings_boundProviders,
                style: TextStyle(
                  fontSize: 16,
                  fontWeight: FontWeight.bold,
                ),
              ),
              const SizedBox(height: 8),
              
              if (_isLoadingProviders)
                const Center(child: CircularProgressIndicator())
              else if (_storageProviders.isEmpty)
                Text(al.folderSettings_noProviders)
              else
                Column(
                  children: _storageProviders.map((provider) {
                    final providerId = provider.id.toInt();
                    final isSelected = _selectedProviderIds.contains(providerId);
                    
                    return CheckboxListTile(
                      title: Text(provider.name),
                      subtitle: Text('ID: $providerId'),
                      value: isSelected,
                      onChanged: (value) {
                        setState(() {
                          if (value == true) {
                            _selectedProviderIds.add(providerId);
                          } else {
                            _selectedProviderIds.remove(providerId);
                          }
                        });
                      },
                    );
                  }).toList(),
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