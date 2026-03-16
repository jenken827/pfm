import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/blocs/blocs.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/proto/native/provider.pb.dart';

/// 存储介质设置页面
class StorageProviderSettingPage extends StatelessWidget {
  const StorageProviderSettingPage({super.key});

  @override
  Widget build(BuildContext context) {
    return BlocProvider(
      create: (context) => StorageProviderBloc(context.read<FmRepository>())
        ..add(const StorageProviderLoadEvent()),
      child: const _StorageProviderView(),
    );
  }
}

class _StorageProviderView extends StatelessWidget {
  const _StorageProviderView();

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('存储介质设置'),
        centerTitle: true,
      ),
      body: BlocBuilder<StorageProviderBloc, StorageProviderState>(
        builder: (context, state) {
          if (state.status == StorageProviderStatus.loading) {
            return const Center(child: CircularProgressIndicator());
          }

          if (state.status == StorageProviderStatus.failure) {
            return Center(
              child: Column(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  const Icon(Icons.error_outline, size: 48),
                  const SizedBox(height: 16),
                  Text(state.message),
                  const SizedBox(height: 16),
                  ElevatedButton(
                    onPressed: () => context
                        .read<StorageProviderBloc>()
                        .add(const StorageProviderLoadEvent()),
                    child: const Text('重试'),
                  ),
                ],
              ),
            );
          }

          if (state.providers.isEmpty) {
            return Center(
              child: Column(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  Icon(Icons.storage_outlined,
                      size: 64,
                      color: Theme.of(context).colorScheme.onSurfaceVariant),
                  const SizedBox(height: 16),
                  const Text('暂无存储介质'),
                  const SizedBox(height: 16),
                  FilledButton.icon(
                    onPressed: () => _showAddDialog(context),
                    icon: const Icon(Icons.add),
                    label: const Text('添加存储介质'),
                  ),
                ],
              ),
            );
          }

          return ListView.builder(
            itemCount: state.providers.length,
            itemBuilder: (context, index) {
              final provider = state.providers[index];
              return _StorageProviderTile(
                provider: provider,
                onEdit: () => _showEditDialog(context, provider),
                onDelete: () => _showDeleteDialog(context, provider),
                onToggle: (enabled) {
                  context.read<StorageProviderBloc>().add(
                        StorageProviderToggleEvent(
                          id: provider.id.toInt(),
                          enabled: enabled,
                        ),
                      );
                },
              );
            },
          );
        },
      ),
      floatingActionButton: FloatingActionButton(
        onPressed: () => _showAddDialog(context),
        child: const Icon(Icons.add),
      ),
    );
  }

  void _showAddDialog(BuildContext context) {
    final nameController = TextEditingController();
    final descController = TextEditingController();
    final configController = TextEditingController();
    StorageProviderType selectedType = StorageProviderType.LOCAL;
    int groupId = 1;

    showDialog(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (context, setState) => AlertDialog(
          title: const Text('添加存储介质'),
          content: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                DropdownButtonFormField<StorageProviderType>(
                  value: selectedType,
                  decoration: const InputDecoration(labelText: '存储类型'),
                  items: const [
                    DropdownMenuItem(
                      value: StorageProviderType.LOCAL,
                      child: Text('本地存储'),
                    ),
                    DropdownMenuItem(
                      value: StorageProviderType.TELEGRAM,
                      child: Text('Telegram'),
                    ),
                  ],
                  onChanged: (value) {
                    if (value != null) {
                      setState(() => selectedType = value);
                    }
                  },
                ),
                const SizedBox(height: 16),
                TextField(
                  controller: nameController,
                  decoration: const InputDecoration(
                    labelText: '名称',
                    hintText: '请输入存储介质名称',
                  ),
                  autofocus: true,
                ),
                const SizedBox(height: 16),
                TextField(
                  controller: descController,
                  decoration: const InputDecoration(
                    labelText: '描述',
                    hintText: '请输入描述（可选）',
                  ),
                ),
                const SizedBox(height: 16),
                TextField(
                  controller: configController,
                  decoration: const InputDecoration(
                    labelText: '配置 (JSON)',
                    hintText: '请输入配置信息',
                  ),
                  maxLines: 3,
                ),
              ],
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () {
                if (nameController.text.isNotEmpty) {
                  context.read<StorageProviderBloc>().add(
                        StorageProviderAddEvent(
                          providerType: selectedType,
                          name: nameController.text,
                          description: descController.text.isEmpty
                              ? null
                              : descController.text,
                          config: configController.text.isEmpty
                              ? null
                              : configController.text,
                          groupId: groupId,
                        ),
                      );
                  Navigator.pop(ctx);
                }
              },
              child: const Text('添加'),
            ),
          ],
        ),
      ),
    );
  }

  void _showEditDialog(BuildContext context, StorageProviderItem provider) {
    final nameController = TextEditingController(text: provider.name);
    final descController =
        TextEditingController(text: provider.description);

    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('编辑存储介质'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: nameController,
              decoration: const InputDecoration(labelText: '名称'),
              autofocus: true,
            ),
            const SizedBox(height: 16),
            TextField(
              controller: descController,
              decoration: const InputDecoration(labelText: '描述'),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () {
              if (nameController.text.isNotEmpty) {
                context.read<StorageProviderBloc>().add(
                      StorageProviderUpdateEvent(
                        id: provider.id.toInt(),
                        name: nameController.text,
                        description: descController.text.isEmpty
                            ? null
                            : descController.text,
                      ),
                    );
                Navigator.pop(ctx);
              }
            },
            child: const Text('保存'),
          ),
        ],
      ),
    );
  }

  void _showDeleteDialog(BuildContext context, StorageProviderItem provider) {
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('删除存储介质'),
        content: Text('确定要删除存储介质 "${provider.name}" 吗？'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('取消'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
              backgroundColor: Theme.of(context).colorScheme.error,
            ),
            onPressed: () {
              context
                  .read<StorageProviderBloc>()
                  .add(StorageProviderDeleteEvent(provider.id.toInt()));
              Navigator.pop(ctx);
            },
            child: const Text('删除'),
          ),
        ],
      ),
    );
  }
}

/// 存储介质列表项
class _StorageProviderTile extends StatelessWidget {
  const _StorageProviderTile({
    required this.provider,
    required this.onEdit,
    required this.onDelete,
    required this.onToggle,
  });

  final StorageProviderItem provider;
  final VoidCallback onEdit;
  final VoidCallback onDelete;
  final ValueChanged<bool> onToggle;

  IconData get _typeIcon {
    switch (provider.providerType.value) {
      case 1:
        return Icons.folder;
      case 2:
        return Icons.send;
      default:
        return Icons.storage;
    }
  }

  String get _typeName {
    switch (provider.providerType.value) {
      case 1:
        return '本地存储';
      case 2:
        return 'Telegram';
      default:
        return '未知';
    }
  }

  @override
  Widget build(BuildContext context) {
    return SwitchListTile(
      secondary: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(_typeIcon),
          PopupMenuButton<String>(
            onSelected: (value) {
              if (value == 'edit') {
                onEdit();
              } else if (value == 'delete') {
                onDelete();
              }
            },
            itemBuilder: (context) => [
              const PopupMenuItem(value: 'edit', child: Text('编辑')),
              PopupMenuItem(
                value: 'delete',
                child: Text('删除',
                    style: TextStyle(color: Theme.of(context).colorScheme.error)),
              ),
            ],
          ),
        ],
      ),
      title: Text(provider.name),
      subtitle: Text('${_typeName} · ${provider.groupName}'),
      value: provider.enabled,
      onChanged: onToggle,
    );
  }
}
