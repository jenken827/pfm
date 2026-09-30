import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:flutter_form_builder/flutter_form_builder.dart';
import 'package:form_builder_validators/form_builder_validators.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/blocs/blocs.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/pages/settings/components/config_field_widget.dart';
import 'package:pfm/pages/settings/components/storage_provider_tile.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
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
    final al = AppLocalizations.of(context);
    return Scaffold(
      appBar: AppBar(title: Text(al.storageProvider_title), centerTitle: true),
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
                    child: Text(al.common_retry),
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
                  Text(al.storageProvider_noProvider),
                  const SizedBox(height: 16),
                  FilledButton.icon(
                    onPressed: () => _showProviderDialog(context: context),
                    icon: const Icon(Icons.add),
                    label: Text(al.storageProvider_add),
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
  Future<void> _showProviderDialog({
    required BuildContext context,
    StorageProvider? provider,
  }) async {
    final al = AppLocalizations.of(context);
    // 获取存储提供者类型列表
    var providerTypes = context.read<StorageProviderBloc>().state.providerTypes;

    // 类型列表为空时现场补拉一次：该请求只在页面首次构建时发出，
    // 若当时网络不可用（首次拉取失败），本次进入页面没有其它重试入口。
    // 新增与编辑都需要类型信息（编辑依赖它渲染存储类型与驱动配置字段）。
    if (providerTypes.isEmpty) {
      providerTypes = await _reloadProviderTypes(context, al);
      if (providerTypes.isEmpty) return;
      // 补拉是异步的，等待期间页面可能已销毁
      if (!context.mounted) return;
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
        existingConfig = jsonDecode(provider.config);
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
          title: Text(
            provider == null ? al.storageProvider_add : al.storageProvider_edit,
          ),
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
                        decoration: InputDecoration(
                          labelText: al.storageProvider_storageType,
                          prefixIcon: const Icon(Icons.category),
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
                        decoration: InputDecoration(
                          labelText: al.storageProvider_storageType,
                          prefixIcon: const Icon(Icons.category),
                        ),
                        child: Text(selectedTypeInfo!.displayName),
                      ),
                      const SizedBox(height: 16),
                    ],
                    const SizedBox(height: 16),
                    // 名称
                    FormBuilderTextField(
                      name: 'name',
                      decoration: InputDecoration(
                        labelText: al.storageProvider_name,
                        hintText: al.storageProvider_nameHint,
                        prefixIcon: const Icon(Icons.label),
                      ),
                      autofocus: provider == null,
                      initialValue: provider?.name,
                      validator: FormBuilderValidators.required(
                        errorText: al.storageProvider_nameRequired,
                      ),
                    ),
                    const SizedBox(height: 16),
                    // 描述
                    FormBuilderTextField(
                      name: 'description',
                      decoration: InputDecoration(
                        labelText: al.storageProvider_description,
                        hintText: al.storageProvider_descHint,
                        prefixIcon: const Icon(Icons.description),
                      ),
                      initialValue: provider?.description,
                      maxLines: 2,
                    ),

                    const SizedBox(height: 16),
                    // 启用状态开关
                    FormBuilderSwitch(
                      name: 'enabled',
                      title: Text(al.storageProvider_enableStatus),
                      subtitle: Text(al.storageProvider_disableHint),
                      initialValue: provider?.enabled ?? true,
                    ),

                    const SizedBox(height: 16),
                    // 默认绑定开关
                    FormBuilderSwitch(
                      name: 'default_bound',
                      title: Text(al.storageProvider_defaultBind),
                      subtitle: Text(al.storageProvider_defaultBindHint),
                      initialValue: provider?.defaultBound ?? true,
                    ),

                    const SizedBox(height: 16),
                    // 优先级
                    FormBuilderTextField(
                      name: 'order',
                      decoration: InputDecoration(
                        labelText: al.storageProvider_priority,
                        hintText: '0',
                        helperText: al.storageProvider_priorityHint,
                        prefixIcon: const Icon(Icons.sort),
                      ),
                      keyboardType: TextInputType.number,
                      initialValue: (provider?.sortOrder ?? 0).toString(),
                    ),

                    const SizedBox(height: 24),
                    // 容量限制
                    Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Expanded(
                          child: FormBuilderTextField(
                            name: 'capacity_value',
                            decoration: InputDecoration(
                              labelText: al.storageProvider_capacityLimit,
                              hintText: '100',
                              helperText: al.storageProvider_inputNumber,
                              prefixIcon: const Icon(Icons.storage),
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
                            decoration: InputDecoration(
                              labelText: al.storageProvider_unit,
                            ),
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
                            decoration: InputDecoration(
                              labelText: al.storageProvider_fileSizeLimit,
                              hintText: '100',
                              helperText: al.storageProvider_inputNumber,
                              prefixIcon: const Icon(Icons.storage),
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
                            decoration: InputDecoration(
                              labelText: al.storageProvider_unit,
                            ),
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
                        al.storageProvider_configInfo,
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
                      al.storageProvider_proxySetting,
                      style: Theme.of(context).textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.bold,
                      ),
                    ),
                    const SizedBox(height: 16),
                    FormBuilderTextField(
                      name: 'proxy',
                      decoration: InputDecoration(
                        labelText: al.storageProvider_proxyAddress,
                        hintText: 'http://127.0.0.1:7890',
                        helperText: al.storageProvider_proxyHint,
                        prefixIcon: const Icon(Icons.settings_ethernet),
                      ),
                      initialValue: provider?.proxy,
                    ),

                    const SizedBox(height: 16),
                    Text(
                      al.storageProvider_concurrencySetting,
                      style: Theme.of(context).textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.bold,
                      ),
                    ),
                    const SizedBox(height: 16),
                    FormBuilderTextField(
                      name: 'max_concurrent_uploads',
                      decoration: InputDecoration(
                        labelText: al.storageProvider_maxConcurrentUpload,
                        hintText: al.storageProvider_maxConcurrentHint,
                        helperText: al.storageProvider_maxConcurrentDesc,
                        prefixIcon: const Icon(Icons.speed),
                      ),
                      keyboardType: TextInputType.number,
                      initialValue: (provider?.maxConcurrentUploads ?? 0)
                          .toString(),
                    ),

                    // 加密设置（仅添加时可编辑）
                    if (provider == null) ...[
                      const SizedBox(height: 24),
                      Text(
                        al.storageProvider_encryptionSetting,
                        style: Theme.of(context).textTheme.titleMedium
                            ?.copyWith(fontWeight: FontWeight.bold),
                      ),
                      const SizedBox(height: 16),

                      FormBuilderTextField(
                        name: 'encryption_key',
                        decoration: InputDecoration(
                          labelText: al.storageProvider_encryptionKey,
                          hintText: al.storageProvider_encryptionKeyHint,
                          helperText: al.storageProvider_encryptionKeyDesc,
                          prefixIcon: const Icon(Icons.vpn_key),
                        ),
                        initialValue: provider?.encryptionKey,
                        maxLines: 1,
                      ),
                      const SizedBox(height: 16),

                      FormBuilderTextField(
                        name: 'encryption_suffix',
                        decoration: InputDecoration(
                          labelText: al.storageProvider_encryptionSuffix,
                          hintText: 'bin',
                          helperText: al.storageProvider_encryptionSuffixHint,
                          prefixIcon: const Icon(Icons.extension),
                        ),
                        initialValue: provider?.encryptionSuffix ?? 'bin',
                      ),
                    ],

                    // 编辑时显示加密信息（只读）
                    if (provider != null) ...[
                      const SizedBox(height: 24),
                      Text(
                        al.storageProvider_encryptionReadonly,
                        style: Theme.of(context).textTheme.titleMedium
                            ?.copyWith(fontWeight: FontWeight.bold),
                      ),
                      const SizedBox(height: 16),
                      InputDecorator(
                        decoration: InputDecoration(
                          labelText: al.storageProvider_encryptionKey,
                          prefixIcon: const Icon(Icons.vpn_key),
                        ),
                        child: Text(
                          provider.encryptionKey.isNotEmpty
                              ? '${provider.encryptionKey.substring(0, provider.encryptionKey.length > 8 ? 8 : provider.encryptionKey.length)}...'
                              : al.common_notSet,
                          style: TextStyle(
                            color: provider.encryptionKey.isNotEmpty
                                ? Theme.of(context).textTheme.bodyLarge?.color
                                : Theme.of(
                                    context,
                                  ).colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ),
                      const SizedBox(height: 16),
                      InputDecorator(
                        decoration: InputDecoration(
                          labelText: al.storageProvider_encryptionSuffix,
                          prefixIcon: const Icon(Icons.extension),
                        ),
                        child: Text(
                          provider.encryptionSuffix.isNotEmpty
                              ? provider.encryptionSuffix
                              : al.common_notSet,
                          style: TextStyle(
                            color: provider.encryptionSuffix.isNotEmpty
                                ? Theme.of(context).textTheme.bodyLarge?.color
                                : Theme.of(
                                    context,
                                  ).colorScheme.onSurfaceVariant,
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
              child: Text(al.common_cancel),
            ),
            FilledButton(
              onPressed: () {
                if (formKey.currentState?.saveAndValidate() ?? false) {
                  final formData = formKey.currentState!.value;

                  // 解析容量值，空值默认为0
                  final capacityValueStr =
                      formData['capacity_value']?.toString() ?? '';
                  final capacityValue = capacityValueStr.isEmpty
                      ? 0.0
                      : (double.tryParse(capacityValueStr) ?? 0.0);
                  final capacity = parseCapacityFromUnit(
                    capacityValue,
                    formData['capacity_unit'] ?? 'GB',
                  );

                  // 解析文件大小限制，空值默认为0
                  final fileSizeLimitValueStr =
                      formData['file_size_limit_value']?.toString() ?? '';
                  final fileSizeLimitValue = fileSizeLimitValueStr.isEmpty
                      ? 0.0
                      : (double.tryParse(fileSizeLimitValueStr) ?? 0.0);
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
                      if (fieldValue != null) {
                        // STRING_LIST 类型：直接使用 List<String>
                        if (field.fieldType == ConfigFieldType.STRING_LIST) {
                          final values = (fieldValue as List)
                              .whereType<String>()
                              .where((s) => s.isNotEmpty)
                              .toList();
                          if (values.isNotEmpty) {
                            configMap[field.name] = values;
                          }
                        } else if (fieldValue.toString().isNotEmpty) {
                          configMap[field.name] = fieldValue;
                        }
                      }
                    }
                    if (configMap.isNotEmpty) {
                      configJson = jsonEncode(configMap);
                    }
                  }

                  // 解析优先级
                  final orderValue =
                      int.tryParse(formData['order']?.toString() ?? '0') ?? 0;

                  // 解析最大并发上传数
                  final maxConcurrentUploads =
                      int.tryParse(
                        formData['max_concurrent_uploads']?.toString() ?? '0',
                      ) ??
                      0;

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
                            sortOrder: orderValue,
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
                            maxConcurrentUploads: maxConcurrentUploads,
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
                            sortOrder: orderValue,
                            capacity: capacity,
                            fileSizeLimit: fileSizeLimit,
                            defaultBound: formData['default_bound'] as bool?,
                            proxy: formData['proxy'] as String?,
                            maxConcurrentUploads: maxConcurrentUploads,
                          ),
                  );

                  Navigator.pop(ctx);
                }
              },
              child: Text(provider == null ? al.common_add : al.common_save),
            ),
          ],
        ),
      ),
    );
  }

  /// 现场补拉存储提供者类型列表（首次拉取失败后的补救）。
  /// 成功后写回 bloc，避免同一次进入页面内反复补拉。
  /// 返回空列表表示仍未成功，失败原因已通过 SnackBar 提示。
  Future<List<StorageProviderTypeInfo>> _reloadProviderTypes(
    BuildContext context,
    AppLocalizations al,
  ) async {
    final repository = context.read<FmRepository>();
    final navigator = Navigator.of(context, rootNavigator: true);

    showDialog(
      context: context,
      barrierDismissible: false,
      builder: (dialogContext) =>
          const Center(child: CircularProgressIndicator()),
    );

    final result = await repository.getStorageProviderTypes();

    // loading 对话框无论页面是否已销毁都要关闭
    if (navigator.mounted) navigator.pop();

    if (!context.mounted) return const [];

    if (!result.success) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(result.message ?? al.storageProvider_typesNotLoaded),
        ),
      );
      return const [];
    }

    final types = result.data?.types.toList() ?? <StorageProviderTypeInfo>[];
    if (types.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(al.storageProvider_typesNotLoaded)),
      );
      return const [];
    }

    context.read<StorageProviderBloc>().add(
      StorageProviderTypesLoadedEvent(types),
    );
    return types;
  }

  void _showDeleteDialog(BuildContext context, StorageProvider provider) {
    final al = AppLocalizations.of(context);
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(al.storageProvider_deleteProvider),
        content: Text(al.storageProvider_deleteConfirm(provider.name)),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: Text(al.common_cancel),
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
            child: Text(al.common_delete),
          ),
        ],
      ),
    );
  }
}
