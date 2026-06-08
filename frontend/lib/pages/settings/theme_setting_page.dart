import 'package:flex_color_scheme/flex_color_scheme.dart';
import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/l10n/app_localizations.dart';
import '../../services/services.dart';
import 'package:pfm/design/design_tokens.dart';

/// 主题设置页面
class ThemeSettingPage extends StatelessWidget {
  const ThemeSettingPage({super.key});

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context)!;
    final ls = GetIt.I.get<LocalStorage>();
    final theme = Theme.of(context);

    final colorScheme = theme.colorScheme;
    final screenWidth = MediaQuery.of(context).size.width;
    final isMobileScreen = screenWidth < PfmConstraints.mobileBreakpoint;

    return Scaffold(
      body: SafeArea(
        child: ListView(
          padding: EdgeInsets.symmetric(
            horizontal: isMobileScreen ? PfmSpacing.lg : PfmSpacing.xxxxl,
            vertical: PfmSpacing.xxl,
          ),
          children: [
            // 返回按钮 + 标题
            Row(
              children: [
                IconButton(
                  icon: const Icon(Icons.arrow_back, size: 20),
                  onPressed: () => Navigator.pop(context),
                ),
                const SizedBox(width: PfmSpacing.sm),
                Text(al.theme_title, style: PfmTypography.titleLarge.copyWith(color: colorScheme.onSurface)),
              ],
            ),
            const SizedBox(height: PfmSpacing.xxxl),

            // 主题模式
            Text(al.theme_mode, style: PfmTypography.titleSmall.copyWith(color: colorScheme.onSurfaceVariant)),
            const SizedBox(height: PfmSpacing.md),
            Card(
              child: RadioGroup<ThemeMode>(
                groupValue: ls.themeLanguage.themeMode,
                onChanged: (ThemeMode? value) async {
                  if (value != null) {
                    final tl = ls.themeLanguage.copyWith(themeMode: value);
                    await ls.saveThemeLanguage(tl);
                  }
                },
                child: Column(
                  children: [
                    RadioListTile<ThemeMode>(
                      title: Text(al.theme_light, style: TextStyle(color: colorScheme.onSurface)),
                      subtitle: Text(al.theme_lightDesc, style: TextStyle(color: colorScheme.onSurfaceVariant)),
                      value: ThemeMode.light,
                    ),
                    RadioListTile<ThemeMode>(
                      title: Text(al.theme_dark, style: TextStyle(color: colorScheme.onSurface)),
                      subtitle: Text(al.theme_darkDesc, style: TextStyle(color: colorScheme.onSurfaceVariant)),
                      value: ThemeMode.dark,
                    ),
                    RadioListTile<ThemeMode>(
                      title: Text(al.theme_system, style: TextStyle(color: colorScheme.onSurface)),
                      subtitle: Text(al.theme_systemDesc, style: TextStyle(color: colorScheme.onSurfaceVariant)),
                      value: ThemeMode.system,
                    ),
                  ],
                ),
              ),
            ),

            const SizedBox(height: PfmSpacing.xxxl),

            // 主题颜色
            Text(al.theme_color, style: PfmTypography.titleSmall.copyWith(color: colorScheme.onSurfaceVariant)),
            const SizedBox(height: PfmSpacing.md),
            _ThemeColorGrid(currentColor: ls.currentThemeName),
          ],
        ),
      ),
    );
  }
}

/// 主题颜色选择网格
class _ThemeColorGrid extends StatelessWidget {
  const _ThemeColorGrid({required this.currentColor});

  final String currentColor;

  List<_ColorOption> get _colors {
    return FlexScheme.values
        .map((scheme) => _ColorOption(name: scheme.name, color: scheme))
        .toList();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Wrap(
        spacing: PfmSpacing.md,
        runSpacing: PfmSpacing.md,
        children: _colors.map((option) {
          final isSelected = currentColor == option.name;
          // 生成该主题方案的预览颜色
          final previewColor = FlexThemeData.light(
            scheme: option.color,
          ).colorScheme.primary;
          return GestureDetector(
            onTap: () async {
              final ls = GetIt.I.get<LocalStorage>();
              final tl = ls.themeLanguage.copyWith(themeName: option.name);
              await ls.saveThemeLanguage(tl);
            },
            child: Container(
              width: 48,
              height: 48,
              decoration: BoxDecoration(
                color: previewColor,
                borderRadius: BorderRadius.circular(PfmRadius.lg),
                border: isSelected
                    ? Border.all(color: theme.colorScheme.primary, width: 3)
                    : null,
              ),
              child: isSelected
                  ? const Icon(Icons.check, color: Colors.white, size: 20)
                  : null,
            ),
          );
        }).toList(),
    );
  }
}

class _ColorOption {
  final String name;
  final FlexScheme color;

  const _ColorOption({required this.name, required this.color});
}
