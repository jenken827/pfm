import 'package:flex_color_scheme/flex_color_scheme.dart';
import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import '../../services/services.dart';

/// 主题设置页面
class ThemeSettingPage extends StatelessWidget {
  const ThemeSettingPage({super.key});

  @override
  Widget build(BuildContext context) {
    final ls = GetIt.I.get<LocalStorage>();
    final theme = Theme.of(context);

    return Scaffold(
      appBar: AppBar(title: const Text('主题设置'), centerTitle: true),
      body: ListView(
        children: [
          // 主题模式
          Padding(
            padding: const EdgeInsets.all(16),
            child: Text('主题模式', style: theme.textTheme.titleMedium),
          ),
          _ThemeModeTile(
            title: '浅色模式',
            subtitle: '使用浅色主题',
            mode: ThemeMode.light,
            currentMode: ls.themeLanguage.themeMode,
          ),
          _ThemeModeTile(
            title: '深色模式',
            subtitle: '使用深色主题',
            mode: ThemeMode.dark,
            currentMode: ls.themeLanguage.themeMode,
          ),
          _ThemeModeTile(
            title: '跟随系统',
            subtitle: '根据系统设置自动切换',
            mode: ThemeMode.system,
            currentMode: ls.themeLanguage.themeMode,
          ),
          const Divider(height: 32),

          // 主题颜色
          Padding(
            padding: const EdgeInsets.all(16),
            child: Text('主题颜色', style: theme.textTheme.titleMedium),
          ),
          _ThemeColorGrid(currentColor: ls.currentThemeName),
        ],
      ),
    );
  }
}

/// 主题模式选择项
class _ThemeModeTile extends StatelessWidget {
  const _ThemeModeTile({
    required this.title,
    required this.subtitle,
    required this.mode,
    required this.currentMode,
  });

  final String title;
  final String subtitle;
  final ThemeMode mode;
  final ThemeMode currentMode;

  @override
  Widget build(BuildContext context) {
    return RadioListTile<ThemeMode>(
      title: Text(title),
      subtitle: Text(subtitle),
      value: mode,
      groupValue: currentMode,
      onChanged: (value) async {
        if (value != null) {
          final ls = GetIt.I.get<LocalStorage>();
          final tl = ls.themeLanguage.copyWith(themeMode: value);
          await ls.saveThemeLanguage(tl);
        }
      },
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
                color: theme.colorScheme.primaryContainer,
                borderRadius: BorderRadius.circular(12),
                border: isSelected
                    ? Border.all(color: theme.colorScheme.primary, width: 3)
                    : null,
              ),
              child: isSelected
                  ? Icon(Icons.check, color: theme.colorScheme.primary)
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
