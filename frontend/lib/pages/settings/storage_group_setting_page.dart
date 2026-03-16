import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/blocs/blocs.dart';
import 'package:pfm/proto/native/group.pb.dart';

/// 存储组设置页面
class StorageGroupSettingPage extends StatelessWidget {
  const StorageGroupSettingPage({super.key});

  @override
  Widget build(BuildContext context) {
    return BlocProvider(
      create: (context) => StorageGroupBloc(context.read<FmRepository>())
        ..add(const StorageGroupLoadEvent()),
      child: const _StorageGroupView(),
    );
  }
}

class _StorageGroupView extends StatelessWidget {
  const _StorageGroupView();

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('存储组设置'),
        centerTitle: true,
      ),
      body: BlocBuilder<StorageGroupBloc, StorageGroupState>(
        builder: (context, state) {
          if (state.status == StorageGroupStatus.loading) {
            return const Center(child: CircularProgressIndicator());
          }

          if (state.status == StorageGroupStatus.failure) {
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
                        .read<StorageGroupBloc>()
                        .add(const StorageGroupLoadEvent()),
                    child: const Text('重试'),
                  ),
                ],
              ),
            );
          }

          if (state.groups.isEmpty) {
            return Center(
              child: Column(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  Icon(Icons.folder_outlined,
                      size: 64,
                      color: Theme.of(context).colorScheme.onSurfaceVariant),
                  const SizedBox(height: 16),
                  const Text('暂无存储组'),
                  const SizedBox(height: 16),
                  FilledButton.icon(
                    onPressed: () => _showAddDialog(context),
                    icon: const Icon(Icons.add),
                    label: const Text('添加存储组'),
                  ),
                ],
              ),
            );
          }

          return ListView.builder(
            itemCount: state.groups.length,
            itemBuilder: (context, index) {
              final group = state.groups[index];
              return _StorageGroupTile(
                group: group,
                onEdit: () => _showEditDialog(context, group),
                onDelete: () => _showDeleteDialog(context, group),
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

    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('添加存储组'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: nameController,
              decoration: const InputDecoration(
                labelText: '名称',
                hintText: '请输入存储组名称',
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
                context.read<StorageGroupBloc>().add(
                      StorageGroupAddEvent(
                        name: nameController.text,
                        description: descController.text.isEmpty
                            ? null
                            : descController.text,
                      ),
                    );
                Navigator.pop(ctx);
              }
            },
            child: const Text('添加'),
          ),
        ],
      ),
    );
  }

  void _showEditDialog(BuildContext context, StorageGroupItem group) {
    final nameController = TextEditingController(text: group.name);
    final descController =
        TextEditingController(text: group.description);

    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('编辑存储组'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: nameController,
              decoration: const InputDecoration(
                labelText: '名称',
              ),
              autofocus: true,
            ),
            const SizedBox(height: 16),
            TextField(
              controller: descController,
              decoration: const InputDecoration(
                labelText: '描述',
              ),
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
                context.read<StorageGroupBloc>().add(
                      StorageGroupUpdateEvent(
                        id: group.id.toInt(),
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

  void _showDeleteDialog(BuildContext context, StorageGroupItem group) {
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('删除存储组'),
        content: Text('确定要删除存储组 "${group.name}" 吗？'),
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
                  .read<StorageGroupBloc>()
                  .add(StorageGroupDeleteEvent(group.id.toInt()));
              Navigator.pop(ctx);
            },
            child: const Text('删除'),
          ),
        ],
      ),
    );
  }
}

/// 存储组列表项
class _StorageGroupTile extends StatelessWidget {
  const _StorageGroupTile({
    required this.group,
    required this.onEdit,
    required this.onDelete,
  });

  final StorageGroupItem group;
  final VoidCallback onEdit;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      leading: const Icon(Icons.folder_outlined, color: Colors.amber),
      title: Text(group.name),
      subtitle: Text(group.description.isEmpty ? '' : group.description),
      trailing: PopupMenuButton<String>(
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
    );
  }
}
