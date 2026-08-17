import 'package:flutter/material.dart';
import 'package:pfm/design/design_tokens.dart';

/// 统计卡片组件
class PfmStatCard extends StatelessWidget {
  const PfmStatCard({
    super.key,
    required this.label,
    required this.value,
    this.change,
    this.changeColor,
    this.onTap,
  });

  final String label;
  final String value;
  final String? change;
  final Color? changeColor;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;

    return Card(
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(PfmRadius.lg),
        child: Padding(
          padding: const EdgeInsets.all(PfmSpacing.xl),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                label,
                style: PfmTypography.statLabel.copyWith(
                  color: colorScheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(height: PfmSpacing.xs),
              Text(
                value,
                style: PfmTypography.statValue.copyWith(
                  color: colorScheme.onSurface,
                ),
              ),
              if (change != null) ...[
                const SizedBox(height: PfmSpacing.xs),
                Text(
                  change!,
                  style: PfmTypography.labelSmall.copyWith(
                    color: changeColor ?? colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

/// 文件卡片组件（网格视图）
class PfmFileCard extends StatelessWidget {
  const PfmFileCard({
    super.key,
    required this.name,
    required this.icon,
    required this.iconColor,
    required this.iconBackground,
    this.subtitle,
    this.onTap,
    this.onLongPress,
  });

  final String name;
  final IconData icon;
  final Color iconColor;
  final Color iconBackground;
  final String? subtitle;
  final VoidCallback? onTap;
  final VoidCallback? onLongPress;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;

    return Card(
      child: InkWell(
        onTap: onTap,
        onLongPress: onLongPress,
        borderRadius: BorderRadius.circular(PfmRadius.lg),
        child: Padding(
          padding: const EdgeInsets.all(PfmSpacing.lg),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Container(
                width: 48,
                height: 48,
                decoration: BoxDecoration(
                  color: iconBackground,
                  borderRadius: BorderRadius.circular(PfmRadius.md),
                ),
                child: Icon(icon, color: iconColor, size: 24),
              ),
              const SizedBox(height: PfmSpacing.md),
              Text(
                name,
                style: PfmTypography.labelLarge.copyWith(
                  color: colorScheme.onSurface,
                ),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
              ),
              if (subtitle != null) ...[
                const SizedBox(height: PfmSpacing.xs),
                Text(
                  subtitle!,
                  style: PfmTypography.labelSmall.copyWith(
                    color: colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

/// 快捷操作卡片组件
class PfmQuickActionCard extends StatelessWidget {
  const PfmQuickActionCard({
    super.key,
    required this.title,
    required this.description,
    required this.icon,
    required this.iconColor,
    required this.iconBackground,
    required this.onTap,
  });

  final String title;
  final String description;
  final IconData icon;
  final Color iconColor;
  final Color iconBackground;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;

    return Card(
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(PfmRadius.lg),
        child: Padding(
          padding: const EdgeInsets.all(PfmSpacing.xl),
          child: Row(
            children: [
              Container(
                width: 44,
                height: 44,
                decoration: BoxDecoration(
                  color: iconBackground,
                  borderRadius: BorderRadius.circular(PfmRadius.md),
                ),
                child: Icon(icon, color: iconColor, size: 20),
              ),
              const SizedBox(width: PfmSpacing.lg),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      title,
                      style: PfmTypography.titleSmall.copyWith(
                        color: colorScheme.onSurface,
                      ),
                    ),
                    const SizedBox(height: PfmSpacing.xs),
                    Text(
                      description,
                      style: PfmTypography.bodyMedium.copyWith(
                        color: colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              ),
              Icon(
                Icons.arrow_forward_ios,
                size: 16,
                color: colorScheme.onSurfaceVariant,
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// 区域标题组件
class PfmSectionHeader extends StatelessWidget {
  const PfmSectionHeader({
    super.key,
    required this.title,
    this.action,
    this.onAction,
  });

  final String title;
  final String? action;
  final VoidCallback? onAction;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;

    return Padding(
      padding: const EdgeInsets.only(bottom: PfmSpacing.lg),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.spaceBetween,
        children: [
          Text(
            title,
            style: PfmTypography.titleMedium.copyWith(
              color: colorScheme.onSurface,
            ),
          ),
          if (action != null)
            TextButton(onPressed: onAction, child: Text(action!)),
        ],
      ),
    );
  }
}

/// 文件列表项组件（用于列表视图）
class PfmFileListTile extends StatelessWidget {
  const PfmFileListTile({
    super.key,
    required this.name,
    required this.icon,
    required this.iconColor,
    required this.iconBackground,
    this.subtitle,
    this.isSelected = false,
    this.isSelectMode = false,
    this.onTap,
    this.onLongPress,
    this.onMenuPressed,
    this.thumbnailUrl,
    this.thumbnailHeaders,
  });

  final String name;
  final IconData icon;
  final Color iconColor;
  final Color iconBackground;
  final String? subtitle;
  final bool isSelected;
  final bool isSelectMode;
  final VoidCallback? onTap;
  final VoidCallback? onLongPress;
  final VoidCallback? onMenuPressed;
  final String? thumbnailUrl;
  final Map<String, String>? thumbnailHeaders;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    return InkWell(
      onTap: onTap,
      onLongPress: onLongPress,
      borderRadius: BorderRadius.circular(PfmRadius.md),
      child: Container(
        padding: const EdgeInsets.symmetric(
          horizontal: PfmSpacing.lg,
          vertical: PfmSpacing.md,
        ),
        decoration: BoxDecoration(
          color: isSelected
              ? colorScheme.primaryContainer.withValues(alpha: 0.3)
              : null,
          borderRadius: BorderRadius.circular(PfmRadius.md),
        ),
        child: Row(
          children: [
            // 图标/缩略图
            _buildLeading(context),
            const SizedBox(width: PfmSpacing.md),
            // 文件名和信息
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    name,
                    style: PfmTypography.bodyLarge.copyWith(
                      color: colorScheme.onSurface,
                      fontWeight: FontWeight.w500,
                    ),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                  if (subtitle != null) ...[
                    const SizedBox(height: PfmSpacing.xs),
                    Text(
                      subtitle!,
                      style: PfmTypography.labelSmall.copyWith(
                        color: colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ],
              ),
            ),
            // 尾部操作
            if (isSelectMode)
              Checkbox(
                value: isSelected,
                onChanged: (_) => onTap?.call(),
                visualDensity: VisualDensity.compact,
              )
            else if (onMenuPressed != null)
              IconButton(
                icon: Icon(
                  Icons.more_vert,
                  size: 20,
                  color: colorScheme.onSurfaceVariant,
                ),
                onPressed: onMenuPressed,
                visualDensity: VisualDensity.compact,
                padding: EdgeInsets.zero,
                constraints: const BoxConstraints(minWidth: 32, minHeight: 32),
              ),
          ],
        ),
      ),
    );
  }

  Widget _buildLeading(BuildContext context) {
    if (thumbnailUrl != null) {
      return Container(
        width: 40,
        height: 40,
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(PfmRadius.sm),
          color: iconBackground,
        ),
        clipBehavior: Clip.antiAlias,
        child: Image.network(
          thumbnailUrl!,
          headers: thumbnailHeaders ?? {},
          fit: BoxFit.cover,
          errorBuilder: (_, _, _) => Icon(icon, color: iconColor, size: 20),
        ),
      );
    }

    return Container(
      width: 40,
      height: 40,
      decoration: BoxDecoration(
        color: iconBackground,
        borderRadius: BorderRadius.circular(PfmRadius.sm),
      ),
      child: Icon(icon, color: iconColor, size: 20),
    );
  }
}

/// 文件网格项组件（用于网格视图）
class PfmFileGridTile extends StatelessWidget {
  const PfmFileGridTile({
    super.key,
    required this.name,
    required this.icon,
    required this.iconColor,
    required this.iconBackground,
    this.subtitle,
    this.isSelected = false,
    this.isSelectMode = false,
    this.onTap,
    this.onLongPress,
    this.thumbnailUrl,
    this.thumbnailHeaders,
  });

  final String name;
  final IconData icon;
  final Color iconColor;
  final Color iconBackground;
  final String? subtitle;
  final bool isSelected;
  final bool isSelectMode;
  final VoidCallback? onTap;
  final VoidCallback? onLongPress;
  final String? thumbnailUrl;
  final Map<String, String>? thumbnailHeaders;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;

    return GestureDetector(
      onTap: onTap,
      onLongPress: onLongPress,
      child: Container(
        decoration: BoxDecoration(
          color: isSelected
              ? colorScheme.primaryContainer.withValues(alpha: 0.3)
              : colorScheme.surface,
          borderRadius: BorderRadius.circular(PfmRadius.lg),
          border: Border.all(
            color: isSelected
                ? colorScheme.primary
                : colorScheme.outlineVariant,
            width: isSelected ? 2 : 1,
          ),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            // 图标/缩略图区域
            Expanded(
              child: Stack(
                alignment: Alignment.center,
                children: [
                  if (thumbnailUrl != null)
                    Container(
                      decoration: BoxDecoration(
                        borderRadius: const BorderRadius.vertical(
                          top: Radius.circular(PfmRadius.lg),
                        ),
                        color: iconBackground,
                      ),
                      clipBehavior: Clip.antiAlias,
                      child: Image.network(
                        thumbnailUrl!,
                        headers: thumbnailHeaders ?? {},
                        width: double.infinity,
                        height: double.infinity,
                        fit: BoxFit.cover,
                        errorBuilder: (_, _, _) =>
                            Icon(icon, color: iconColor, size: 32),
                      ),
                    )
                  else
                    Container(
                      width: 48,
                      height: 48,
                      decoration: BoxDecoration(
                        color: iconBackground,
                        borderRadius: BorderRadius.circular(PfmRadius.md),
                      ),
                      child: Icon(icon, color: iconColor, size: 24),
                    ),
                  // 选择模式下的复选框
                  if (isSelectMode)
                    Positioned(
                      top: PfmSpacing.sm,
                      right: PfmSpacing.sm,
                      child: Container(
                        decoration: BoxDecoration(
                          color: colorScheme.surface.withValues(alpha: 0.9),
                          shape: BoxShape.circle,
                        ),
                        padding: const EdgeInsets.all(2),
                        child: Icon(
                          isSelected
                              ? Icons.check_circle
                              : Icons.radio_button_unchecked,
                          size: 20,
                          color: isSelected
                              ? colorScheme.primary
                              : colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ),
                ],
              ),
            ),
            // 文件名区域
            Container(
              padding: const EdgeInsets.symmetric(
                horizontal: PfmSpacing.md,
                vertical: PfmSpacing.sm,
              ),
              decoration: BoxDecoration(
                color: isSelected
                    ? colorScheme.primaryContainer.withValues(alpha: 0.2)
                    : colorScheme.surfaceContainerHighest.withValues(
                        alpha: 0.3,
                      ),
                borderRadius: const BorderRadius.vertical(
                  bottom: Radius.circular(PfmRadius.lg),
                ),
              ),
              child: Text(
                name,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                textAlign: TextAlign.center,
                style: PfmTypography.labelMedium.copyWith(
                  color: colorScheme.onSurface,
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// 存储提供者列表项
class PfmStorageProviderTile extends StatelessWidget {
  const PfmStorageProviderTile({
    super.key,
    required this.name,
    required this.usage,
    required this.icon,
    required this.iconColor,
    required this.iconBackground,
    required this.progress,
    this.onTap,
  });

  final String name;
  final String usage;
  final IconData icon;
  final Color iconColor;
  final Color iconBackground;
  final double progress;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;

    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(PfmRadius.md),
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: PfmSpacing.md),
        child: Row(
          children: [
            Container(
              width: 32,
              height: 32,
              decoration: BoxDecoration(
                color: iconBackground,
                borderRadius: BorderRadius.circular(PfmRadius.sm),
              ),
              child: Icon(icon, color: iconColor, size: 16),
            ),
            const SizedBox(width: PfmSpacing.md),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    name,
                    style: PfmTypography.labelLarge.copyWith(
                      color: colorScheme.onSurface,
                    ),
                  ),
                  const SizedBox(height: PfmSpacing.xs),
                  Text(
                    usage,
                    style: PfmTypography.labelSmall.copyWith(
                      color: colorScheme.onSurfaceVariant,
                    ),
                  ),
                  const SizedBox(height: PfmSpacing.sm),
                  ClipRRect(
                    borderRadius: BorderRadius.circular(PfmRadius.full),
                    child: LinearProgressIndicator(
                      value: progress,
                      backgroundColor: colorScheme.outlineVariant,
                      valueColor: AlwaysStoppedAnimation<Color>(
                        colorScheme.primary,
                      ),
                      minHeight: 4,
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// 统一对话框样式辅助类
/// 提供一致的确认对话框、底部菜单、拖拽指示器
class PfmDialogs {
  PfmDialogs._();

  /// 显示确认对话框（返回 bool）
  static Future<bool> confirm(
    BuildContext context, {
    required String title,
    required String message,
    String? confirmLabel,
    String? cancelLabel,
    bool isDestructive = false,
  }) async {
    final result = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(title),
        content: Text(message, style: PfmTypography.bodyLarge),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: Text(cancelLabel ?? '取消'),
          ),
          FilledButton(
            style: isDestructive
                ? FilledButton.styleFrom(backgroundColor: PfmColors.error)
                : null,
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(confirmLabel ?? '确定'),
          ),
        ],
      ),
    );
    return result ?? false;
  }

  /// 底部菜单拖拽指示器
  static Widget buildDragHandle(BuildContext context) {
    return Center(
      child: Container(
        width: 40,
        height: 4,
        margin: const EdgeInsets.only(
          top: PfmSpacing.md,
          bottom: PfmSpacing.lg,
        ),
        decoration: BoxDecoration(
          color: Theme.of(context).colorScheme.outlineVariant,
          borderRadius: BorderRadius.circular(PfmRadius.full),
        ),
      ),
    );
  }

  /// 底部菜单项
  static Widget menuItem({
    required IconData icon,
    required String title,
    required VoidCallback onTap,
    Color? color,
  }) {
    return Builder(
      builder: (context) {
        final colorScheme = Theme.of(context).colorScheme;
        final effectiveColor = color ?? colorScheme.onSurface;
        return InkWell(
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.symmetric(
              horizontal: PfmSpacing.xxl,
              vertical: PfmSpacing.md,
            ),
            child: Row(
              children: [
                Icon(icon, size: 20, color: effectiveColor),
                const SizedBox(width: PfmSpacing.lg),
                Text(
                  title,
                  style: PfmTypography.bodyLarge.copyWith(
                    color: effectiveColor,
                  ),
                ),
              ],
            ),
          ),
        );
      },
    );
  }

  /// 底部菜单分隔线
  static const Widget divider = Divider(height: 1);
}
