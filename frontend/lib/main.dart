import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import 'package:hive_ce_flutter/hive_ce_flutter.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/blocs/blocs.dart';
import 'package:pfm/hive/hive.dart';
import 'services/services.dart';
import 'router/router.dart';
import 'app.dart';

void main() async {
  WidgetsFlutterBinding.ensureInitialized();

  initTalkerLogger();
  await Hive.initFlutter();
  Hive.registerAdapters();

  final ls = await LocalStorage.instance();
  GetIt.I.registerSingleton<LocalStorage>(ls);

  // 初始化后台服务
  await BackgroundService.initialize();
  final bgService = await BackgroundService.instance();
  GetIt.I.registerSingleton<BackgroundService>(bgService);
  final transferBloc = TransferBloc(bgService)
    ..add(TransferUploadSubscribeEvent())
    ..add(TransferDownloadSubscribeEvent());
  GetIt.I.registerSingleton(transferBloc);

  // 根据登录状态设置初始路由
  if (ls.isLoggedIn) {
    approuter.go(HomeRoute().location);
  } else {
    approuter.go(LoginRoute().location);
  }

  final pfmr = FmRepository();

  runApp(App(fmRepository: pfmr));
}
