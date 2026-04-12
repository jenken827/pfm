import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:pfm/api/api.dart';
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
  bool _enableEncryption = false;
  List<int> _selectedProviderIds = [];
  List<StorageProvider> _storageProviders = [];
  bool _isLoading = false;
  bool _isLoadingProviders = true;

  @override
  void initState() {
    super.initState();
    _nameController = TextEditingController(text: widget.folder.name);
    
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
    super.dispose();
  }

  Future<void> _updateFolder() async {
    if (_nameController.text.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('文件夹名称不能为空')),
      );
      return;
    }

    setState(() => _isLoading = true);

    try {
      final repository = context.read<FmRepository>();
      final result = await repository.updateDir(
        id: widget.folder.id,
        name: _nameController.text,
        enableEncryption: _enableEncryption,
        providerIds: _selectedProviderIds,
      );

      if (result.success) {
        if (mounted) {
          Navigator.of(context).pop();
          ScaffoldMessenger.of(context).showSnackBar(
            const SnackBar(content: Text('文件夹设置已更新')),
          );
          widget.onUpdate?.call();
        }
      } else {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('更新失败: ${result.message}')),
          );
        }
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('更新失败: $e')),
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
    return AlertDialog(
      title: const Text('文件夹设置'),
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
                decoration: const InputDecoration(
                  labelText: '文件夹名称',
                  border: OutlineInputBorder(),
                ),
              ),
              const SizedBox(height: 16),

              // 加密配置
              SwitchListTile(
                title: const Text('启用加密'),
                subtitle: const Text('文件夹内的文件将使用加密存储'),
                value: _enableEncryption,
                onChanged: (value) {
                  setState(() => _enableEncryption = value);
                },
              ),
              const SizedBox(height: 16),

              // 绑定的存储提供者
              const Text(
                '绑定的存储提供者',
                style: TextStyle(
                  fontSize: 16,
                  fontWeight: FontWeight.bold,
                ),
              ),
              const SizedBox(height: 8),
              
              if (_isLoadingProviders)
                const Center(child: CircularProgressIndicator())
              else if (_storageProviders.isEmpty)
                const Text('暂无可用存储提供者')
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
          child: const Text('取消'),
        ),
        ElevatedButton(
          onPressed: _isLoading ? null : _updateFolder,
          child: _isLoading
              ? const SizedBox(
                  width: 16,
                  height: 16,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : const Text('保存'),
        ),
      ],
    );
  }
}