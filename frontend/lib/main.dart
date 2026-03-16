import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/api/api.dart';
import 'package:talker_flutter/talker_flutter.dart';
import 'services/services.dart';
import 'router/router.dart';
import 'app.dart';

void main() async {
  WidgetsFlutterBinding.ensureInitialized();

  _initTalkerLogger();

  final ls = await LocalStorage.instance();
  GetIt.I.registerSingleton<LocalStorage>(ls);

  // 根据登录状态设置初始路由
  if (ls.isLoggedIn) {
    approuter.go(HomeRoute().location);
  } else {
    approuter.go(LoginRoute().location);
  }

  runApp(App());
}

void _initTalkerLogger() {
  final talker = TalkerFlutter.init(
    logger: TalkerLogger(
      settings: TalkerLoggerSettings(
        level: kDebugMode ? LogLevel.debug : LogLevel.info,
      ),
      formatter: CustomLoggerFormatter(),
    ),
    settings: TalkerSettings(
      colors: <String, AnsiPen>{TalkerKey.debug: AnsiPen()..magenta()},
    ),
  );
  GetIt.I.registerSingleton<Talker>(talker);
  talker.info('talker init complete');
}
