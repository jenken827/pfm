import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/api/api.dart';
import 'services/services.dart';
import 'l10n/l10n.dart';
import 'router/router.dart';

class App extends StatelessWidget {
  const App({super.key});

  @override
  Widget build(BuildContext context) {
    final ls = GetIt.I<LocalStorage>();

    return StreamBuilder(
      stream: ls.settingsBox.watch(),
      builder: (context, snapshot) {
        final tl = ls.themeLanguage;
        getLogger().info(
          'current theme: ${tl.themeName},current locale: ${tl.locale}',
        );

        return RepositoryProvider(
          create: (context) => FmRepository(),
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
