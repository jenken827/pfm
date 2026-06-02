import 'package:flex_color_scheme/flex_color_scheme.dart';
import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/l10n/app_localizations.dart';
import '../../services/services.dart';

/// 主题设置页面
class ThemeSettingPage extends StatelessWidget {
  const ThemeSettingPage({super.key});

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context)!;
    final ls = GetIt.I.get<LocalStorage>();
    final theme = Theme.of(context);

    return Scaffold(
      appBar: AppBar(title: Text(al.theme_title), centerTitle: true),
      body: ListView(
        children: [
          // 主题模式
          Padding(
            padding: const EdgeInsets.all(16),
            child: Text(al.theme_mode, style: theme.textTheme.titleMedium),
          ),
          RadioGroup<ThemeMode>(
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
                  title: Text(al.theme_light),
                  subtitle: Text(al.theme_lightDesc),
                  value: ThemeMode.light,
                ),
                RadioListTile<ThemeMode>(
                  title: Text(al.theme_dark),
                  subtitle: Text(al.theme_darkDesc),
                  value: ThemeMode.dark,
                ),
                RadioListTile<ThemeMode>(
                  title: Text(al.theme_system),
                  subtitle: Text(al.theme_systemDesc),
                  value: ThemeMode.system,
                ),
              ],
            ),
          ),
          const Divider(height: 32),

          // 主题颜色
          Padding(
            padding: const EdgeInsets.all(16),
            child: Text(al.theme_color, style: theme.textTheme.titleMedium),
          ),
          _ThemeColorGrid(currentColor: ls.currentThemeName),
        ],
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
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16),
      child: Wrap(
        spacing: 12,
        runSpacing: 12,
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
              width: 56,
              height: 56,
              decoration: BoxDecoration(
                color: previewColor,
                borderRadius: BorderRadius.circular(12),
                border: isSelected
                    ? Border.all(color: theme.colorScheme.primary, width: 3)
                    : null,
              ),
              child: isSelected
                  ? Icon(Icons.check, color: Colors.white)
                  : null,
            ),
          );
        }).toList(),
      ),
    );
  }
}

class _ColorOption {
  final String name;
  final FlexScheme color;

  const _ColorOption({required this.name, required this.color});
}
