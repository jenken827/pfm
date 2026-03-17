import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:flutter_form_builder/flutter_form_builder.dart';
import 'package:form_builder_validators/form_builder_validators.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/blocs/blocs.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/proto/native/provider.pb.dart';
import 'package:pfm/utils/format.dart';
import 'dart:convert';

/// 存储介质设置页面
class StorageProviderSettingPage extends StatelessWidget {
  const StorageProviderSettingPage({super.key});

  @override
  Widget build(BuildContext context) {
    return MultiBlocProvider(
      providers: [
        BlocProvider(
          create: (context) =>
              StorageProviderBloc(context.read<FmRepository>())
                ..add(const StorageProviderLoadEvent()),
        ),
        BlocProvider(
          create: (context) =>
              StorageGroupBloc(context.read<FmRepository>())
                ..add(const StorageGroupLoadEvent()),
          lazy: false,
        ),
      ],
      child: const _StorageProviderView(),
    );
  }
}

class _StorageProviderView extends StatelessWidget {
  const _StorageProviderView();

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('存储介质设置'), centerTitle: true),
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
                    onPressed: () => context.read<StorageProviderBloc>().add(
                      const StorageProviderLoadEvent(),
                    ),
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
                  Icon(
                    Icons.storage_outlined,
                    size: 64,
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                  const SizedBox(height: 16),
                  const Text('暂无存储介质'),
                  const SizedBox(height: 16),
                  FilledButton.icon(
                    onPressed: () => _showProviderDialog(context: context),
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
                onEdit: () =>
                    _showProviderDialog(context: context, provider: provider),
                onDelete: () => _showDeleteDialog(context, provider),
              );
            },
          );
        },
      ),
      floatingActionButton: FloatingActionButton(
        onPressed: () => _showProviderDialog(context: context),
        child: const Icon(Icons.add),
      ),
    );
  }

  /// Shows dialog for adding or editing storage provider
  void _showProviderDialog({
    BuildContext? context,
    StorageProviderItem? provider,
  }) {
    final formKey = GlobalKey<FormBuilderState>();
    int? selectedGroupId;
    StorageProviderType currentType =
        provider?.providerType ?? StorageProviderType.LOCAL;

    // Parse existing config for edit mode
    Map<String, dynamic> existingConfig = {};
    if (provider != null && provider.config.isNotEmpty) {
      try {
        // 处理 Windows 路径中的反斜杠转义问题
        // 后端返回的 JSON 字符串中，反斜杠可能没有正确双重转义
        String configStr = provider.config;
        // 将单个反斜杠替换为双反斜杠（但跳过已经是双反斜杠的情况）
        configStr = configStr.replaceAll(r'\', r'\\');
        // 然后将过度转义的双反斜杠还原（\" 应该保持为 \"）
        configStr = configStr.replaceAll(r'\\\"', r'\"');
        existingConfig = jsonDecode(configStr);
      } catch (e) {
        debugPrint('Failed to parse config: $e');
      }
    }

    // Get storage groups from BLoC BEFORE showing dialog
    final groupState = context!.read<StorageGroupBloc>().state;
    final groups = groupState.groups;
    if (groups.isNotEmpty) {
      selectedGroupId = provider?.groupId.toInt() ?? groups.first.id.toInt();
    }

    // Calculate capacity value and unit for display
    String capacityValue = '';
    String capacityUnit = 'GB';
    if (existingConfig.containsKey('capacity')) {
      final capacityInBytes = existingConfig['capacity'] as int;
      final result = formatCapacity(capacityInBytes);
      capacityValue = result.value == result.value.toInt()
          ? result.value.toInt().toString()
          : result.value.toString();
      capacityUnit = result.unit.label;
    }

    showDialog(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setDialogState) => AlertDialog(
          title: Text(provider == null ? '添加存储介质' : '编辑存储介质'),
          content: FormBuilder(
            key: formKey,
            child: SingleChildScrollView(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  const SizedBox(height: 12),
                  // Show type selector only for add dialog
                  if (provider == null) ...[
                    FormBuilderDropdown<StorageProviderType>(
                      name: 'provider_type',
                      decoration: const InputDecoration(
                        labelText: '存储类型',
                        prefixIcon: Icon(Icons.category),
                      ),
                      initialValue: StorageProviderType.LOCAL,
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
                          setDialogState(() => currentType = value);
                        }
                      },
                    ),
                    const SizedBox(height: 16),
                  ],
                  // 存储组选择
                  if (groups.isEmpty)
                    const Padding(
                      padding: EdgeInsets.all(12),
                      child: Text('请先创建存储组', style: TextStyle(color: Colors.red)),
                    )
                  else
                    FormBuilderDropdown<int>(
                      name: 'group_id',
                      decoration: const InputDecoration(
                        labelText: '所属存储组',
                        prefixIcon: Icon(Icons.folder),
                      ),
                      initialValue: selectedGroupId,
                      items: groups.map((group) {
                        return DropdownMenuItem(
                          value: group.id.toInt(),
                          child: Text(group.name),
                        );
                      }).toList(),
                      onChanged: (value) {
                        if (value != null) {
                          selectedGroupId = value;
                        }
                      },
                      validator: FormBuilderValidators.required(
                        errorText: '请选择存储组',
                      ),
                    ),
                  const SizedBox(height: 16),
                  // 名称
                  FormBuilderTextField(
                    name: 'name',
                    decoration: const InputDecoration(
                      labelText: '名称',
                      hintText: '请输入存储介质名称',
                      prefixIcon: Icon(Icons.label),
                    ),
                    autofocus: provider == null,
                    initialValue: provider?.name,
                    validator: FormBuilderValidators.required(errorText: '请输入名称'),
                  ),
                  const SizedBox(height: 16),
                  // 描述
                  FormBuilderTextField(
                    name: 'description',
                    decoration: const InputDecoration(
                      labelText: '描述',
                      hintText: '请输入描述（可选）',
                      prefixIcon: Icon(Icons.description),
                    ),
                    initialValue: provider?.description,
                    maxLines: 2,
                  ),
                  // 启用状态开关（仅编辑模式显示）
                  if (provider != null) ...[
                    const SizedBox(height: 16),
                    FormBuilderSwitch(
                      name: 'enabled',
                      title: const Text('启用状态'),
                      subtitle: const Text('禁用后将无法使用此存储介质'),
                      initialValue: provider.enabled,
                    ),
                  ],
                  const SizedBox(height: 24),
                  // 配置信息标题
                  Text(
                    '配置信息',
                    style: Theme.of(context).textTheme.titleMedium?.copyWith(
                      fontWeight: FontWeight.bold,
                    ),
                  ),
                  const SizedBox(height: 16),
                  // Type-specific configuration - 使用 currentType 响应变化
                  if (currentType == StorageProviderType.LOCAL)
                    Column(
                      children: [
                        FormBuilderTextField(
                          name: 'root_path',
                          decoration: const InputDecoration(
                            labelText: '根路径',
                            hintText: '例如：/data/storage',
                            helperText: '存储文件的根目录路径',
                            prefixIcon: Icon(Icons.folder_open),
                          ),
                          initialValue: existingConfig['root_path'],
                        ),
                        const SizedBox(height: 16),
                        // 容量限制
                        Row(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Expanded(
                              child: FormBuilderTextField(
                                name: 'capacity_value',
                                decoration: const InputDecoration(
                                  labelText: '容量限制',
                                  hintText: '100',
                                  helperText: '输入数值',
                                  prefixIcon: Icon(Icons.storage),
                                ),
                                keyboardType: TextInputType.number,
                                initialValue: capacityValue,
                              ),
                            ),
                            const SizedBox(width: 8),
                            SizedBox(
                              width: 100,
                              child: FormBuilderDropdown<String>(
                                name: 'capacity_unit',
                                decoration: const InputDecoration(
                                  labelText: '单位',
                                ),
                                initialValue: capacityUnit,
                                items: const [
                                  DropdownMenuItem(
                                    value: 'KB',
                                    child: Text('KB'),
                                  ),
                                  DropdownMenuItem(
                                    value: 'MB',
                                    child: Text('MB'),
                                  ),
                                  DropdownMenuItem(
                                    value: 'GB',
                                    child: Text('GB'),
                                  ),
                                  DropdownMenuItem(
                                    value: 'TB',
                                    child: Text('TB'),
                                  ),
                                  DropdownMenuItem(
                                    value: 'PB',
                                    child: Text('PB'),
                                  ),
                                ],
                                onChanged: (value) {},
                              ),
                            ),
                          ],
                        ),
                        const SizedBox(height: 16),
                        FormBuilderSwitch(
                          name: 'show_hidden',
                          title: const Text('显示隐藏文件'),
                          subtitle: const Text('是否显示隐藏的文件'),
                          initialValue:
                              existingConfig['show_hidden'] ?? false,
                        ),
                        const SizedBox(height: 16),
                        FormBuilderTextField(
                          name: 'mkdir_permission',
                          decoration: const InputDecoration(
                            labelText: '文件夹权限',
                            hintText: '0755',
                            helperText: '新建文件夹的权限（八进制）',
                            prefixIcon: Icon(Icons.security),
                          ),
                          initialValue:
                              existingConfig['mkdir_permission'] ?? '0755',
                        ),
                      ],
                    )
                  else if (currentType == StorageProviderType.TELEGRAM)
                    FormBuilderTextField(
                      name: 'bot_token',
                      decoration: const InputDecoration(
                        labelText: 'Bot Token',
                        hintText: '123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11',
                        helperText: '从 @BotFather 获取的 Telegram Bot Token',
                        prefixIcon: Icon(Icons.send),
                      ),
                      initialValue: existingConfig['bot_token'],
                      maxLines: 1,
                    ),
                ],
              ),
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () {
              if (formKey.currentState?.saveAndValidate() ?? false) {
                final formData = formKey.currentState!.value;

                // Build configuration JSON
                String? configJson;
                final effectiveProviderType =
                    provider?.providerType ??
                    (formData['provider_type'] as StorageProviderType);

                if (effectiveProviderType == StorageProviderType.LOCAL) {
                  int? capacityInBytes;
                  final capacityValue = formData['capacity_value']?.toString();
                  final capacityUnit = formData['capacity_unit'] ?? 'GB';

                  if (capacityValue?.isNotEmpty == true) {
                    final value = int.tryParse(capacityValue!);
                    if (value != null) {
                      switch (capacityUnit) {
                        case 'KB':
                          capacityInBytes = value * 1024;
                          break;
                        case 'MB':
                          capacityInBytes = value * 1024 * 1024;
                          break;
                        case 'GB':
                          capacityInBytes = value * 1024 * 1024 * 1024;
                          break;
                        case 'TB':
                          capacityInBytes = value * 1024 * 1024 * 1024 * 1024;
                          break;
                        case 'PB':
                          capacityInBytes =
                              value * 1024 * 1024 * 1024 * 1024 * 1024;
                          break;
                      }
                    }
                  }

                  // 使用 Map 和 jsonEncode 构建 JSON，确保正确转义
                  final configMap = <String, dynamic>{
                    if (formData['root_path']?.isNotEmpty == true)
                      'root_path': formData['root_path'],
                    if (capacityInBytes != null) 'capacity': capacityInBytes,
                    'show_hidden': formData['show_hidden'] ?? false,
                    if (formData['mkdir_permission']?.isNotEmpty == true)
                      'mkdir_permission': formData['mkdir_permission'],
                  };
                  configJson = jsonEncode(configMap);
                } else if (effectiveProviderType ==
                    StorageProviderType.TELEGRAM) {
                  if (formData['bot_token']?.isNotEmpty == true) {
                    configJson = jsonEncode({'bot_token': formData['bot_token']});
                  }
                }

                context.read<StorageProviderBloc>().add(
                  provider == null
                      ? StorageProviderAddEvent(
                          providerType: effectiveProviderType,
                          name: formData['name'],
                          description:
                              formData['description']?.isNotEmpty == true
                              ? formData['description']
                              : null,
                          config: configJson,
                          groupId: selectedGroupId!,
                        )
                      : StorageProviderUpdateEvent(
                          id: provider.id.toInt(),
                          name: formData['name'],
                          description:
                              formData['description']?.isNotEmpty == true
                              ? formData['description']
                              : null,
                          config: configJson,
                          groupId: selectedGroupId!,
                          enabled: formData['enabled'] as bool? ?? true,
                        ),
                );

                Navigator.pop(ctx);
              } else if (selectedGroupId == null) {
                ScaffoldMessenger.of(
                  context,
                ).showSnackBar(const SnackBar(content: Text('请先创建存储组')));
              }
            },
            child: Text(provider == null ? '添加' : '保存'),
          ),
        ],
      ),
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
              context.read<StorageProviderBloc>().add(
                StorageProviderDeleteEvent(provider.id.toInt()),
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

/// 存储介质列表项
class _StorageProviderTile extends StatelessWidget {
  const _StorageProviderTile({
    required this.provider,
    required this.onEdit,
    required this.onDelete,
  });

  final StorageProviderItem provider;
  final VoidCallback onEdit;
  final VoidCallback onDelete;

  IconData get _typeIcon {
    switch (provider.providerType.value) {
      case 1:
        return Icons.storage;
      case 2:
        return Icons.telegram;
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

  /// 计算容量使用百分比
  double get _capacityPercent {
    final total = provider.totalCapacity.toInt();
    final remaining = provider.remainingCapacity.toInt();
    if (total <= 0) return 0;
    final used = total - remaining;
    return (used / total).clamp(0.0, 1.0);
  }

  /// 是否有容量信息
  bool get _hasCapacity => provider.hasTotalCapacity() && provider.totalCapacity.toInt() > 0;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return InkWell(
      onTap: onEdit,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
        child: Row(
          children: [
            // 图标
            Icon(_typeIcon, size: 40),
            const SizedBox(width: 16),
            // 内容区域
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  // 第一行：名称和状态标签
                  Row(
                    children: [
                      Expanded(
                        child: Text(
                          provider.name,
                          style: theme.textTheme.titleMedium,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                      // 状态标签
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                        decoration: BoxDecoration(
                          color: provider.enabled
                              ? theme.colorScheme.primaryContainer
                              : theme.colorScheme.surfaceContainerHighest,
                          borderRadius: BorderRadius.circular(10),
                        ),
                        child: Text(
                          provider.enabled ? '已启用' : '已禁用',
                          style: TextStyle(
                            fontSize: 11,
                            color: provider.enabled
                                ? theme.colorScheme.onPrimaryContainer
                                : theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ),
                      const SizedBox(width: 8),
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
                          const PopupMenuItem(value: 'edit', child: Text('编辑')),
                          PopupMenuItem(
                            value: 'delete',
                            child: Text(
                              '删除',
                              style: TextStyle(color: theme.colorScheme.error),
                            ),
                          ),
                        ],
                      ),
                    ],
                  ),
                  const SizedBox(height: 4),
                  // 第二行：类型和存储组
                  Text(
                    '$_typeName · ${provider.groupName}',
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                  // 第三行：容量信息（如果有）
                  if (_hasCapacity) ...[
                    const SizedBox(height: 8),
                    // 进度条
                    ClipRRect(
                      borderRadius: BorderRadius.circular(4),
                      child: LinearProgressIndicator(
                        value: _capacityPercent,
                        backgroundColor: theme.colorScheme.surfaceContainerHighest,
                        valueColor: AlwaysStoppedAnimation<Color>(
                          _capacityPercent > 0.9
                              ? theme.colorScheme.error
                              : _capacityPercent > 0.7
                                  ? Colors.orange
                                  : theme.colorScheme.primary,
                        ),
                        minHeight: 6,
                      ),
                    ),
                    const SizedBox(height: 4),
                    // 容量文字
                    Row(
                      mainAxisAlignment: MainAxisAlignment.spaceBetween,
                      children: [
                        Text(
                          '${formatCapacityString(provider.remainingCapacity.toInt())} / ${formatCapacityString(provider.totalCapacity.toInt())}',
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                        Text(
                          '已用 ${(_capacityPercent * 100).toStringAsFixed(1)}%',
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: _capacityPercent > 0.9
                                ? theme.colorScheme.error
                                : theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ],
                    ),
                  ],
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}
