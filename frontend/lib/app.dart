import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/platform/platform.dart';
import 'package:window_manager/window_manager.dart';
import 'services/services.dart';
import 'l10n/l10n.dart';
import 'router/router.dart';

class App extends StatefulWidget {
  const App({super.key, required this.fmRepository});
  final FmRepository fmRepository;

  @override
  State<App> createState() => _AppState();
}

class _AppState extends State<App> with WindowListener, WidgetsBindingObserver {
  @override
  void initState() {
    super.initState();
    // 桌面平台注册窗口监听器
    if (_isDesktop()) {
      windowManager.addListener(this);
    }
    // 注册生命周期监听（移动端触发相册自动备份）
    WidgetsBinding.instance.addObserver(this);
  }

  @override
  void dispose() {
    // 桌面平台移除窗口监听器
    if (_isDesktop()) {
      windowManager.removeListener(this);
    }
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    super.didChangeAppLifecycleState(state);
    // app 回到前台时触发相册自动备份
    if (state == AppLifecycleState.resumed &&
        (Platform.isAndroid || Platform.isIOS)) {
      _triggerAlbumBackup();
    }
  }

  void _triggerAlbumBackup() async {
    try {
      final albumBackup = await AlbumBackupService.instance();
      await albumBackup.autoTrigger();
    } catch (e) {
      getLogger().warning('Album auto-trigger on resume failed: $e');
    }
  }

  @override
  void onWindowClose() async {
    // 拦截关闭事件，隐藏到托盘而不是退出
    if (_isDesktop()) {
      await windowManager.hide();
    }
  }

  bool _isDesktop() {
    return Platform.isWindows || Platform.isMacOS || Platform.isLinux;
  }

  @override
  Widget build(BuildContext context) {
    final ls = GetIt.I.get<LocalStorage>();

    return StreamBuilder(
      stream: ls.settingsBox.watch(),
      builder: (context, snapshot) {
        final tl = ls.themeLanguage;
        getLogger().info(
          'current theme: ${tl.themeName},current locale: ${tl.locale}',
        );

        return RepositoryProvider.value(
          value: widget.fmRepository,
          child: MaterialApp.router(
            debugShowCheckedModeBanner: false,
            theme: AppTheme(themeName: ls.currentThemeName).light(),
            darkTheme: AppTheme(themeName: ls.currentThemeName).dark(),
            themeMode: tl.themeMode,
            localizationsDelegates: AppLocalizations.localizationsDelegates,
            supportedLocales: AppLocalizations.supportedLocales,
            locale: Locale(tl.locale),
            routerConfig: approuter,
          ),
        );
      },
    );
  }
}
