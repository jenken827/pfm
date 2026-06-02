import 'package:flutter/material.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/proto/native/provider.pb.dart';
import 'package:pfm/utils/format.dart';

/// 存储介质列表项
class StorageProviderTile extends StatelessWidget {
  const StorageProviderTile({
    super.key,
    required this.provider,
    this.providerTypes = const [],
    required this.onEdit,
    required this.onDelete,
  });

  final StorageProvider provider;
  final List<StorageProviderTypeInfo> providerTypes;
  final VoidCallback onEdit;
  final VoidCallback onDelete;

  IconData _typeIcon(String iconName) {
    final iconMap = {
      'storage': Icons.storage,
      'telegram': Icons.telegram,
      'discord': Icons.discord,
    };
    return iconMap[iconName] ?? Icons.storage;
  }

  String get _typeName {
    // 从 providerTypes 中查找匹配的类型信息
    final typeInfo = providerTypes.where(
      (t) => t.type == provider.providerType,
    ).firstOrNull;
    return typeInfo?.displayName.isNotEmpty == true
        ? typeInfo!.displayName
        : '未知';
  }

  String get _iconName {
    // 从 providerTypes 中查找匹配的类型信息
    final typeInfo = providerTypes.where(
      (t) => t.type == provider.providerType,
    ).firstOrNull;
    return typeInfo?.iconName.isNotEmpty == true ? typeInfo!.iconName : '';
  }

  /// 计算容量使用百分比
  double get _capacityPercent {
    final total = provider.totalCapacity.toInt();
    final used = provider.usedCapacity.toInt();
    if (total <= 0) return 0;
    return (used / total).clamp(0.0, 1.0);
  }

  /// 是否有容量信息
  bool get _hasCapacity =>
      provider.hasTotalCapacity() && provider.totalCapacity.toInt() > 0;

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context)!;
    final theme = Theme.of(context);

    return InkWell(
      onTap: onEdit,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
        child: Row(
          children: [
            // 图标
            Icon(_typeIcon(_iconName), size: 40),
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
                        padding: const EdgeInsets.symmetric(
                          horizontal: 8,
                          vertical: 2,
                        ),
                        decoration: BoxDecoration(
                          color: provider.enabled
                              ? theme.colorScheme.primaryContainer
                              : theme.colorScheme.surfaceContainerHighest,
                          borderRadius: BorderRadius.circular(10),
                        ),
                        child: Text(
                          provider.enabled ? al.common_enabled : al.common_disabled,
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
                          PopupMenuItem(value: 'edit', child: Text(al.common_edit)),
                          PopupMenuItem(
                            value: 'delete',
                            child: Text(
                              al.common_delete,
                              style: TextStyle(color: theme.colorScheme.error),
                            ),
                          ),
                        ],
                      ),
                    ],
                  ),
                  const SizedBox(height: 4),
                  // 第二行：类型
                  Text(
                    _typeName,
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
                        backgroundColor:
                            theme.colorScheme.surfaceContainerHighest,
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
                          '${formatCapacityString(provider.usedCapacity.toInt())} / ${formatCapacityString(provider.totalCapacity.toInt())}',
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                        Text(
                          al.storageProvider_usedPercent((_capacityPercent * 100).toStringAsFixed(1)),
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
