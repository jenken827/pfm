import 'dart:math';

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
      create: (context) =>
          StorageGroupBloc(context.read<FmRepository>())
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
      appBar: AppBar(title: const Text('存储组设置'), centerTitle: true),
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
                  Icon(
                    Icons.error_outline,
                    size: 48,
                    color: Theme.of(context).colorScheme.error,
                  ),
                  const SizedBox(height: 16),
                  Text(state.message),
                  const SizedBox(height: 16),
                  FilledButton.tonal(
                    onPressed: () => context.read<StorageGroupBloc>().add(
                      const StorageGroupLoadEvent(),
                    ),
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
                  Container(
                    padding: const EdgeInsets.all(24),
                    decoration: BoxDecoration(
                      color: Theme.of(
                        context,
                      ).colorScheme.surfaceContainerHighest,
                      shape: BoxShape.circle,
                    ),
                    child: Icon(
                      Icons.folder_outlined,
                      size: 64,
                      color: Theme.of(context).colorScheme.onSurfaceVariant,
                    ),
                  ),
                  const SizedBox(height: 24),
                  Text(
                    '暂无存储组',
                    style: Theme.of(context).textTheme.titleMedium?.copyWith(
                      color: Theme.of(context).colorScheme.onSurfaceVariant,
                    ),
                  ),
                  const SizedBox(height: 8),
                  Text(
                    '创建存储组来管理您的存储介质',
                    style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                      color: Theme.of(context).colorScheme.outline,
                    ),
                  ),
                  const SizedBox(height: 24),
                  FilledButton.icon(
                    onPressed: () => _showAddDialog(context),
                    icon: const Icon(Icons.add),
                    label: const Text('添加存储组'),
                  ),
                ],
              ),
            );
          }

          return ReorderableListView.builder(
            padding: const EdgeInsets.only(top: 8, bottom: 88),
            itemCount: state.groups.length,
            buildDefaultDragHandles: false,
            onReorder: (oldIndex, newIndex) {
              if (oldIndex < newIndex) {
                newIndex -= 1;
              }
              final groups = List<StorageGroupItem>.from(state.groups);
              final item = groups.removeAt(oldIndex);
              groups.insert(newIndex, item);
              context.read<StorageGroupBloc>().add(
                StorageGroupReorderEvent(groups),
              );
            },
            proxyDecorator: (child, index, animation) {
              return AnimatedBuilder(
                animation: animation,
                builder: (context, child) {
                  final elevation = Tween<double>(
                    begin: 0,
                    end: 8,
                  ).evaluate(animation);
                  final scale = Tween<double>(
                    begin: 1.0,
                    end: 1.02,
                  ).evaluate(animation);
                  return Transform.scale(
                    scale: scale,
                    child: Material(
                      elevation: elevation,
                      borderRadius: BorderRadius.circular(16),
                      shadowColor: Theme.of(
                        context,
                      ).colorScheme.primary.withValues(alpha: 0.3),
                      child: child,
                    ),
                  );
                },
                child: child,
              );
            },
            itemBuilder: (context, index) {
              final group = state.groups[index];
              return _StorageGroupTile(
                key: ValueKey(group.id),
                index: index,
                group: group,
                onEdit: () => _showEditDialog(context, group),
                onDelete: () => _showDeleteDialog(context, group),
              );
            },
          );
        },
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => _showAddDialog(context),
        icon: const Icon(Icons.add),
        label: const Text('添加'),
      ),
    );
  }

  void _showAddDialog(BuildContext context) {
    final nameController = TextEditingController();
    final descController = TextEditingController();
    final orderController = TextEditingController(text: '0');

    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('添加存储组'),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: nameController,
                decoration: const InputDecoration(
                  labelText: '名称',
                  hintText: '请输入存储组名称',
                  prefixIcon: Icon(Icons.label_outline),
                ),
                autofocus: true,
                textCapitalization: TextCapitalization.words,
              ),
              const SizedBox(height: 16),
              TextField(
                controller: descController,
                decoration: const InputDecoration(
                  labelText: '描述',
                  hintText: '请输入描述（可选）',
                  prefixIcon: Icon(Icons.notes),
                ),
                maxLines: 2,
              ),
              const SizedBox(height: 16),
              TextField(
                controller: orderController,
                decoration: const InputDecoration(
                  labelText: '排序',
                  hintText: '请输入排序值（数字越小越靠前）',
                  prefixIcon: Icon(Icons.sort),
                  helperText: '数值越小，排序越靠前',
                ),
                keyboardType: TextInputType.number,
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
                final order = int.tryParse(orderController.text) ?? 0;
                context.read<StorageGroupBloc>().add(
                  StorageGroupAddEvent(
                    name: nameController.text,
                    description: descController.text.isEmpty
                        ? null
                        : descController.text,
                    order: order,
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
    final descController = TextEditingController(text: group.description);
    final orderController = TextEditingController(
      text: group.hasOrder() ? group.order.toString() : '0',
    );

    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('编辑存储组'),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: nameController,
                decoration: const InputDecoration(
                  labelText: '名称',
                  prefixIcon: Icon(Icons.label_outline),
                ),
                autofocus: true,
                textCapitalization: TextCapitalization.words,
              ),
              const SizedBox(height: 16),
              TextField(
                controller: descController,
                decoration: const InputDecoration(
                  labelText: '描述',
                  prefixIcon: Icon(Icons.notes),
                ),
                maxLines: 2,
              ),
              const SizedBox(height: 16),
              TextField(
                controller: orderController,
                decoration: const InputDecoration(
                  labelText: '排序',
                  hintText: '请输入排序值（数字越小越靠前）',
                  prefixIcon: Icon(Icons.sort),
                  helperText: '数值越小，排序越靠前',
                ),
                keyboardType: TextInputType.number,
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
                final order = int.tryParse(orderController.text);
                context.read<StorageGroupBloc>().add(
                  StorageGroupUpdateEvent(
                    id: group.id.toInt(),
                    name: nameController.text,
                    description: descController.text.isEmpty
                        ? null
                        : descController.text,
                    order: order,
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
              context.read<StorageGroupBloc>().add(
                StorageGroupDeleteEvent(group.id.toInt()),
              );
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
    required this.index,
    required this.group,
    required this.onEdit,
    required this.onDelete,
    super.key,
  });

  final int index;
  final StorageGroupItem group;
  final VoidCallback onEdit;
  final VoidCallback onDelete;

  /// 格式化容量显示
  String _formatCapacity(int bytes) {
    if (bytes <= 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    final i = (bytes == 0) ? 0 : (log(bytes) / log(1024)).floor();
    final size = bytes / pow(1024, i);
    return '${size.toStringAsFixed(i > 0 ? 1 : 0)} ${units[i]}';
  }

  @override
  Widget build(BuildContext context) {
    final memberCount = group.hasMemberCount() ? group.memberCount : 0;
    final totalCapacity = group.hasTotalCapacity()
        ? group.totalCapacity.toInt()
        : 0;
    final remainingCapacity = group.hasRemainingCapacity()
        ? group.remainingCapacity.toInt()
        : 0;
    final usedCapacity = totalCapacity - remainingCapacity;
    final usagePercent = totalCapacity > 0 ? usedCapacity / totalCapacity : 0.0;

    // 使用 ReorderableDragStartListener 包裹整个卡片，实现长按拖拽
    return ReorderableDragStartListener(
      index: index,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
        child: Card(
          elevation: 0,
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(16),
            side: BorderSide(
              color: Theme.of(context).colorScheme.outlineVariant,
              width: 1,
            ),
          ),
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    // 存储组图标
                    Container(
                      padding: const EdgeInsets.all(8),
                      decoration: BoxDecoration(
                        color: Colors.amber.withValues(alpha: 0.15),
                        borderRadius: BorderRadius.circular(10),
                      ),
                      child: const Icon(
                        Icons.folder_outlined,
                        color: Colors.amber,
                        size: 24,
                      ),
                    ),
                    const SizedBox(width: 12),
                    // 名称和描述
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            group.name,
                            style: Theme.of(context).textTheme.titleMedium
                                ?.copyWith(fontWeight: FontWeight.w600),
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                          ),
                          if (group.description.isNotEmpty)
                            Text(
                              group.description,
                              style: Theme.of(context).textTheme.bodySmall
                                  ?.copyWith(
                                    color: Theme.of(
                                      context,
                                    ).colorScheme.outline,
                                  ),
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                            ),
                        ],
                      ),
                    ),
                    // 操作菜单
                    PopupMenuButton<String>(
                      onSelected: (value) {
                        if (value == 'edit') {
                          onEdit();
                        } else if (value == 'delete') {
                          onDelete();
                        }
                      },
                      itemBuilder: (context) => [
                        const PopupMenuItem(
                          value: 'edit',
                          child: Row(
                            children: [
                              Icon(Icons.edit_outlined, size: 20),
                              SizedBox(width: 8),
                              Text('编辑'),
                            ],
                          ),
                        ),
                        PopupMenuItem(
                          value: 'delete',
                          child: Row(
                            children: [
                              Icon(
                                Icons.delete_outline,
                                size: 20,
                                color: Theme.of(context).colorScheme.error,
                              ),
                              const SizedBox(width: 8),
                              Text(
                                '删除',
                                style: TextStyle(
                                  color: Theme.of(context).colorScheme.error,
                                ),
                              ),
                            ],
                          ),
                        ),
                      ],
                    ),
                  ],
                ),
                const SizedBox(height: 12),
                // 统计信息
                Row(
                  children: [
                    _StatChip(
                      icon: Icons.storage_outlined,
                      label: '$memberCount 个介质',
                      color: Theme.of(context).colorScheme.tertiary,
                    ),
                    const SizedBox(width: 12),
                    if (totalCapacity > 0)
                      Expanded(
                        child: _StatChip(
                          icon: Icons.pie_chart_outline,
                          label:
                              '${_formatCapacity(usedCapacity)} / ${_formatCapacity(totalCapacity)}',
                          color: Theme.of(context).colorScheme.secondary,
                        ),
                      ),
                  ],
                ),
                if (totalCapacity > 0) ...[
                  const SizedBox(height: 12),
                  ClipRRect(
                    borderRadius: BorderRadius.circular(4),
                    child: LinearProgressIndicator(
                      value: usagePercent,
                      minHeight: 6,
                      backgroundColor: Theme.of(
                        context,
                      ).colorScheme.surfaceContainerHighest,
                      valueColor: AlwaysStoppedAnimation<Color>(
                        usagePercent > 0.9
                            ? Theme.of(context).colorScheme.error
                            : Theme.of(context).colorScheme.primary,
                      ),
                    ),
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// 统计信息芯片
class _StatChip extends StatelessWidget {
  const _StatChip({
    required this.icon,
    required this.label,
    required this.color,
  });

  final IconData icon;
  final String label;
  final Color color;

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(icon, size: 14, color: color),
        const SizedBox(width: 4),
        Flexible(
          child: Text(
            label,
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
          ),
        ),
      ],
    );
  }
}
