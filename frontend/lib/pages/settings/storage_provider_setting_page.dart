import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:flutter_form_builder/flutter_form_builder.dart';
import 'package:form_builder_validators/form_builder_validators.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/blocs/blocs.dart';
import 'package:pfm/pages/settings/components/config_field_widget.dart';
import 'package:pfm/pages/settings/components/storage_provider_tile.dart';
import 'package:pfm/proto/native/provider.pb.dart';
import 'package:pfm/utils/format.dart';
import 'dart:convert';

/// 存储介质设置页面
class StorageProviderSettingPage extends StatelessWidget {
  const StorageProviderSettingPage({super.key});

  @override
  Widget build(BuildContext context) {
    return BlocProvider(
      create: (context) => StorageProviderBloc(context.read<FmRepository>())
        ..add(const StorageProviderLoadTypesEvent())
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
              return StorageProviderTile(
                provider: provider,
                providerTypes: state.providerTypes,
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
    required BuildContext context,
    StorageProvider? provider,
  }) {
    // 获取存储提供者类型列表
    final state = context.read<StorageProviderBloc>().state;
    final providerTypes = state.providerTypes;

    if (providerTypes.isEmpty && provider == null) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('存储提供者类型列表未加载，请稍后重试')));
      return;
    }

    final formKey = GlobalKey<FormBuilderState>();
    StorageProviderTypeInfo? selectedTypeInfo;

    // 初始化选中的类型
    if (provider != null) {
      selectedTypeInfo = providerTypes.isNotEmpty
          ? providerTypes.firstWhere(
              (t) => t.type == provider.providerType,
              orElse: () => providerTypes.first,
            )
          : null;
    } else if (providerTypes.isNotEmpty) {
      selectedTypeInfo = providerTypes.first;
    }

    // Parse existing config for edit mode
    Map<String, dynamic> existingConfig = {};
    if (provider != null && provider.config.isNotEmpty) {
      try {
        String configStr = provider.config;
        configStr = configStr.replaceAll(r'\', r'\\');
        configStr = configStr.replaceAll(r'\\\"', r'\"');
        existingConfig = jsonDecode(configStr);
      } catch (e) {
        debugPrint('Failed to parse config: $e');
      }
    }

    // Calculate capacity value and unit for display
    String capacityValue = '';
    String capacityUnit = 'GB';
    String fileSizeLimitValue = '';
    String fileSizeLimitUnit = 'MB';
    if (provider != null) {
      final capacityInBytes = provider.totalCapacity.toInt();
      final result = formatCapacity(capacityInBytes);
      capacityValue = result.value == result.value.toInt()
          ? result.value.toInt().toString()
          : result.value.toString();
      capacityUnit = result.unit.label;

      final fileSizeInBytes = provider.fileSizeLimit.toInt();
      final res = formatCapacity(fileSizeInBytes);
      fileSizeLimitValue = res.value == res.value.toInt()
          ? res.value.toInt().toString()
          : res.value.toString();
      fileSizeLimitUnit = res.unit.label;
    }
    final unitDropItems = const [
      DropdownMenuItem(value: 'KB', child: Text('KB')),
      DropdownMenuItem(value: 'MB', child: Text('MB')),
      DropdownMenuItem(value: 'GB', child: Text('GB')),
      DropdownMenuItem(value: 'TB', child: Text('TB')),
      DropdownMenuItem(value: 'PB', child: Text('PB')),
    ];

    showDialog(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setDialogState) => AlertDialog(
          title: Text(provider == null ? '添加存储介质' : '编辑存储介质'),
          content: ConstrainedBox(
            constraints: BoxConstraints(
              maxWidth: 500, // 桌面端最大宽度
              minWidth: MediaQuery.of(ctx).size.width < 600
                  ? MediaQuery.of(ctx).size.width * 0.9
                  : 300,
            ),
            child: FormBuilder(
              key: formKey,
              child: SingleChildScrollView(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    const SizedBox(height: 12),
                    // 添加时显示类型选择器
                    if (provider == null && providerTypes.isNotEmpty) ...[
                      FormBuilderDropdown<StorageProviderTypeInfo>(
                        name: 'provider_type',
                        decoration: const InputDecoration(
                          labelText: '存储类型',
                          prefixIcon: Icon(Icons.category),
                        ),
                        initialValue: selectedTypeInfo,
                        items: providerTypes.map((type) {
                          return DropdownMenuItem(
                            value: type,
                            child: Text(type.displayName),
                          );
                        }).toList(),
                        onChanged: (value) {
                          if (value != null) {
                            setDialogState(() => selectedTypeInfo = value);
                          }
                        },
                      ),
                      const SizedBox(height: 16),
                    ],
                    // 编辑时显示类型（只读）
                    if (provider != null && selectedTypeInfo != null) ...[
                      InputDecorator(
                        decoration: const InputDecoration(
                          labelText: '存储类型',
                          prefixIcon: Icon(Icons.category),
                        ),
                        child: Text(selectedTypeInfo!.displayName),
                      ),
                      const SizedBox(height: 16),
                    ],
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
                      validator: FormBuilderValidators.required(
                        errorText: '请输入名称',
                      ),
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

                    const SizedBox(height: 16),
                    // 启用状态开关
                    FormBuilderSwitch(
                      name: 'enabled',
                      title: const Text('启用状态'),
                      subtitle: const Text('禁用后将无法使用此存储介质'),
                      initialValue: provider?.enabled ?? true,
                    ),

                    const SizedBox(height: 16),
                    // 默认绑定开关
                    FormBuilderSwitch(
                      name: 'default_bound',
                      title: const Text('默认绑定'),
                      subtitle: const Text('设为默认存储提供者'),
                      initialValue: provider?.defaultBound ?? true,
                    ),

                    const SizedBox(height: 16),
                    // 优先级
                    FormBuilderTextField(
                      name: 'order',
                      decoration: const InputDecoration(
                        labelText: '优先级',
                        hintText: '0',
                        helperText: '数值越小优先级越高',
                        prefixIcon: Icon(Icons.sort),
                      ),
                      keyboardType: TextInputType.number,
                      initialValue: (provider?.order ?? 0).toString(),
                    ),

                    const SizedBox(height: 24),
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
                            decoration: const InputDecoration(labelText: '单位'),
                            initialValue: capacityUnit,
                            items: unitDropItems,
                            onChanged: (value) {},
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 16),
                    // 单文件大小限制配置
                    Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Expanded(
                          child: FormBuilderTextField(
                            name: 'file_size_limit_value',
                            decoration: const InputDecoration(
                              labelText: '单文件大小限制',
                              hintText: '100',
                              helperText: '输入数值',
                              prefixIcon: Icon(Icons.storage),
                            ),
                            keyboardType: TextInputType.number,
                            initialValue: fileSizeLimitValue,
                          ),
                        ),
                        const SizedBox(width: 8),
                        SizedBox(
                          width: 100,
                          child: FormBuilderDropdown<String>(
                            name: 'file_size_limit_unit',
                            decoration: const InputDecoration(labelText: '单位'),
                            initialValue: fileSizeLimitUnit,
                            items: unitDropItems,
                            onChanged: (value) {},
                          ),
                        ),
                      ],
                    ),

                    // 动态配置字段（添加和编辑时都显示，但编辑时需要重新初始化）
                    if (selectedTypeInfo != null &&
                        selectedTypeInfo!.configFields.isNotEmpty) ...[
                      const SizedBox(height: 16),
                      Text(
                        '配置信息',
                        style: Theme.of(context).textTheme.titleMedium
                            ?.copyWith(fontWeight: FontWeight.bold),
                      ),
                      const SizedBox(height: 16),
                      // 动态渲染配置字段
                      ...selectedTypeInfo!.configFields.map(
                        (field) => buildConfigField(field, existingConfig),
                      ),
                    ],

                    // 代理设置
                    const SizedBox(height: 16),
                    Text(
                      '代理设置',
                      style: Theme.of(context).textTheme.titleMedium
                          ?.copyWith(fontWeight: FontWeight.bold),
                    ),
                    const SizedBox(height: 16),
                    FormBuilderTextField(
                      name: 'proxy',
                      decoration: const InputDecoration(
                        labelText: '代理地址',
                        hintText: 'http://127.0.0.1:7890',
                        helperText: '可选，支持 http/socks5 代理',
                        prefixIcon: Icon(Icons.settings_ethernet),
                      ),
                      initialValue: provider?.proxy,
                    ),

                    // 加密设置（仅添加时可编辑）
                    if (provider == null) ...[
                      const SizedBox(height: 24),
                      Text(
                        '加密设置',
                        style: Theme.of(context).textTheme.titleMedium
                            ?.copyWith(fontWeight: FontWeight.bold),
                      ),
                      const SizedBox(height: 16),

                      FormBuilderTextField(
                        name: 'encryption_key',
                        decoration: const InputDecoration(
                          labelText: '加密密钥',
                          hintText: '请输入加密密钥（可选）',
                          helperText: '用于文件加密的密钥',
                          prefixIcon: Icon(Icons.vpn_key),
                        ),
                        initialValue: provider?.encryptionKey,
                        maxLines: 1,
                      ),
                      const SizedBox(height: 16),

                      FormBuilderTextField(
                        name: 'encryption_suffix',
                        decoration: const InputDecoration(
                          labelText: '加密文件后缀',
                          hintText: 'bin',
                          helperText: '加密后的文件后缀（如：bin、enc）',
                          prefixIcon: Icon(Icons.extension),
                        ),
                        initialValue: provider?.encryptionSuffix ?? 'bin',
                      ),
                    ],

                    // 编辑时显示加密信息（只读）
                    if (provider != null) ...[
                      const SizedBox(height: 24),
                      Text(
                        '加密设置（只读）',
                        style: Theme.of(context).textTheme.titleMedium
                            ?.copyWith(fontWeight: FontWeight.bold),
                      ),
                      const SizedBox(height: 16),
                      InputDecorator(
                        decoration: const InputDecoration(
                          labelText: '加密密钥',
                          prefixIcon: Icon(Icons.vpn_key),
                        ),
                        child: Text(
                          provider.encryptionKey.isNotEmpty
                              ? '${provider.encryptionKey.substring(0, provider.encryptionKey.length > 8 ? 8 : provider.encryptionKey.length)}...'
                              : '未设置',
                          style: TextStyle(
                            color: provider.encryptionKey.isNotEmpty
                                ? Theme.of(context).textTheme.bodyLarge?.color
                                : Theme.of(context).colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ),
                      const SizedBox(height: 16),
                      InputDecorator(
                        decoration: const InputDecoration(
                          labelText: '加密文件后缀',
                          prefixIcon: Icon(Icons.extension),
                        ),
                        child: Text(
                          provider.encryptionSuffix.isNotEmpty
                              ? provider.encryptionSuffix
                              : '未设置',
                          style: TextStyle(
                            color: provider.encryptionSuffix.isNotEmpty
                                ? Theme.of(context).textTheme.bodyLarge?.color
                                : Theme.of(context).colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ),
                    ],
                  ],
                ),
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

                  // 解析容量值，空值默认为0
                  final capacityValueStr = formData['capacity_value']?.toString() ?? '';
                  final capacityValue = capacityValueStr.isEmpty ? 0.0 : (double.tryParse(capacityValueStr) ?? 0.0);
                  final capacity = parseCapacityFromUnit(
                    capacityValue,
                    formData['capacity_unit'] ?? 'GB',
                  );

                  // 解析文件大小限制，空值默认为0
                  final fileSizeLimitValueStr = formData['file_size_limit_value']?.toString() ?? '';
                  final fileSizeLimitValue = fileSizeLimitValueStr.isEmpty ? 0.0 : (double.tryParse(fileSizeLimitValueStr) ?? 0.0);
                  final fileSizeLimit = parseCapacityFromUnit(
                    fileSizeLimitValue,
                    formData['file_size_limit_unit'] ?? 'MB',
                  );

                  // Build configuration JSON dynamically
                  String? configJson;
                  // 获取有效的提供者类型（确保非空）
                  final effectiveTypeInfo = selectedTypeInfo;
                  final effectiveProviderType =
                      provider?.providerType ?? effectiveTypeInfo?.type;

                  if (effectiveTypeInfo != null) {
                    final configMap = <String, dynamic>{};
                    for (final field in effectiveTypeInfo.configFields) {
                      final fieldValue = formData[field.name];
                      if (fieldValue != null &&
                          fieldValue.toString().isNotEmpty) {
                        configMap[field.name] = fieldValue;
                      }
                    }
                    if (configMap.isNotEmpty) {
                      configJson = jsonEncode(configMap);
                    }
                  }

                  // 解析优先级
                  final orderValue =
                      int.tryParse(formData['order']?.toString() ?? '0') ?? 0;

                  context.read<StorageProviderBloc>().add(
                    provider == null
                        ? StorageProviderAddEvent(
                            providerType:
                                effectiveProviderType ??
                                effectiveTypeInfo!.type,
                            name: formData['name'],
                            description:
                                formData['description']?.isNotEmpty == true
                                ? formData['description']
                                : null,
                            config: configJson,
                            enabled: formData['enabled'] as bool? ?? true,
                            order: orderValue,
                            capacity: capacity,
                            fileSizeLimit: fileSizeLimit,
                            encryptionKey:
                                formData['encryption_key']?.isNotEmpty == true
                                ? formData['encryption_key']
                                : null,
                            encryptionSuffix:
                                formData['encryption_suffix']?.isNotEmpty ==
                                    true
                                ? formData['encryption_suffix']
                                : null,
                            defaultBound: formData['default_bound'] as bool?,
                            proxy: formData['proxy']?.isNotEmpty == true
                                ? formData['proxy']
                                : null,
                          )
                        : StorageProviderUpdateEvent(
                            id: provider.id.toInt(),
                            name: formData['name'],
                            description:
                                formData['description']?.isNotEmpty == true
                                ? formData['description']
                                : null,
                            config: configJson,
                            enabled: formData['enabled'] as bool?,
                            order: orderValue,
                            capacity: capacity,
                            fileSizeLimit: fileSizeLimit,
                            defaultBound: formData['default_bound'] as bool?,
                            proxy: formData['proxy']?.isNotEmpty == true
                                ? formData['proxy']
                                : null,
                          ),
                  );

                  Navigator.pop(ctx);
                }
              },
              child: Text(provider == null ? '添加' : '保存'),
            ),
          ],
        ),
      ),
    );
  }

  void _showDeleteDialog(BuildContext context, StorageProvider provider) {
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
